package archetype

import (
	"context"
	"sort"
	"strings"

	"github.com/ludanortmun/sync-assign/internal/grading"
)

type testOutcome struct {
	Passed  bool
	Message string
}

type suiteResolver func(context.Context) (map[string]testOutcome, error)

// One check represents the complete suite, with named failing-case diagnostics.
func newSuiteCheck(resolve suiteResolver) grading.Check {
	return grading.NewCheck("unit-tests", func(ctx context.Context) (grading.Result, error) {
		outcomes, err := resolve(ctx)
		result := grading.Result{Name: "unit-tests", Passed: len(outcomes) > 0 && err == nil}
		var reasons []string
		if len(outcomes) == 0 {
			reasons = append(reasons, "no tests ran")
		}
		names := make([]string, 0, len(outcomes))
		for name := range outcomes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			outcome := outcomes[name]
			if !outcome.Passed {
				result.Passed = false
				reason := name
				if outcome.Message != "" {
					reason += ": " + outcome.Message
				}
				reasons = append(reasons, reason)
			}
		}
		result.Reason = strings.Join(reasons, "\n")
		return result, err
	})
}
