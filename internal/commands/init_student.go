package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ludanortmun/sync-assign/internal/config"
)

// InitStudentOptions controls creation of a student repository configuration.
// Pointer fields preserve whether an optional default was explicitly provided.
type InitStudentOptions struct {
	RepositoryRoot string
	Interactive    bool
	Force          bool
	Commit         *bool
	Clean          *bool
	MirrorPath     *string
	Ephemeral      *bool
	Branch         *string
}

// GitRootValidator verifies that a directory is the root of a Git repository.
type GitRootValidator interface {
	ValidateGitRoot(ctx context.Context, directory string) error
}

// InitStudent creates a student configuration from command arguments and
// optional interactive input.
type InitStudent struct {
	input       io.Reader
	output      io.Writer
	rootChecker GitRootValidator
}

// NewInitStudent returns an init-student command using the installed git
// executable.
func NewInitStudent(input io.Reader, output io.Writer) *InitStudent {
	return NewInitStudentWithGitRootValidator(input, output, execGitRootValidator{})
}

// NewInitStudentWithGitRootValidator returns an init-student command with an
// injected repository validator.
func NewInitStudentWithGitRootValidator(
	input io.Reader,
	output io.Writer,
	rootChecker GitRootValidator,
) *InitStudent {
	return &InitStudent{
		input:       input,
		output:      output,
		rootChecker: rootChecker,
	}
}

// Run creates .sync-assign.yml. args may contain one teacher repository URL.
func (command *InitStudent) Run(
	ctx context.Context,
	args []string,
	options InitStudentOptions,
) error {
	if command == nil || command.rootChecker == nil {
		return errors.New("git root validator is not configured")
	}
	if len(args) > 1 {
		return fmt.Errorf("init-student accepts at most one teacher repository argument")
	}

	root := options.RepositoryRoot
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve student repository root %q: %w", root, err)
	}
	if err := command.rootChecker.ValidateGitRoot(ctx, root); err != nil {
		return fmt.Errorf("validate student repository root %q: %w", root, err)
	}

	teacherRepository := ""
	if len(args) == 1 {
		teacherRepository = args[0]
	} else {
		if !options.Interactive {
			return errors.New("teacher repository argument is required in non-interactive mode")
		}
		teacherRepository, err = command.promptTeacherRepository()
		if err != nil {
			return err
		}
	}

	studentConfig := config.StudentConfig{
		TeacherRepository: teacherRepository,
		Commit:            options.Commit,
		Clean:             options.Clean,
		TeacherPath:       options.MirrorPath,
		Ephemeral:         options.Ephemeral,
		Branch:            options.Branch,
	}
	if studentConfig.Commit == nil {
		commit := true
		studentConfig.Commit = &commit
	}
	if err := studentConfig.Validate(); err != nil {
		return fmt.Errorf("validate student config: %w", err)
	}

	filename := filepath.Join(root, config.StudentConfigFilename)
	if !options.Force {
		_, err := os.Stat(filename)
		switch {
		case err == nil:
			return fmt.Errorf("student config %q already exists; use force to overwrite it", filename)
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("check student config %q: %w", filename, err)
		}
	}

	if err := config.WriteStudentFile(filename, studentConfig); err != nil {
		return fmt.Errorf("write student config: %w", err)
	}
	if command.output != nil {
		if _, err := fmt.Fprintf(command.output, "Created %s\n", filename); err != nil {
			return fmt.Errorf("report created student config: %w", err)
		}
	}
	return nil
}

func (command *InitStudent) promptTeacherRepository() (string, error) {
	if command.input == nil {
		return "", errors.New("interactive input is not configured")
	}
	if command.output == nil {
		return "", errors.New("interactive output is not configured")
	}
	if _, err := fmt.Fprint(command.output, "Teacher repository URL: "); err != nil {
		return "", fmt.Errorf("write teacher repository prompt: %w", err)
	}

	value, err := bufio.NewReader(command.input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read teacher repository: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("teacher repository must not be empty")
	}
	return value, nil
}

type execGitRootValidator struct{}

func (execGitRootValidator) ValidateGitRoot(ctx context.Context, directory string) error {
	git := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	git.Dir = directory
	output, err := git.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			stderr := strings.TrimSpace(string(exitError.Stderr))
			if stderr != "" {
				return fmt.Errorf("git rev-parse failed: %s", stderr)
			}
		}
		return fmt.Errorf("git rev-parse failed: %w", err)
	}

	actualRoot := strings.TrimSpace(string(output))
	actualRootInfo, err := os.Stat(actualRoot)
	if err != nil {
		return fmt.Errorf("inspect git repository root %q: %w", actualRoot, err)
	}
	requestedRootInfo, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("inspect requested repository root %q: %w", directory, err)
	}
	if !os.SameFile(actualRootInfo, requestedRootInfo) {
		return fmt.Errorf("directory is inside git repository %q, but is not its root", actualRoot)
	}
	return nil
}
