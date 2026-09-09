package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/archetype"
	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/grading"
)

type errorChecksArchetype struct{ executed *[]string }

func (errorChecksArchetype) Name() string { return "test-error-continuation" }
func (errorChecksArchetype) DefaultImmutableFiles(string, map[string]string) ([]string, error) {
	return nil, errors.New("discovery diagnostic")
}
func (a errorChecksArchetype) Checks(archetype.CheckInput) ([]grading.Check, error) {
	return []grading.Check{
		grading.NewCheck("broken", func(context.Context) (grading.Result, error) {
			*a.executed = append(*a.executed, "broken")
			return grading.Result{Passed: true, Reason: "partial result"}, errors.New("execution diagnostic")
		}),
		grading.NewCheck("remaining", func(context.Context) (grading.Result, error) {
			*a.executed = append(*a.executed, "remaining")
			return grading.Result{Passed: true}, nil
		}),
	}, errors.New("construction diagnostic")
}
func (a errorChecksArchetype) Tests(archetype.TestInput) ([]grading.Check, error) {
	return []grading.Check{grading.NewCheck("unit-tests", func(context.Context) (grading.Result, error) {
		*a.executed = append(*a.executed, "unit-tests")
		return grading.Result{Passed: true}, nil
	})}, nil
}

func TestCheckErrorsRetainResultsAndExecuteRemainingChecks(t *testing.T) {
	var executed []string
	archetype.Register(errorChecksArchetype{executed: &executed})
	report, err := runAssignmentPipeline(t.Context(), assignmentPipelineInput{
		AssignmentID: "lab", StudentAssignmentDir: t.TempDir(), TeacherAssignmentDir: t.TempDir(), IsCheck: true,
		Spec: config.AssignmentSpec{Archetype: "test-error-continuation"},
	})
	if err != nil || report.Passed {
		t.Fatalf("report=%+v error=%v", report, err)
	}
	if strings.Join(executed, ",") != "broken,remaining,unit-tests" {
		t.Fatalf("executed: %v", executed)
	}
	text, _ := (grading.HumanRenderer{}).Render(report)
	for _, expected := range []string{"discovery diagnostic", "construction diagnostic", "partial result", "execution diagnostic", "[PASS] remaining", "[PASS] unit-tests"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("lost %q: %s", expected, text)
		}
	}
}

func TestImmutableFailureStillRunsSuite(t *testing.T) {
	teacher, student := t.TempDir(), t.TempDir()
	script := "#!/bin/sh\necho PASS first\necho FAIL specific_case\n"
	writeTestFile(t, filepath.Join(teacher, "grade.sh"), script)
	writeTestFile(t, filepath.Join(student, "grade.sh"), script+"echo executed > marker\n")
	if err := os.Chmod(filepath.Join(student, "grade.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := runAssignmentPipeline(t.Context(), assignmentPipelineInput{
		AssignmentID: "lab", StudentAssignmentDir: student, TeacherAssignmentDir: teacher, IsCheck: true,
		Spec: config.AssignmentSpec{Archetype: "generic-shell-script", Checks: []string{"file-structure"}, ArchetypeOptions: map[string]string{"command": "./grade.sh"}},
	})
	if err != nil || report.Passed || len(report.Results) != 3 {
		t.Fatalf("report=%+v error=%v", report, err)
	}
	if _, err := os.Stat(filepath.Join(student, "marker")); err != nil {
		t.Fatalf("suite did not execute: %v", err)
	}
	if report.Results[1].Passed || report.Results[2].Passed || !strings.Contains(report.Results[2].Reason, "specific_case") {
		t.Fatalf("report=%+v", report)
	}
}
