package grader

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPythonUnitTestCheckerUVArguments(t *testing.T) {
	tests := []struct {
		name             string
		withRequirements bool
		testPath         string
		wantArgs         []string
	}{
		{
			name:     "root test without requirements",
			testPath: "test_root.py",
			wantArgs: []string{
				"run", "--with", "pytest", "--", "pytest", "-q", "--tb=no",
				"--ignore-glob=*extra_credit.py",
			},
		},
		{
			name:             "nested test with requirements",
			withRequirements: true,
			testPath:         filepath.Join("nested", "test_nested.py"),
			wantArgs: []string{
				"run", "--with-requirements", "requirements.txt",
				"--with", "pytest", "--", "pytest", "-q", "--tb=no",
				"--ignore-glob=*extra_credit.py",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			studentDir := t.TempDir()
			writeFile(t, filepath.Join(studentDir, test.testPath), "")
			if test.withRequirements {
				writeFile(t, filepath.Join(studentDir, "requirements.txt"), "example==1\n")
			}

			var gotDir, gotName string
			var gotArgs []string
			executor := func(dir, name string, args ...string) ([]byte, []byte, error) {
				gotDir, gotName = dir, name
				gotArgs = append([]string(nil), args...)
				return nil, nil, nil
			}
			result := newPythonUnitTestChecker(executor).Check(Environment{StudentDir: studentDir})

			if result.Status != Passed {
				t.Fatalf("status = %q, want %q: %s", result.Status, Passed, result.Detail)
			}
			if gotDir != studentDir || gotName != "uv" || !reflect.DeepEqual(gotArgs, test.wantArgs) {
				t.Fatalf("command = (%q, %q, %#v), want (%q, uv, %#v)", gotDir, gotName, gotArgs, studentDir, test.wantArgs)
			}
		})
	}
}

func TestNotebookUnitTestCheckerUVArguments(t *testing.T) {
	studentDir := t.TempDir()
	writeFile(t, filepath.Join(studentDir, "requirements.txt"), "")
	writeNotebook(t, filepath.Join(studentDir, "root.ipynb"), nil)
	writeNotebook(t, filepath.Join(studentDir, "nested", "work.ipynb"), nil)

	var gotArgs []string
	executor := func(dir, name string, args ...string) ([]byte, []byte, error) {
		if dir != studentDir || name != "uv" {
			t.Fatalf("command target = (%q, %q), want (%q, uv)", dir, name, studentDir)
		}
		gotArgs = append([]string(nil), args...)
		return nil, nil, nil
	}
	result := newNotebookUnitTestChecker(executor).Check(Environment{StudentDir: studentDir})

	want := []string{
		"run", "--with-requirements", "requirements.txt",
		"--with", "pytest", "--with", "nbmake", "--",
		"pytest", "-q", "--tb=no", "--nbmake", "notebooks",
	}
	if result.Status != Passed || !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("result = %#v, args = %#v, want passed and %#v", result, gotArgs, want)
	}
}

func TestUnitTestCheckerReportsCommandFailures(t *testing.T) {
	executor := func(string, string, ...string) ([]byte, []byte, error) {
		return []byte("partial output"), []byte("test failed"), errors.New("exit status 1")
	}
	result := newPythonUnitTestChecker(executor).Check(Environment{StudentDir: t.TempDir()})
	if result.Status != Failed ||
		!strings.Contains(result.Detail, "test failed") ||
		!strings.Contains(result.Detail, "exit status 1") {
		t.Fatalf("failure result = %#v, want concise command output and error", result)
	}

	missingUV := func(string, string, ...string) ([]byte, []byte, error) {
		return nil, nil, &exec.Error{Name: "uv", Err: exec.ErrNotFound}
	}
	result = newPythonUnitTestChecker(missingUV).Check(Environment{StudentDir: t.TempDir()})
	if result.Status != Failed || !strings.Contains(result.Detail, "not found") {
		t.Fatalf("missing uv result = %#v, want clear failure", result)
	}
}

