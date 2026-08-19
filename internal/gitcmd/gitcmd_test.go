package gitcmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneAndUpdateMirror(t *testing.T) {
	t.Parallel()

	remote, source := createRemote(t)
	writeFile(t, filepath.Join(source, "assignment", "answer.txt"), "one\n")
	writeFile(t, filepath.Join(source, ".gitignore"), "ignored-output/\n")
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-m", "initial")
	runGit(t, source, "push", "-u", "origin", "main")

	mirror := filepath.Join(t.TempDir(), "mirror")
	client := New()
	if err := client.Clone(context.Background(), remote, mirror, "main"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if got := readFile(t, filepath.Join(mirror, "assignment", "answer.txt")); got != "one\n" {
		t.Fatalf("cloned content = %q, want %q", got, "one\n")
	}

	writeFile(t, filepath.Join(source, "assignment", "answer.txt"), "two\n")
	runGit(t, source, "commit", "-am", "update")
	runGit(t, source, "push")
	writeFile(t, filepath.Join(mirror, "assignment", "answer.txt"), "local change\n")
	writeFile(t, filepath.Join(mirror, "local-only.txt"), "untracked\n")
	writeFile(t, filepath.Join(mirror, "untracked", "nested.txt"), "untracked\n")
	writeFile(t, filepath.Join(mirror, "ignored-output", "generated.txt"), "ignored\n")

	if err := client.UpdateMirror(context.Background(), mirror, "main"); err != nil {
		t.Fatalf("UpdateMirror() error = %v", err)
	}
	if got := readFile(t, filepath.Join(mirror, "assignment", "answer.txt")); got != "two\n" {
		t.Fatalf("updated content = %q, want %q", got, "two\n")
	}
	if got := strings.TrimSpace(runGit(t, mirror, "rev-parse", "HEAD")); got != strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD")) {
		t.Fatalf("mirror HEAD = %q, source HEAD = %q", got, strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD")))
	}
	for _, path := range []string{"local-only.txt", "untracked", "ignored-output"} {
		if _, err := os.Lstat(filepath.Join(mirror, path)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale mirror path %q still exists: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(mirror, ".git")); err != nil {
		t.Fatalf("UpdateMirror() removed git metadata: %v", err)
	}
}

func TestUpdateMirrorRunsCleanAfterResetAndDescribesFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("clean failed")
	var calls [][]string
	client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if args[0] == "clean" {
			return nil, []byte("cannot remove stale directory"), sentinel
		}
		return nil, nil, nil
	}))

	err := client.UpdateMirror(context.Background(), "/mirror", "course")
	if !errors.Is(err, sentinel) {
		t.Fatalf("UpdateMirror() error = %v, want wrapped clean failure", err)
	}
	for _, want := range []string{"clean mirror worktree", `branch "course"`, "cannot remove stale directory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("UpdateMirror() error %q does not contain %q", err, want)
		}
	}

	wantCalls := [][]string{
		{"fetch", "--prune", "origin", "+refs/heads/course:refs/remotes/origin/course"},
		{"checkout", "--force", "-B", "course", "refs/remotes/origin/course"},
		{"reset", "--hard", "refs/remotes/origin/course"},
		{"clean", "-ffdx"},
	}
	if len(calls) != len(wantCalls) {
		t.Fatalf("git call count = %d, want %d: %#v", len(calls), len(wantCalls), calls)
	}
	for index := range wantCalls {
		if strings.Join(calls[index], "\x00") != strings.Join(wantCalls[index], "\x00") {
			t.Errorf("git call %d = %#v, want %#v", index, calls[index], wantCalls[index])
		}
	}
}

