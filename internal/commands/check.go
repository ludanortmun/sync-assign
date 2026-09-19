package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/grader"
	"github.com/ludanortmun/sync-assign/internal/mirror"
)

// CheckOptions contains command-line inputs and student configuration overrides.
type CheckOptions struct {
	RepositoryRoot string
	ConfigPath     string
	MirrorPath     *string
	Ephemeral      *bool
	TeacherBranch  *string
}

// Check evaluates the current student working directory.
type Check struct {
	output      io.Writer
	errorOutput io.Writer
	rootChecker GitRootValidator
	openMirror  mirrorOpener
	checkersFor checkerFactory
}

// NewCheck returns a check command using the standard mirror and grader implementations.
func NewCheck(output, errorOutput io.Writer) *Check {
	return newCheckWithDependencies(
		output,
		errorOutput,
		execGitRootValidator{},
		func(ctx context.Context, cfg config.StudentConfig) (teacherMirror, error) {
			return mirror.Open(ctx, cfg)
		},
		grader.CheckersFor,
	)
}

func newCheckWithDependencies(
	output io.Writer,
	errorOutput io.Writer,
	rootChecker GitRootValidator,
	openMirror mirrorOpener,
	checkersFor checkerFactory,
) *Check {
	return &Check{
		output:      output,
		errorOutput: errorOutput,
		rootChecker: rootChecker,
		openMirror:  openMirror,
		checkersFor: checkersFor,
	}
}

// Run checks assignmentID from the current working tree, including uncommitted files.
func (command *Check) Run(
	ctx context.Context,
	assignmentID string,
	options CheckOptions,
) (err error) {
	if command == nil || command.rootChecker == nil {
		return errors.New("git root validator is not configured")
	}
	if command.openMirror == nil {
		return errors.New("mirror opener is not configured")
	}
	if command.checkersFor == nil {
		return errors.New("checker factory is not configured")
	}
	if command.output == nil {
		return errors.New("check output writer is not configured")
	}
	if command.errorOutput == nil {
		return errors.New("check error output writer is not configured")
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
		return fmt.Errorf("check must run from the student git repository root: %w", err)
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
		return fmt.Errorf("configure checkers for assignment %q: %w", assignmentID, err)
	}

	snapshotRoot, err := os.MkdirTemp(filepath.Dir(root), ".sync-assign-check-*")
	if err != nil {
		return fmt.Errorf("create check snapshot directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(snapshotRoot); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove check snapshot directory %q: %w", snapshotRoot, cleanupErr))
		}
	}()
	snapshot := filepath.Join(snapshotRoot, "assignment")
	if err := os.Mkdir(snapshot, 0o755); err != nil {
		return fmt.Errorf("create assignment snapshot: %w", err)
	}
	source := filepath.Join(root, spec.Path)
	if _, statErr := os.Stat(source); statErr == nil {
		if err := os.CopyFS(snapshot, os.DirFS(source)); err != nil {
			return fmt.Errorf("snapshot assignment for checking: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect assignment for checking: %w", statErr)
	}

	results := make(chan grader.Result)
	go grader.Run(grader.Environment{
		StudentDir: snapshot,
		TeacherDir: filepath.Join(teacherMirror.Path(), spec.Path),
	}, checkers, results)
	return writeCheckReport(command.output, command.errorOutput, assignmentID, results)
}

func writeCheckReport(
	output io.Writer,
	errorOutput io.Writer,
	assignmentID string,
	results <-chan grader.Result,
) error {
	if _, err := fmt.Fprintf(
		output,
		"check: %s\nsource: working directory\nchecks:\n",
		assignmentID,
	); err != nil {
		return fmt.Errorf("write check report: %w", err)
	}
	return writeCheckResults(output, errorOutput, results)
}
