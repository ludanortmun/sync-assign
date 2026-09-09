package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
	"go.yaml.in/yaml/v4"
)

func TestCheckRun(t *testing.T) {
	tests := map[string]struct {
		mutateStudent      func(t *testing.T, studentAssignmentDir string)
		mutateSpec         func(spec *config.AssignmentSpec)
		wantErr            string
		wantOutput         []string
		wantOutputExcludes []string
	}{
		"happy path": {
			wantOutput: []string{
				"PREVIEW - not authoritative",
				"Assignment: lab",
				"[PASS] file-structure",
				"[PASS] immutable-files-unmodified",
				"[PASS] unit-tests",
				"Status: PASS",
			},
			wantOutputExcludes: []string{
				"commit-before-due-date",
			},
		},
		"missing required file fails checks": {
			mutateStudent: func(t *testing.T, studentAssignmentDir string) {
				t.Helper()
				if err := os.Remove(filepath.Join(studentAssignmentDir, "answer.txt")); err != nil {
					t.Fatalf("Remove(answer.txt) error = %v", err)
				}
			},
			wantErr: "assignment \"lab\" failed: one or more checks failed",
			wantOutput: []string{
				"missing required paths: answer.txt",
				"Status: FAIL",
			},
		},
		"failing tests return error after printing report": {
			mutateStudent: func(t *testing.T, studentAssignmentDir string) {
				t.Helper()
				writeTestFile(t, filepath.Join(studentAssignmentDir, "answer.txt"), "wrong\n")
			},
			wantErr: "assignment \"lab\" failed: one or more checks failed",
			wantOutput: []string{
				"[FAIL] unit-tests: smoke",
				"Status: FAIL",
			},
		},
		"unsupported archetype": {
			mutateSpec: func(spec *config.AssignmentSpec) {
				spec.Archetype = "unknown-archetype"
			},
			wantErr:    "one or more checks failed",
			wantOutput: []string{`unsupported archetype "unknown-archetype"`, "[PASS] file-structure", "[FAIL] unit-tests"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			studentRoot, mirrorRoot := writeCheckFixture(t)
			if test.mutateStudent != nil {
				test.mutateStudent(t, filepath.Join(studentRoot, "lab"))
			}
			if test.mutateSpec != nil {
				specPath := filepath.Join(mirrorRoot, "lab", config.AssignmentSpecFilename)
				spec, err := config.LoadAssignmentSpecFile(specPath)
				if err != nil {
					t.Fatalf("LoadAssignmentSpecFile() error = %v", err)
				}
				test.mutateSpec(&spec)
				if err := writeAssignmentSpecTestFile(specPath, spec); err != nil {
					t.Fatalf("rewrite assignment spec: %v", err)
				}
			}

			var output strings.Builder
			command := newCheckWithDependencies(
				acceptingGitRootValidator{},
				func(context.Context, config.StudentConfig) (teacherMirror, error) {
					return &fakeTeacherMirror{path: mirrorRoot, close: func() error { return nil }}, nil
				},
				&output,
			)

			err := command.Run(context.Background(), "lab", CheckOptions{RepositoryRoot: studentRoot})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Run() error = %v, want containing %q", err, test.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			for _, want := range test.wantOutput {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("output %q does not contain %q", output.String(), want)
				}
			}
			for _, unwanted := range test.wantOutputExcludes {
				if strings.Contains(output.String(), unwanted) {
					t.Fatalf("output %q unexpectedly contains %q", output.String(), unwanted)
				}
			}
		})
	}
}

