package syncer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
)

type fakeGit struct {
	dirty bool
	err   error
	calls int
	repo  string
	path  string
}

func (git *fakeGit) IsDirty(_ context.Context, repository, path string) (bool, error) {
	git.calls++
	git.repo = repository
	git.path = path
	return git.dirty, git.err
}

func TestSyncCopiesAssignmentContents(t *testing.T) {
	teacher := t.TempDir()
	student := t.TempDir()
	source := filepath.Join(teacher, "lab")
	mustMkdir(t, filepath.Join(source, "nested"), 0o750)
	mustWrite(t, filepath.Join(source, "nested", "answer.txt"), "answer\n", 0o640)
	mustWrite(t, filepath.Join(source, ".env"), "VALUE=1\n", 0o600)
	mustWrite(t, filepath.Join(source, ".gitignore"), "*.out\n", 0o644)
	mustWrite(t, filepath.Join(source, ".gitkeep"), "", 0o644)
	mustWrite(t, filepath.Join(source, "run.sh"), "#!/bin/sh\n", 0o755)
	mustMkdir(t, filepath.Join(source, ".git", "objects"), 0o755)
	mustWrite(t, filepath.Join(source, ".git", "config"), "secret", 0o644)
	mustMkdir(t, filepath.Join(source, "nested", ".git"), 0o755)
	mustWrite(t, filepath.Join(source, "nested", ".git", "metadata"), "secret", 0o644)

	target, err := Sync(context.Background(), nil, teacherConfig("lab"), teacher, student, "assignment", Options{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if target != filepath.Join(student, "lab") {
		t.Fatalf("Sync() target = %q", target)
	}
	for path, want := range map[string]string{
		"nested/answer.txt": "answer\n",
		".env":              "VALUE=1\n",
		".gitignore":        "*.out\n",
		".gitkeep":          "",
	} {
		content, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		if string(content) != want {
			t.Errorf("%s content = %q, want %q", path, content, want)
		}
	}
	for _, path := range []string{".git", "nested/.git"} {
		if _, err := os.Lstat(filepath.Join(target, filepath.FromSlash(path))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s was copied or produced unexpected error: %v", path, err)
		}
	}
	if mode := mustStat(t, filepath.Join(target, "run.sh")).Mode().Perm(); mode != 0o755 {
		t.Errorf("run.sh mode = %o, want 755", mode)
	}
	if mode := mustStat(t, filepath.Join(target, "nested")).Mode().Perm(); mode != 0o750 {
		t.Errorf("nested mode = %o, want 750", mode)
	}
}

func TestSyncCopiesSymlinksWithoutFollowingThem(t *testing.T) {
	teacher := t.TempDir()
	student := t.TempDir()
	source := filepath.Join(teacher, "lab")
	mustMkdir(t, source, 0o755)
	mustWrite(t, filepath.Join(source, "inside.txt"), "inside", 0o644)
	outside := filepath.Join(teacher, "outside.txt")
	mustWrite(t, outside, "outside", 0o644)
	if err := os.Symlink("inside.txt", filepath.Join(source, "inside-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, ".git")); err != nil {
		t.Fatal(err)
	}

	target, err := Sync(context.Background(), nil, teacherConfig("lab"), teacher, student, "assignment", Options{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	for name, want := range map[string]string{"inside-link": "inside.txt", "outside-link": outside} {
		got, err := os.Readlink(filepath.Join(target, name))
		if err != nil {
			t.Fatalf("Readlink(%q): %v", name, err)
		}
		if got != want {
			t.Errorf("Readlink(%q) = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(target, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".git symlink was copied or produced unexpected error: %v", err)
	}
}

func TestSyncExistingTargetRequiresClean(t *testing.T) {
	teacher, student := syncRoots(t)
	mustMkdir(t, filepath.Join(student, "lab"), 0o755)

	git := &fakeGit{}
	_, err := Sync(context.Background(), git, teacherConfig("lab"), teacher, student, "assignment", Options{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Sync() error = %v, want existing target error", err)
	}
	if git.calls != 0 {
		t.Fatalf("dirty checks = %d, want 0", git.calls)
	}
}

func TestSyncCleanReplacesCleanTarget(t *testing.T) {
	teacher, student := syncRoots(t)
	target := filepath.Join(student, "lab")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "old.txt"), "old", 0o644)
	git := &fakeGit{}

	_, err := Sync(context.Background(), git, teacherConfig("lab"), teacher, student, "assignment", Options{Clean: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if git.calls != 1 || git.path != "lab" || git.repo != student {
		t.Fatalf("dirty check = calls:%d repo:%q path:%q", git.calls, git.repo, git.path)
	}
	if _, err := os.Lstat(filepath.Join(target, "old.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old file remains or produced unexpected error: %v", err)
	}
	if got := mustRead(t, filepath.Join(target, "new.txt")); got != "new" {
		t.Errorf("new content = %q", got)
	}
	assertNoSyncArtifacts(t, student)
}

func TestSyncCleanPreservesTargetOnCopyFailure(t *testing.T) {
	teacher, student := syncRoots(t)
	target := filepath.Join(student, "lab")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "old.txt"), "old", 0o644)
	pipe := filepath.Join(teacher, "lab", "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("cannot create FIFO: %v", err)
	}

	_, err := Sync(
		context.Background(),
		&fakeGit{},
		teacherConfig("lab"),
		teacher,
		student,
		"assignment",
		Options{Clean: true},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("Sync() error = %v, want unsupported file type error", err)
	}
	if got := mustRead(t, filepath.Join(target, "old.txt")); got != "old" {
		t.Fatalf("old target content = %q, want old", got)
	}
	if _, err := os.Lstat(filepath.Join(target, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("new assignment was published or produced unexpected error: %v", err)
	}
	assertNoSyncArtifacts(t, student)
}

func TestPublishDirectoryRestoresTargetOnPublicationFailure(t *testing.T) {
	student := t.TempDir()
	target := filepath.Join(student, "lab")
	staging := filepath.Join(student, ".sync-assign-lab-staged")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "old.txt"), "old", 0o644)
	mustMkdir(t, staging, 0o755)
	mustWrite(t, filepath.Join(staging, "new.txt"), "new", 0o644)

	renames := 0
	rename := func(oldPath, newPath string) error {
		renames++
		if renames == 2 {
			return errors.New("injected publication failure")
		}
		return os.Rename(oldPath, newPath)
	}
	err := publishDirectory(staging, target, student, true, rename, os.RemoveAll)
	if err == nil || !strings.Contains(err.Error(), "injected publication failure") {
		t.Fatalf("publishDirectory() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(target, "old.txt")); got != "old" {
		t.Fatalf("restored target content = %q, want old", got)
	}
	if got := mustRead(t, filepath.Join(staging, "new.txt")); got != "new" {
		t.Fatalf("staged target content = %q, want new", got)
	}
	assertNoSyncArtifactsExcept(t, student, filepath.Base(staging))
}

func TestPublishDirectoryOnlyCleansBackupOwnedByInvocation(t *testing.T) {
	student := t.TempDir()
	target := filepath.Join(student, "lab")
	staging := filepath.Join(student, ".sync-assign-lab-staged")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "old.txt"), "old", 0o644)
	mustMkdir(t, staging, 0o755)
	mustWrite(t, filepath.Join(staging, "new.txt"), "new", 0o644)

	var backupContainer string
	removeAll := func(path string) error {
		backupContainer = path
		return errors.New("injected backup cleanup failure")
	}

	err := publishDirectory(staging, target, student, true, os.Rename, removeAll)
	if err != nil {
		t.Fatalf("publishDirectory() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(target, "new.txt")); got != "new" {
		t.Fatalf("published target content = %q, want new", got)
	}
	if _, err := os.Lstat(filepath.Join(target, "old.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old target was restored or produced unexpected error: %v", err)
	}
	if backupContainer == "" {
		t.Fatal("backup cleanup was not attempted")
	}
	if got := mustRead(t, filepath.Join(backupContainer, "lab", "old.txt")); got != "old" {
		t.Fatalf("retained backup content = %q, want old", got)
	}

	nextStaging := filepath.Join(student, ".sync-assign-lab-next")
	mustMkdir(t, nextStaging, 0o755)
	mustWrite(t, filepath.Join(nextStaging, "newer.txt"), "newer", 0o644)
	if err := publishDirectory(nextStaging, target, student, true, os.Rename, os.RemoveAll); err != nil {
		t.Fatalf("publishDirectory() second publication error = %v", err)
	}
	if got := mustRead(t, filepath.Join(target, "newer.txt")); got != "newer" {
		t.Fatalf("republished target content = %q, want newer", got)
	}
	if got := mustRead(t, filepath.Join(backupContainer, "lab", "old.txt")); got != "old" {
		t.Fatalf("previous invocation backup content = %q, want old", got)
	}
}

func TestSyncCleanNeverTouchesOldDeterministicBackupName(t *testing.T) {
	teacher, student := syncRoots(t)
	target := filepath.Join(student, "lab")
	userOwned := filepath.Join(student, ".sync-assign-backup-lab")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "old.txt"), "old", 0o644)
	mustMkdir(t, userOwned, 0o755)
	mustWrite(t, filepath.Join(userOwned, "student.txt"), "keep", 0o644)

	if _, err := Sync(
		context.Background(),
		&fakeGit{},
		teacherConfig("lab"),
		teacher,
		student,
		"assignment",
		Options{Clean: true},
	); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(userOwned, "student.txt")); got != "keep" {
		t.Fatalf("user-owned backup-like directory content = %q, want keep", got)
	}
}

func TestSyncCleanRefusesDirtyTarget(t *testing.T) {
	teacher, student := syncRoots(t)
	target := filepath.Join(student, "lab")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "student.txt"), "work", 0o644)

	_, err := Sync(context.Background(), &fakeGit{dirty: true}, teacherConfig("lab"), teacher, student, "assignment", Options{Clean: true})
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("Sync() error = %v, want dirty error", err)
	}
	if got := mustRead(t, filepath.Join(target, "student.txt")); got != "work" {
		t.Fatalf("dirty target changed to %q", got)
	}
}

