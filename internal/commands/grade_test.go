package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/gitcmd"
)

func TestGradeRun(t *testing.T) {
	t.Run("happy path grades newest commit at or before due date", func(t *testing.T) {
		teacherRemote, teacher := newGitRemote(t, "teacher")
		_, student := newGitRemote(t, "student")
		writeGradeAssignmentSpec(t, teacher, time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC))
		runGitCommand(t, teacher, "add", ".")
		runGitCommand(t, teacher, "commit", "-m", "Initial assignment")
		runGitCommand(t, teacher, "push", "-u", "origin", "main")

		writeTestFile(t, filepath.Join(student, config.StudentConfigFilename), "teacher-repository: "+teacherRemote+"\n")

		writeGradeStudentCommit(t, student, "expected\n", time.Date(2026, time.January, 9, 10, 0, 0, 0, time.UTC), "answer before due date")
		gradedCommit := strings.TrimSpace(runGitCommand(t, student, "rev-parse", "HEAD"))
		runGitCommand(t, student, "push", "-u", "origin", "main")
		writeGradeStudentCommit(t, student, "wrong\n", time.Date(2026, time.January, 11, 10, 0, 0, 0, time.UTC), "answer after due date")
		runGitCommand(t, student, "push")
		writeGradeStudentCommit(t, student, "wrong local\n", time.Date(2026, time.January, 9, 11, 0, 0, 0, time.UTC), "unpublished backdated answer")
		localCommit := strings.TrimSpace(runGitCommand(t, student, "rev-parse", "HEAD"))
		writeTestFile(t, filepath.Join(student, "lab", "answer.txt"), "uncommitted answer\n")
		writeTestFile(t, filepath.Join(student, "untracked.txt"), "local notes\n")

		var output strings.Builder
		command := newGradeWithDependencies(
			acceptingGitRootValidator{},
			gitcmd.New(),
			func(context.Context, config.StudentConfig) (teacherMirror, error) {
				return &fakeTeacherMirror{path: teacher, close: func() error { return nil }}, nil
			},
			&output,
		)

		err := command.Run(context.Background(), "lab", GradeOptions{RepositoryRoot: student})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		for _, want := range []string{
			"Grading commit " + gradedCommit + " (2026-01-09T10:00:00Z)",
			"Assignment: lab",
			"[PASS] commit-before-due-date: commit time is on or before the due date",
			"[PASS] unit-tests",
			"Status: PASS",
		} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("output %q does not contain %q", output.String(), want)
			}
		}
		if strings.Contains(output.String(), "PREVIEW - not authoritative") {
			t.Fatalf("output %q unexpectedly contains preview banner", output.String())
		}
		if got := strings.TrimSpace(runGitCommand(t, student, "rev-parse", "HEAD")); got != localCommit {
			t.Fatalf("local HEAD changed to %s, want %s", got, localCommit)
		}
		for path, want := range map[string]string{"lab/answer.txt": "uncommitted answer\n", "untracked.txt": "local notes\n"} {
			got, err := os.ReadFile(filepath.Join(student, path))
			if err != nil || string(got) != want {
				t.Fatalf("local file %s = %q, %v", path, got, err)
			}
		}
	})

	t.Run("fast-forward failure returns clear error", func(t *testing.T) {
		teacherRemote, teacher := newGitRemote(t, "teacher")
		studentRemote, student := newGitRemote(t, "student")
		writeGradeAssignmentSpec(t, teacher, time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC))
		runGitCommand(t, teacher, "add", ".")
		runGitCommand(t, teacher, "commit", "-m", "Initial assignment")
		runGitCommand(t, teacher, "push", "-u", "origin", "main")

		writeTestFile(t, filepath.Join(student, config.StudentConfigFilename), "teacher-repository: "+teacherRemote+"\n")
		writeGradeStudentCommit(t, student, "expected\n", time.Date(2026, time.January, 8, 10, 0, 0, 0, time.UTC), "initial shared")
		runGitCommand(t, student, "push", "-u", "origin", "main")
		writeGradeStudentCommit(t, student, "wrong\n", time.Date(2026, time.January, 9, 10, 0, 0, 0, time.UTC), "local only")

		remoteAdvance := filepath.Join(t.TempDir(), "student-remote-advance")
		runGitCommand(t, "", "clone", studentRemote, remoteAdvance)
		configureWorkflowIdentity(t, remoteAdvance)
		writeTestFile(t, filepath.Join(remoteAdvance, "remote.txt"), "remote advance\n")
		runGitCommand(t, remoteAdvance, "add", ".")
		runGitCommand(t, remoteAdvance, "commit", "-m", "Remote advance")
		runGitCommand(t, remoteAdvance, "push", "origin", "main")

		command := newGradeWithDependencies(
			acceptingGitRootValidator{},
			gitcmd.New(),
			func(context.Context, config.StudentConfig) (teacherMirror, error) {
				return &fakeTeacherMirror{path: teacher, close: func() error { return nil }}, nil
			},
			new(strings.Builder),
		)

		err := command.Run(context.Background(), "lab", GradeOptions{RepositoryRoot: student})
		if err == nil || !strings.Contains(err.Error(), `update local branch "main" to match remote before grading`) {
			t.Fatalf("Run() error = %v", err)
		}
	})

	t.Run("no qualifying commit before due date", func(t *testing.T) {
		teacherRemote, teacher := newGitRemote(t, "teacher")
		_, student := newGitRemote(t, "student")
		writeGradeAssignmentSpec(t, teacher, time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC))
		runGitCommand(t, teacher, "add", ".")
		runGitCommand(t, teacher, "commit", "-m", "Initial assignment")
		runGitCommand(t, teacher, "push", "-u", "origin", "main")

		writeTestFile(t, filepath.Join(student, config.StudentConfigFilename), "teacher-repository: "+teacherRemote+"\n")
		writeGradeStudentCommit(t, student, "expected\n", time.Date(2026, time.January, 11, 10, 0, 0, 0, time.UTC), "too late")
		runGitCommand(t, student, "push", "-u", "origin", "main")

		command := newGradeWithDependencies(
			acceptingGitRootValidator{},
			gitcmd.New(),
			func(context.Context, config.StudentConfig) (teacherMirror, error) {
				return &fakeTeacherMirror{path: teacher, close: func() error { return nil }}, nil
			},
			new(strings.Builder),
		)

		err := command.Run(context.Background(), "lab", GradeOptions{RepositoryRoot: student})
		if err == nil || !strings.Contains(err.Error(), `grade assignment "lab": no commit at or before 2026-01-10T12:00:00Z found in "`) {
			t.Fatalf("Run() error = %v", err)
		}
	})

	t.Run("assignment directory missing in graded commit", func(t *testing.T) {
		teacherRemote, teacher := newGitRemote(t, "teacher")
		_, student := newGitRemote(t, "student")
		writeGradeAssignmentSpec(t, teacher, time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC))
		runGitCommand(t, teacher, "add", ".")
		runGitCommand(t, teacher, "commit", "-m", "Initial assignment")
		runGitCommand(t, teacher, "push", "-u", "origin", "main")

		writeTestFile(t, filepath.Join(student, config.StudentConfigFilename), "teacher-repository: "+teacherRemote+"\n")
		writeTestFile(t, filepath.Join(student, "notes.txt"), "before assignment exists\n")
		commitGradeEnv(t, student, time.Date(2026, time.January, 9, 10, 0, 0, 0, time.UTC), "pre-assignment commit")
		runGitCommand(t, student, "push", "-u", "origin", "main")
		writeGradeStudentCommit(t, student, "expected\n", time.Date(2026, time.January, 11, 10, 0, 0, 0, time.UTC), "add assignment late")
		runGitCommand(t, student, "push")

		command := newGradeWithDependencies(
			acceptingGitRootValidator{},
			gitcmd.New(),
			func(context.Context, config.StudentConfig) (teacherMirror, error) {
				return &fakeTeacherMirror{path: teacher, close: func() error { return nil }}, nil
			},
			new(strings.Builder),
		)

		err := command.Run(context.Background(), "lab", GradeOptions{RepositoryRoot: student})
		if err == nil || !strings.Contains(err.Error(), `assignment "lab" does not exist in the graded commit`) {
			t.Fatalf("Run() error = %v", err)
		}
	})

	t.Run("removes temporary worktree after grading", func(t *testing.T) {
		teacherRemote, teacher := newGitRemote(t, "teacher")
		_, student := newGitRemote(t, "student")
		writeGradeAssignmentSpec(t, teacher, time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC))
		runGitCommand(t, teacher, "add", ".")
		runGitCommand(t, teacher, "commit", "-m", "Initial assignment")
		runGitCommand(t, teacher, "push", "-u", "origin", "main")

		writeTestFile(t, filepath.Join(student, config.StudentConfigFilename), "teacher-repository: "+teacherRemote+"\n")
		beforeEntries, err := os.ReadDir(os.TempDir())
		if err != nil {
			t.Fatalf("ReadDir(temp) before grading error = %v", err)
		}
		before := make(map[string]struct{}, len(beforeEntries))
		for _, entry := range beforeEntries {
			before[entry.Name()] = struct{}{}
		}
		writeGradeStudentCommit(t, student, "expected\n", time.Date(2026, time.January, 9, 10, 0, 0, 0, time.UTC), "answer before due date")
		runGitCommand(t, student, "push", "-u", "origin", "main")

		var output strings.Builder
		command := newGradeWithDependencies(
			acceptingGitRootValidator{},
			gitcmd.New(),
			func(context.Context, config.StudentConfig) (teacherMirror, error) {
				return &fakeTeacherMirror{path: teacher, close: func() error { return nil }}, nil
			},
			&output,
		)

		if err := command.Run(context.Background(), "lab", GradeOptions{RepositoryRoot: student}); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		prefix := "Grading commit "
		line := strings.Split(output.String(), "\n")[0]
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("first output line = %q, want grading header", line)
		}
		if got := strings.TrimSpace(runGitCommand(t, student, "worktree", "list")); strings.Count(got, "\n") != 0 {
			t.Fatalf("git worktree list = %q, want only main worktree", got)
		}
		entries, err := os.ReadDir(os.TempDir())
		if err != nil {
			t.Fatalf("ReadDir(temp) error = %v", err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "sync-assign-grade-") {
				if _, existed := before[entry.Name()]; existed {
					continue
				}
				t.Fatalf("temporary grade worktree %q still exists", entry.Name())
			}
		}
	})
}

