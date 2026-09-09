package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
)

func TestTeacherPinBothCommands(t *testing.T) {
	remote, teacher := newGitRemote(t, "teacher")
	_, student := newGitRemote(t, "student")
	writeGradeAssignmentSpec(t, teacher, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	runGitCommand(t, teacher, "add", ".")
	runGitCommand(t, teacher, "commit", "-m", "original")
	pin := strings.TrimSpace(runGitCommand(t, teacher, "rev-parse", "HEAD"))
	runGitCommand(t, teacher, "push", "-u", "origin", "main")
	writeTestFile(t, filepath.Join(student, config.StudentConfigFilename), "teacher-repository: "+remote+"\nbranch: ignored\nteacher-path: "+teacher+"\n")
	writeGradeStudentCommit(t, student, "expected\n", time.Date(2026, 1, 9, 10, 0, 0, 0, time.UTC), "answer")
	runGitCommand(t, student, "push", "-u", "origin", "main")
	// Change both mapping and baseline on main; an old pin must use neither.
	writeTestFile(t, filepath.Join(teacher, config.TeacherConfigFilename), "assignments:\n  newer: lab\n")
	writeTestFile(t, filepath.Join(teacher, "lab", "grade.sh"), "#!/bin/sh\necho FAIL smoke\n")
	runGitCommand(t, teacher, "add", ".")
	runGitCommand(t, teacher, "commit", "-m", "new teacher")
	runGitCommand(t, teacher, "push")
	writeTestFile(t, filepath.Join(teacher, "notes.txt"), "dirty notes")
	for _, grade := range []bool{false, true} {
		for _, pinned := range []bool{false, true} {
			var output strings.Builder
			selected := ""
			if pinned {
				selected = pin
			}
			var err error
			if grade {
				command := NewGrade()
				command.stdout = &output
				err = command.Run(context.Background(), "lab", GradeOptions{RepositoryRoot: student, TeacherCommit: selected})
			} else {
				command := NewCheck()
				command.stdout = &output
				err = command.Run(context.Background(), "lab", CheckOptions{RepositoryRoot: student, TeacherCommit: selected})
			}
			if pinned && (err != nil || !strings.Contains(output.String(), "Status: PASS")) {
				t.Fatalf("grade=%v pin: %v\n%s", grade, err, &output)
			}
			if !pinned && (err == nil || !strings.Contains(err.Error(), "not configured")) {
				t.Fatalf("grade=%v default did not use main: %v", grade, err)
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(teacher, "notes.txt")); err != nil || string(data) != "dirty notes" {
		t.Fatalf("shared checkout changed: %q, %v", data, err)
	}
	if _, err := openTeacherSnapshot(context.Background(), config.StudentConfig{TeacherRepository: remote}, "--help"); err == nil {
		t.Fatal("accepted option as commit")
	}
}

func TestPipelineTimeoutAndImmutableContinuation(t *testing.T) {
	if timeout, err := checkTimeout(config.AssignmentSpec{}, nil); err != nil || timeout != 120*time.Second {
		t.Fatalf("default timeout = %v, %v", timeout, err)
	}
	teacher, student := t.TempDir(), t.TempDir()
	script := "#!/bin/sh\nsleep 2\necho PASS smoke\n"
	for _, dir := range []string{teacher, student} {
		writeTestFile(t, filepath.Join(dir, "grade.sh"), script)
		if err := os.Chmod(filepath.Join(dir, "grade.sh"), 0o755); err != nil {
			t.Fatal(err)
		}

	}
	short, long := 40*time.Millisecond, 4*time.Second
	spec := config.AssignmentSpec{
		Archetype: "generic-shell-script", Timeout: &short,
		Checks: []string{"file-structure"}, ImmutableFiles: []string{"extra.txt"},
		ArchetypeOptions: map[string]string{"command": "./grade.sh"},
	}
	for _, dir := range []string{teacher, student} {
		writeTestFile(t, filepath.Join(dir, "extra.txt"), "same")
	}
	input := assignmentPipelineInput{AssignmentID: "lab", StudentAssignmentDir: student, TeacherAssignmentDir: teacher, Spec: spec, IsCheck: true}
	start := time.Now()
	report, err := runAssignmentPipeline(context.Background(), input)
	if err != nil || report.Passed || !strings.Contains(report.Results[2].Reason, "deadline exceeded") || time.Since(start) > time.Second {
		t.Fatalf("timeout report=%+v err=%v elapsed=%v", report, err, time.Since(start))
	}
	if len(report.Results) != 3 {
		t.Fatalf("check results lost: %+v", report)
	}
	input.Timeout = &long
	report, err = runAssignmentPipeline(context.Background(), input)
	if err != nil || !report.Passed {
		t.Fatalf("CLI override: %+v, %v", report, err)
	}
	input.Timeout = nil
	input.Spec.Timeout = nil
	report, err = runAssignmentPipeline(context.Background(), input)
	if err != nil || !report.Passed {
		t.Fatalf("default: %+v, %v", report, err)
	}
	for _, changed := range []string{script + "\n", strings.ReplaceAll(script, "\n", "\r\n"), strings.ReplaceAll(script, "sleep 2", "sleep  2")} {
		writeTestFile(t, filepath.Join(student, "grade.sh"), changed)
		report, err = runAssignmentPipeline(context.Background(), input)
		if err != nil || report.Passed || strings.Contains(report.Results[2].Reason, "not run") {
			t.Fatalf("checks did not continue: %+v, %v", report, err)
		}
	}
	zero := time.Duration(0)
	input.Timeout = &zero
	if _, err := runAssignmentPipeline(context.Background(), input); err == nil {
		t.Fatal("accepted zero timeout")
	}
}
