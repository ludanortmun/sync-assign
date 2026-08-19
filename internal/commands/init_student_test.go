package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
)

type acceptingGitRootValidator struct{}

func (acceptingGitRootValidator) ValidateGitRoot(context.Context, string) error {
	return nil
}

type rejectingGitRootValidator struct{}

func (rejectingGitRootValidator) ValidateGitRoot(context.Context, string) error {
	return errors.New("not a git repository root")
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("input should not be read")
}

func TestInitStudentUsesTeacherRepositoryArgument(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	command := NewInitStudentWithGitRootValidator(failingReader{}, &output, acceptingGitRootValidator{})

	if err := command.Run(context.Background(), []string{"https://example.com/course.git"}, InitStudentOptions{
		RepositoryRoot: root,
	}); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	studentConfig, err := config.LoadStudentFile(filepath.Join(root, config.StudentConfigFilename))
	if err != nil {
		t.Fatalf("LoadStudentFile returned an error: %v", err)
	}
	if studentConfig.TeacherRepository != "https://example.com/course.git" {
		t.Fatalf("TeacherRepository = %q, want argument value", studentConfig.TeacherRepository)
	}
	if studentConfig.Commit == nil || !*studentConfig.Commit {
		t.Fatalf("Commit = %v, want default true", studentConfig.Commit)
	}
}

func TestInitStudentPromptsForTeacherRepository(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	command := NewInitStudentWithGitRootValidator(
		strings.NewReader("https://example.com/prompted.git\n"),
		&output,
		acceptingGitRootValidator{},
	)

	if err := command.Run(context.Background(), nil, InitStudentOptions{
		RepositoryRoot: root,
		Interactive:    true,
	}); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if !strings.Contains(output.String(), "Teacher repository URL:") {
		t.Fatalf("output %q does not contain prompt", output.String())
	}

	studentConfig, err := config.LoadStudentFile(filepath.Join(root, config.StudentConfigFilename))
	if err != nil {
		t.Fatalf("LoadStudentFile returned an error: %v", err)
	}
	if studentConfig.TeacherRepository != "https://example.com/prompted.git" {
		t.Fatalf("TeacherRepository = %q, want prompted value", studentConfig.TeacherRepository)
	}
}

func TestInitStudentRequiresTeacherRepositoryWhenNonInteractive(t *testing.T) {
	command := NewInitStudentWithGitRootValidator(nil, io.Discard, acceptingGitRootValidator{})
	err := command.Run(context.Background(), nil, InitStudentOptions{
		RepositoryRoot: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "required in non-interactive mode") {
		t.Fatalf("Run error = %v, want non-interactive missing argument error", err)
	}
}

func TestInitStudentRefusesToOverwriteConfig(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, config.StudentConfigFilename)
	const original = "teacher-repository: https://example.com/original.git\n"
	if err := os.WriteFile(filename, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile returned an error: %v", err)
	}

	command := NewInitStudentWithGitRootValidator(nil, io.Discard, acceptingGitRootValidator{})
	err := command.Run(
		context.Background(),
		[]string{"https://example.com/replacement.git"},
		InitStudentOptions{RepositoryRoot: root},
	)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Run error = %v, want overwrite protection error", err)
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile returned an error: %v", err)
	}
	if string(contents) != original {
		t.Fatalf("existing config changed to %q", contents)
	}
}

func TestInitStudentWritesOptionalDefaults(t *testing.T) {
	root := t.TempDir()
	commit := false
	clean := true
	mirrorPath := "../teacher"
	ephemeral := false
	branch := "spring-2026"
	command := NewInitStudentWithGitRootValidator(nil, io.Discard, acceptingGitRootValidator{})

	err := command.Run(
		context.Background(),
		[]string{"https://example.com/course.git"},
		InitStudentOptions{
			RepositoryRoot: root,
			Commit:         &commit,
			Clean:          &clean,
			MirrorPath:     &mirrorPath,
			Ephemeral:      &ephemeral,
			Branch:         &branch,
		},
	)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	studentConfig, err := config.LoadStudentFile(filepath.Join(root, config.StudentConfigFilename))
	if err != nil {
		t.Fatalf("LoadStudentFile returned an error: %v", err)
	}
	if studentConfig.Commit == nil || *studentConfig.Commit {
		t.Fatalf("Commit = %v, want explicit false", studentConfig.Commit)
	}
	if studentConfig.Clean == nil || !*studentConfig.Clean {
		t.Fatalf("Clean = %v, want true", studentConfig.Clean)
	}
	if studentConfig.TeacherPath == nil || *studentConfig.TeacherPath != mirrorPath {
		t.Fatalf("TeacherPath = %v, want %q", studentConfig.TeacherPath, mirrorPath)
	}
	if studentConfig.Ephemeral == nil || *studentConfig.Ephemeral {
		t.Fatalf("Ephemeral = %v, want explicit false", studentConfig.Ephemeral)
	}
	if studentConfig.Branch == nil || *studentConfig.Branch != branch {
		t.Fatalf("Branch = %v, want %q", studentConfig.Branch, branch)
	}
}

func TestInitStudentRejectsMirrorPathWithEphemeralBeforeWriting(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, config.StudentConfigFilename)
	const original = "teacher-repository: https://example.com/original.git\n"
	if err := os.WriteFile(filename, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile returned an error: %v", err)
	}

	mirrorPath := "../teacher"
	ephemeral := true
	command := NewInitStudentWithGitRootValidator(nil, io.Discard, acceptingGitRootValidator{})
	err := command.Run(
		context.Background(),
		[]string{"https://example.com/course.git"},
		InitStudentOptions{
			RepositoryRoot: root,
			MirrorPath:     &mirrorPath,
			Ephemeral:      &ephemeral,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("Run error = %v, want conflicting mirror mode error", err)
	}
	contents, readErr := os.ReadFile(filename)
	if readErr != nil {
		t.Fatalf("ReadFile returned an error: %v", readErr)
	}
	if string(contents) != original {
		t.Fatalf("invalid init changed existing config to %q", contents)
	}
}

func TestInitStudentValidatesGitRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	command := NewInitStudentWithGitRootValidator(nil, io.Discard, rejectingGitRootValidator{})

	err := command.Run(
		context.Background(),
		[]string{"https://example.com/course.git"},
		InitStudentOptions{RepositoryRoot: root},
	)
	if err == nil || !strings.Contains(err.Error(), "not a git repository root") {
		t.Fatalf("Run error = %v, want git repository root error", err)
	}

	if _, err := os.Stat(filepath.Join(root, config.StudentConfigFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config exists after root validation failure: %v", err)
	}
}

func TestExecGitRootValidatorUsesDirectoryIdentity(t *testing.T) {
	root := t.TempDir()
	runGitCommand(t, root, "init", "--initial-branch=main")
	link := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}

	if err := (execGitRootValidator{}).ValidateGitRoot(context.Background(), link); err != nil {
		t.Fatalf("ValidateGitRoot() rejected the same directory through another path: %v", err)
	}

	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	err := (execGitRootValidator{}).ValidateGitRoot(context.Background(), nested)
	if err == nil || !strings.Contains(err.Error(), "is not its root") {
		t.Fatalf("ValidateGitRoot() error = %v, want nested-directory rejection", err)
	}
}
