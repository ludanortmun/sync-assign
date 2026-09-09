package gitcmd

import (
	"context"
	"errors"
	"fmt"
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

func TestCurrentBranch(t *testing.T) {
	t.Parallel()

	t.Run("returns trimmed branch and exact args", func(t *testing.T) {
		t.Parallel()

		var gotDir string
		var gotArgs []string
		client := NewWithRunner(runnerFunc(func(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
			gotDir = dir
			gotArgs = append([]string(nil), args...)
			return []byte("main\n"), nil, nil
		}))

		branch, err := client.CurrentBranch(context.Background(), "/repo")
		if err != nil {
			t.Fatalf("CurrentBranch() error = %v", err)
		}
		if branch != "main" {
			t.Fatalf("CurrentBranch() = %q, want %q", branch, "main")
		}
		if gotDir != "/repo" {
			t.Fatalf("git dir = %q, want %q", gotDir, "/repo")
		}
		wantArgs := []string{"rev-parse", "--abbrev-ref", "HEAD"}
		if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
			t.Fatalf("git args = %#v, want %#v", gotArgs, wantArgs)
		}
	})

	t.Run("validates repository", func(t *testing.T) {
		t.Parallel()

		_, err := New().CurrentBranch(context.Background(), " ")
		if err == nil || !strings.Contains(err.Error(), "repository must not be empty") {
			t.Fatalf("CurrentBranch() error = %v, want repository validation", err)
		}
	})

	t.Run("propagates command errors", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("exit status 1")
		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return nil, []byte("fatal"), sentinel
		}))

		err := mustCurrentBranchError(t, client, "/repo")
		var commandErr *CommandError
		if !errors.As(err, &commandErr) {
			t.Fatalf("CurrentBranch() error = %T, want *CommandError", err)
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("CurrentBranch() error = %v, want wrapped sentinel", err)
		}
	})
}

func TestFastForwardToRemote(t *testing.T) {
	t.Parallel()

	t.Run("fetches and merges immutable revision", func(t *testing.T) {
		t.Parallel()

		var calls [][]string
		client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
			calls = append(calls, append([]string(nil), args...))
			if args[0] == "rev-parse" {
				return []byte("abc123\n"), nil, nil
			}
			return nil, nil, nil
		}))

		if revision, err := client.FastForwardToRemote(context.Background(), "/repo", "main"); err != nil || revision != "abc123" {
			t.Fatalf("FastForwardToRemote() = %q, %v", revision, err)
		}

		wantCalls := [][]string{
			{"fetch", "origin", "main"},
			{"rev-parse", "--verify", "FETCH_HEAD^{commit}"},
			{"merge", "--ff-only", "abc123"},
		}
		if len(calls) != len(wantCalls) {
			t.Fatalf("git call count = %d, want %d: %#v", len(calls), len(wantCalls), calls)
		}
		for index := range wantCalls {
			if strings.Join(calls[index], "\x00") != strings.Join(wantCalls[index], "\x00") {
				t.Errorf("git call %d = %#v, want %#v", index, calls[index], wantCalls[index])
			}
		}
	})

	t.Run("validates required parameters", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			repository string
			branch     string
			want       string
		}{
			{name: "repository", repository: "", branch: "main", want: "repository must not be empty"},
			{name: "branch", repository: "/repo", branch: " ", want: "branch must not be empty"},
		}

		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				_, err := New().FastForwardToRemote(context.Background(), test.repository, test.branch)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("FastForwardToRemote() error = %v, want %q", err, test.want)
				}
			})
		}
	})

	t.Run("wraps diverged history failure clearly", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("exit status 1")
		client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
			if args[0] == "merge" {
				return nil, []byte("fatal: Not possible to fast-forward, aborting."), sentinel
			}
			if args[0] == "rev-parse" {
				return []byte("abc123\n"), nil, nil
			}
			return nil, nil, nil
		}))

		_, err := client.FastForwardToRemote(context.Background(), "/repo", "main")
		if !errors.Is(err, sentinel) {
			t.Fatalf("FastForwardToRemote() error = %v, want wrapped sentinel", err)
		}
		for _, want := range []string{
			`fast-forward branch "main" to remote`,
			"local branch has diverged from origin",
			`git merge --ff-only abc123 in "/repo" failed`,
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("FastForwardToRemote() error %q does not contain %q", err, want)
			}
		}
	})
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

