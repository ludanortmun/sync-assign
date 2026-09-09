package grading

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
)

const (
	CheckFileStructure            = "file-structure"
	CheckImmutableFilesUnmodified = "immutable-files-unmodified"
	CheckCommitBeforeDueDate      = "commit-before-due-date"
)

// DefaultChecks applies when an assignment does not specify checks.
var DefaultChecks = []string{
	CheckFileStructure,
	CheckImmutableFilesUnmodified,
	CheckCommitBeforeDueDate,
}

// CheckContext bundles the inputs check construction needs for a single
// assignment.
type CheckContext struct {
	AssignmentDir        string
	TeacherAssignmentDir string
	ImmutableFiles       []string
	RequiredFiles        []string
	DueDate              time.Time
	CommitTime           time.Time
}

// EffectiveChecks returns spec.Checks when it is non-empty, or DefaultChecks
// otherwise.
func EffectiveChecks(spec config.AssignmentSpec) []string {
	if len(spec.Checks) > 0 {
		return slices.Clone(spec.Checks)
	}
	return slices.Clone(DefaultChecks)
}

// BuildChecks constructs one Check per name, bound to ctx.
// Callers that do not want commit-before-due-date evaluated (e.g. `check`,
// which has no historical commit to check) should simply omit that name from
// checkNames rather than including it in a "skipped" state.
func BuildChecks(checkNames []string, ctx CheckContext) ([]Check, error) {
	checks := make([]Check, 0, len(checkNames))
	for _, name := range deduplicatePaths(checkNames) {
		switch name {
		case CheckFileStructure:
			checks = append(checks, newFileStructureCheck(ctx))
		case CheckImmutableFilesUnmodified:
			checks = append(checks, newImmutableFilesUnmodifiedCheck(ctx))
		case CheckCommitBeforeDueDate:
			checks = append(checks, newCommitBeforeDueDateCheck(ctx))
		default:
			checks = append(checks, NewCheck(name, func(context.Context) (Result, error) {
				return Result{}, fmt.Errorf("unsupported check %q", name)
			}))
		}
	}
	return checks, nil
}

func newFileStructureCheck(ctx CheckContext) Check {
	return NewCheck(CheckFileStructure, func(context.Context) (Result, error) {
		paths := deduplicatePaths(ctx.RequiredFiles, ctx.ImmutableFiles)
		missing := make([]string, 0)
		for _, relativePath := range paths {
			filename := filepath.Join(ctx.AssignmentDir, filepath.FromSlash(relativePath))
			if _, err := os.Stat(filename); err != nil {
				if errorsIsNotExist(err) {
					missing = append(missing, relativePath)
					continue
				}
				return Result{}, fmt.Errorf("inspect assignment path %q: %w", relativePath, err)
			}
		}

		if len(missing) > 0 {
			return Result{
				Name:   CheckFileStructure,
				Passed: false,
				Reason: fmt.Sprintf("missing required paths: %s", strings.Join(missing, ", ")),
			}, nil
		}
		return Result{
			Name:   CheckFileStructure,
			Passed: true,
			Reason: "all required paths are present",
		}, nil
	})
}

func newImmutableFilesUnmodifiedCheck(ctx CheckContext) Check {
	return NewCheck(CheckImmutableFilesUnmodified, func(context.Context) (Result, error) {
		modified := make([]string, 0)
		for _, relativePath := range ctx.ImmutableFiles {
			studentFilename := filepath.Join(ctx.AssignmentDir, filepath.FromSlash(relativePath))
			teacherFilename := filepath.Join(ctx.TeacherAssignmentDir, filepath.FromSlash(relativePath))
			if !regularPath(ctx.AssignmentDir, relativePath) {
				modified = append(modified, relativePath+" (file missing or unreadable)")
				continue
			}
			if !regularPath(ctx.TeacherAssignmentDir, relativePath) {
				return Result{}, fmt.Errorf("read teacher immutable file %q: missing, non-regular or symlink path", relativePath)
			}

			studentData, err := os.ReadFile(studentFilename)
			if err != nil {
				modified = append(modified, relativePath+" (file missing or unreadable)")
				continue
			}
			teacherData, err := os.ReadFile(teacherFilename)
			if err != nil {
				return Result{}, fmt.Errorf("read teacher immutable file %q: %w", relativePath, err)
			}

			if !bytes.Equal(studentData, teacherData) {
				modified = append(modified, relativePath)
			}
		}

		if len(modified) > 0 {
			return Result{
				Name:   CheckImmutableFilesUnmodified,
				Passed: false,
				Reason: fmt.Sprintf("immutable files were modified: %s", strings.Join(modified, ", ")),
			}, nil
		}
		return Result{
			Name:   CheckImmutableFilesUnmodified,
			Passed: true,
			Reason: "immutable files match the teacher copy",
		}, nil
	})
}

func regularPath(root, relative string) bool {
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(filepath.Clean(relative), "../") {
		return false
	}
	current := root
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func newCommitBeforeDueDateCheck(ctx CheckContext) Check {
	return NewCheck(CheckCommitBeforeDueDate, func(context.Context) (Result, error) {
		if ctx.CommitTime.IsZero() {
			return Result{}, fmt.Errorf("commit time must not be zero when the commit-before-due-date check is enabled")
		}

		if !ctx.CommitTime.After(ctx.DueDate) {
			return Result{
				Name:   CheckCommitBeforeDueDate,
				Passed: true,
				Reason: "commit time is on or before the due date",
			}, nil
		}
		return Result{
			Name:   CheckCommitBeforeDueDate,
			Passed: false,
			Reason: fmt.Sprintf(
				"commit time %s is after due date %s",
				ctx.CommitTime.Format(time.RFC3339),
				ctx.DueDate.Format(time.RFC3339),
			),
		}, nil
	})
}

func deduplicatePaths(groups ...[]string) []string {
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	for _, group := range groups {
		for _, path := range group {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	return paths
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
