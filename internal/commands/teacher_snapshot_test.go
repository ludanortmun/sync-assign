package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
)

func TestTeacherSnapshotUsesBlobBytes(t *testing.T) {
	remote, teacher := newGitRemote(t, "teacher")
	writeTestFile(t, filepath.Join(teacher, ".gitattributes"), "*.txt text eol=crlf\n")
	writeTestFile(t, filepath.Join(teacher, "baseline.txt"), "full bytes\n")
	runGitCommand(t, teacher, "add", ".")
	runGitCommand(t, teacher, "commit", "-m", "baseline")
	runGitCommand(t, teacher, "push", "-u", "origin", "main")
	pin := strings.TrimSpace(runGitCommand(t, teacher, "rev-parse", "HEAD"))
	for _, revision := range []string{"", pin[:12]} {
		snapshot, err := openTeacherSnapshot(context.Background(), config.StudentConfig{TeacherRepository: remote}, revision)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(snapshot.Path(), "baseline.txt"))
		if err != nil || string(data) != "full bytes\n" {
			t.Fatalf("snapshot normalized blob: %q, %v", data, err)
		}
		if err := snapshot.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
