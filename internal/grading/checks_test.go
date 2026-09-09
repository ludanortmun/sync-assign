package grading

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
)

func TestEffectiveChecks(t *testing.T) {
	tests := map[string]struct {
		spec config.AssignmentSpec
		want []string
	}{
		"default checks for nil override": {
			spec: config.AssignmentSpec{},
			want: DefaultChecks,
		},
		"default checks for empty override": {
			spec: config.AssignmentSpec{Checks: []string{}},
			want: DefaultChecks,
		},
		"explicit override": {
			spec: config.AssignmentSpec{Checks: []string{CheckFileStructure}},
			want: []string{CheckFileStructure},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := EffectiveChecks(test.spec)
			if !slicesEqual(got, test.want) {
				t.Fatalf("EffectiveChecks() = %#v, want %#v", got, test.want)
			}
			if len(got) > 0 && len(test.want) > 0 && &got[0] == &test.want[0] {
				t.Fatalf("EffectiveChecks() returned original backing array")
			}
		})
	}
}

func TestBuildChecks(t *testing.T) {
	tests := map[string]struct {
		setup       func(t *testing.T) CheckContext
		checks      []string
		wantResults []Result
		wantErr     string
	}{
		"dispatches supported checks": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				assignmentDir := t.TempDir()
				teacherDir := t.TempDir()
				writeFile(t, assignmentDir, "README.md", "hello\n")
				writeFile(t, teacherDir, "README.md", "hello\n")
				dueDate := time.Date(2026, 10, 15, 23, 59, 0, 0, time.UTC)
				return CheckContext{
					AssignmentDir:        assignmentDir,
					TeacherAssignmentDir: teacherDir,
					ImmutableFiles:       []string{"README.md"},
					RequiredFiles:        []string{"README.md"},
					DueDate:              dueDate,
					CommitTime:           dueDate,
				}
			},
			checks: []string{
				CheckFileStructure,
				CheckImmutableFilesUnmodified,
				CheckCommitBeforeDueDate,
			},
			wantResults: []Result{
				{Name: CheckFileStructure, Passed: true, Reason: "all required paths are present"},
				{Name: CheckImmutableFilesUnmodified, Passed: true, Reason: "immutable files match the teacher copy"},
				{Name: CheckCommitBeforeDueDate, Passed: true, Reason: "commit time is on or before the due date"},
			},
		},
		"returns error for unsupported check": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				return CheckContext{}
			},
			checks:  []string{"unknown-check"},
			wantErr: `unsupported check "unknown-check"`,
		},
		"file structure reports missing deduplicated paths": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				assignmentDir := t.TempDir()
				return CheckContext{
					AssignmentDir:  assignmentDir,
					RequiredFiles:  []string{"present.txt", "missing.txt"},
					ImmutableFiles: []string{"missing.txt", "also-missing"},
				}
			},
			checks: []string{CheckFileStructure},
			wantResults: []Result{
				{Name: CheckFileStructure, Passed: false, Reason: "missing required paths: present.txt, missing.txt, also-missing"},
			},
		},
		"file structure allows directories": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				assignmentDir := t.TempDir()
				if err := os.Mkdir(filepath.Join(assignmentDir, "docs"), 0o755); err != nil {
					t.Fatalf("Mkdir() error = %v", err)
				}
				return CheckContext{
					AssignmentDir: assignmentDir,
					RequiredFiles: []string{"docs"},
				}
			},
			checks: []string{CheckFileStructure},
			wantResults: []Result{
				{Name: CheckFileStructure, Passed: true, Reason: "all required paths are present"},
			},
		},
		"immutable files reject whitespace only differences": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				assignmentDir := t.TempDir()
				teacherDir := t.TempDir()
				writeFile(t, assignmentDir, "main.txt", "alpha\tbeta \n gamma\r\n")
				writeFile(t, teacherDir, "main.txt", "alpha beta gamma")
				return CheckContext{
					AssignmentDir:        assignmentDir,
					TeacherAssignmentDir: teacherDir,
					ImmutableFiles:       []string{"main.txt"},
				}
			},
			checks: []string{CheckImmutableFilesUnmodified},
			wantResults: []Result{
				{Name: CheckImmutableFilesUnmodified, Passed: false, Reason: "immutable files were modified: main.txt"},
			},
		},
		"immutable files detect content differences": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				assignmentDir := t.TempDir()
				teacherDir := t.TempDir()
				writeFile(t, assignmentDir, "main.txt", "alpha\tbetaX\r\n")
				writeFile(t, teacherDir, "main.txt", "alpha beta\n")
				return CheckContext{
					AssignmentDir:        assignmentDir,
					TeacherAssignmentDir: teacherDir,
					ImmutableFiles:       []string{"main.txt"},
				}
			},
			checks: []string{CheckImmutableFilesUnmodified},
			wantResults: []Result{
				{Name: CheckImmutableFilesUnmodified, Passed: false, Reason: "immutable files were modified: main.txt"},
			},
		},
		"immutable files report missing student file as failure": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				assignmentDir := t.TempDir()
				teacherDir := t.TempDir()
				writeFile(t, teacherDir, "main.txt", "teacher")
				return CheckContext{
					AssignmentDir:        assignmentDir,
					TeacherAssignmentDir: teacherDir,
					ImmutableFiles:       []string{"main.txt"},
				}
			},
			checks: []string{CheckImmutableFilesUnmodified},
			wantResults: []Result{
				{Name: CheckImmutableFilesUnmodified, Passed: false, Reason: "immutable files were modified: main.txt (file missing or unreadable)"},
			},
		},
		"immutable files report missing teacher file as error": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				assignmentDir := t.TempDir()
				teacherDir := t.TempDir()
				writeFile(t, assignmentDir, "main.txt", "teacher")
				return CheckContext{
					AssignmentDir:        assignmentDir,
					TeacherAssignmentDir: teacherDir,
					ImmutableFiles:       []string{"main.txt"},
				}
			},
			checks:  []string{CheckImmutableFilesUnmodified},
			wantErr: `read teacher immutable file "main.txt"`,
		},
		"commit before due date passes on time": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				return CheckContext{
					DueDate:    time.Date(2026, 10, 15, 23, 59, 0, 0, time.UTC),
					CommitTime: time.Date(2026, 10, 15, 23, 59, 0, 0, time.UTC),
				}
			},
			checks: []string{CheckCommitBeforeDueDate},
			wantResults: []Result{
				{Name: CheckCommitBeforeDueDate, Passed: true, Reason: "commit time is on or before the due date"},
			},
		},
		"commit before due date fails late": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				return CheckContext{
					DueDate:    time.Date(2026, 10, 15, 23, 59, 0, 0, time.UTC),
					CommitTime: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
				}
			},
			checks: []string{CheckCommitBeforeDueDate},
			wantResults: []Result{
				{Name: CheckCommitBeforeDueDate, Passed: false, Reason: "commit time 2026-10-16T00:00:00Z is after due date 2026-10-15T23:59:00Z"},
			},
		},
		"commit before due date requires commit time": {
			setup: func(t *testing.T) CheckContext {
				t.Helper()
				return CheckContext{DueDate: time.Date(2026, 10, 15, 23, 59, 0, 0, time.UTC)}
			},
			checks:  []string{CheckCommitBeforeDueDate},
			wantErr: "commit time must not be zero",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := test.setup(t)
			checks, err := BuildChecks(test.checks, ctx)
			if err != nil {
				if test.wantErr != "" && strings.Contains(err.Error(), test.wantErr) {
					return
				}
				t.Fatalf("BuildChecks() error = %v, want containing %q", err, test.wantErr)
			}

			results := make([]Result, 0, len(checks))
			var execErr error
			for _, check := range checks {
				result, err := check.Execute(context.Background())
				if err != nil {
					execErr = err
					break
				}
				results = append(results, result)
			}

			if test.wantErr != "" {
				if execErr == nil || !strings.Contains(execErr.Error(), test.wantErr) {
					t.Fatalf("Execute() error = %v, want containing %q", execErr, test.wantErr)
				}
				return
			}
			if execErr != nil {
				t.Fatalf("Execute() error = %v", execErr)
			}
			if !resultsEqual(results, test.wantResults) {
				t.Fatalf("BuildChecks() results = %#v, want %#v", results, test.wantResults)
			}
		})
	}
}

func writeFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filename, err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", filename, err)
	}
}

func resultsEqual(got, want []Result) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func slicesEqual[T comparable](got, want []T) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