func TestSyncForceWithCleanReplacesDirtyTarget(t *testing.T) {
	teacher, student := syncRoots(t)
	target := filepath.Join(student, "lab")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "student.txt"), "work", 0o644)
	git := &fakeGit{dirty: true}

	_, err := Sync(context.Background(), git, teacherConfig("lab"), teacher, student, "assignment", Options{Clean: true, Force: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if git.calls != 0 {
		t.Fatalf("dirty checks = %d, want force to bypass check", git.calls)
	}
	if got := mustRead(t, filepath.Join(target, "new.txt")); got != "new" {
		t.Errorf("new content = %q", got)
	}
}

func TestSyncCleanTreatsIgnoredStudentFileAsDirty(t *testing.T) {
	teacher, student := syncRoots(t)
	target := filepath.Join(student, "lab")
	mustMkdir(t, target, 0o755)
	mustWrite(t, filepath.Join(target, "tracked.txt"), "tracked", 0o644)
	mustWrite(t, filepath.Join(student, ".gitignore"), "lab/student.log\n", 0o644)
	runGit(t, student, "init")
	runGit(t, student, "config", "user.name", "Sync Assign Test")
	runGit(t, student, "config", "user.email", "sync-assign@example.invalid")
	runGit(t, student, "add", ".")
	runGit(t, student, "commit", "-m", "initial")
	mustWrite(t, filepath.Join(target, "student.log"), "ignored work", 0o644)

	client := gitcmd.New()
	_, err := Sync(
		context.Background(),
		client,
		teacherConfig("lab"),
		teacher,
		student,
		"assignment",
		Options{Clean: true},
	)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("Sync() error = %v, want ignored-file dirty error", err)
	}
	if got := mustRead(t, filepath.Join(target, "student.log")); got != "ignored work" {
		t.Fatalf("ignored student content after refused clean = %q", got)
	}

	if _, err := Sync(
		context.Background(),
		client,
		teacherConfig("lab"),
		teacher,
		student,
		"assignment",
		Options{Clean: true, Force: true},
	); err != nil {
		t.Fatalf("forced Sync() error = %v", err)
	}
	if got := mustRead(t, filepath.Join(target, "new.txt")); got != "new" {
		t.Fatalf("forced replacement content = %q, want new", got)
	}
	if _, err := os.Lstat(filepath.Join(target, "student.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ignored student file remains after force or stat failed: %v", err)
	}
}

