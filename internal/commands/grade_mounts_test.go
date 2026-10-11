package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/grader"
)

func TestResolveGradeMountsRejectsInvalidSpecifications(t *testing.T) {
	root := t.TempDir()
	writeWorkflowFile(t, filepath.Join(root, "data", "sample"), "dataset")
	writeWorkflowFile(t, filepath.Join(root, "file"), "file")
	tests := map[string][]string{
		"missing separator":    {"data"},
		"empty source":         {":data"},
		"empty destination":    {"data:"},
		"blank source":         {" :data"},
		"blank destination":    {"data: "},
		"missing source":       {"missing:data"},
		"file source":          {"file:data"},
		"absolute destination": {"data:/data"},
		"root destination":     {"data:."},
		"normalized root":      {"data:./"},
		"traversal":            {"data:../data"},
		"internal traversal":   {"data:assets/../data"},
		"git metadata":         {"data:assets/.GiT/data"},
		"backslash":            {`data:assets\data`},
		"nul":                  {"data:assets/\x00data"},
		"mode suffix":          {"data:data:ro"},
		"duplicate":            {"data:data", "data:./data"},
		"nested":               {"data:assets", "data:assets/data"},
		"reverse nested":       {"data:assets/data", "data:assets"},
	}
	for name, specifications := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveGradeMounts(root, specifications); err == nil || !strings.Contains(err.Error(), "mount ") {
				t.Fatalf("error = %v, want mount error", err)
			}
		})
	}
}

func TestResolveGradeMountsSplitsAtFinalColon(t *testing.T) {
	root := t.TempDir()
	writeWorkflowFile(t, filepath.Join(root, "data:large", "sample"), "dataset")
	mounts, err := resolveGradeMounts(root, []string{"data:large:assets/data"})
	if err != nil {
		t.Fatal(err)
	}
	if mounts[0].destination != "assets/data" || filepath.Base(mounts[0].source) != "data:large" {
		t.Fatalf("mount = %#v", mounts[0])
	}
}

func TestGradeMountsExposeLiveAssetsAndPreserveSources(t *testing.T) {
	for _, failChecker := range []bool{false, true} {
		name := "success"
		if failChecker {
			name = "checker failure"
		}
		t.Run(name, func(t *testing.T) {
			teacher := newGradeTeacher(t, "python")
			student := newGradeStudent(t, teacher)
			writeWorkflowFile(t, filepath.Join(student, ".gitignore"), "lab/data/\nlab/unmounted/\n")
			commitGradeFileAt(t, student, "lab/answer.txt", "on time", "2026-01-10T10:00:00Z")
			commitGradeFileAt(t, student, "lab/answer.txt", "late", "2026-01-12T10:00:00Z")
			writeWorkflowFile(t, filepath.Join(student, "lab/data/sample"), "live dataset")
			writeWorkflowFile(t, filepath.Join(student, "lab/unmounted/sample"), "absent")
			external := t.TempDir()
			writeWorkflowFile(t, filepath.Join(external, "sample"), "external dataset")
			sourceLink := filepath.Join(student, "models")
			if err := os.Symlink(external, sourceLink); err != nil {
				t.Fatal(err)
			}
			called := false
			var output strings.Builder
			command := newGradeCommand(t, teacher, &output, io.Discard, func(environment grader.Environment) grader.Result {
				called = true
				if got := readWorkflowFile(t, filepath.Join(environment.StudentDir, "answer.txt")); got != "on time" {
					t.Errorf("answer = %q", got)
				}
				for destination, want := range map[string]string{"data": "live dataset", "assets/models": "external dataset", "assets/absolute": "external dataset"} {
					path := filepath.Join(environment.StudentDir, destination)
					info, err := os.Lstat(path)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Fatalf("mount %s is not a symlink: %v", destination, err)
					}
					if got := readWorkflowFile(t, filepath.Join(path, "sample")); got != want {
						t.Errorf("dataset = %q, want %q", got, want)
					}
				}
				if _, err := os.Lstat(filepath.Join(environment.StudentDir, "unmounted")); !os.IsNotExist(err) {
					t.Errorf("unmounted ignored assets exist: %v", err)
				}
				writeWorkflowFile(t, filepath.Join(environment.StudentDir, "data/written"), "shared write")
				status := grader.Passed
				if failChecker {
					status = grader.Failed
				}
				return grader.Result{Status: status}
			})
			err := command.Run(context.Background(), "lab", GradeOptions{
				RepositoryRoot: student, DueDate: "2026-01-11",
				Mounts: []string{"lab/data:data", "models:assets/models", external + ":assets/absolute"},
			})
			if (err != nil) != failChecker || !called {
				t.Fatalf("error = %v, checker called = %v", err, called)
			}
			resolvedSource, err := filepath.EvalSymlinks(external)
			if err != nil {
				t.Fatal(err)
			}
			if want := "  " + resolvedSource + " -> assets/models\n"; !strings.Contains(output.String(), want) {
				t.Errorf("report missing resolved mount %q:\n%s", want, output.String())
			}
			assertNoGradeWorktrees(t, student)
			if got := readWorkflowFile(t, filepath.Join(student, "lab/data/written")); got != "shared write" {
				t.Errorf("write through mount = %q", got)
			}
			if got := readWorkflowFile(t, filepath.Join(external, "sample")); got != "external dataset" {
				t.Errorf("source changed during cleanup: %q", got)
			}
		})
	}
}