func TestCheckRunAssignmentNotConfigured(t *testing.T) {
	studentRoot, mirrorRoot := writeCheckFixture(t)
	command := newCheckWithDependencies(
		acceptingGitRootValidator{},
		func(context.Context, config.StudentConfig) (teacherMirror, error) {
			return &fakeTeacherMirror{path: mirrorRoot, close: func() error { return nil }}, nil
		},
		new(strings.Builder),
	)

	err := command.Run(context.Background(), "missing", CheckOptions{RepositoryRoot: studentRoot})
	if err == nil || !strings.Contains(err.Error(), `assignment "missing" is not configured`) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestCheckRunStudentAssignmentDirectoryMissing(t *testing.T) {
	studentRoot, mirrorRoot := writeCheckFixture(t)
	if err := os.RemoveAll(filepath.Join(studentRoot, "lab")); err != nil {
		t.Fatalf("RemoveAll(lab) error = %v", err)
	}

	var output strings.Builder
	command := newCheckWithDependencies(
		acceptingGitRootValidator{},
		func(context.Context, config.StudentConfig) (teacherMirror, error) {
			return &fakeTeacherMirror{path: mirrorRoot, close: func() error { return nil }}, nil
		},
		&output,
	)

	err := command.Run(context.Background(), "lab", CheckOptions{RepositoryRoot: studentRoot})
	if err == nil || !strings.Contains(err.Error(), "run `sync-assign lab` first") {
		t.Fatalf("Run() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty", output.String())
	}
}

func TestCheckRunClosesMirrorWhenTeacherConfigFails(t *testing.T) {
	studentRoot, mirrorRoot := writeCheckFixture(t)
	if err := os.Remove(filepath.Join(mirrorRoot, config.TeacherConfigFilename)); err != nil {
		t.Fatalf("Remove(teacher config) error = %v", err)
	}
	closed := false
	command := newCheckWithDependencies(
		acceptingGitRootValidator{},
		func(context.Context, config.StudentConfig) (teacherMirror, error) {
			return &fakeTeacherMirror{path: mirrorRoot, close: func() error {
				closed = true
				return nil
			}}, nil
		},
		new(strings.Builder),
	)

	if err := command.Run(context.Background(), "lab", CheckOptions{RepositoryRoot: studentRoot}); err == nil {
		t.Fatal("Run() succeeded without teacher config")
	}
	if !closed {
		t.Fatal("mirror was not closed after teacher config failure")
	}
}

func TestDefaultRequiredFilesSkipsGitAndAssignmentSpec(t *testing.T) {
	teacherAssignmentDir := t.TempDir()
	writeTestFile(t, filepath.Join(teacherAssignmentDir, config.AssignmentSpecFilename), "ignored\n")
	writeTestFile(t, filepath.Join(teacherAssignmentDir, "answer.txt"), "answer\n")
	writeTestFile(t, filepath.Join(teacherAssignmentDir, "nested", "notes.md"), "notes\n")
	writeTestFile(t, filepath.Join(teacherAssignmentDir, ".git", "config"), "secret\n")
	writeTestFile(t, filepath.Join(teacherAssignmentDir, "nested", ".git", "HEAD"), "secret\n")

	got, err := defaultRequiredFiles(teacherAssignmentDir)
	if err != nil {
		t.Fatalf("defaultRequiredFiles() error = %v", err)
	}
	want := []string{"answer.txt", "nested/notes.md"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("defaultRequiredFiles() = %#v, want %#v", got, want)
	}
}

func writeCheckFixture(t *testing.T) (studentRoot, mirrorRoot string) {
	t.Helper()
	studentRoot = t.TempDir()
	mirrorRoot = t.TempDir()
	writeTestFile(t, filepath.Join(studentRoot, config.StudentConfigFilename), "teacher-repository: teacher\n")
	writeTestFile(t, filepath.Join(mirrorRoot, config.TeacherConfigFilename), "assignments:\n  lab: lab\n")

	spec := config.AssignmentSpec{
		Archetype:        "generic-shell-script",
		DueDate:          time.Date(2026, 10, 15, 23, 59, 0, 0, time.UTC),
		ArchetypeOptions: map[string]string{"command": "./grade.sh"},
	}
	if err := writeAssignmentSpecTestFile(filepath.Join(mirrorRoot, "lab", config.AssignmentSpecFilename), spec); err != nil {
		t.Fatalf("write assignment spec: %v", err)
	}
	writeTestFile(t, filepath.Join(mirrorRoot, "lab", "grade.sh"), "#!/bin/sh\nif grep -qx 'expected' answer.txt; then\n  echo 'PASS smoke'\nelse\n  echo 'FAIL smoke'\nfi\n")
	writeTestFile(t, filepath.Join(mirrorRoot, "lab", "answer.txt"), "expected\n")
	writeTestFile(t, filepath.Join(studentRoot, "lab", "grade.sh"), "#!/bin/sh\nif grep -qx 'expected' answer.txt; then\n  echo 'PASS smoke'\nelse\n  echo 'FAIL smoke'\nfi\n")
	writeTestFile(t, filepath.Join(studentRoot, "lab", "answer.txt"), "expected\n")
	mustChmodTestFile(t, filepath.Join(mirrorRoot, "lab", "grade.sh"), 0o755)
	mustChmodTestFile(t, filepath.Join(studentRoot, "lab", "grade.sh"), 0o755)
	return studentRoot, mirrorRoot
}

func writeAssignmentSpecTestFile(filename string, spec config.AssignmentSpec) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0o644)
}

func mustChmodTestFile(t *testing.T, filename string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(filename, mode); err != nil {
		t.Fatalf("Chmod(%q) error = %v", filename, err)
	}
}
