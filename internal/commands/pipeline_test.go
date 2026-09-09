package commands

import (
	"context"
	"testing"
	"time"

	"github.com/ludanortmun/sync-assign/internal/archetype"
	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/grading"
)

func TestRunAssignmentPipelineUsesExplicitFilesWithoutDefaults(t *testing.T) {
	const archetypeName = "test-pipeline-explicit-files"

	archetype.Register(explicitFilesArchetype{name: archetypeName})

	teacherDir := t.TempDir()
	studentDir := t.TempDir()

	writeTestFile(t, teacherDir+"/keep.txt", "teacher text\n")
	writeTestFile(t, teacherDir+"/required.txt", "required\n")
	writeTestFile(t, teacherDir+"/teacher-only.txt", "would fail if default required files were used\n")

	writeTestFile(t, studentDir+"/keep.txt", "teacher text\n")
	writeTestFile(t, studentDir+"/required.txt", "required\n")

	report, err := runAssignmentPipeline(context.Background(), assignmentPipelineInput{
		AssignmentID:         "lab",
		StudentAssignmentDir: studentDir,
		TeacherAssignmentDir: teacherDir,
		Spec: config.AssignmentSpec{
			Archetype:      archetypeName,
			DueDate:        time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC),
			ImmutableFiles: []string{"keep.txt"},
			RequiredFiles:  []string{"required.txt"},
		},
		Authoritative: true,
		IsCheck:       false,
		CommitTime:    time.Date(2026, time.January, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("runAssignmentPipeline() error = %v", err)
	}

	if !report.Passed {
		t.Fatalf("report.Valid = false, want true: %#v", report)
	}
	if len(report.Results) != 4 {
		t.Fatalf("results = %#v, want 4", report.Results)
	}
	if test := report.Results[3]; !test.Passed || test.Name != "unit-tests" {
		t.Fatalf("suite = %#v", test)
	}
}

type explicitFilesArchetype struct {
	name string
}

func (a explicitFilesArchetype) Name() string {
	return a.name
}

func (explicitFilesArchetype) Checks(archetype.CheckInput) ([]grading.Check, error) {
	return nil, nil
}

func (explicitFilesArchetype) Tests(input archetype.TestInput) ([]grading.Check, error) {
	return []grading.Check{grading.NewCheck("unit-tests", func(context.Context) (grading.Result, error) {
		return grading.Result{Passed: true}, nil
	})}, nil
}

func (explicitFilesArchetype) DefaultImmutableFiles(string, map[string]string) ([]string, error) {
	return nil, nil
}

type unreachableError string

func (e unreachableError) Error() string {
	return string(e)
}

func assertUnreachable(name string) error {
	return unreachableError(name + " should not be called")
}