func writeGradeAssignmentSpec(t *testing.T, teacher string, dueDate time.Time) {
	t.Helper()
	writeTestFile(t, filepath.Join(teacher, config.TeacherConfigFilename), "assignments:\n  lab: lab\n")
	spec := config.AssignmentSpec{
		Archetype:        "generic-shell-script",
		DueDate:          dueDate,
		ArchetypeOptions: map[string]string{"command": "./grade.sh"},
	}
	if err := writeAssignmentSpecTestFile(filepath.Join(teacher, "lab", config.AssignmentSpecFilename), spec); err != nil {
		t.Fatalf("write assignment spec: %v", err)
	}
	writeTestFile(t, filepath.Join(teacher, "lab", "grade.sh"), "#!/bin/sh\nif grep -qx 'expected' answer.txt; then\n  echo 'PASS smoke'\nelse\n  echo 'FAIL smoke'\nfi\n")
	writeTestFile(t, filepath.Join(teacher, "lab", "answer.txt"), "expected\n")
	mustChmodTestFile(t, filepath.Join(teacher, "lab", "grade.sh"), 0o755)
}

func writeGradeStudentCommit(t *testing.T, repository, answer string, commitTime time.Time, message string) {
	t.Helper()
	writeTestFile(t, filepath.Join(repository, "lab", "grade.sh"), "#!/bin/sh\nif grep -qx 'expected' answer.txt; then\n  echo 'PASS smoke'\nelse\n  echo 'FAIL smoke'\nfi\n")
	writeTestFile(t, filepath.Join(repository, "lab", "answer.txt"), answer)
	mustChmodTestFile(t, filepath.Join(repository, "lab", "grade.sh"), 0o755)
	commitGradeEnv(t, repository, commitTime, message)
}

func commitGradeEnv(t *testing.T, repository string, commitTime time.Time, message string) {
	t.Helper()
	command := exec.Command("git", "commit", "-m", message)
	command.Dir = repository
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+commitTime.Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+commitTime.Format(time.RFC3339),
	)
	runGitCommand(t, repository, "add", ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit -m %q failed: %v\n%s", message, err, output)
	}
}
