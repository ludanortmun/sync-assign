package grader

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ludanortmun/sync-assign/internal/config"
)

var pythonTestPatterns = []string{"tests/**", "test_*.py"}

// CheckersFor returns the ordered checker pipeline for an assignment archetype.
func CheckersFor(archetype *config.Archetype) ([]Checker, error) {
	if archetype == nil {
		return nil, fmt.Errorf("assignment archetype is not set")
	}

	switch *archetype {
	case config.ArchetypePython:
		return []Checker{
			NewPythonUnitTestChecker(),
			NewNoFileModifiedChecker(pythonTestPatterns...),
		}, nil
	case config.ArchetypePythonJupyter:
		return []Checker{
			CheckIf(NewPythonUnitTestChecker(), hasPythonUnitTests),
			NewNotebookUnitTestChecker(),
			NewClearedOutputChecker(),
			NewUnchangedCellsChecker(),
			NewNoFileModifiedChecker(pythonTestPatterns...),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported assignment archetype %q", *archetype)
	}
}

func hasPythonUnitTests(environment Environment) bool {
	info, err := os.Stat(filepath.Join(environment.StudentDir, "tests"))
	return err == nil && info.IsDir()
}

func unavailableChecker(name string) Checker {
	return Checker{
		Name: name,
		Check: func(Environment) Result {
			return Result{
				Checker: name,
				Status:  Failed,
				Detail:  "checker implementation is unavailable",
			}
		},
	}
}
