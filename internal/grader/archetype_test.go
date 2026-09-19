package grader

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
)

func TestCheckersFor(t *testing.T) {
	tests := []struct {
		archetype config.Archetype
		wantNames []string
	}{
		{
			archetype: config.ArchetypePython,
			wantNames: []string{"python unit tests", "python extra credit tests", "unmodified test files"},
		},
		{
			archetype: config.ArchetypePythonJupyter,
			wantNames: []string{
				"python unit tests",
				"python extra credit tests",
				"notebook unit tests",
				"cleared notebook output",
				"unchanged notebook cells",
				"unmodified test files",
			},
		},
	}
	for _, test := range tests {
		t.Run(string(test.archetype), func(t *testing.T) {
			checkers, err := CheckersFor(&test.archetype)
			if err != nil {
				t.Fatalf("CheckersFor() error = %v", err)
			}
			gotNames := make([]string, len(checkers))
			for index, checker := range checkers {
				gotNames[index] = checker.Name
			}
			if !reflect.DeepEqual(gotNames, test.wantNames) {
				t.Fatalf("CheckersFor() names = %#v, want %#v", gotNames, test.wantNames)
			}
		})
	}
}

func TestCheckersForRejectsUnsetAndUnknownArchetypes(t *testing.T) {
	if _, err := CheckersFor(nil); err == nil {
		t.Fatal("CheckersFor(nil) error = nil, want error")
	}

	unknown := config.Archetype("unknown")
	if _, err := CheckersFor(&unknown); err == nil {
		t.Fatal("CheckersFor(unknown) error = nil, want error")
	}
}

func TestJupyterPythonTestsDependOnTestsDirectory(t *testing.T) {
	archetype := config.ArchetypePythonJupyter
	checkers, err := CheckersFor(&archetype)
	if err != nil {
		t.Fatalf("CheckersFor() error = %v", err)
	}

	studentDir := t.TempDir()
	environment := Environment{StudentDir: studentDir}
	if result := checkers[0].Check(environment); result.Status != Skipped {
		t.Fatalf("without tests directory status = %q, want %q", result.Status, Skipped)
	}

	if err := os.Mkdir(filepath.Join(studentDir, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if result := checkers[0].Check(environment); result.Status == Skipped {
		t.Fatalf("with tests directory status = %q, want checker to run", result.Status)
	}
}