func TestCommitsUpTo(t *testing.T) {
	t.Parallel()

	t.Run("selects newest commit at or before cutoff", func(t *testing.T) {
		t.Parallel()

		upTo := time.Date(2025, time.March, 2, 12, 0, 0, 0, time.FixedZone("UTC-7", -7*60*60))
		output := strings.Join([]string{
			"cccccccccccccccccccccccccccccccccccccccc|2025-03-03T12:00:00Z",
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb|2025-03-02T19:00:00Z",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|2025-03-01T08:00:00Z",
		}, "\n") + "\n"

		var gotArgs []string
		client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
			gotArgs = append([]string(nil), args...)
			return []byte(output), nil, nil
		}))

		commit, err := client.CommitsUpTo(context.Background(), "/repo", "HEAD", upTo)
		if err != nil {
			t.Fatalf("CommitsUpTo() error = %v", err)
		}
		if commit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
			t.Fatalf("CommitsUpTo() = %q, want matching boundary commit", commit)
		}
		wantArgs := []string{"log", "HEAD", "--date=iso-strict", "--format=%H|%cI"}
		if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
			t.Fatalf("git args = %#v, want %#v", gotArgs, wantArgs)
		}
	})

	t.Run("returns no commit error when all commits are after cutoff", func(t *testing.T) {
		t.Parallel()

		upTo := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|2025-01-02T00:00:00Z\n"), nil, nil
		}))

		_, err := client.CommitsUpTo(context.Background(), "/repo", "HEAD", upTo)
		want := fmt.Sprintf("no commit at or before %s found in %q history", upTo.Format(time.RFC3339), "HEAD")
		if err == nil || err.Error() != want {
			t.Fatalf("CommitsUpTo() error = %v, want %q", err, want)
		}
	})

	t.Run("reports malformed log line", func(t *testing.T) {
		t.Parallel()

		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return []byte("missing-separator\n"), nil, nil
		}))

		_, err := client.CommitsUpTo(context.Background(), "/repo", "HEAD", time.Now())
		if err == nil || !strings.Contains(err.Error(), `parse git log output for "HEAD": malformed line "missing-separator"`) {
			t.Fatalf("CommitsUpTo() error = %v, want malformed line error", err)
		}
	})

	t.Run("reports malformed commit time", func(t *testing.T) {
		t.Parallel()

		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa|not-a-time\n"), nil, nil
		}))

		_, err := client.CommitsUpTo(context.Background(), "/repo", "HEAD", time.Now())
		if err == nil || !strings.Contains(err.Error(), `parse git log output for "HEAD": parse commit time "not-a-time"`) {
			t.Fatalf("CommitsUpTo() error = %v, want parse time error", err)
		}
	})

	t.Run("validates required parameters", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			repository string
			ref        string
			want       string
		}{
			{name: "repository", repository: "", ref: "HEAD", want: "repository must not be empty"},
			{name: "ref", repository: "/repo", ref: " ", want: "ref must not be empty"},
		}

		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				_, err := New().CommitsUpTo(context.Background(), test.repository, test.ref, time.Now())
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("CommitsUpTo() error = %v, want %q", err, test.want)
				}
			})
		}
	})
}

func TestCommitTime(t *testing.T) {
	t.Parallel()

	t.Run("returns parsed commit time and exact args", func(t *testing.T) {
		t.Parallel()

		var gotDir string
		var gotArgs []string
		client := NewWithRunner(runnerFunc(func(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
			gotDir = dir
			gotArgs = append([]string(nil), args...)
			return []byte("2026-01-09T10:00:00Z\n"), nil, nil
		}))

		commitTime, err := client.CommitTime(context.Background(), "/repo", "abc123")
		if err != nil {
			t.Fatalf("CommitTime() error = %v", err)
		}
		if want := time.Date(2026, time.January, 9, 10, 0, 0, 0, time.UTC); !commitTime.Equal(want) {
			t.Fatalf("CommitTime() = %s, want %s", commitTime.Format(time.RFC3339), want.Format(time.RFC3339))
		}
		if gotDir != "/repo" {
			t.Fatalf("git dir = %q, want %q", gotDir, "/repo")
		}
		wantArgs := []string{"log", "-1", "--format=%cI", "abc123"}
		if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
			t.Fatalf("git args = %#v, want %#v", gotArgs, wantArgs)
		}
	})

	t.Run("reports malformed output", func(t *testing.T) {
		t.Parallel()

		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return []byte("not-a-time\n"), nil, nil
		}))

		_, err := client.CommitTime(context.Background(), "/repo", "abc123")
		if err == nil || !strings.Contains(err.Error(), `parse commit time for "abc123"`) {
			t.Fatalf("CommitTime() error = %v, want parse error", err)
		}
	})

	t.Run("propagates git command failure", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("exit status 1")
		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return nil, []byte("fatal"), sentinel
		}))

		_, err := client.CommitTime(context.Background(), "/repo", "abc123")
		var commandErr *CommandError
		if !errors.As(err, &commandErr) || !errors.Is(err, sentinel) {
			t.Fatalf("CommitTime() error = %v, want wrapped command error", err)
		}
	})
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

