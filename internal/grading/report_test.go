package grading

import (
	"strings"
	"testing"
)

func TestReportRequiresEveryCheck(t *testing.T) {
	for _, results := range [][]Result{nil, {{Name: "unit-tests"}}, {{Name: "files", Passed: true}, {Name: "unit-tests"}}, {{Name: "files"}, {Name: "unit-tests", Passed: true}}} {
		if BuildReport("lab", true, results).Passed {
			t.Fatalf("passed: %+v", results)
		}
	}
	results := []Result{{Name: "files", Passed: true}, {Name: "unit-tests", Passed: true}}
	report := BuildReport("lab", false, results)
	results[0].Passed = false
	if !report.Passed || !report.Results[0].Passed {
		t.Fatal("report changed")
	}
	text, err := (HumanRenderer{}).Render(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"PREVIEW - not authoritative", "Status: PASS", "Checks:", "[PASS] unit-tests"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q: %s", expected, text)
		}
	}
	for _, obsolete := range []string{"Score", "points", "Gates:", "Tests:"} {
		if strings.Contains(text, obsolete) {
			t.Fatalf("obsolete %q: %s", obsolete, text)
		}
	}
}
