package grader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoFileModifiedCheckerPassesForUnchangedProtectedFiles(t *testing.T) {
	teacher, student := assignmentDirs(t)
	writeAssignmentFile(t, teacher, "tests/test_unit.py", "assert True\n")
	writeAssignmentFile(t, teacher, "src/test_helpers.py", "HELPER = True\n")
	writeAssignmentFile(t, teacher, "src/solution.py", "teacher\n")
	writeAssignmentFile(t, student, "tests/test_unit.py", "assert True\n")
	writeAssignmentFile(t, student, "src/test_helpers.py", "HELPER = True\n")

	result := NewNoFileModifiedChecker("tests/**", "test_*.py").Check(Environment{
		TeacherDir: teacher,
		StudentDir: student,
	})

	if result.Status != Passed {
		t.Fatalf("status = %q, detail = %q; want %q", result.Status, result.Detail, Passed)
	}
}

func TestNoFileModifiedCheckerReportsModifiedAndMissingFilesDeterministically(t *testing.T) {
	teacher, student := assignmentDirs(t)
	writeAssignmentFile(t, teacher, "tests/z_test.py", "teacher z\n")
	writeAssignmentFile(t, teacher, "tests/a_test.py", "teacher a\n")
	writeAssignmentFile(t, teacher, "pkg/test_prefix.py", "teacher prefix\n")
	writeAssignmentFile(t, student, "tests/z_test.py", "student z\n")
	writeAssignmentFile(t, student, "pkg/test_prefix.py", "student prefix\n")

	result := NewNoFileModifiedChecker().Check(Environment{
		TeacherDir: teacher,
		StudentDir: student,
	})

	want := strings.Join([]string{
		"pkg/test_prefix.py: modified",
		"tests/a_test.py: missing",
		"tests/z_test.py: modified",
	}, "\n")
	if result.Status != Failed || result.Detail != want {
		t.Fatalf("result = %#v, want failed with detail %q", result, want)
	}
}

func TestNoFileModifiedCheckerProtectsNestedTestsDirectories(t *testing.T) {
	teacher, student := assignmentDirs(t)
	writeAssignmentFile(t, teacher, "course/week/tests/fixtures/data.json", "teacher\n")
	writeAssignmentFile(t, student, "course/week/tests/fixtures/data.json", "student\n")

	result := NewNoFileModifiedChecker().Check(Environment{TeacherDir: teacher, StudentDir: student})

	if result.Status != Failed || result.Detail != "course/week/tests/fixtures/data.json: modified" {
		t.Fatalf("result = %#v, want nested test file modification", result)
	}
}

func TestNoFileModifiedCheckerProtectsTestPrefixFilesOutsideTests(t *testing.T) {
	teacher, student := assignmentDirs(t)
	writeAssignmentFile(t, teacher, "package/test_public_api.py", "teacher\n")
	writeAssignmentFile(t, student, "package/test_public_api.py", "student\n")

	result := NewNoFileModifiedChecker().Check(Environment{TeacherDir: teacher, StudentDir: student})

	if result.Status != Failed || result.Detail != "package/test_public_api.py: modified" {
		t.Fatalf("result = %#v, want prefixed file modification", result)
	}
}

func TestNoFileModifiedCheckerIgnoresExtraStudentFilesAndGeneratedDirectories(t *testing.T) {
	teacher, student := assignmentDirs(t)
	writeAssignmentFile(t, teacher, "tests/test_unit.py", "same\n")
	writeAssignmentFile(t, student, "tests/test_unit.py", "same\n")
	writeAssignmentFile(t, student, "tests/extra.py", "extra\n")
	writeAssignmentFile(t, student, "test_extra.py", "extra\n")
	writeAssignmentFile(t, teacher, "tests/__pycache__/generated.pyc", "teacher\n")
	writeAssignmentFile(t, teacher, ".venv/tests/vendor.py", "teacher\n")

	result := NewNoFileModifiedChecker().Check(Environment{TeacherDir: teacher, StudentDir: student})

	if result.Status != Passed {
		t.Fatalf("result = %#v, want extra and generated files ignored", result)
	}
}

func TestNoFileModifiedCheckerReportsWrongStudentFileType(t *testing.T) {
	teacher, student := assignmentDirs(t)
	writeAssignmentFile(t, teacher, "tests/fixture.json", "teacher\n")
	if err := os.MkdirAll(filepath.Join(student, "tests", "fixture.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := NewNoFileModifiedChecker().Check(Environment{TeacherDir: teacher, StudentDir: student})

	if result.Status != Failed || result.Detail != "tests/fixture.json: wrong type" {
		t.Fatalf("result = %#v, want wrong type failure", result)
	}
}

func assignmentDirs(t *testing.T) (string, string) {
	t.Helper()
	return t.TempDir(), t.TempDir()
}

func writeAssignmentFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
