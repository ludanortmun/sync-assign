package mirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
)

func TestPersistentPathUsesXDGCacheAndRepositoryHash(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	repository := "https://example.com/school/course.git"
	got, err := PersistentPath(repository)
	if err != nil {
		t.Fatalf("PersistentPath() error = %v", err)
	}
	sum := sha256.Sum256([]byte(repository))
	want := filepath.Join(cache, "sync-assign", hex.EncodeToString(sum[:]))
	if got != want {
		t.Fatalf("PersistentPath() = %q, want %q", got, want)
	}

	other, err := PersistentPath(repository + "?fork=1")
	if err != nil {
		t.Fatalf("PersistentPath(other) error = %v", err)
	}
	if other == got {
		t.Fatal("PersistentPath() returned the same path for different repositories")
	}
}

func TestPersistentPathRejectsRelativeXDGCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "relative/cache")

	if _, err := PersistentPath("https://example.com/course.git"); err == nil ||
		!strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("PersistentPath() error = %v, want absolute-path error", err)
	}
}

func TestPersistentPathFallsBackToUserCacheDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UNIX cache fallback")
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", t.TempDir())

	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("UserCacheDir() error = %v", err)
	}
	got, err := PersistentPath("https://example.com/course.git")
	if err != nil {
		t.Fatalf("PersistentPath() error = %v", err)
	}
	if filepath.Dir(filepath.Dir(got)) != cache {
		t.Fatalf("PersistentPath() = %q, want path under %q", got, cache)
	}
}

func TestOpenClonesAndUpdatesPersistentMirrorOnDefaultBranch(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	remote, source := createRemote(t)
	commitFile(t, source, "assignment.txt", "one\n", "initial")

	cfg := config.StudentConfig{TeacherRepository: remote}
	first, err := OpenWithClient(context.Background(), cfg, gitcmd.New())
	if err != nil {
		t.Fatalf("OpenWithClient() clone error = %v", err)
	}
	path := first.Path()
	if got := readFile(t, filepath.Join(path, "assignment.txt")); got != "one\n" {
		t.Fatalf("cloned content = %q, want %q", got, "one\n")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() persistent error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("persistent mirror removed by Close(): %v", err)
	}

	commitFile(t, source, "assignment.txt", "two\n", "update")
	second, err := OpenWithClient(context.Background(), cfg, gitcmd.New())
	if err != nil {
		t.Fatalf("OpenWithClient() update error = %v", err)
	}
	defer second.Close()
	if second.Path() != path {
		t.Fatalf("updated mirror path = %q, want %q", second.Path(), path)
	}
	if got := readFile(t, filepath.Join(path, "assignment.txt")); got != "two\n" {
		t.Fatalf("updated content = %q, want %q", got, "two\n")
	}
}

func TestOpenEphemeralCleansUp(t *testing.T) {
	remote, source := createRemote(t)
	commitFile(t, source, "assignment.txt", "answer\n", "initial")
	ephemeral := true

	mirror, err := OpenWithClient(context.Background(), config.StudentConfig{
		TeacherRepository: remote,
		Ephemeral:         &ephemeral,
	}, gitcmd.New())
	if err != nil {
		t.Fatalf("OpenWithClient() error = %v", err)
	}
	path := mirror.Path()
	if got := readFile(t, filepath.Join(path, "assignment.txt")); got != "answer\n" {
		t.Fatalf("ephemeral content = %q, want %q", got, "answer\n")
	}
	if err := mirror.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := mirror.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ephemeral path still exists after Close(): %v", err)
	}
}

func TestOpenConfiguredPathIsNeverRemoved(t *testing.T) {
	remote, source := createRemote(t)
	commitFile(t, source, "assignment.txt", "answer\n", "initial")
	path := filepath.Join(t.TempDir(), "configured", "teacher")

	mirror, err := OpenWithClient(context.Background(), config.StudentConfig{
		TeacherRepository: remote,
		TeacherPath:       &path,
	}, gitcmd.New())
	if err != nil {
		t.Fatalf("OpenWithClient() error = %v", err)
	}
	if mirror.Path() != path {
		t.Fatalf("Path() = %q, want %q", mirror.Path(), path)
	}
	if err := mirror.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("configured path removed by Close(): %v", err)
	}
}

func TestOpenUsesConfiguredBranch(t *testing.T) {
	remote, source := createRemote(t)
	runGit(t, source, "checkout", "-b", "fall-2026")
	commitFileOnBranch(t, source, "assignment.txt", "fall\n", "initial", "fall-2026")
	ephemeral := true
	branch := "fall-2026"

	mirror, err := OpenWithClient(context.Background(), config.StudentConfig{
		TeacherRepository: remote,
		Ephemeral:         &ephemeral,
		Branch:            &branch,
	}, gitcmd.New())
	if err != nil {
		t.Fatalf("OpenWithClient() error = %v", err)
	}
	defer mirror.Close()
	if got := readFile(t, filepath.Join(mirror.Path(), "assignment.txt")); got != "fall\n" {
		t.Fatalf("configured branch content = %q, want %q", got, "fall\n")
	}
}

func TestOpenRejectsInvalidExistingConfiguredPathWithoutRemovingIt(t *testing.T) {
	path := t.TempDir()
	sentinel := filepath.Join(path, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := OpenWithClient(context.Background(), config.StudentConfig{
		TeacherRepository: "https://example.com/course.git",
		TeacherPath:       &path,
	}, gitcmd.New())
	if err == nil || !strings.Contains(err.Error(), "git metadata is missing") {
		t.Fatalf("OpenWithClient() error = %v, want missing git metadata", err)
	}
	if got := readFile(t, sentinel); got != "keep\n" {
		t.Fatalf("sentinel content = %q, want unchanged", got)
	}
}

func TestOpenRejectsConfiguredPathWithEphemeralMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teacher")
	ephemeral := true

	_, err := OpenWithClient(context.Background(), config.StudentConfig{
		TeacherRepository: "https://example.com/course.git",
		TeacherPath:       &path,
		Ephemeral:         &ephemeral,
	}, gitcmd.New())
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("OpenWithClient() error = %v, want conflicting-mode error", err)
	}
}

func TestOpenDoesNotRemoveConfiguredPathAfterCloneFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teacher")
	sentinel := filepath.Join(path, "partial-clone")
	client := gitcmd.NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		if err := os.MkdirAll(args[len(args)-1], 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return nil, []byte("clone failed"), errors.New("exit status 1")
	}))

	_, err := OpenWithClient(context.Background(), config.StudentConfig{
		TeacherRepository: "https://example.com/course.git",
		TeacherPath:       &path,
	}, client)
	if err == nil {
		t.Fatal("OpenWithClient() error = nil, want clone failure")
	}
	if got := readFile(t, sentinel); got != "keep\n" {
		t.Fatalf("configured path content = %q, want unchanged", got)
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

func commitFile(t *testing.T, repository, name, contents, message string) {
	t.Helper()
	commitFileOnBranch(t, repository, name, contents, message, "main")
}

func commitFileOnBranch(t *testing.T, repository, name, contents, message, branch string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repository, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", name)
	runGit(t, repository, "commit", "-m", message)
	runGit(t, repository, "push", "-u", "origin", branch)
}

func runGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
