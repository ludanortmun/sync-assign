package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
	"github.com/ludanortmun/sync-assign/internal/grader"
	"github.com/ludanortmun/sync-assign/internal/mirror"
)

// GradeOptions contains command-line inputs and student configuration overrides.
type GradeOptions struct {
	RepositoryRoot string
	ConfigPath     string
	DueDate        string
	Branch         string
	Pull           bool
	MirrorPath     *string
	Ephemeral      *bool
	TeacherBranch  *string
}

type gradeGit interface {
	CurrentBranch(context.Context, string) (string, error)
	PullBranch(context.Context, string, string, bool) error
	CommitBefore(context.Context, string, string, time.Time) (string, bool, error)
	AddWorktree(context.Context, string, string, string) error
	RemoveWorktree(context.Context, string, string) error
}

type checkerFactory func(*config.Archetype) ([]grader.Checker, error)

// Grade evaluates an assignment from the last student commit at or before its due date.
type Grade struct {
	output      io.Writer
	errorOutput io.Writer
	rootChecker GitRootValidator
	git         gradeGit
	openMirror  mirrorOpener
	checkersFor checkerFactory
}

// NewGrade returns a grade command using git and the standard mirror and grader implementations.
func NewGrade(output, errorOutput io.Writer) *Grade {
	return newGradeWithDependencies(
		output,
		errorOutput,
		execGitRootValidator{},
		gitcmd.New(),
		func(ctx context.Context, cfg config.StudentConfig) (teacherMirror, error) {
			return mirror.Open(ctx, cfg)
		},
		grader.CheckersFor,
	)
}

func newGradeWithDependencies(
	output io.Writer,
	errorOutput io.Writer,
	rootChecker GitRootValidator,
	git gradeGit,
	openMirror mirrorOpener,
	checkersFor checkerFactory,
) *Grade {
	return &Grade{
		output:      output,
		errorOutput: errorOutput,
		rootChecker: rootChecker,
		git:         git,
		openMirror:  openMirror,
		checkersFor: checkersFor,
	}
}

// Run grades assignmentID and writes an ordered, CI-like report.
func (command *Grade) Run(
	ctx context.Context,
	assignmentID string,
	options GradeOptions,
) (err error) {
	if command == nil || command.rootChecker == nil {
		return errors.New("git root validator is not configured")
	}
	if command.git == nil {
		return errors.New("git client is not configured")
	}
	if command.openMirror == nil {
		return errors.New("mirror opener is not configured")
	}
	if command.checkersFor == nil {
		return errors.New("checker factory is not configured")
	}
	if command.output == nil {
		return errors.New("grade output writer is not configured")
	}
	if command.errorOutput == nil {
		return errors.New("grade error output writer is not configured")
	}
	if strings.TrimSpace(assignmentID) == "" {
		return errors.New("assignment ID must not be empty")
	}

	root := options.RepositoryRoot
	if root == "" {
		root = "."
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve student repository root %q: %w", root, err)
	}
	if err := command.rootChecker.ValidateGitRoot(ctx, root); err != nil {
		return fmt.Errorf("grade must run from the student git repository root: %w", err)
	}

	var due time.Time
	if options.DueDate == "" {
		due = time.Now()
	} else {
		due, err = parseDueDate(options.DueDate, time.Local)
		if err != nil {
			return err
		}
	}
	branch := options.Branch
	currentBranch := ""
	if branch == "" || options.Pull {
		currentBranch, err = command.git.CurrentBranch(ctx, root)
		if err != nil {
			return err
		}
	}
	if branch == "" {
		branch = currentBranch
	}
	if strings.TrimSpace(branch) == "" {
		return errors.New("student branch is required when HEAD is detached")
	}
	if branch != strings.TrimSpace(branch) {
		return errors.New("student branch must not have surrounding whitespace")
	}
	if options.Pull {
		if err := command.git.PullBranch(ctx, root, branch, branch == currentBranch); err != nil {
			return fmt.Errorf("pull student branch %q: %w", branch, err)
		}
	}

	configPath, err := resolveStudentConfigPath(root, options.ConfigPath)
	if err != nil {
		return err
	}
	studentConfig, err := config.LoadStudentFile(configPath)
	if err != nil {
		return err
	}
	if options.MirrorPath != nil && options.Ephemeral != nil && *options.Ephemeral {
		return errors.New("--mirror-path and --ephemeral cannot be used together")
	}
	resolved := applyMirrorOverrides(
		studentConfig,
		options.MirrorPath,
		options.Ephemeral,
		options.TeacherBranch,
	)
	teacherMirror, err := command.openMirror(ctx, resolved)
	if err != nil {
		return fmt.Errorf("prepare teacher mirror: %w", err)
	}
	defer func() {
		if closeErr := teacherMirror.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("clean up teacher mirror: %w", closeErr))
		}
	}()

	teacherConfig, err := config.LoadTeacherFile(
		filepath.Join(teacherMirror.Path(), config.TeacherConfigFilename),
	)
	if err != nil {
		return err
	}
	spec, found := teacherConfig.Assignments[assignmentID]
	if !found {
		return fmt.Errorf("assignment %q is not configured by the teacher", assignmentID)
	}
	checkers, err := command.checkersFor(spec.Archetype)
	if err != nil {
		return fmt.Errorf("configure graders for assignment %q: %w", assignmentID, err)
	}

	commit, found, err := command.git.CommitBefore(ctx, root, branch, due)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf(
			"no commit found on student branch %q at or before %s",
			branch,
			due.Format(time.RFC3339),
		)
	}

	container, err := os.MkdirTemp(filepath.Dir(root), ".sync-assign-grade-*")
	if err != nil {
		return fmt.Errorf("create grade worktree directory: %w", err)
	}
	worktree := filepath.Join(container, "student")
	worktreeAdded := false
	defer func() {
		if worktreeAdded {
			cleanupCtx := context.WithoutCancel(ctx)
			if cleanupErr := command.git.RemoveWorktree(cleanupCtx, root, worktree); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("clean up grade worktree: %w", cleanupErr))
			}
		}
		if cleanupErr := os.RemoveAll(container); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove grade temporary directory %q: %w", container, cleanupErr))
		}
	}()
	if err := command.git.AddWorktree(ctx, root, worktree, commit); err != nil {
		return fmt.Errorf("create grade worktree: %w", err)
	}
	worktreeAdded = true

	results := make(chan grader.Result)
	go grader.Run(grader.Environment{
		StudentDir: filepath.Join(worktree, spec.Path),
		TeacherDir: filepath.Join(teacherMirror.Path(), spec.Path),
	}, checkers, results)
	if err := writeGradeReport(command.output, command.errorOutput, assignmentID, branch, commit, due, results); err != nil {
		return err
	}
	return nil
}

