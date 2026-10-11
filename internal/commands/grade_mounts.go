package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type gradeMount struct {
	specification string
	source        string
	destination   string
}

func (mount gradeMount) errorf(format string, args ...any) error {
	return fmt.Errorf("mount %q: %w", mount.specification, fmt.Errorf(format, args...))
}

func resolveGradeMounts(root string, specifications []string) ([]gradeMount, error) {
	mounts := make([]gradeMount, 0, len(specifications))
	for _, specification := range specifications {
		mount := gradeMount{specification: specification}
		separator := strings.LastIndexByte(specification, ':')
		if separator < 0 {
			return nil, mount.errorf("expected SOURCE:DESTINATION")
		}
		source, destination := specification[:separator], specification[separator+1:]
		if strings.TrimSpace(source) == "" || strings.TrimSpace(destination) == "" {
			return nil, mount.errorf("source and destination must not be empty")
		}
		if filepath.IsAbs(destination) || filepath.VolumeName(destination) != "" ||
			strings.ContainsAny(destination, "\\:\x00") {
			return nil, mount.errorf("destination must be a relative path inside the assignment")
		}
		for _, component := range strings.Split(destination, "/") {
			if component == ".." || strings.EqualFold(component, ".git") {
				return nil, mount.errorf("destination must not contain traversal or Git metadata")
			}
		}
		mount.destination = filepath.Clean(destination)
		if mount.destination == "." {
			return nil, mount.errorf("destination must not be the assignment root")
		}
		for _, previous := range mounts {
			if mount.destination == previous.destination ||
				strings.HasPrefix(mount.destination, previous.destination+string(filepath.Separator)) ||
				strings.HasPrefix(previous.destination, mount.destination+string(filepath.Separator)) {
				return nil, mount.errorf("destination overlaps mount %q", previous.specification)
			}
		}
		if !filepath.IsAbs(source) {
			source = filepath.Join(root, source)
		}
		absolute, err := filepath.Abs(source)
		if err != nil {
			return nil, mount.errorf("resolve source: %w", err)
		}
		mount.source, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, mount.errorf("resolve source directory: %w", err)
		}
		info, err := os.Stat(mount.source)
		if err != nil {
			return nil, mount.errorf("inspect source directory: %w", err)
		}
		if !info.IsDir() {
			return nil, mount.errorf("source must be a directory")
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

func installGradeMounts(assignmentDir string, mounts []gradeMount) error {
	// Validate every target before installing links so one mount cannot redirect
	// validation or installation of another mount into its source directory.
	for _, mount := range mounts {
		if err := validateGradeMountDestination(assignmentDir, mount); err != nil {
			return err
		}
	}
	for _, mount := range mounts {
		// Recheck after earlier mounts, including aliases on case-insensitive
		// filesystems, so no destination parent can be a newly created link.
		if err := validateGradeMountDestination(assignmentDir, mount); err != nil {
			return err
		}
		destination := filepath.Join(assignmentDir, mount.destination)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return mount.errorf("create destination parents: %w", err)
		}
		if err := os.Symlink(mount.source, destination); err != nil {
			return mount.errorf("create directory symlink: %w", err)
		}
	}
	return nil
}

func validateGradeMountDestination(assignmentDir string, mount gradeMount) error {
	parent := assignmentDir
	components := strings.Split(mount.destination, string(filepath.Separator))
	for index := 0; index < len(components); index++ {
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) && index > 0 {
			// The remaining parents and target cannot exist beneath a missing parent.
			return nil
		}
		if err != nil {
			return mount.errorf("inspect destination parent %q: %w", parent, err)
		}
		if !info.IsDir() {
			return mount.errorf("destination parent %q must be a directory, not a symlink or file", parent)
		}
		parent = filepath.Join(parent, components[index])
	}
	if _, err := os.Lstat(parent); err == nil {
		return mount.errorf("destination %q already exists", mount.destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return mount.errorf("inspect destination %q: %w", mount.destination, err)
	}
	return nil
}
