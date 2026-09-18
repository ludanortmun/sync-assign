package grader

import (
	"reflect"
	"testing"
)

func TestRunPreservesOrderAndSetsCheckerName(t *testing.T) {
	var calls []string
	checkers := []Checker{
		{Name: "first", Check: func(Environment) Result {
			calls = append(calls, "first")
			return Result{Checker: "wrong", Status: Failed}
		}},
		{Name: "second", Check: func(Environment) Result {
			calls = append(calls, "second")
			return Result{Status: Skipped, Detail: "optional"}
		}},
	}

	report := Run(Environment{}, checkers)

	want := []Result{
		{Checker: "first", Status: Failed},
		{Checker: "second", Status: Skipped, Detail: "optional"},
	}
	if !reflect.DeepEqual(report.Results, want) {
		t.Fatalf("Run results = %#v, want %#v", report.Results, want)
	}
	if !reflect.DeepEqual(calls, []string{"first", "second"}) {
		t.Fatalf("checker call order = %#v, want first then second", calls)
	}
	if report.Passed() {
		t.Fatal("Report.Passed() = true, want false")
	}
}

func TestReportPassedOnlyFailsForFailedResult(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		want    bool
	}{
		{name: "empty", want: true},
		{name: "passed and skipped", results: []Result{{Status: Passed}, {Status: Skipped}}, want: true},
		{name: "failed", results: []Result{{Status: Passed}, {Status: Failed}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := (Report{Results: test.results}).Passed(); got != test.want {
				t.Fatalf("Report.Passed() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRunHandlesNilCheck(t *testing.T) {
	report := Run(Environment{}, []Checker{{Name: "nil"}})

	if len(report.Results) != 1 || report.Results[0].Status != Failed {
		t.Fatalf("Run result = %#v, want one failed result", report.Results)
	}
}

func TestCheckIf(t *testing.T) {
	calls := 0
	checker := Checker{
		Name: "optional",
		Check: func(Environment) Result {
			calls++
			return Result{Status: Passed}
		},
	}

	skipped := CheckIf(checker, func(Environment) bool { return false }).Check(Environment{})
	if skipped.Checker != checker.Name || skipped.Status != Skipped || skipped.Detail == "" {
		t.Fatalf("false condition result = %#v, want named skip with detail", skipped)
	}
	if calls != 0 {
		t.Fatalf("underlying checker called %d times, want 0", calls)
	}

	passed := CheckIf(checker, func(Environment) bool { return true }).Check(Environment{})
	if passed.Checker != checker.Name || passed.Status != Passed {
		t.Fatalf("true condition result = %#v, want named pass", passed)
	}
	if calls != 1 {
		t.Fatalf("underlying checker called %d times, want 1", calls)
	}
}

func TestCheckIfHandlesNilFunctions(t *testing.T) {
	if result := CheckIf(Checker{Name: "nil condition"}, nil).Check(Environment{}); result.Status != Skipped {
		t.Fatalf("nil condition status = %q, want %q", result.Status, Skipped)
	}
	if result := CheckIf(Checker{Name: "nil checker"}, func(Environment) bool { return true }).Check(Environment{}); result.Status != Failed {
		t.Fatalf("nil checker status = %q, want %q", result.Status, Failed)
	}
}

func TestCheckIfEvaluatesConditionAtRunTimeAndUsesWrapperName(t *testing.T) {
	enabled := false
	conditionCalls := 0
	checker := CheckIf(Checker{
		Name: "runtime checker",
		Check: func(environment Environment) Result {
			return Result{Checker: "incorrect", Status: Passed, Detail: environment.StudentDir}
		},
	}, func(environment Environment) bool {
		conditionCalls++
		return enabled && environment.StudentDir == "student"
	})

	if checker.Name != "runtime checker" {
		t.Fatalf("wrapped checker name = %q, want %q", checker.Name, "runtime checker")
	}
	if result := checker.Check(Environment{StudentDir: "student"}); result.Status != Skipped {
		t.Fatalf("disabled result = %#v, want skipped", result)
	}
	enabled = true
	result := checker.Check(Environment{StudentDir: "student"})
	if result.Checker != checker.Name || result.Status != Passed || result.Detail != "student" {
		t.Fatalf("enabled result = %#v, want named underlying result", result)
	}
	if conditionCalls != 2 {
		t.Fatalf("condition called %d times, want once per run", conditionCalls)
	}
}
