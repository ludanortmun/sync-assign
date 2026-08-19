package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ludanortmun/sync-assign/internal/config"
)

// DirtyChecker reports whether a path in a Git worktree has uncommitted
// changes. *gitcmd.Client implements DirtyChecker.
type DirtyChecker interface {
	IsDirty(ctx context.Context, repository, path string) (bool, error)
}

// Options controls replacement of an existing assignment.
type Options struct {
	Clean bool
	Force bool
}

// Sync copies assignmentID from teacherRoot into studentRoot and returns the
// target directory. Staging and committing the result are intentionally left
// to the caller.
func Sync(
	ctx context.Context,
	git DirtyChecker,
	teacherConfig config.TeacherConfig,
	teacherRoot string,
	studentRoot string,
	assignmentID string,
	options Options,
) (string, error) {
	if err := teacherConfig.Validate(); err != nil {
		return "", fmt.Errorf("validate teacher config: %w", err)
	}
	if options.Force && !options.Clean {
		return "", errors.New("force requires clean")
	}
	if strings.TrimSpace(assignmentID) == "" {
		return "", errors.New("assignment ID must not be empty")
	}

	assignmentDir, found := teacherConfig.Assignments[assignmentID]
	if !found {
		return "", fmt.Errorf("assignment %q is not configured", assignmentID)
	}

	teacherRoot, err := existingDirectory("teacher repository", teacherRoot)
	if err != nil {
		return "", err
	}
	studentRoot, err = existingDirectory("student repository", studentRoot)
	if err != nil {
		return "", err
	}

	source := filepath.Join(teacherRoot, assignmentDir)
	if err := requireChild(teacherRoot, source); err != nil {
		return "", fmt.Errorf("unsafe assignment source: %w", err)
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return "", fmt.Errorf("inspect assignment source %q: %w", source, err)
	}
	if !sourceInfo.IsDir() {
		return "", fmt.Errorf("assignment source %q is not a directory", source)
	}

	target := filepath.Join(studentRoot, assignmentDir)
	if err := requireChild(studentRoot, target); err != nil {
		return "", fmt.Errorf("unsafe assignment target: %w", err)
	}
	comparisonSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", fmt.Errorf("resolve assignment source %q: %w", source, err)
	}
	comparisonStudentRoot, err := filepath.EvalSymlinks(studentRoot)
	if err != nil {
		return "", fmt.Errorf("resolve student repository %q: %w", studentRoot, err)
	}
	comparisonTarget := filepath.Join(comparisonStudentRoot, assignmentDir)
	if pathsOverlap(comparisonSource, comparisonTarget) {
		return "", fmt.Errorf("assignment source %q and target %q overlap", source, target)
	}
	_, targetErr := os.Lstat(target)
	targetExists := targetErr == nil
	if targetErr != nil && !errors.Is(targetErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect assignment target %q: %w", target, targetErr)
	}
	if targetExists && !options.Clean {
		return "", fmt.Errorf("assignment target %q already exists; use clean to replace it", target)
	}
	if targetExists && !options.Force {
		if git == nil {
			return "", errors.New("git dirty checker is required when cleaning an existing assignment")
		}
		dirty, err := git.IsDirty(ctx, studentRoot, assignmentDir)
		if err != nil {
			return "", fmt.Errorf("check assignment target %q for changes: %w", target, err)
		}
		if dirty {
			return "", fmt.Errorf("assignment target %q has uncommitted changes; use force with clean to replace it", target)
		}
	}

	if err := copyDirectory(source, studentRoot, assignmentDir, sourceInfo.Mode().Perm(), targetExists); err != nil {
		return "", err
	}
	return target, nil
}

func existingDirectory(name, directory string) (string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", fmt.Errorf("%s path must not be empty", name)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolve %s path %q: %w", name, directory, err)
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect %s %q: %w", name, absolute, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s %q is not a directory", name, absolute)
	}
	return absolute, nil
}

func requireChild(root, child string) error {
	relative, err := filepath.Rel(root, child)
	if err != nil {
		return err
	}
	if relative == "." || relative == ".." || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is not below %q", child, root)
	}
	return nil
}