func TestUnitTestCheckerFormatsFailureOutput(t *testing.T) {
	executor := func(string, string, ...string) ([]byte, []byte, error) {
		return []byte(`============================= test session starts ==============================
tests/test_answer.py F
=========================== short test summary info ============================
FAILED tests/test_answer.py::test_answer - assert 1 == 2
============================== 1 failed in 0.02s ===============================`), []byte("uv diagnostic that should be hidden"), errors.New("exit status 1")
	}
	result := newNotebookUnitTestChecker(executor).Check(Environment{StudentDir: t.TempDir()})

	want := `uv run failed: exit status 1
=========================== short test summary info ============================
FAILED tests/test_answer.py::test_answer - assert 1 == 2
============================== 1 failed in 0.02s ===============================`
	if result.Status != Failed || result.Detail != want {
		t.Fatalf("result = %#v, want failure detail %q", result, want)
	}
}

func TestPythonExtraCreditTestCheckerRunsOnlyExtraCreditFiles(t *testing.T) {
	studentDir := t.TempDir()
	writeFile(t, filepath.Join(studentDir, "tests", "test_main.py"), "")
	writeFile(t, filepath.Join(studentDir, "tests", "test_bonus_extra_credit.py"), "")
	writeFile(t, filepath.Join(studentDir, "nested", "another_extra_credit.py"), "")
	writeFile(t, filepath.Join(studentDir, ".venv", "ignored_extra_credit.py"), "")

	var gotArgs []string
	executor := func(_, _ string, args ...string) ([]byte, []byte, error) {
		gotArgs = append([]string(nil), args...)
		return nil, nil, nil
	}
	result := newPythonExtraCreditTestChecker(executor).Check(Environment{StudentDir: studentDir})

	want := []string{
		"run", "--with", "pytest", "--", "pytest", "-q", "--tb=no",
		"nested/another_extra_credit.py", "tests/test_bonus_extra_credit.py",
	}
	if result.Status != Passed || !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("result = %#v, args = %#v, want passed and %#v", result, gotArgs, want)
	}
}

func TestPythonExtraCreditTestCheckerSkipsWhenNoFilesExist(t *testing.T) {
	called := false
	executor := func(_, _ string, _ ...string) ([]byte, []byte, error) {
		called = true
		return nil, nil, nil
	}
	result := newPythonExtraCreditTestChecker(executor).Check(Environment{StudentDir: t.TempDir()})

	if result.Status != Skipped || called {
		t.Fatalf("result = %#v, executor called = %t; want skipped without execution", result, called)
	}
}

func TestClearedOutputChecker(t *testing.T) {
	tests := []struct {
		name       string
		notebook   string
		wantStatus Status
	}{
		{
			name:       "cleared",
			notebook:   `{"cells":[{"cell_type":"code","source":["x = 1\n"],"outputs":[],"execution_count":null}]}`,
			wantStatus: Passed,
		},
		{
			name:       "saved output",
			notebook:   `{"cells":[{"cell_type":"code","source":"","outputs":[{"output_type":"stream"}],"execution_count":null}]}`,
			wantStatus: Failed,
		},
		{
			name:       "execution count",
			notebook:   `{"cells":[{"cell_type":"code","source":"","outputs":[],"execution_count":2}]}`,
			wantStatus: Failed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			studentDir := t.TempDir()
			writeFile(t, filepath.Join(studentDir, "notebooks", "work.ipynb"), test.notebook)
			result := NewClearedOutputChecker().Check(Environment{StudentDir: studentDir})
			if result.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q: %s", result.Status, test.wantStatus, result.Detail)
			}
		})
	}
}

func TestClearedOutputCheckerSkipsCheckpoints(t *testing.T) {
	studentDir := t.TempDir()
	writeFile(t, filepath.Join(studentDir, ".ipynb_checkpoints", "dirty.ipynb"),
		`{"cells":[{"cell_type":"code","source":"","outputs":[{}],"execution_count":1}]}`)

	result := NewClearedOutputChecker().Check(Environment{StudentDir: studentDir})
	if result.Status != Passed {
		t.Fatalf("status = %q, want %q: %s", result.Status, Passed, result.Detail)
	}
}

