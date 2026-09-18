package gitcmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestCurrentBranch(t *testing.T) {
	t.Parallel()

	var calls [][]string
	client := NewWithRunner(runnerFunc(func(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
		if dir != "/repo" {
			t.Errorf("git dir = %q, want /repo", dir)
		}
		calls = append(calls, append([]string(nil), args...))
		return []byte("feature/grade\n"), nil, nil
	}))

	branch, err := client.CurrentBranch(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if branch != "feature/grade" {
		t.Errorf("CurrentBranch() = %q, want %q", branch, "feature/grade")
	}
	assertGitCalls(t, calls, [][]string{
		{"branch", "--show-current"},
		{"check-ref-format", "--branch", "feature/grade"},
	})
}

func TestCurrentBranchRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("invalid ref")
	var calls [][]string
	client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) == 1 {
			return []byte("-grade\n"), nil, nil
		}
		return nil, []byte("fatal: '-grade' is not a valid branch name"), sentinel
	}))

	_, err := client.CurrentBranch(context.Background(), "/repo")
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), `validate current branch "-grade"`) {
		t.Fatalf("CurrentBranch() error = %v, want invalid current branch error", err)
	}
	assertGitCalls(t, calls, [][]string{
		{"branch", "--show-current"},
		{"check-ref-format", "--branch", "-grade"},
	})
}

func TestCommitBefore(t *testing.T) {
	t.Parallel()

	before := time.Date(2026, time.September, 18, 15, 10, 56, 0, time.FixedZone("PDT", -7*60*60))
	var calls [][]string
	client := NewWithRunner(runnerFunc(func(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
		if dir != "/repo" {
			t.Errorf("git dir = %q, want /repo", dir)
		}
		calls = append(calls, append([]string(nil), args...))
		return []byte("abc123\n"), nil, nil
	}))

	sha, ok, err := client.CommitBefore(context.Background(), "/repo", "main", before)
	if err != nil {
		t.Fatalf("CommitBefore() error = %v", err)
	}
	if sha != "abc123" || !ok {
		t.Errorf("CommitBefore() = (%q, %v), want (%q, true)", sha, ok, "abc123")
	}
	assertGitCalls(t, calls, [][]string{
		{"check-ref-format", "--branch", "main"},
		{"log", "--until", "2026-09-18T15:10:56-07:00", "-1", "--format=%H", "refs/heads/main"},
	})
}

func TestGradeBranchesAreValidatedBeforeUse(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("invalid ref")
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{
			name: "commit before",
			call: func(client *Client) error {
				_, _, err := client.CommitBefore(context.Background(), "/repo", "--all", time.Now())
				return err
			},
		},
		{
			name: "pull",
			call: func(client *Client) error {
				return client.PullBranch(context.Background(), "/repo", "--all", false)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var calls [][]string
			client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
				calls = append(calls, append([]string(nil), args...))
				return nil, []byte("fatal: '--all' is not a valid branch name"), sentinel
			}))
			err := test.call(client)
			if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), `invalid branch "--all"`) {
				t.Fatalf("error = %v, want invalid branch error", err)
			}
			assertGitCalls(t, calls, [][]string{{"check-ref-format", "--branch", "--all"}})
		})
	}
}

func TestCommitBeforeEmptyResult(t *testing.T) {
	t.Parallel()

	client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte("\n"), nil, nil
	}))
	sha, ok, err := client.CommitBefore(context.Background(), "/repo", "main", time.Now())
	if err != nil {
		t.Fatalf("CommitBefore() error = %v", err)
	}
	if sha != "" || ok {
		t.Errorf("CommitBefore() = (%q, %v), want (empty, false)", sha, ok)
	}
}

func TestCommitBeforePrefersBranchOverSameNamedTag(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(t.TempDir(), "repository")
	runGit(t, "", "init", "--initial-branch=main", repository)
	runGit(t, repository, "config", "user.name", "Sync Assign Test")
	runGit(t, repository, "config", "user.email", "sync-assign@example.invalid")
	writeFile(t, filepath.Join(repository, "answer.txt"), "first\n")
	runGit(t, repository, "add", "answer.txt")
	runGit(t, repository, "commit", "-m", "first")
	runGit(t, repository, "tag", "main")
	writeFile(t, filepath.Join(repository, "answer.txt"), "second\n")
	runGit(t, repository, "commit", "-am", "second")
	want := strings.TrimSpace(runGit(t, repository, "rev-parse", "refs/heads/main"))

	got, found, err := New().CommitBefore(context.Background(), repository, "main", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CommitBefore() error = %v", err)
	}
	if !found || got != want {
		t.Fatalf("CommitBefore() = (%q, %v), want (%q, true)", got, found, want)
	}
}

func TestPullBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		checkedOut bool
		want       [][]string
	}{
		{
			name:       "checked out",
			checkedOut: true,
			want: [][]string{
				{"check-ref-format", "--branch", "topic"},
				{"fetch", "origin", "refs/heads/topic:refs/remotes/origin/topic"},
				{"merge", "--ff-only", "refs/remotes/origin/topic"},
			},
		},
		{
			name:       "not checked out",
			checkedOut: false,
			want: [][]string{
				{"check-ref-format", "--branch", "topic"},
				{"fetch", "origin", "refs/heads/topic:refs/heads/topic"},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var calls [][]string
			client := NewWithRunner(runnerFunc(func(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
				if dir != "/repo" {
					t.Errorf("git dir = %q, want /repo", dir)
				}
				calls = append(calls, append([]string(nil), args...))
				return nil, nil, nil
			}))
			if err := client.PullBranch(context.Background(), "/repo", "topic", test.checkedOut); err != nil {
				t.Fatalf("PullBranch() error = %v", err)
			}
			assertGitCalls(t, calls, test.want)
		})
	}
}