func TestSyncRejectsForceWithoutClean(t *testing.T) {
	teacher, student := syncRoots(t)
	_, err := Sync(context.Background(), nil, teacherConfig("lab"), teacher, student, "assignment", Options{Force: true})
	if err == nil || !strings.Contains(err.Error(), "force requires clean") {
		t.Fatalf("Sync() error = %v, want force/clean error", err)
	}
}

func TestSyncRejectsInvalidInputs(t *testing.T) {
	teacher, student := syncRoots(t)
	tests := []struct {
		name    string
		config  config.TeacherConfig
		id      string
		teacher string
		want    string
	}{
		{"unknown assignment", teacherConfig("lab"), "other", teacher, "not configured"},
		{"traversal", teacherConfig("../outside"), "assignment", teacher, "single top-level"},
		{"absolute", teacherConfig(filepath.Join(string(filepath.Separator), "outside")), "assignment", teacher, "relative"},
		{"source missing", teacherConfig("missing"), "assignment", teacher, "inspect assignment source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Sync(context.Background(), nil, test.config, test.teacher, student, test.id, Options{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Sync() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestSyncRejectsSourceFileAndDirectorySymlink(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		teacher := t.TempDir()
		student := t.TempDir()
		mustWrite(t, filepath.Join(teacher, "lab"), "not a directory", 0o644)
		_, err := Sync(context.Background(), nil, teacherConfig("lab"), teacher, student, "assignment", Options{})
		if err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("Sync() error = %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		teacher := t.TempDir()
		student := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(teacher, "lab")); err != nil {
			t.Fatal(err)
		}
		_, err := Sync(context.Background(), nil, teacherConfig("lab"), teacher, student, "assignment", Options{})
		if err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("Sync() error = %v", err)
		}
	})
}

