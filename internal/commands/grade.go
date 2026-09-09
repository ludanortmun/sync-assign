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
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
	"github.com/ludanortmun/sync-assign/internal/grading"
)

// GradeOptions contains command-line overrides for the grade command.
type GradeOptions struct {
	RepositoryRoot string
	TeacherCommit  string
	Timeout        *time.Duration
}

type Grade struct {
	rootChecker GitRootValidator
	git         *gitcmd.Client
	openMirror  mirrorOpener
	stdout      io.Writer
}

func NewGrade() *Grade {
	return newGradeWithDependencies(execGitRootValidator{}, gitcmd.New(), nil, os.Stdout)
}

func newGradeWithDependencies(rootChecker GitRootValidator, git *gitcmd.Client, openMirror mirrorOpener, stdout io.Writer) *Grade {
	return &Grade{rootChecker: rootChecker, git: git, openMirror: openMirror, stdout: stdout}
}

// Run loads the teacher assignment, grades the newest commit at or before the
// due date, prints an authoritative report to stdout, and returns a non-nil
// error only when grading could not be completed. Unlike Check.Run, a failed
// report is still a successful grading run for teachers.
func (command *Grade) Run(ctx context.Context, assignmentID string, options GradeOptions) (err error) {
	if command == nil || command.rootChecker == nil {
		return errors.New("git root validator is not configured")
	}
	if command.git == nil {
		return errors.New("git client is not configured")
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
		return fmt.Errorf("grade must run from the student git repository root: %w", err)
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
	spec, err := config.LoadAssignmentSpecFile(filepath.Join(teacherAssignmentDir, config.AssignmentSpecFilename))
	if err != nil {
		return err
	}

	branch, err := command.git.CurrentBranch(ctx, root)
	if err != nil {
		return fmt.Errorf("detect current branch for grading: %w", err)
	}
	remoteRevision, err := command.git.FastForwardToRemote(ctx, root, branch)
	if err != nil {
		return fmt.Errorf("update local branch %q to match remote before grading: %w", branch, err)
	}

	commitHash, err := command.git.CommitsUpTo(ctx, root, remoteRevision, spec.DueDate)
	if err != nil {
		return fmt.Errorf("grade assignment %q: %w", assignmentID, err)
	}
	commitTime, err := command.git.CommitTime(ctx, root, commitHash)
	if err != nil {
		return fmt.Errorf("grade assignment %q: %w", assignmentID, err)
	}

	worktreePath, err := os.MkdirTemp(root, ".sync-assign-grade-*")
	if err != nil {
		return fmt.Errorf("create grade worktree path: %w", err)
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		return fmt.Errorf("prepare empty grade worktree path %q: %w", worktreePath, err)
	}
	if err := command.git.AddWorktree(ctx, root, worktreePath, commitHash); err != nil {
		return fmt.Errorf("create grade worktree for commit %q: %w", commitHash, err)
	}
	defer func() {
		if removeErr := command.git.RemoveWorktree(ctx, root, worktreePath); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("clean up grade worktree %q: %w", worktreePath, removeErr))
		}
		_ = os.RemoveAll(worktreePath)
	}()

	studentAssignmentDir := filepath.Join(worktreePath, assignmentDir)
	info, err := os.Stat(studentAssignmentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("assignment %q does not exist in the graded commit %s", assignmentID, commitHash)
		}
		return fmt.Errorf("inspect graded assignment directory %q: %w", studentAssignmentDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("assignment %q does not exist in the graded commit %s", assignmentID, commitHash)
	}

	report, err := runAssignmentPipeline(ctx, assignmentPipelineInput{
		AssignmentID:         assignmentID,
		StudentAssignmentDir: studentAssignmentDir,
		TeacherAssignmentDir: teacherAssignmentDir,
		Spec:                 spec,
		Timeout:              options.Timeout,
		Authoritative:        true,
		IsCheck:              false,
		CommitTime:           commitTime,
	})
	if err != nil {
		return err
	}

	rendered, err := (grading.HumanRenderer{}).Render(report)
	if err != nil {
		return fmt.Errorf("render grade report: %w", err)
	}
	if _, err := fmt.Fprintf(command.stdout, "Grading commit %s (%s)\n", commitHash, commitTime.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("write grade header: %w", err)
	}
	if _, err := io.WriteString(command.stdout, rendered); err != nil {
		return fmt.Errorf("write grade report: %w", err)
	}

	return nil
}
