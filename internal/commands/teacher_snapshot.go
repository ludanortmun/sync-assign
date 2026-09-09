package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ludanortmun/sync-assign/internal/config"
)

type teacherSnapshot struct{ path string }

func (s teacherSnapshot) Path() string { return s.path }
func (s teacherSnapshot) Close() error { return os.RemoveAll(s.path) }

// A private clone avoids resetting a configured checkout or racing another run.
func openTeacherSnapshot(ctx context.Context, cfg config.StudentConfig, pin string) (teacherMirror, error) {
	if pin != "" && !regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`).MatchString(pin) {
		return nil, fmt.Errorf("teacher-commit must be a 7–40 character hexadecimal commit ID")
	}
	path, err := os.MkdirTemp(".", ".sync-assign-teacher-*")
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	snapshot := teacherSnapshot{path}
	run := func(args ...string) (string, error) {
		output, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("prepare teacher snapshot: %w: %s", err, output)
		}
		return strings.TrimSpace(string(output)), nil
	}
	if _, err = run("clone", "--no-checkout", "--", cfg.TeacherRepository, path); err != nil {
		_ = snapshot.Close()
		return nil, err
	}
	revision := "refs/remotes/origin/main"
	if pin != "" {
		revision = pin
	}
	hash, err := run("-C", path, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err == nil {
		err = materializeTeacher(ctx, path, hash)
	}

	if err != nil {
		_ = snapshot.Close()
		return nil, err
	}
	return snapshot, nil
}

func materializeTeacher(ctx context.Context, root, hash string) error {
	tree, err := exec.CommandContext(ctx, "git", "-C", root, "ls-tree", "-rz", hash).Output()
	if err != nil {
		return err
	}
	for _, record := range strings.Split(string(tree), "\x00") {
		if record == "" {
			continue
		}
		header, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return fmt.Errorf("teacher snapshot supports regular files only: %q", name)
		}
		data, err := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "blob", fields[2]).Output()
		if err != nil {
			return err
		}
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if fields[0] == "100755" {
			mode = 0o755
		}
		if err := os.WriteFile(filename, data, mode); err != nil {
			return err
		}
	}
	return nil
}