func TestWorktreeCommands(t *testing.T) {
	t.Parallel()

	var calls [][]string
	client := NewWithRunner(runnerFunc(func(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
		if dir != "/repo" {
			t.Errorf("git dir = %q, want /repo", dir)
		}
		calls = append(calls, append([]string(nil), args...))
		return nil, nil, nil
	}))

	if err := client.AddWorktree(context.Background(), "/repo", "/worktree", "abc123"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}
	if err := client.RemoveWorktree(context.Background(), "/repo", "/worktree"); err != nil {
		t.Fatalf("RemoveWorktree() error = %v", err)
	}
	assertGitCalls(t, calls, [][]string{
		{"worktree", "add", "--detach", "/worktree", "abc123"},
		{"worktree", "remove", "--force", "/worktree"},
	})
}

func TestGradeHelpersValidateRequiredArguments(t *testing.T) {
	t.Parallel()

	client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
		t.Fatal("runner called for invalid arguments")
		return nil, nil, nil
	}))
	before := time.Now()
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{"current branch repository", func() error { _, err := client.CurrentBranch(context.Background(), " "); return err }, "repository"},
		{"commit before repository", func() error { _, _, err := client.CommitBefore(context.Background(), "", "main", before); return err }, "repository"},
		{"commit before branch", func() error { _, _, err := client.CommitBefore(context.Background(), "/repo", "", before); return err }, "branch"},
		{"commit before time", func() error {
			_, _, err := client.CommitBefore(context.Background(), "/repo", "main", time.Time{})
			return err
		}, "before time"},
		{"pull repository", func() error { return client.PullBranch(context.Background(), "", "main", true) }, "repository"},
		{"pull branch", func() error { return client.PullBranch(context.Background(), "/repo", "", true) }, "branch"},
		{"add repository", func() error { return client.AddWorktree(context.Background(), "", "/worktree", "abc") }, "repository"},
		{"add path", func() error { return client.AddWorktree(context.Background(), "/repo", "", "abc") }, "worktree path"},
		{"add commit", func() error { return client.AddWorktree(context.Background(), "/repo", "/worktree", "") }, "commit"},
		{"remove repository", func() error { return client.RemoveWorktree(context.Background(), "", "/worktree") }, "repository"},
		{"remove path", func() error { return client.RemoveWorktree(context.Background(), "/repo", "") }, "worktree path"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.call()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestGradeHelpersPropagateFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("runner failed")
	tests := []struct {
		name string
		fail int
		call func(*Client) error
		want string
	}{
		{"current branch", 1, func(c *Client) error { _, err := c.CurrentBranch(context.Background(), "/repo"); return err }, "current branch"},
		{"commit before validation", 1, func(c *Client) error {
			_, _, err := c.CommitBefore(context.Background(), "/repo", "main", time.Now())
			return err
		}, "invalid branch"},
		{"commit before log", 2, func(c *Client) error {
			_, _, err := c.CommitBefore(context.Background(), "/repo", "main", time.Now())
			return err
		}, "commit"},
		{"pull validation", 1, func(c *Client) error { return c.PullBranch(context.Background(), "/repo", "main", true) }, "invalid branch"},
		{"pull fetch checked out", 2, func(c *Client) error { return c.PullBranch(context.Background(), "/repo", "main", true) }, "fetch"},
		{"pull merge checked out", 3, func(c *Client) error { return c.PullBranch(context.Background(), "/repo", "main", true) }, "fast-forward"},
		{"pull fetch other", 2, func(c *Client) error { return c.PullBranch(context.Background(), "/repo", "main", false) }, "fetch"},
		{"add worktree", 1, func(c *Client) error { return c.AddWorktree(context.Background(), "/repo", "/worktree", "abc") }, "add detached worktree"},
		{"remove worktree", 1, func(c *Client) error { return c.RemoveWorktree(context.Background(), "/repo", "/worktree") }, "remove worktree"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			call := 0
			client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
				call++
				if call == test.fail {
					return nil, []byte("details"), sentinel
				}
				return nil, nil, nil
			}))
			err := test.call(client)
			if !errors.Is(err, sentinel) {
				t.Fatalf("error = %v, want wrapped runner failure", err)
			}
			for _, want := range []string{test.want, "details"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func assertGitCall(t *testing.T, gotDir string, gotArgs []string, wantDir string, wantArgs ...string) {
	t.Helper()
	if gotDir != wantDir {
		t.Errorf("git dir = %q, want %q", gotDir, wantDir)
	}
	assertGitCalls(t, [][]string{gotArgs}, [][]string{wantArgs})
}

func assertGitCalls(t *testing.T, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("git call count = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if strings.Join(got[index], "\x00") != strings.Join(want[index], "\x00") {
			t.Errorf("git call %d = %#v, want %#v", index, got[index], want[index])
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
