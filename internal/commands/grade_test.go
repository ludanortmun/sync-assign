package commands

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
	"github.com/ludanortmun/sync-assign/internal/grader"
)

func TestGradeSelectsLatestCommitAtOrBeforeDueAndCleansWorktree(t *testing.T) {
	teacher := newGradeTeacher(t, "python")
	student := newGradeStudent(t, teacher)
	sharedConfig := filepath.Join(t.TempDir(), config.StudentConfigFilename)
	if err := os.Rename(filepath.Join(student, config.StudentConfigFilename), sharedConfig); err != nil {
		t.Fatal(err)
	}
	before := commitGradeFileAt(t, student, "lab/answer.txt", "on time\n", "2026-01-10T10:00:00Z")
	after := commitGradeFileAt(t, student, "lab/answer.txt", "late\n", "2026-01-12T10:00:00Z")

	var output strings.Builder
	var gradedContents string
	command := newGradeCommand(t, teacher, &output, io.Discard, func(environment grader.Environment) grader.Result {
		gradedContents = readWorkflowFile(t, filepath.Join(environment.StudentDir, "answer.txt"))
		return grader.Result{Status: grader.Passed}
	})
	if err := command.Run(context.Background(), "lab", GradeOptions{
		RepositoryRoot: student,
		ConfigPath:     sharedConfig,
		DueDate:        "2026-01-11T23:59:59Z",
	}); err != nil {
		t.Fatalf("grade: %v", err)
	}

	if gradedContents != "on time\n" {
		t.Fatalf("graded contents = %q, want on-time revision", gradedContents)
	}
	report := output.String()
	if !strings.Contains(report, "branch: main\n") || !strings.Contains(report, "commit: "+before+"\n") {
		t.Fatalf("report did not identify default branch and on-time commit:\n%s", report)
	}
	if strings.Contains(report, after) {
		t.Fatalf("report selected late commit %s:\n%s", after, report)
	}
	assertNoGradeWorktrees(t, student)
}

