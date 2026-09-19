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
			CheckIf(NewPythonExtraCreditTestChecker(), hasExtraCreditTests, "assignment has no extra credits"),
			NewNoFileModifiedChecker(pythonTestPatterns...),
		}, nil
	case config.ArchetypeJupyter:
		return []Checker{
			CheckIf(NewPythonUnitTestChecker(), hasPythonUnitTests, "assignment has no unit tests"),
			CheckIf(NewPythonExtraCreditTestChecker(), hasExtraCreditTests, "assignment has no extra credits"),
			NewNotebookUnitTestChecker(),
			CheckIf(NewNotebookExtraCreditTestChecker(), hasExtraCreditNotebooks, "assignment has no extra-credit notebooks"),
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

func hasExtraCreditTests(environment Environment) bool {
	extraCreditsFiles, err := extraCreditTestPaths(environment.StudentDir)
	return err == nil && len(extraCreditsFiles) > 0
}

func hasExtraCreditNotebooks(environment Environment) bool {
	extraCreditFiles, err := extraCreditNotebookPaths(environment.StudentDir)
	return err == nil && len(extraCreditFiles) > 0
}
