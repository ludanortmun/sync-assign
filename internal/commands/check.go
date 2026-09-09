package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/grading"
)

// CheckOptions contains command-line overrides for the check command.
type CheckOptions struct {
	RepositoryRoot string
	TeacherCommit  string
	Timeout        *time.Duration
}

type Check struct {
	rootChecker GitRootValidator
	openMirror  mirrorOpener
	stdout      io.Writer
}

func NewCheck() *Check {
	return newCheckWithDependencies(execGitRootValidator{}, nil, os.Stdout)
}

func newCheckWithDependencies(rootChecker GitRootValidator, openMirror mirrorOpener, stdout io.Writer) *Check {
	return &Check{rootChecker: rootChecker, openMirror: openMirror, stdout: stdout}
}

// Run prints a non-authoritative report and fails if any check failed.
func (command *Check) Run(ctx context.Context, assignmentID string, options CheckOptions) (err error) {
	if command == nil || command.rootChecker == nil {
		return errors.New("git root validator is not configured")
	}
	if command.stdout == nil {
		return errors.New("stdout writer is not configured")
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

	studentConfig, err := config.LoadStudentFile(filepath.Join(root, config.StudentConfigFilename))
	if err != nil {
		return err
	}

	opener := command.openMirror
	if opener == nil {
		opener = func(ctx context.Context, cfg config.StudentConfig) (teacherMirror, error) {
			return openTeacherSnapshot(ctx, cfg, options.TeacherCommit)
		}
	}
	teacherMirror, err := opener(ctx, studentConfig)
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
	assignmentDir, found := teacherConfig.Assignments[assignmentID]
	if !found {
		return fmt.Errorf("assignment %q is not configured", assignmentID)
	}

	teacherAssignmentDir := filepath.Join(teacherMirror.Path(), assignmentDir)
	studentAssignmentDir := filepath.Join(root, assignmentDir)
	info, err := os.Stat(studentAssignmentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("student assignment directory %q does not exist; run `sync-assign %s` first", studentAssignmentDir, assignmentID)
		}
		return fmt.Errorf("inspect student assignment directory %q: %w", studentAssignmentDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("student assignment directory %q is not a directory; run `sync-assign %s` first", studentAssignmentDir, assignmentID)
	}

	spec, err := config.LoadAssignmentSpecFile(filepath.Join(teacherAssignmentDir, config.AssignmentSpecFilename))
	if err != nil {
		return err
	}

	report, err := runAssignmentPipeline(ctx, assignmentPipelineInput{
		AssignmentID:         assignmentID,
		StudentAssignmentDir: studentAssignmentDir,
		TeacherAssignmentDir: teacherAssignmentDir,
		Spec:                 spec,
		Timeout:              options.Timeout,
		Authoritative:        false,
		IsCheck:              true,
		CommitTime:           time.Time{},
	})
	if err != nil {
		return err
	}

	rendered, err := (grading.HumanRenderer{}).Render(report)
	if err != nil {
		return fmt.Errorf("render check report: %w", err)
	}
	if _, err := io.WriteString(command.stdout, rendered); err != nil {
		return fmt.Errorf("write check report: %w", err)
	}

	if !report.Passed {
		return fmt.Errorf("assignment %q failed: one or more checks failed", assignmentID)
	}
	return nil
}
