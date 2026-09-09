package grading

import (
	"fmt"
	"strings"
)

// Report contains every check outcome in execution order.
type Report struct {
	AssignmentID  string
	Authoritative bool
	Results       []Result
	Passed        bool
}

// BuildReport requires a nonempty set of exclusively passing checks.
func BuildReport(id string, authoritative bool, results []Result) Report {
	passed := len(results) > 0
	for _, result := range results {
		passed = passed && result.Passed
	}
	return Report{AssignmentID: id, Authoritative: authoritative, Results: append([]Result(nil), results...), Passed: passed}
}

type Renderer interface {
	Render(Report) (string, error)
}

type HumanRenderer struct{}

func (HumanRenderer) Render(r Report) (string, error) {
	var b strings.Builder
	if !r.Authoritative {
		b.WriteString("PREVIEW - not authoritative\n")
	}
	fmt.Fprintf(&b, "Assignment: %s\nStatus: %s\nChecks:\n", r.AssignmentID, passFailLabel(r.Passed))
	for _, result := range r.Results {
		fmt.Fprintf(&b, "- [%s] %s", passFailLabel(result.Passed), result.Name)
		if result.Reason != "" {
			fmt.Fprintf(&b, ": %s", result.Reason)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func passFailLabel(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}
