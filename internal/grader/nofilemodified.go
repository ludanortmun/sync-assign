package grader

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
func NewNoFileModifiedChecker(patterns ...string) Checker {
	if len(patterns) == 0 {
		patterns = []string{"tests/**", "test_*.py"}
	}
	return Checker{
		Name: noFileModifiedCheckerName,
		Check: func(environment Environment) Result {
			issues := protectedFileIssues(environment, patterns)
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

func protectedFileIssues(environment Environment, patterns []string) []string {
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
		if !matchesProtectedPath(relativePath, patterns) {
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

func matchesProtectedPath(relativePath string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(pattern)
		if strings.HasSuffix(pattern, "/**") {
			directory := strings.TrimSuffix(pattern, "/**")
			if relativePath == directory || strings.Contains(relativePath, "/"+directory+"/") ||
				strings.HasPrefix(relativePath, directory+"/") {
				return true
			}
			continue
		}
		if matched, err := filepath.Match(pattern, relativePath); err == nil && matched {
			return true
		}
		if !strings.Contains(pattern, "/") {
			if matched, err := filepath.Match(pattern, filepath.Base(relativePath)); err == nil && matched {
				return true
			}
		}
	}
	return false
}