func TestGradeRejectsUnsafeMountDestinationsBeforeChecks(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "dangling symlink", "parent file", "parent symlink", "assignment symlink", "missing assignment"} {
		t.Run(kind, func(t *testing.T) {
			teacher := newGradeTeacher(t, "python")
			student := newGradeStudent(t, teacher)
			external := t.TempDir()
			writeWorkflowFile(t, filepath.Join(external, "sample"), "keep")
			path := filepath.Join(student, "lab", "blocked")
			destination := "blocked"
			switch kind {
			case "file", "parent file":
				writeWorkflowFile(t, path, "committed")
			case "directory":
				writeWorkflowFile(t, filepath.Join(path, "file"), "committed")
			case "assignment symlink":
				path = filepath.Join(student, "lab")
				fallthrough
			case "symlink", "parent symlink", "dangling symlink":
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				target := external
				if kind == "dangling symlink" {
					target = filepath.Join(external, "missing")
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(kind, "parent") {
				destination += "/data"
			}
			commitGradeFileAt(t, student, "marker", "commit", "2026-01-10T10:00:00Z")
			called := false
			command := newGradeCommand(t, teacher, io.Discard, io.Discard, func(grader.Environment) grader.Result {
				called = true
				return grader.Result{Status: grader.Passed}
			})
			// A valid mount appears first to verify all targets are checked before
			// any links or missing parent directories are installed.
			err := command.Run(context.Background(), "lab", GradeOptions{
				RepositoryRoot: student, DueDate: "2026-01-11",
				Mounts: []string{external + ":new/data", external + ":" + destination},
			})
			if err == nil || !strings.Contains(err.Error(), "mount ") || called {
				t.Fatalf("error = %v, checker called = %v", err, called)
			}
			assertNoGradeWorktrees(t, student)
			if got := readWorkflowFile(t, filepath.Join(external, "sample")); got != "keep" {
				t.Errorf("source changed: %q", got)
			}
			if _, err := os.Lstat(filepath.Join(external, "new")); !os.IsNotExist(err) {
				t.Errorf("created parents through assignment symlink: %v", err)
			}
		})
	}
}

func TestInstallGradeMountsValidatesAllTargetsBeforeInstalling(t *testing.T) {
	assignment := t.TempDir()
	source := t.TempDir()
	writeWorkflowFile(t, filepath.Join(assignment, "blocked"), "committed")
	mounts, err := resolveGradeMounts(source, []string{source + ":new/data", source + ":blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := installGradeMounts(assignment, mounts); err == nil {
		t.Fatal("accepted existing destination")
	}
	if _, err := os.Lstat(filepath.Join(assignment, "new")); !os.IsNotExist(err) {
		t.Fatalf("installed parents before validation finished: %v", err)
	}
}

func TestGradeCleansPartialMountSetupOnCaseInsensitiveFilesystem(t *testing.T) {
	teacher := newGradeTeacher(t, "python")
	student := newGradeStudent(t, teacher)
	external := t.TempDir()
	writeWorkflowFile(t, filepath.Join(external, "sample"), "keep")
	if _, err := os.Stat(filepath.Join(external, "SAMPLE")); os.IsNotExist(err) {
		t.Skip("requires a case-insensitive filesystem")
	} else if err != nil {
		t.Fatal(err)
	}
	commitGradeFileAt(t, student, "lab/answer.txt", "answer", "2026-01-10T10:00:00Z")
	called := false
	command := newGradeCommand(t, teacher, io.Discard, io.Discard, func(grader.Environment) grader.Result {
		called = true
		return grader.Result{Status: grader.Passed}
	})
	err := command.Run(context.Background(), "lab", GradeOptions{
		RepositoryRoot: student, DueDate: "2026-01-11",
		Mounts: []string{external + ":assets", external + ":ASSETS/nested/data"},
	})
	if err == nil || !strings.Contains(err.Error(), "mount ") || called {
		t.Fatalf("error = %v, checker called = %v", err, called)
	}
	assertNoGradeWorktrees(t, student)
	if _, err := os.Lstat(filepath.Join(external, "nested")); !os.IsNotExist(err) {
		t.Fatalf("mount setup wrote through an earlier mount: %v", err)
	}
	if got := readWorkflowFile(t, filepath.Join(external, "sample")); got != "keep" {
		t.Errorf("source changed during cleanup: %q", got)
	}
}
