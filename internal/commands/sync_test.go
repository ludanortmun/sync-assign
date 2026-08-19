package commands

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
)

func TestSyncCreatesLocalCommitForOnlyAssignment(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	teacher := t.TempDir()
	runGitCommand(t, teacher, "init", "--initial-branch=main")
	runGitCommand(t, teacher, "config", "user.name", "Teacher")
	runGitCommand(t, teacher, "config", "user.email", "teacher@example.invalid")
	writeTestFile(t, filepath.Join(teacher, config.TeacherConfigFilename), "assignments:\n  lab-1: lab-1\n")
	writeTestFile(t, filepath.Join(teacher, "lab-1", "README.md"), "starter\n")
	runGitCommand(t, teacher, "add", ".")
	runGitCommand(t, teacher, "commit", "-m", "Add assignment")

	student := t.TempDir()
	runGitCommand(t, student, "init", "--initial-branch=main")
	runGitCommand(t, student, "config", "user.name", "Student")
	runGitCommand(t, student, "config", "user.email", "student@example.invalid")
	studentRemote := filepath.Join(t.TempDir(), "student.git")
	runGitCommand(t, "", "init", "--bare", studentRemote)
	runGitCommand(t, student, "remote", "add", "origin", studentRemote)
	writeTestFile(t, filepath.Join(student, "notes.txt"), "original\n")
	runGitCommand(t, student, "add", "notes.txt")
	runGitCommand(t, student, "commit", "-m", "Initial student work")
	runGitCommand(t, student, "push", "-u", "origin", "main")
	remoteHeadBefore := strings.TrimSpace(runGitCommand(t, "", "--git-dir", studentRemote, "rev-parse", "refs/heads/main"))
	writeTestFile(t, filepath.Join(student, "already-staged.txt"), "preserve me\n")
	runGitCommand(t, student, "add", "already-staged.txt")
	commit := true
	if err := config.WriteStudentFile(filepath.Join(student, config.StudentConfigFilename), config.StudentConfig{
		TeacherRepository: teacher,
		Commit:            &commit,
	}); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(student, "notes.txt"), "unstaged change\n")

	if err := NewSync().Run(context.Background(), "lab-1", SyncOptions{
		RepositoryRoot: student,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := strings.TrimSpace(runGitCommand(t, student, "log", "-1", "--pretty=%s")); got != "Sync assignment lab-1" {
		t.Fatalf("commit subject = %q", got)
	}
	if got := strings.TrimSpace(runGitCommand(t, student, "show", "--pretty=", "--name-only", "HEAD")); got != "lab-1/README.md" {
		t.Fatalf("committed paths = %q, want only assignment", got)
	}
	if got := strings.TrimSpace(runGitCommand(t, student, "status", "--short", "--", "notes.txt")); got != "M notes.txt" {
		t.Fatalf("unrelated worktree status = %q, want unstaged modification", got)
	}
	if got := strings.TrimSpace(runGitCommand(t, student, "diff", "--cached", "--name-only")); got != "already-staged.txt" {
		t.Fatalf("remaining staged paths = %q, want preexisting staged file", got)
	}
	if got := strings.TrimSpace(runGitCommand(t, "", "--git-dir", studentRemote, "rev-parse", "refs/heads/main")); got != remoteHeadBefore {
		t.Fatalf("remote HEAD changed from %q to %q; sync must not push", remoteHeadBefore, got)
	}
	if localHead := strings.TrimSpace(runGitCommand(t, student, "rev-parse", "HEAD")); localHead == remoteHeadBefore {
		t.Fatal("local assignment commit was not created")
	}
}

func TestSyncCommitsByDefault(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	teacher := t.TempDir()
	runGitCommand(t, teacher, "init", "--initial-branch=main")
	runGitCommand(t, teacher, "config", "user.name", "Teacher")
	runGitCommand(t, teacher, "config", "user.email", "teacher@example.invalid")
	writeTestFile(t, filepath.Join(teacher, config.TeacherConfigFilename), "assignments:\n  lab-1: lab-1\n")
	writeTestFile(t, filepath.Join(teacher, "lab-1", "README.md"), "starter\n")
	runGitCommand(t, teacher, "add", ".")
	runGitCommand(t, teacher, "commit", "-m", "Add assignment")

	student := t.TempDir()
	runGitCommand(t, student, "init", "--initial-branch=main")
	runGitCommand(t, student, "config", "user.name", "Student")
	runGitCommand(t, student, "config", "user.email", "student@example.invalid")
	writeTestFile(t, filepath.Join(student, "README.md"), "student\n")
	runGitCommand(t, student, "add", ".")
	runGitCommand(t, student, "commit", "-m", "Initial student work")
	if err := config.WriteStudentFile(filepath.Join(student, config.StudentConfigFilename), config.StudentConfig{
		TeacherRepository: teacher,
	}); err != nil {
		t.Fatal(err)
	}

	if err := NewSync().Run(context.Background(), "lab-1", SyncOptions{
		RepositoryRoot: student,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := strings.TrimSpace(runGitCommand(t, student, "log", "-1", "--pretty=%s")); got != "Sync assignment lab-1" {
		t.Fatalf("commit subject = %q, want default assignment commit", got)
	}
}

func TestSyncFlagOverridesConfigFalse(t *testing.T) {
	trueValue := true
	falseValue := false
	path := "configured"
	overridePath := "flag"
	branch := "configured-branch"
	overrideBranch := "flag-branch"
	studentConfig := config.StudentConfig{
		TeacherRepository: "repository",
		Commit:            &trueValue,
		Clean:             &trueValue,
		TeacherPath:       &path,
		Ephemeral:         &trueValue,
		Branch:            &branch,
	}

	got := applySyncOverrides(studentConfig, SyncOptions{
		Commit:     &falseValue,
		Clean:      &falseValue,
		MirrorPath: &overridePath,
		Ephemeral:  &falseValue,
		Branch:     &overrideBranch,
	})
	if *got.Commit || *got.Clean || *got.Ephemeral {
		t.Fatalf("explicit false overrides were not retained: %#v", got)
	}
	if *got.TeacherPath != overridePath || *got.Branch != overrideBranch {
		t.Fatalf("string overrides were not retained: %#v", got)
	}
}

func TestSyncMirrorModeOverridesAreMutuallyConsistent(t *testing.T) {
	trueValue := true
	path := "configured"
	overridePath := "flag"

	got := applySyncOverrides(config.StudentConfig{
		TeacherRepository: "repository",
		TeacherPath:       &path,
	}, SyncOptions{Ephemeral: &trueValue})
	if got.TeacherPath != nil || got.Ephemeral == nil || !*got.Ephemeral {
		t.Fatalf("ephemeral override did not clear configured path: %#v", got)
	}

	got = applySyncOverrides(config.StudentConfig{
		TeacherRepository: "repository",
		Ephemeral:         &trueValue,
	}, SyncOptions{MirrorPath: &overridePath})
	if got.TeacherPath == nil || *got.TeacherPath != overridePath || got.Ephemeral == nil || *got.Ephemeral {
		t.Fatalf("mirror path override did not disable ephemeral mode: %#v", got)
	}
}

func TestSyncRejectsForceWithoutCleanBeforeOpeningMirror(t *testing.T) {
	root := t.TempDir()
	if err := config.WriteStudentFile(filepath.Join(root, config.StudentConfigFilename), config.StudentConfig{
		TeacherRepository: "repository",
	}); err != nil {
		t.Fatal(err)
	}
	opened := false
	command := newSyncWithDependencies(
		acceptingGitRootValidator{},
		gitcmd.New(),
		func(context.Context, config.StudentConfig) (teacherMirror, error) {
			opened = true
			return nil, errors.New("unexpected")
		},
	)

	err := command.Run(context.Background(), "lab", SyncOptions{
		RepositoryRoot: root,
		Force:          true,
	})
	if err == nil || !strings.Contains(err.Error(), "--force requires --clean") {
		t.Fatalf("Run() error = %v, want force/clean error", err)
	}
	if opened {
		t.Fatal("mirror opened for invalid options")
	}
}

func TestSyncClosesMirrorWhenTeacherConfigFails(t *testing.T) {
	root := t.TempDir()
	if err := config.WriteStudentFile(filepath.Join(root, config.StudentConfigFilename), config.StudentConfig{
		TeacherRepository: "repository",
	}); err != nil {
		t.Fatal(err)
	}
	mirrorRoot := t.TempDir()
	closed := false
	command := newSyncWithDependencies(
		acceptingGitRootValidator{},
		gitcmd.New(),
		func(context.Context, config.StudentConfig) (teacherMirror, error) {
			return &fakeTeacherMirror{path: mirrorRoot, close: func() error {
				closed = true
				return nil
			}}, nil
		},
	)

	if err := command.Run(context.Background(), "lab", SyncOptions{RepositoryRoot: root}); err == nil {
		t.Fatal("Run() succeeded without teacher config")
	}
	if !closed {
		t.Fatal("mirror was not closed after teacher config failure")
	}
}

type fakeTeacherMirror struct {
	path  string
	close func() error
}

func (mirror *fakeTeacherMirror) Path() string {
	return mirror.path
}

func (mirror *fakeTeacherMirror) Close() error {
	return mirror.close()
}

func runGitCommand(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func writeTestFile(t *testing.T, filename, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
