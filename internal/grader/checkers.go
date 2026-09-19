package grader

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type commandExecutor func(dir, name string, args ...string) (stdout, stderr []byte, err error)

func executeCommand(dir, name string, args ...string) ([]byte, []byte, error) {
	command := exec.Command(name, args...)
	command.Dir = dir

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// NewPythonUnitTestChecker checks the student repository with pytest.
func NewPythonUnitTestChecker() Checker {
	return newPythonUnitTestChecker(executeCommand)
}

func newPythonUnitTestChecker(executor commandExecutor) Checker {
	const name = "python unit tests"
	return Checker{
		Name: name,
		Check: func(environment Environment) Result {
			args, err := uvArgs(
				environment.StudentDir,
				"--with", "pytest", "--", "pytest", "-q", "--tb=no",
				"--ignore-glob=*extra_credit.py",
			)
			if err != nil {
				return failedResult(name, err.Error())
			}
			return runUV(name, environment.StudentDir, args, executor)
		},
	}
}

// NewPythonExtraCreditTestChecker checks extra-credit Python tests separately.
// Extra-credit tests are expected to be in files named "*extra_credit.py" in the student repository.
func NewPythonExtraCreditTestChecker() Checker {
	return newPythonExtraCreditTestChecker(executeCommand)
}

func newPythonExtraCreditTestChecker(executor commandExecutor) Checker {
	const name = "python extra credit tests"
	return Checker{
		Name: name,
		Check: func(environment Environment) Result {
			paths, err := extraCreditTestPaths(environment.StudentDir)
			if err != nil {
				return failedResult(name, fmt.Sprintf("find extra-credit tests: %v", err))
			}
			if len(paths) == 0 {
				return Result{Checker: name, Status: Skipped, Detail: "no extra-credit tests found"}
			}
			args := []string{"--with", "pytest", "--", "pytest", "-q", "--tb=no"}
			args = append(args, paths...)
			args, err = uvArgs(environment.StudentDir, args...)
			if err != nil {
				return failedResult(name, err.Error())
			}
			return runUV(name, environment.StudentDir, args, executor)
		},
	}
}

// NewNotebookUnitTestChecker checks notebooks in the student repository with nbmake.
// Notebooks must be in a subdirectory named "notebooks" in the student repository.
func NewNotebookUnitTestChecker() Checker {
	return newNotebookUnitTestChecker(executeCommand)
}

func newNotebookUnitTestChecker(executor commandExecutor) Checker {
	const name = "notebook unit tests"
	return Checker{
		Name: name,
		Check: func(environment Environment) Result {
			args, err := uvArgs(
				environment.StudentDir,
				"--with", "pytest", "--with", "nbmake", "--",
				"pytest", "-q", "--tb=no", "--nbmake", "notebooks",
			)
			if err != nil {
				return failedResult(name, err.Error())
			}
			return runUV(name, environment.StudentDir, args, executor)
		},
	}
}

func uvArgs(studentDir string, args ...string) ([]string, error) {
	result := []string{"run"}
	requirements := filepath.Join(studentDir, "requirements.txt")
	if _, err := os.Stat(requirements); err == nil {
		result = append(result, "--with-requirements", "requirements.txt")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect requirements.txt: %w", err)
	}
	return append(result, args...), nil
}

func runUV(name, dir string, args []string, executor commandExecutor) Result {
	stdout, stderr, err := executor(dir, "uv", args...)
	if err == nil {
		return Result{Checker: name, Status: Passed}
	}

	var execError *exec.Error
	if errors.As(err, &execError) && errors.Is(execError.Err, exec.ErrNotFound) {
		return failedResult(name, "uv executable was not found on PATH")
	}
	if errors.Is(err, exec.ErrNotFound) {
		return failedResult(name, "uv executable was not found on PATH")
	}

	detail := fmt.Sprintf("uv run failed: %v", err)
	if output := formatPytestSummary(stdout, stderr); output != "" {
		detail += "\n" + output
	}
	return failedResult(name, detail)
}

func formatPytestSummary(stdout, stderr []byte) string {
	for _, raw := range []string{string(stdout), string(stderr)} {
		output := strings.TrimSpace(raw)
		if index := strings.Index(output, "short test summary info"); index >= 0 {
			lineStart := strings.LastIndex(output[:index], "\n")
			if lineStart < 0 {
				lineStart = 0
			} else {
				lineStart++
			}
			return output[lineStart:]
		}
	}
	output := strings.TrimSpace(strings.Join([]string{string(stdout), string(stderr)}, "\n"))
	if output == "" {
		return ""
	}
	lines := strings.Split(output, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func extraCreditTestPaths(studentDir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(studentDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".venv", "__pycache__", ".ipynb_checkpoints":
				if path != studentDir {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), "extra_credit.py") {
			return nil
		}
		relative, err := filepath.Rel(studentDir, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

// NewClearedOutputChecker ensures submitted notebooks have no saved output or execution count.
func NewClearedOutputChecker() Checker {
	const name = "cleared notebook output"
	return Checker{
		Name: name,
		Check: func(environment Environment) Result {
			paths, err := notebookPaths(environment.StudentDir)
			if err != nil {
				return failedResult(name, fmt.Sprintf("find student notebooks: %v", err))
			}
			for _, path := range paths {
				value, err := readNotebook(path)
				if err != nil {
					return failedResult(name, fmt.Sprintf("%s: %v", relativePath(environment.StudentDir, path), err))
				}
				for index, cell := range value.Cells {
					if len(cell.Outputs) != 0 || cell.ExecutionCount != nil {
						return failedResult(name, fmt.Sprintf("%s cell %d has saved output or an execution count", relativePath(environment.StudentDir, path), index+1))
					}
				}
			}
			return Result{Checker: name, Status: Passed}
		},
	}
}

// NewUnchangedCellsChecker ensures every teacher cell remains in order in the student notebook.
func NewUnchangedCellsChecker() Checker {
	const name = "unchanged notebook cells"
	return Checker{
		Name: name,
		Check: func(environment Environment) Result {
			teacherPaths, err := notebookPathMap(environment.TeacherDir)
			if err != nil {
				return failedResult(name, fmt.Sprintf("find teacher notebooks: %v", err))
			}
			studentPaths, err := notebookPathMap(environment.StudentDir)
			if err != nil {
				return failedResult(name, fmt.Sprintf("find student notebooks: %v", err))
			}

			keys := make([]string, 0, len(teacherPaths))
			for path := range teacherPaths {
				keys = append(keys, path)
			}
			sort.Strings(keys)

			for _, relative := range keys {
				teacherPath := teacherPaths[relative]
				studentPath, studentFound := studentPaths[relative]
				if !studentFound {
					return failedResult(name, fmt.Sprintf("student notebook %q is missing", relative))
				}

				teacher, err := readNotebook(teacherPath)
				if err != nil {
					return failedResult(name, fmt.Sprintf("teacher notebook %q: %v", relative, err))
				}
				student, err := readNotebook(studentPath)
				if err != nil {
					return failedResult(name, fmt.Sprintf("student notebook %q: %v", relative, err))
				}
				if !cellsAreSubsequence(teacher.Cells, student.Cells) {
					return failedResult(name, fmt.Sprintf("student notebook %q modifies or reorders teacher cells", relative))
				}
			}
			return Result{Checker: name, Status: Passed}
		},
	}
}

func notebookPathMap(root string) (map[string]string, error) {
	paths, err := notebookPaths(root)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		result[filepath.ToSlash(relative)] = path
	}
	return result, nil
}

func notebookPaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".ipynb_checkpoints" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".ipynb") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func cellsAreSubsequence(teacher, student []notebookCell) bool {
	nextTeacher := 0
	for _, cell := range student {
		if nextTeacher < len(teacher) && sameCell(teacher[nextTeacher], cell) {
			nextTeacher++
		}
	}
	return nextTeacher == len(teacher)
}

func relativePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(relative)
}

func failedResult(name, detail string) Result {
	return Result{Checker: name, Status: Failed, Detail: detail}
}
