package commands

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
	"github.com/ludanortmun/sync-assign/internal/mirror"
)

func TestStudentWorkflowUsesLatestTeacherRevisionAndProtectsWork(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	teacherRemote, teacher := newGitRemote(t, "teacher")
	writeTeacherRevision(t, teacher, "one\n", "Initial assignment")

	student := newGitRepository(t, "student")
	var output strings.Builder
	if err := NewInitStudent(nil, &output).Run(
		context.Background(),
		[]string{teacherRemote},
		InitStudentOptions{RepositoryRoot: student},
	); err != nil {
		t.Fatalf("init-student: %v", err)
	}
	if !strings.Contains(output.String(), config.StudentConfigFilename) {
		t.Fatalf("init-student output = %q, want created config path", output.String())
	}

	command := NewSync()
	if err := command.Run(context.Background(), "lab", SyncOptions{RepositoryRoot: student}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	assignment := filepath.Join(student, "lab", "starter.txt")
	if got := readWorkflowFile(t, assignment); got != "one\n" {
		t.Fatalf("initial assignment = %q, want %q", got, "one\n")
	}

	mirrorPath, err := mirror.PersistentPath(teacherRemote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mirrorPath, ".git")); err != nil {
		t.Fatalf("persistent mirror was not cloned: %v", err)
	}
	if err := command.Run(context.Background(), "lab", SyncOptions{RepositoryRoot: student}); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second sync error = %v, want existing target refusal", err)
	}

	runGitCommand(t, student, "add", config.StudentConfigFilename)
	runGitCommand(t, student, "commit", "-m", "Configure assignment sync")
	writeWorkflowFile(t, filepath.Join(mirrorPath, "lab", "stale-untracked.txt"), "must not distribute\n")
	writeWorkflowFile(t, filepath.Join(mirrorPath, "lab", "stale-ignored.txt"), "must not distribute\n")
	writeWorkflowFile(t, filepath.Join(mirrorPath, ".git", "info", "exclude"), "lab/stale-ignored.txt\n")
	writeTeacherRevision(t, teacher, "two\n", "Revise assignment")
	clean := true
	if err := command.Run(context.Background(), "lab", SyncOptions{
		RepositoryRoot: student,
		Clean:          &clean,
	}); err != nil {
		t.Fatalf("clean sync: %v", err)
	}
	if got := readWorkflowFile(t, assignment); got != "two\n" {
		t.Fatalf("updated assignment = %q, want latest teacher revision", got)
	}
	for _, name := range []string{"stale-untracked.txt", "stale-ignored.txt"} {
		if _, err := os.Lstat(filepath.Join(student, "lab", name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale mirror file %q was distributed: %v", name, err)
		}
		if _, err := os.Lstat(filepath.Join(mirrorPath, "lab", name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale mirror file %q was not cleaned: %v", name, err)
		}
	}
	if mirrorHead := strings.TrimSpace(runGitCommand(t, mirrorPath, "rev-parse", "HEAD")); mirrorHead !=
		strings.TrimSpace(runGitCommand(t, teacher, "rev-parse", "HEAD")) {
		t.Fatalf("mirror HEAD = %q, want latest teacher HEAD", mirrorHead)
	}

	writeWorkflowFile(t, assignment, "student work\n")
	writeTeacherRevision(t, teacher, "three\n", "Revise assignment again")
	if err := command.Run(context.Background(), "lab", SyncOptions{
		RepositoryRoot: student,
		Clean:          &clean,
	}); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("dirty clean sync error = %v, want dirty target refusal", err)
	}
	if got := readWorkflowFile(t, assignment); got != "student work\n" {
		t.Fatalf("refused sync changed student work to %q", got)
	}

	if err := command.Run(context.Background(), "lab", SyncOptions{
		RepositoryRoot: student,
		Clean:          &clean,
		Force:          true,
	}); err != nil {
		t.Fatalf("forced clean sync: %v", err)
	}
	if got := readWorkflowFile(t, assignment); got != "three\n" {
		t.Fatalf("forced assignment = %q, want latest teacher revision", got)
	}
}

func TestSyncEphemeralWorkflowRemovesMirror(t *testing.T) {
	teacherRemote, teacher := newGitRemote(t, "teacher")
	writeTeacherRevision(t, teacher, "ephemeral\n", "Initial assignment")
	student := newGitRepository(t, "student")
	ephemeral := true
	if err := NewInitStudent(nil, io.Discard).Run(
		context.Background(),
		[]string{teacherRemote},
		InitStudentOptions{
			RepositoryRoot: student,
			Ephemeral:      &ephemeral,
		},
	); err != nil {
		t.Fatalf("init-student: %v", err)
	}

	var openedPath string
	command := newSyncWithDependencies(
		execGitRootValidator{},
		gitcmd.New(),
		func(ctx context.Context, cfg config.StudentConfig) (teacherMirror, error) {
			opened, err := mirror.Open(ctx, cfg)
			if err == nil {
				openedPath = opened.Path()
			}
			return opened, err
		},
	)
	if err := command.Run(context.Background(), "lab", SyncOptions{RepositoryRoot: student}); err != nil {
		t.Fatalf("ephemeral sync: %v", err)
	}
	if openedPath == "" {
		t.Fatal("ephemeral mirror path was not observed")
	}
	if _, err := os.Stat(openedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ephemeral mirror still exists after sync: %v", err)
	}
	if got := readWorkflowFile(t, filepath.Join(student, "lab", "starter.txt")); got != "ephemeral\n" {
		t.Fatalf("synced assignment = %q", got)
	}
}

func newGitRemote(t *testing.T, name string) (remote, worktree string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, name+".git")
	runGitCommand(t, "", "init", "--bare", remote)
	worktree = filepath.Join(root, name)
	runGitCommand(t, "", "init", "--initial-branch=main", worktree)
	configureWorkflowIdentity(t, worktree)
	runGitCommand(t, worktree, "remote", "add", "origin", remote)
	return remote, worktree
}

func newGitRepository(t *testing.T, name string) string {
	t.Helper()
	repository := filepath.Join(t.TempDir(), name)
	runGitCommand(t, "", "init", "--initial-branch=main", repository)
	configureWorkflowIdentity(t, repository)
	return repository
}

func configureWorkflowIdentity(t *testing.T, repository string) {
	t.Helper()
	runGitCommand(t, repository, "config", "user.name", "Sync Assign Test")
	runGitCommand(t, repository, "config", "user.email", "sync-assign@example.invalid")
}

func writeTeacherRevision(t *testing.T, teacher, contents, message string) {
	t.Helper()
	writeWorkflowFile(t, filepath.Join(teacher, config.TeacherConfigFilename), "assignments:\n  lab: lab\n")
	writeWorkflowFile(t, filepath.Join(teacher, "lab", "starter.txt"), contents)
	runGitCommand(t, teacher, "add", ".")
	runGitCommand(t, teacher, "commit", "-m", message)
	runGitCommand(t, teacher, "push", "-u", "origin", "main")
}

func writeWorkflowFile(t *testing.T, filename, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("create parent for %q: %v", filename, err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %q: %v", filename, err)
	}
}

func readWorkflowFile(t *testing.T, filename string) string {
	t.Helper()
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read %q: %v", filename, err)
	}
	return string(contents)
}
