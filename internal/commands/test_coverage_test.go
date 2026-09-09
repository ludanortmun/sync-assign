package commands

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
)

func TestPytestCoverageFailsWithoutBlockingSuite(t *testing.T) {
	for _, file := range []string{"test_main.py", "helper_test.py", "conftest.py", "pytest.ini", "tests/fixture.txt", "test_added.py"} {
		t.Run(file, func(t *testing.T) {
			teacher, student := t.TempDir(), t.TempDir()
			for _, dir := range []string{teacher, student} {
				for _, path := range []string{"test_main.py", "helper_test.py", "conftest.py", "pytest.ini", "tests/fixture.txt"} {
					writeTestFile(t, filepath.Join(dir, path), "")
				}
			}
			writeTestFile(t, filepath.Join(student, file), "\n")
			report, err := runAssignmentPipeline(context.Background(), assignmentPipelineInput{
				AssignmentID: "lab", StudentAssignmentDir: student, TeacherAssignmentDir: teacher, IsCheck: true,
				Spec: config.AssignmentSpec{
					Archetype: "python-pytest", Checks: []string{"file-structure"},
					ArchetypeOptions: map[string]string{"test-path": "tests"},
				},
			})
			if err != nil || report.Passed || len(report.Results) != 4 || !strings.Contains(report.Results[3].Reason, "requirements.txt not found") {
				t.Fatalf("test coverage failed: %+v, %v", report, err)
			}
		})
	}
}