func TestGradeRejectsUnsetAndExplicitUnsupportedArchetypes(t *testing.T) {
	tests := []struct {
		name      string
		archetype string
		want      string
	}{
		{name: "unset", want: "assignment archetype is not set"},
		{name: "explicit unsupported", archetype: "ruby", want: `unknown archetype "ruby"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			teacher := newGradeTeacher(t, test.archetype)
			student := newGradeStudent(t, teacher)
			commitGradeFileAt(t, student, "lab/answer.txt", "answer\n", "2026-01-10T10:00:00Z")
			command := newGradeWithDependencies(
				&strings.Builder{},
				io.Discard,
				execGitRootValidator{},
				gitcmd.New(),
				gradeMirrorOpener(teacher),
				grader.CheckersFor,
			)

			err := command.Run(context.Background(), "lab", GradeOptions{
				RepositoryRoot: student,
				DueDate:        "2026-01-11",
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
			assertNoGradeWorktrees(t, student)
		})
	}
}

func TestGradeWritesReportAndReturnsErrorWhenCheckerFails(t *testing.T) {
	teacher := newGradeTeacher(t, "python")
	student := newGradeStudent(t, teacher)
	commitGradeFileAt(t, student, "lab/answer.txt", "answer\n", "2026-01-10T10:00:00Z")
	var output strings.Builder
	var errorOutput strings.Builder
	command := newGradeCommand(t, teacher, &output, &errorOutput, func(grader.Environment) grader.Result {
		return grader.Result{Status: grader.Failed, Detail: "tests failed deliberately"}
	})

	err := command.Run(context.Background(), "lab", GradeOptions{
		RepositoryRoot: student,
		DueDate:        "2026-01-11",
	})
	if err == nil || !strings.Contains(err.Error(), "failed grading checks") {
		t.Fatalf("error = %v, want grading failure", err)
	}
	report := output.String()
	for _, want := range []string{
		"summary: \x1b[32m0 passed\x1b[0m, \x1b[31m1 failed\x1b[0m, \x1b[33m0 skipped\x1b[0m",
		"result: \x1b[31mfailed\x1b[0m",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	for _, want := range []string{
		"\x1b[31m[FAILED] injected checker\x1b[0m",
		"tests failed deliberately",
	} {
		if !strings.Contains(errorOutput.String(), want) {
			t.Errorf("error report missing %q:\n%s", want, errorOutput.String())
		}
	}
	if strings.Contains(report, "[FAILED]") {
		t.Fatalf("failed check was written to stdout:\n%s", report)
	}
	assertNoGradeWorktrees(t, student)
}

func TestWriteGradeReportStylesAndRoutesStates(t *testing.T) {
	results := make(chan grader.Result, 4)
	results <- grader.Result{Checker: "active", Status: grader.Running}
	results <- grader.Result{Checker: "complete", Status: grader.Passed}
	results <- grader.Result{Checker: "optional", Status: grader.Skipped, Detail: "not configured"}
	results <- grader.Result{Checker: "broken", Status: grader.Failed, Detail: "assertion failed"}
	close(results)

	var output strings.Builder
	var errorOutput strings.Builder
	err := writeGradeReport(
		&output,
		&errorOutput,
		"lab",
		"main",
		"abc123",
		time.Date(2026, 9, 18, 23, 59, 59, 0, time.UTC),
		results,
	)
	if err == nil || !strings.Contains(err.Error(), "failed grading checks") {
		t.Fatalf("writeGradeReport() error = %v, want failed checks error", err)
	}

	stdout := output.String()
	for _, want := range []string{
		"[RUNNING] active",
		"\x1b[32m[SUCCESS] complete\x1b[0m",
		"\x1b[33m[SKIPPED] optional\x1b[0m",
		"summary: \x1b[32m1 passed\x1b[0m, \x1b[31m1 failed\x1b[0m, \x1b[33m1 skipped\x1b[0m",
		"result: \x1b[31mfailed\x1b[0m",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}

	if strings.Contains(stdout, "[FAILED]") {
		t.Fatalf("stdout contains failed state:\n%s", stdout)
	}

	stderr := errorOutput.String()
	for _, want := range []string{
		"\x1b[31m[FAILED] broken\x1b[0m",
		"assertion failed",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}

}

func TestWriteGradeReportTreatsSkippedChecksAsPassing(t *testing.T) {
	results := make(chan grader.Result, 2)
	results <- grader.Result{Checker: "complete", Status: grader.Passed}
	results <- grader.Result{Checker: "optional", Status: grader.Skipped}
	close(results)

	var output strings.Builder
	if err := writeGradeReport(
		&output,
		io.Discard,
		"lab",
		"main",
		"abc123",
		time.Date(2026, 9, 18, 23, 59, 59, 0, time.UTC),
		results,
	); err != nil {
		t.Fatalf("writeGradeReport() error = %v", err)
	}
	if want := "result: \x1b[32mpassed\x1b[0m"; !strings.Contains(output.String(), want) {
		t.Fatalf("stdout missing %q:\n%s", want, output.String())
	}
}

func TestParseDueDate(t *testing.T) {
	location := time.FixedZone("test local", -7*60*60)
	t.Run("date uses local end of day", func(t *testing.T) {
		got, err := parseDueDate("2026-09-18", location)
		if err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 9, 18, 23, 59, 59, int(time.Second-time.Nanosecond), location)
		if !got.Equal(want) || got.Location() != location {
			t.Fatalf("parseDueDate = %s (%s), want %s (%s)", got, got.Location(), want, location)
		}
	})
	t.Run("RFC3339 preserves instant and offset", func(t *testing.T) {
		got, err := parseDueDate("2026-09-18T15:30:50-07:00", location)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := time.Parse(time.RFC3339, "2026-09-18T15:30:50-07:00")
		if !got.Equal(want) || got.Format(time.RFC3339) != want.Format(time.RFC3339) {
			t.Fatalf("parseDueDate = %s, want %s", got, want)
		}
	})
}

func TestGradePullFastForwardsCheckedOutBranch(t *testing.T) {
	teacher := newGradeTeacher(t, "python")
	remote, upstream := newGitRemote(t, "student-upstream")
	writeGradeStudentConfig(t, upstream, teacher)
	commitGradeFileAt(t, upstream, "lab/answer.txt", "initial\n", "2026-01-10T10:00:00Z")
	runGitCommand(t, upstream, "push", "-u", "origin", "main")

	student := filepath.Join(t.TempDir(), "student")
	runGitCommand(t, "", "clone", "--branch", "main", remote, student)
	configureWorkflowIdentity(t, student)
	wantCommit := commitGradeFileAt(t, upstream, "lab/answer.txt", "remote update\n", "2026-01-11T10:00:00Z")
	runGitCommand(t, upstream, "push")

	var gradedContents string
	command := newGradeCommand(t, teacher, &strings.Builder{}, io.Discard, func(environment grader.Environment) grader.Result {
		gradedContents = readWorkflowFile(t, filepath.Join(environment.StudentDir, "answer.txt"))
		return grader.Result{Status: grader.Passed}
	})
	if err := command.Run(context.Background(), "lab", GradeOptions{
		RepositoryRoot: student,
		DueDate:        "2026-01-12",
		Pull:           true,
	}); err != nil {
		t.Fatalf("grade with pull: %v", err)
	}
	if got := strings.TrimSpace(runGitCommand(t, student, "rev-parse", "HEAD")); got != wantCommit {
		t.Fatalf("student HEAD = %s, want fast-forwarded %s", got, wantCommit)
	}
	if gradedContents != "remote update\n" {
		t.Fatalf("graded contents = %q, want pulled revision", gradedContents)
	}
	assertNoGradeWorktrees(t, student)
}

func TestGradePullRejectsNonFastForward(t *testing.T) {
	teacher := newGradeTeacher(t, "python")
	remote, upstream := newGitRemote(t, "student-upstream")
	writeGradeStudentConfig(t, upstream, teacher)
	commitGradeFileAt(t, upstream, "lab/answer.txt", "initial\n", "2026-01-10T10:00:00Z")
	runGitCommand(t, upstream, "push", "-u", "origin", "main")

	student := filepath.Join(t.TempDir(), "student")
	runGitCommand(t, "", "clone", "--branch", "main", remote, student)
	configureWorkflowIdentity(t, student)
	commitGradeFileAt(t, upstream, "lab/answer.txt", "remote\n", "2026-01-11T10:00:00Z")
	runGitCommand(t, upstream, "push")
	commitGradeFileAt(t, student, "lab/local.txt", "local\n", "2026-01-11T11:00:00Z")

	checkerRan := false
	command := newGradeCommand(t, teacher, &strings.Builder{}, io.Discard, func(grader.Environment) grader.Result {
		checkerRan = true
		return grader.Result{Status: grader.Passed}
	})
	err := command.Run(context.Background(), "lab", GradeOptions{
		RepositoryRoot: student,
		DueDate:        "2026-01-12",
		Pull:           true,
	})
	if err == nil || !strings.Contains(err.Error(), "fast-forward") {
		t.Fatalf("error = %v, want non-fast-forward failure", err)
	}
	if checkerRan {
		t.Fatal("checker ran after pull failed")
	}
	assertNoGradeWorktrees(t, student)
}

func newGradeCommand(
	t *testing.T,
	teacher string,
	output io.Writer,
	errorOutput io.Writer,
	check func(grader.Environment) grader.Result,
) *Grade {
	t.Helper()
	return newGradeWithDependencies(
		output,
		errorOutput,
		execGitRootValidator{},
		gitcmd.New(),
		gradeMirrorOpener(teacher),
		func(*config.Archetype) ([]grader.Checker, error) {
			return []grader.Checker{{Name: "injected checker", Check: check}}, nil
		},
	)
}

func gradeMirrorOpener(path string) mirrorOpener {
	return func(context.Context, config.StudentConfig) (teacherMirror, error) {
		return &fakeTeacherMirror{path: path, close: func() error { return nil }}, nil
	}
}

func newGradeTeacher(t *testing.T, archetype string) string {
	t.Helper()
	teacher := newGitRepository(t, "teacher")
	value := "assignments:\n  lab:\n    path: lab\n"
	if archetype != "" {
		value += "    archetype: " + archetype + "\n"
	}
	writeWorkflowFile(t, filepath.Join(teacher, config.TeacherConfigFilename), value)
	writeWorkflowFile(t, filepath.Join(teacher, "lab", "starter.txt"), "starter\n")
	runGitCommand(t, teacher, "add", ".")
	runGitCommand(t, teacher, "commit", "-m", "Configure assignment")
	return teacher
}

func newGradeStudent(t *testing.T, teacher string) string {
	t.Helper()
	student := newGitRepository(t, "student")
	writeGradeStudentConfig(t, student, teacher)
	runGitCommand(t, student, "add", config.StudentConfigFilename)
	runGitCommand(t, student, "commit", "-m", "Configure sync")
	return student
}

func writeGradeStudentConfig(t *testing.T, student, teacher string) {
	t.Helper()
	writeWorkflowFile(t, filepath.Join(student, config.StudentConfigFilename),
		"teacher-repository: "+teacher+"\nteacher-path: "+teacher+"\n")
}

func commitGradeFileAt(t *testing.T, repository, name, contents, timestamp string) string {
	t.Helper()
	writeWorkflowFile(t, filepath.Join(repository, name), contents)
	runGitCommand(t, repository, "add", ".")
	command := exec.Command("git", "commit", "-m", "Update "+name)
	command.Dir = repository
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+timestamp, "GIT_COMMITTER_DATE="+timestamp)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("commit %s: %v\n%s", name, err, output)
	}
	return strings.TrimSpace(runGitCommand(t, repository, "rev-parse", "HEAD"))
}

func assertNoGradeWorktrees(t *testing.T, repository string) {
	t.Helper()
	if output := runGitCommand(t, repository, "worktree", "list", "--porcelain"); strings.Contains(output, ".sync-assign-grade-") {
		t.Errorf("temporary grade worktree remains:\n%s", output)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(repository), ".sync-assign-grade-*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		if _, err := os.Stat(match); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temporary grade directory remains: %s", match)
		}
	}
}
