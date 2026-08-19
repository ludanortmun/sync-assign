package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
	"github.com/ludanortmun/sync-assign/internal/mirror"
	"github.com/ludanortmun/sync-assign/internal/syncer"
)

const defaultCommitMessageFormat = "Sync assignment %s"

// SyncOptions contains command-line overrides for student configuration.
type SyncOptions struct {
	RepositoryRoot string
	Commit         *bool
	Clean          *bool
	Force          bool
	MirrorPath     *string
	Ephemeral      *bool
	Branch         *string
	Message        string
}

type teacherMirror interface {
	Path() string
	Close() error
}

type mirrorOpener func(context.Context, config.StudentConfig) (teacherMirror, error)

// Sync applies one assignment to a student repository.
type Sync struct {
	rootChecker GitRootValidator
	git         *gitcmd.Client
	openMirror  mirrorOpener
}

// NewSync returns a sync command using git and the standard mirror manager.
func NewSync() *Sync {
	return newSyncWithDependencies(execGitRootValidator{}, gitcmd.New(), func(
		ctx context.Context,
		cfg config.StudentConfig,
	) (teacherMirror, error) {
		return mirror.Open(ctx, cfg)
	})
}

func newSyncWithDependencies(
	rootChecker GitRootValidator,
	git *gitcmd.Client,
	openMirror mirrorOpener,
) *Sync {
	return &Sync{
		rootChecker: rootChecker,
		git:         git,
		openMirror:  openMirror,
	}
}

// Run synchronizes assignmentID and optionally creates a local commit.
func (command *Sync) Run(
	ctx context.Context,
	assignmentID string,
	options SyncOptions,
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

	root := options.RepositoryRoot
	if root == "" {
		root = "."
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve student repository root %q: %w", root, err)
	}
	if err := command.rootChecker.ValidateGitRoot(ctx, root); err != nil {
		return fmt.Errorf("sync must run from the student git repository root: %w", err)
	}

	studentConfig, err := config.LoadStudentFile(filepath.Join(root, config.StudentConfigFilename))
	if err != nil {
		return err
	}
	if options.MirrorPath != nil && options.Ephemeral != nil && *options.Ephemeral {
		return errors.New("--mirror-path and --ephemeral cannot be used together")
	}
	resolved := applySyncOverrides(studentConfig, options)
	if options.Force && !config.Resolve(config.Optional[bool]{}, resolved.Clean, false) {
		return errors.New("--force requires --clean (or clean: true in student config)")
	}

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
	clean := config.Resolve(config.Optional[bool]{}, resolved.Clean, false)
	target, err := syncer.Sync(
		ctx,
		command.git,
		teacherConfig,
		teacherMirror.Path(),
		root,
		assignmentID,
		syncer.Options{Clean: clean, Force: options.Force},
	)
	if err != nil {
		return fmt.Errorf("sync assignment %q: %w", assignmentID, err)
	}

	if config.Resolve(config.Optional[bool]{}, resolved.Commit, true) {
		assignmentDir := filepath.Base(target)
		if err := command.git.StageAssignment(ctx, root, assignmentDir); err != nil {
			return fmt.Errorf("stage assignment %q: %w", assignmentID, err)
		}
		message := options.Message
		if message == "" {
			message = fmt.Sprintf(defaultCommitMessageFormat, assignmentID)
		}
		if err := command.git.CommitAssignment(ctx, root, assignmentDir, message); err != nil {
			return fmt.Errorf("commit assignment %q: %w", assignmentID, err)
		}
	}
	return nil
}

func applySyncOverrides(studentConfig config.StudentConfig, options SyncOptions) config.StudentConfig {
	if options.Commit != nil {
		studentConfig.Commit = options.Commit
	}
	if options.Clean != nil {
		studentConfig.Clean = options.Clean
	}
	if options.MirrorPath != nil {
		studentConfig.TeacherPath = options.MirrorPath
		ephemeral := false
		studentConfig.Ephemeral = &ephemeral
		studentConfig.SkipMirror = nil
	}
	if options.Ephemeral != nil {
		studentConfig.Ephemeral = options.Ephemeral
		studentConfig.SkipMirror = nil
		if *options.Ephemeral {
			studentConfig.TeacherPath = nil
		}
	}
	if options.Branch != nil {
		studentConfig.Branch = options.Branch
	}
	return studentConfig
}

// CurrentDirectory returns the process working directory with context-rich
// errors for command entry points.
func CurrentDirectory() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current working directory: %w", err)
	}
	return directory, nil
}
