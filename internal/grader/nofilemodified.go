package grader

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const noFileModifiedCheckerName = "unmodified test files"

var ignoredProtectedFileDirs = map[string]struct{}{
	".git":               {},
	".venv":              {},
	"__pycache__":        {},
	".ipynb_checkpoints": {},
}

// NewNoFileModifiedChecker checks that tests supplied by the teacher are unchanged.
func NewNoFileModifiedChecker(_ ...string) Checker {
	return Checker{
		Name: noFileModifiedCheckerName,
		Check: func(environment Environment) Result {
			issues := protectedFileIssues(environment)
			if len(issues) == 0 {
				return Result{
					Checker: noFileModifiedCheckerName,
					Status:  Passed,
					Detail:  "all protected files are unchanged",
				}
			}

			sort.Strings(issues)
			return Result{
				Checker: noFileModifiedCheckerName,
				Status:  Failed,
				Detail:  strings.Join(issues, "\n"),
			}
		},
	}
}

func protectedFileIssues(environment Environment) []string {
	var issues []string
	walkErr := filepath.WalkDir(environment.TeacherDir, func(path string, entry fs.DirEntry, err error) error {
		relativePath, relativeErr := filepath.Rel(environment.TeacherDir, path)
		if relativeErr != nil {
			issues = append(issues, fmt.Sprintf("%s: teacher path error: %v", path, relativeErr))
			return nil
		}
		relativePath = filepath.ToSlash(relativePath)

		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: teacher read error: %v", relativePath, err))
			return nil
		}
		if entry.IsDir() {
			if relativePath != "." {
				if _, ignored := ignoredProtectedFileDirs[entry.Name()]; ignored {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !isProtectedFile(relativePath) {
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			issues = append(issues, fmt.Sprintf("%s: teacher read error: %v", relativePath, infoErr))
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		teacherContents, readErr := os.ReadFile(path)
		if readErr != nil {
			issues = append(issues, fmt.Sprintf("%s: teacher read error: %v", relativePath, readErr))
			return nil
		}

		studentPath := filepath.Join(environment.StudentDir, filepath.FromSlash(relativePath))
		studentInfo, statErr := os.Lstat(studentPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				issues = append(issues, relativePath+": missing")
			} else {
				issues = append(issues, fmt.Sprintf("%s: student read error: %v", relativePath, statErr))
			}
			return nil
		}
		if !studentInfo.Mode().IsRegular() {
			issues = append(issues, relativePath+": wrong type")
			return nil
		}

		studentContents, readErr := os.ReadFile(studentPath)
		if readErr != nil {
			issues = append(issues, fmt.Sprintf("%s: student read error: %v", relativePath, readErr))
		} else if !bytes.Equal(teacherContents, studentContents) {
			issues = append(issues, relativePath+": modified")
		}
		return nil
	})
	if walkErr != nil {
		issues = append(issues, fmt.Sprintf(".: teacher read error: %v", walkErr))
	}
	return issues
}

func isProtectedFile(relativePath string) bool {
	segments := strings.Split(relativePath, "/")
	if slices.Contains(segments[:len(segments)-1], "tests") {
		return true
	}
	matched, err := filepath.Match("test_*.py", segments[len(segments)-1])
	return err == nil && matched
}