func pathsOverlap(first, second string) bool {
	return pathContains(first, second) || pathContains(second, first)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." ||
		(relative != ".." && !filepath.IsAbs(relative) &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

func copyDirectory(source, studentRoot, assignmentDir string, mode os.FileMode, replace bool) (err error) {
	staging, err := os.MkdirTemp(studentRoot, ".sync-assign-"+assignmentDir+"-")
	if err != nil {
		return fmt.Errorf("create staging directory in %q: %w", studentRoot, err)
	}
	defer func() {
		if removeErr := os.RemoveAll(staging); err == nil && removeErr != nil {
			err = fmt.Errorf("remove staging directory %q: %w", staging, removeErr)
		}
	}()

	if err := copyContents(source, staging); err != nil {
		return fmt.Errorf("copy assignment %q: %w", source, err)
	}
	if err := os.Chmod(staging, mode); err != nil {
		return fmt.Errorf("set assignment directory mode: %w", err)
	}
	stagingInfo, err := os.Lstat(staging)
	if err != nil {
		return fmt.Errorf("validate staged assignment %q: %w", staging, err)
	}
	if !stagingInfo.IsDir() {
		return fmt.Errorf("validate staged assignment %q: not a directory", staging)
	}

	target := filepath.Join(studentRoot, assignmentDir)
	if err := publishDirectory(staging, target, studentRoot, replace, os.Rename, os.RemoveAll); err != nil {
		return err
	}
	return nil
}

func publishDirectory(
	staging string,
	target string,
	studentRoot string,
	replace bool,
	rename func(string, string) error,
	removeAll func(string) error,
) error {
	if !replace {
		if err := rename(staging, target); err != nil {
			return fmt.Errorf("publish assignment target %q: %w", target, err)
		}
		return nil
	}

	backupContainer, err := os.MkdirTemp(studentRoot, ".sync-assign-backup-")
	if err != nil {
		return fmt.Errorf("create assignment backup container in %q: %w", studentRoot, err)
	}
	backup := filepath.Join(backupContainer, filepath.Base(target))
	if err := rename(target, backup); err != nil {
		_ = removeAll(backupContainer)
		return fmt.Errorf("back up assignment target %q: %w", target, err)
	}
	if err := rename(staging, target); err != nil {
		if restoreErr := rename(backup, target); restoreErr != nil {
			return fmt.Errorf(
				"publish assignment target %q: %w; restore backup %q: %v",
				target,
				err,
				backup,
				restoreErr,
			)
		}
		_ = removeAll(backupContainer)
		return fmt.Errorf("publish assignment target %q: %w", target, err)
	}
	_ = removeAll(backupContainer)
	return nil
}

func copyContents(source, target string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("read directory %q: %w", source, err)
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}

		sourcePath := filepath.Join(source, entry.Name())
		targetPath := filepath.Join(target, entry.Name())
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return fmt.Errorf("inspect %q: %w", sourcePath, err)
		}

		switch {
		case info.IsDir():
			if err := os.Mkdir(targetPath, 0o700); err != nil {
				return fmt.Errorf("create directory %q: %w", targetPath, err)
			}
			if err := copyContents(sourcePath, targetPath); err != nil {
				return err
			}
			if err := os.Chmod(targetPath, info.Mode().Perm()); err != nil {
				return fmt.Errorf("set directory mode for %q: %w", targetPath, err)
			}
		case info.Mode().IsRegular():
			if err := copyFile(sourcePath, targetPath, info.Mode().Perm()); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(sourcePath)
			if err != nil {
				return fmt.Errorf("read symbolic link %q: %w", sourcePath, err)
			}
			if err := os.Symlink(link, targetPath); err != nil {
				return fmt.Errorf("create symbolic link %q: %w", targetPath, err)
			}
		default:
			return fmt.Errorf("unsupported file type at %q", sourcePath)
		}
	}
	return nil
}

func copyFile(source, target string, mode os.FileMode) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source file %q: %w", source, err)
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create target file %q: %w", target, err)
	}
	defer func() {
		if closeErr := output.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close target file %q: %w", target, closeErr)
		}
	}()

	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy %q to %q: %w", source, target, err)
	}
	if err := output.Chmod(mode); err != nil {
		return fmt.Errorf("set file mode for %q: %w", target, err)
	}
	return nil
}