func TestAddAndRemoveWorktree(t *testing.T) {
	t.Parallel()

	t.Run("add worktree passes exact args", func(t *testing.T) {
		t.Parallel()

		var gotDir string
		var gotArgs []string
		client := NewWithRunner(runnerFunc(func(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
			gotDir = dir
			gotArgs = append([]string(nil), args...)
			return nil, nil, nil
		}))

		if err := client.AddWorktree(context.Background(), "/repo", "/worktree", "abc123"); err != nil {
			t.Fatalf("AddWorktree() error = %v", err)
		}
		if gotDir != "/repo" {
			t.Fatalf("git dir = %q, want %q", gotDir, "/repo")
		}
		wantArgs := []string{"worktree", "add", "--detach", "/worktree", "abc123"}
		if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
			t.Fatalf("git args = %#v, want %#v", gotArgs, wantArgs)
		}
	})

	t.Run("remove worktree passes exact args", func(t *testing.T) {
		t.Parallel()

		var gotArgs []string
		client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
			gotArgs = append([]string(nil), args...)
			return nil, nil, nil
		}))

		if err := client.RemoveWorktree(context.Background(), "/repo", "/worktree"); err != nil {
			t.Fatalf("RemoveWorktree() error = %v", err)
		}
		wantArgs := []string{"worktree", "remove", "--force", "/worktree"}
		if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
			t.Fatalf("git args = %#v, want %#v", gotArgs, wantArgs)
		}
	})

	t.Run("add worktree validates params and propagates command errors", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			repository  string
			worktree    string
			commit      string
			wantError   string
			commandFail bool
		}{
			{name: "repository", worktree: "/worktree", commit: "abc123", wantError: "repository must not be empty"},
			{name: "worktree", repository: "/repo", commit: "abc123", wantError: "worktree path must not be empty"},
			{name: "commit", repository: "/repo", worktree: "/worktree", wantError: "commit must not be empty"},
			{name: "command", repository: "/repo", worktree: "/worktree", commit: "abc123", commandFail: true},
		}

		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				client := New()
				if test.commandFail {
					sentinel := errors.New("exit status 1")
					client = NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
						return nil, []byte("fatal"), sentinel
					}))
					err := client.AddWorktree(context.Background(), test.repository, test.worktree, test.commit)
					var commandErr *CommandError
					if !errors.As(err, &commandErr) || !errors.Is(err, sentinel) {
						t.Fatalf("AddWorktree() error = %v, want wrapped command error", err)
					}
					return
				}

				err := client.AddWorktree(context.Background(), test.repository, test.worktree, test.commit)
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("AddWorktree() error = %v, want %q", err, test.wantError)
				}
			})
		}
	})

	t.Run("remove worktree validates params and propagates command errors", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			repository string
			worktree   string
			wantError  string
		}{
			{name: "repository", worktree: "/worktree", wantError: "repository must not be empty"},
			{name: "worktree", repository: "/repo", wantError: "worktree path must not be empty"},
		}

		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				err := New().RemoveWorktree(context.Background(), test.repository, test.worktree)
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("RemoveWorktree() error = %v, want %q", err, test.wantError)
				}
			})
		}

		sentinel := errors.New("exit status 1")
		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return nil, []byte("fatal"), sentinel
		}))

		err := client.RemoveWorktree(context.Background(), "/repo", "/worktree")
		var commandErr *CommandError
		if !errors.As(err, &commandErr) || !errors.Is(err, sentinel) {
			t.Fatalf("RemoveWorktree() error = %v, want wrapped command error", err)
		}
	})
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

func mustCurrentBranchError(t *testing.T, client *Client, repository string) error {
	t.Helper()

	_, err := client.CurrentBranch(context.Background(), repository)
	if err == nil {
		t.Fatal("CurrentBranch() error = nil, want error")
	}
	return err
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
