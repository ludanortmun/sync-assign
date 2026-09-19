package commands

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/grader"
)

func TestCheckUsesCurrentWorkingDirectoryIncludingUncommittedChanges(t *testing.T) {
	teacher := newGradeTeacher(t, "python")
	student := newGradeStudent(t, teacher)
	commitGradeFileAt(t, student, "lab/answer.txt", "committed\n", "2026-01-10T10:00:00Z")
	writeWorkflowFile(t, filepath.Join(student, "lab", "answer.txt"), "uncommitted\n")
	writeWorkflowFile(t, filepath.Join(student, "lab", "new.txt"), "untracked\n")

	var output strings.Builder
	var checkedAnswer, checkedNewFile string
	command := newCheckWithDependencies(
		&output,
		io.Discard,
		execGitRootValidator{},
		gradeMirrorOpener(teacher),
		func(*config.Archetype) ([]grader.Checker, error) {
			return []grader.Checker{{
				Name: "injected checker",
				Check: func(environment grader.Environment) grader.Result {
					if environment.StudentDir == filepath.Join(student, "lab") {
						t.Fatal("checker received the live assignment directory")
					}
					checkedAnswer = readWorkflowFile(t, filepath.Join(environment.StudentDir, "answer.txt"))
					checkedNewFile = readWorkflowFile(t, filepath.Join(environment.StudentDir, "new.txt"))
					writeWorkflowFile(t, filepath.Join(environment.StudentDir, "answer.txt"), "modified snapshot\n")
					return grader.Result{Status: grader.Passed}
				},
			}}, nil
		},
	)

	if err := command.Run(context.Background(), "lab", CheckOptions{
		RepositoryRoot: student,
	}); err != nil {
		t.Fatalf("check: %v", err)
	}

	if checkedAnswer != "uncommitted\n" || checkedNewFile != "untracked\n" {
		t.Fatalf("checked files = (%q, %q), want current working tree", checkedAnswer, checkedNewFile)
	}
	if got := readWorkflowFile(t, filepath.Join(student, "lab", "answer.txt")); got != "uncommitted\n" {
		t.Fatalf("live assignment was modified to %q", got)
	}
	for _, want := range []string{
		"check: lab\n",
		"source: working directory\n",
		"[RUNNING] injected checker",
		"\x1b[32m[SUCCESS] injected checker\x1b[0m",
		"result: \x1b[32mpassed\x1b[0m",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("report missing %q:\n%s", want, output.String())
		}
	}

	status := runGitCommand(t, student, "status", "--short")
	if !strings.Contains(status, " M lab/answer.txt") || !strings.Contains(status, "?? lab/new.txt") {
		t.Fatalf("check changed working tree status:\n%s", status)
	}
}

func TestCheckReturnsErrorForFailedChecker(t *testing.T) {
	teacher := newGradeTeacher(t, "python")
	student := newGradeStudent(t, teacher)
	var output, errorOutput strings.Builder
	command := newCheckWithDependencies(
		&output,
		&errorOutput,
		execGitRootValidator{},
		gradeMirrorOpener(teacher),
		func(*config.Archetype) ([]grader.Checker, error) {
			return []grader.Checker{{
				Name: "injected checker",
				Check: func(grader.Environment) grader.Result {
					return grader.Result{Status: grader.Failed, Detail: "failed deliberately"}
				},
			}}, nil
		},
	)

	err := command.Run(context.Background(), "lab", CheckOptions{RepositoryRoot: student})
	if err == nil || !strings.Contains(err.Error(), "failed grading checks") {
		t.Fatalf("check error = %v, want failed checks error", err)
	}
	if !strings.Contains(errorOutput.String(), "\x1b[31m[FAILED] injected checker\x1b[0m") {
		t.Fatalf("stderr missing failed result:\n%s", errorOutput.String())
	}
}