func TestSyncRejectsOverlappingSourceAndTarget(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "lab"), 0o755)

	_, err := Sync(
		context.Background(),
		&fakeGit{},
		teacherConfig("lab"),
		root,
		root,
		"assignment",
		Options{Clean: true},
	)
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("Sync() error = %v, want overlap error", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lab")); err != nil {
		t.Fatalf("source was removed: %v", err)
	}
}

func TestSyncRejectsUnsupportedFileType(t *testing.T) {
	teacher, student := syncRoots(t)
	pipe := filepath.Join(teacher, "lab", "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("cannot create FIFO: %v", err)
	}
	_, err := Sync(context.Background(), nil, teacherConfig("lab"), teacher, student, "assignment", Options{})
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("Sync() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(student, "lab")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed copy published target or produced unexpected error: %v", err)
	}
}

func teacherConfig(directory string) config.TeacherConfig {
	return config.TeacherConfig{Assignments: map[string]string{"assignment": directory}}
}

func syncRoots(t *testing.T) (string, string) {
	t.Helper()
	teacher := t.TempDir()
	student := t.TempDir()
	mustMkdir(t, filepath.Join(teacher, "lab"), 0o755)
	mustWrite(t, filepath.Join(teacher, "lab", "new.txt"), "new", 0o644)
	return teacher, student
}

func mustMkdir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, mode); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%q): %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%q): %v", path, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(content)
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	return info
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func assertNoSyncArtifacts(t *testing.T, root string) {
	t.Helper()
	assertNoSyncArtifactsExcept(t, root, "")
}

func assertNoSyncArtifactsExcept(t *testing.T, root, allowed string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", root, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sync-assign-") && entry.Name() != allowed {
			t.Errorf("temporary sync artifact remains: %q", entry.Name())
		}
	}
}