func TestUnchangedCellsAllowsInsertedCells(t *testing.T) {
	teacherCells := []testCell{{"markdown", "first"}, {"code", "second"}, {"markdown", "third"}}
	insertions := map[string][]testCell{
		"before":  {{"code", "new"}, teacherCells[0], teacherCells[1], teacherCells[2]},
		"between": {teacherCells[0], {"code", "new"}, teacherCells[1], teacherCells[2]},
		"after":   {teacherCells[0], teacherCells[1], teacherCells[2], {"code", "new"}},
	}
	for name, studentCells := range insertions {
		t.Run(name, func(t *testing.T) {
			environment := notebookEnvironment(t, teacherCells, studentCells)
			result := NewUnchangedCellsChecker().Check(environment)
			if result.Status != Passed {
				t.Fatalf("status = %q, want %q: %s", result.Status, Passed, result.Detail)
			}
		})
	}
}

func TestUnchangedCellsRejectsModifiedAndReorderedCells(t *testing.T) {
	teacherCells := []testCell{{"markdown", "first"}, {"code", "second"}}
	tests := map[string][]testCell{
		"modified":  {{"markdown", "changed"}, teacherCells[1]},
		"reordered": {teacherCells[1], teacherCells[0]},
	}
	for name, studentCells := range tests {
		t.Run(name, func(t *testing.T) {
			result := NewUnchangedCellsChecker().Check(notebookEnvironment(t, teacherCells, studentCells))
			if result.Status != Failed || !strings.Contains(result.Detail, "modifies or reorders") {
				t.Fatalf("result = %#v, want modified/reordered failure", result)
			}
		})
	}
}

func TestUnchangedCellsReportsMissingTeacherNotebookInStudent(t *testing.T) {
	teacherDir := t.TempDir()
	studentDir := t.TempDir()
	writeNotebook(t, filepath.Join(teacherDir, "work.ipynb"), []testCell{{"code", "x"}})

	result := NewUnchangedCellsChecker().Check(Environment{TeacherDir: teacherDir, StudentDir: studentDir})
	if result.Status != Failed || !strings.Contains(result.Detail, "student notebook") || !strings.Contains(result.Detail, "missing") {
		t.Fatalf("missing student result = %#v, want clear failure", result)
	}
}

func TestUnchangedCellsAllowsStudentAddedNotebook(t *testing.T) {
	teacherDir := t.TempDir()
	studentDir := t.TempDir()
	writeNotebook(t, filepath.Join(teacherDir, "work.ipynb"), []testCell{{"code", "x"}})
	writeNotebook(t, filepath.Join(studentDir, "work.ipynb"), []testCell{{"code", "x"}})
	writeNotebook(t, filepath.Join(studentDir, "extra", "student.ipynb"), []testCell{{"code", "new"}})

	result := NewUnchangedCellsChecker().Check(Environment{TeacherDir: teacherDir, StudentDir: studentDir})
	if result.Status != Passed {
		t.Fatalf("student-added notebook result = %#v, want passed", result)
	}
}

type testCell struct {
	cellType string
	source   string
}

func notebookEnvironment(t *testing.T, teacherCells, studentCells []testCell) Environment {
	t.Helper()
	teacherDir := t.TempDir()
	studentDir := t.TempDir()
	writeNotebook(t, filepath.Join(teacherDir, "notebooks", "work.ipynb"), teacherCells)
	writeNotebook(t, filepath.Join(studentDir, "notebooks", "work.ipynb"), studentCells)
	return Environment{TeacherDir: teacherDir, StudentDir: studentDir}
}

func writeNotebook(t *testing.T, path string, cells []testCell) {
	t.Helper()
	var builder strings.Builder
	builder.WriteString(`{"cells":[`)
	for index, cell := range cells {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`{"cell_type":`)
		builder.WriteString(quoteJSON(cell.cellType))
		builder.WriteString(`,"source":`)
		builder.WriteString(quoteJSON(cell.source))
		builder.WriteString(`,"outputs":[],"execution_count":null}`)
	}
	builder.WriteString(`]}`)
	writeFile(t, path, builder.String())
}

func quoteJSON(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return `"` + value + `"`
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