func parseDueDate(value string, location *time.Location) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, errors.New("due date is required")
	}
	if value != strings.TrimSpace(value) {
		return time.Time{}, errors.New("due date must not have surrounding whitespace")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if location == nil {
		location = time.Local
	}
	parsed, err := time.ParseInLocation(time.DateOnly, value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"parse due date %q: expected RFC3339 or YYYY-MM-DD: %w",
			value,
			err,
		)
	}
	return parsed.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
}

func writeGradeReport(
	output io.Writer,
	errorOutput io.Writer,
	assignmentID string,
	branch string,
	commit string,
	due time.Time,
	results <-chan grader.Result,
) error {
	if _, err := fmt.Fprintf(
		output,
		"grade: %s\nbranch: %s\ncommit: %s\ndue: %s\nchecks:\n",
		assignmentID,
		branch,
		commit,
		due.Format(time.RFC3339),
	); err != nil {
		drainResults(results)
		return fmt.Errorf("write grade report: %w", err)
	}
	return writeCheckResults(output, errorOutput, results)
}

func writeCheckResults(
	output io.Writer,
	errorOutput io.Writer,
	results <-chan grader.Result,
) error {
	report := grader.Report{}
	passed, failed, skipped := 0, 0, 0
	var writeErr error
	for result := range results {
		if writeErr != nil {
			continue
		}
		writer := output
		label := strings.ToUpper(string(result.Status))
		color, reset := "", ""
		switch result.Status {
		case grader.Running:
			label = "RUNNING"
		case grader.Passed:
			label = "SUCCESS"
			color, reset = "\x1b[32m", "\x1b[0m"
			passed++
		case grader.Failed:
			color, reset = "\x1b[31m", "\x1b[0m"
			writer = errorOutput
			failed++
		case grader.Skipped:
			color, reset = "\x1b[33m", "\x1b[0m"
			skipped++
		}
		if result.Status != grader.Running {
			report.Results = append(report.Results, result)
		}
		if _, err := fmt.Fprintf(writer, "  %s[%s] %s%s\n", color, label, result.Checker, reset); err != nil {
			writeErr = fmt.Errorf("write check results: %w", err)
			continue
		}
		if result.Detail != "" {
			for _, line := range strings.Split(result.Detail, "\n") {
				if _, err := fmt.Fprintf(writer, "    %s\n", line); err != nil {
					writeErr = fmt.Errorf("write check results: %w", err)
					break
				}
			}
		}
	}
	if writeErr != nil {
		return writeErr
	}
	outcome := "passed"
	outcomeColor := "\x1b[32m"
	if !report.Passed() {
		outcome = "failed"
		outcomeColor = "\x1b[31m"
	}
	if _, err := fmt.Fprintf(
		output,
		"\nsummary: \x1b[32m%d passed\x1b[0m, \x1b[31m%d failed\x1b[0m, \x1b[33m%d skipped\x1b[0m\nresult: %s%s\x1b[0m\n",
		passed,
		failed,
		skipped,
		outcomeColor,
		outcome,
	); err != nil {
		return fmt.Errorf("write check results: %w", err)
	}
	if !report.Passed() {
		return errors.New("assignment has failed grading checks")
	}
	return nil
}

func drainResults(results <-chan grader.Result) {
	for range results {
	}
}