func TestIsDirtyAndStageAssignment(t *testing.T) {
	t.Parallel()

	_, repository := createRemote(t)
	writeFile(t, filepath.Join(repository, "assignment", "answer.txt"), "original\n")
	writeFile(t, filepath.Join(repository, "other", "notes.txt"), "original\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "initial")

	client := New()
	dirty, err := client.IsDirty(context.Background(), repository, "assignment")
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if dirty {
		t.Fatal("IsDirty() = true for clean assignment")
	}

	writeFile(t, filepath.Join(repository, "assignment", "answer.txt"), "changed\n")
	writeFile(t, filepath.Join(repository, "other", "notes.txt"), "changed\n")
	dirty, err = client.IsDirty(context.Background(), repository, "assignment")
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if !dirty {
		t.Fatal("IsDirty() = false for changed assignment")
	}

	if err := client.StageAssignment(context.Background(), repository, "assignment"); err != nil {
		t.Fatalf("StageAssignment() error = %v", err)
	}
	staged := runGit(t, repository, "diff", "--cached", "--name-only")
	if strings.TrimSpace(staged) != "assignment/answer.txt" {
		t.Fatalf("staged files = %q, want only assignment/answer.txt", staged)
	}
}

func TestIsDirtyIncludesIgnoredFilesOnlyUnderPath(t *testing.T) {
	t.Parallel()

	_, repository := createRemote(t)
	writeFile(t, filepath.Join(repository, ".gitignore"), "*.log\n")
	writeFile(t, filepath.Join(repository, "assignment", "answer.txt"), "original\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "initial")

	client := New()
	writeFile(t, filepath.Join(repository, "outside.log"), "ignored outside\n")
	dirty, err := client.IsDirty(context.Background(), repository, "assignment")
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if dirty {
		t.Fatal("IsDirty() = true for ignored file outside assignment")
	}

	writeFile(t, filepath.Join(repository, "assignment", "student.log"), "ignored student work\n")
	dirty, err = client.IsDirty(context.Background(), repository, "assignment")
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if !dirty {
		t.Fatal("IsDirty() = false for ignored file inside assignment")
	}
}

func TestIsDirtyDoesNotReportGitMetadata(t *testing.T) {
	t.Parallel()

	_, repository := createRemote(t)
	writeFile(t, filepath.Join(repository, "assignment", "answer.txt"), "original\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "initial")
	writeFile(t, filepath.Join(repository, ".git", "sync-assign-test-metadata"), "metadata\n")

	dirty, err := New().IsDirty(context.Background(), repository, "assignment")
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if dirty {
		t.Fatal("IsDirty() = true for .git metadata")
	}
}

func TestCommit(t *testing.T) {
	t.Parallel()

	_, repository := createRemote(t)
	writeFile(t, filepath.Join(repository, "assignment", "answer.txt"), "answer\n")

	client := New()
	if err := client.StageAssignment(context.Background(), repository, "assignment"); err != nil {
		t.Fatalf("StageAssignment() error = %v", err)
	}
	if err := client.Commit(context.Background(), repository, "sync assignment"); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got := strings.TrimSpace(runGit(t, repository, "log", "-1", "--pretty=%s")); got != "sync assignment" {
		t.Fatalf("commit subject = %q, want %q", got, "sync assignment")
	}
}

func TestCommitAssignmentExcludesOtherStagedChanges(t *testing.T) {
	t.Parallel()

	_, repository := createRemote(t)
	writeFile(t, filepath.Join(repository, "assignment", "answer.txt"), "answer\n")
	writeFile(t, filepath.Join(repository, "notes.txt"), "notes\n")
	runGit(t, repository, "add", "notes.txt")

	client := New()
	if err := client.StageAssignment(context.Background(), repository, "assignment"); err != nil {
		t.Fatalf("StageAssignment() error = %v", err)
	}
	if err := client.CommitAssignment(context.Background(), repository, "assignment", "sync assignment"); err != nil {
		t.Fatalf("CommitAssignment() error = %v", err)
	}
	if got := strings.TrimSpace(runGit(t, repository, "show", "--pretty=", "--name-only", "HEAD")); got != "assignment/answer.txt" {
		t.Fatalf("committed files = %q, want only assignment", got)
	}
	if got := strings.TrimSpace(runGit(t, repository, "diff", "--cached", "--name-only")); got != "notes.txt" {
		t.Fatalf("remaining staged files = %q, want notes.txt", got)
	}
}

func TestLiteralAssignmentPath(t *testing.T) {
	t.Parallel()

	_, repository := createRemote(t)
	assignmentDir := ":(literal)README.md"
	writeFile(t, filepath.Join(repository, ".gitignore"), "*.log\n")
	writeFile(t, filepath.Join(repository, assignmentDir, "answer.txt"), "original\n")
	writeFile(t, filepath.Join(repository, "README.md"), "original\n")
	writeFile(t, filepath.Join(repository, "notes.txt"), "original\n")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "initial")

	client := New()
	writeFile(t, filepath.Join(repository, "README.md"), "changed outside assignment\n")
	dirty, err := client.IsDirty(context.Background(), repository, assignmentDir)
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if dirty {
		t.Fatal("IsDirty() reported pathspec-decoded path outside assignment")
	}

	ignored := filepath.Join(repository, assignmentDir, "student.log")
	writeFile(t, ignored, "ignored work\n")
	dirty, err = client.IsDirty(context.Background(), repository, assignmentDir)
	if err != nil {
		t.Fatalf("IsDirty() with ignored file error = %v", err)
	}
	if !dirty {
		t.Fatal("IsDirty() did not report ignored file in literal assignment path")
	}
	if err := os.Remove(ignored); err != nil {
		t.Fatalf("Remove(%q): %v", ignored, err)
	}

	writeFile(t, filepath.Join(repository, assignmentDir, "submission.txt"), "new work\n")
	writeFile(t, filepath.Join(repository, assignmentDir, "answer.txt"), "changed\n")
	dirty, err = client.IsDirty(context.Background(), repository, assignmentDir)
	if err != nil {
		t.Fatalf("IsDirty() with untracked file error = %v", err)
	}
	if !dirty {
		t.Fatal("IsDirty() did not report untracked file in literal assignment path")
	}

	if err := client.StageAssignment(context.Background(), repository, assignmentDir); err != nil {
		t.Fatalf("StageAssignment() error = %v", err)
	}
	wantAssignmentFiles := strings.Join([]string{
		assignmentDir + "/answer.txt",
		assignmentDir + "/submission.txt",
	}, "\n")
	if got := strings.TrimSpace(runGit(t, repository, "diff", "--cached", "--name-only")); got != wantAssignmentFiles {
		t.Fatalf("staged files = %q, want %q", got, wantAssignmentFiles)
	}

	writeFile(t, filepath.Join(repository, "notes.txt"), "staged outside assignment\n")
	runGit(t, repository, "add", "notes.txt")
	if err := client.CommitAssignment(context.Background(), repository, assignmentDir, "sync literal assignment"); err != nil {
		t.Fatalf("CommitAssignment() error = %v", err)
	}
	if got := strings.TrimSpace(runGit(t, repository, "show", "--pretty=", "--name-only", "HEAD")); got != wantAssignmentFiles {
		t.Fatalf("committed files = %q, want %q", got, wantAssignmentFiles)
	}
	if got := strings.TrimSpace(runGit(t, repository, "diff", "--cached", "--name-only")); got != "notes.txt" {
		t.Fatalf("remaining staged files = %q, want notes.txt", got)
	}
}

func TestCommandErrorCapturesOutput(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("exit status 1")
	client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte("standard output\n"), []byte("specific failure\n"), sentinel
	}))

	err := client.Verify(context.Background())
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("Verify() error = %T, want *CommandError", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Verify() error does not wrap runner error: %v", err)
	}
	for _, want := range []string{"git --version", "standard output", "specific failure"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

type runnerFunc func(context.Context, string, ...string) ([]byte, []byte, error)

func (f runnerFunc) Run(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	return f(ctx, dir, args...)
}

func createRemote(t *testing.T) (remote, source string) {
	t.Helper()

	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	source = filepath.Join(root, "source")
	runGit(t, "", "init", "--bare", remote)
	runGit(t, "", "init", "--initial-branch=main", source)
	runGit(t, source, "config", "user.name", "Sync Assign Test")
	runGit(t, source, "config", "user.email", "sync-assign@example.invalid")
	runGit(t, source, "remote", "add", "origin", remote)
	return remote, source
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(content)
}
