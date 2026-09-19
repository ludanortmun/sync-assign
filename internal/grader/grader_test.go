package grader

import (
	"reflect"
	"testing"
	"time"
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

	results := make(chan Result)
	go Run(Environment{}, checkers, results)
	var got []Result
	for result := range results {
		got = append(got, result)
	}

	want := []Result{
		{Checker: "first", Status: Running},
		{Checker: "first", Status: Failed},
		{Checker: "second", Status: Running},
		{Checker: "second", Status: Skipped, Detail: "optional"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Run results = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(calls, []string{"first", "second"}) {
		t.Fatalf("checker call order = %#v, want first then second", calls)
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
		{name: "non-blocking failed", results: []Result{{Status: Failed, NonBlocking: true}}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := (Report{Results: test.results}).Passed(); got != test.want {
				t.Fatalf("Report.Passed() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestNonBlockingCheckerMarksResults(t *testing.T) {
	results := make(chan Result, 2)
	go Run(Environment{}, []Checker{NonBlocking(Checker{
		Name: "extra credit",
		Check: func(Environment) Result {
			return Result{Status: Failed}
		},
	})}, results)

	<-results
	result := <-results
	if !result.NonBlocking || !(Report{Results: []Result{result}}).Passed() {
		t.Fatalf("result = %#v, want non-blocking failed result", result)
	}
}

func TestRunHandlesNilCheck(t *testing.T) {
	results := make(chan Result)
	go Run(Environment{}, []Checker{{Name: "nil"}}, results)
	var got []Result
	for result := range results {
		got = append(got, result)
	}

	if len(got) != 2 || got[0].Status != Running || got[1].Status != Failed {
		t.Fatalf("Run results = %#v, want running then failed", got)
	}
}

func TestRunEmitsRunningBeforeCheckerCompletes(t *testing.T) {
	release := make(chan struct{})
	results := make(chan Result)
	go Run(Environment{}, []Checker{{
		Name: "slow",
		Check: func(Environment) Result {
			<-release
			return Result{Status: Passed}
		},
	}}, results)

	select {
	case result := <-results:
		if result.Checker != "slow" || result.Status != Running {
			t.Fatalf("first result = %#v, want running slow checker", result)
		}
	case <-time.After(time.Second):
		t.Fatal("running result was not emitted before checker completion")
	}

	close(release)
	if result := <-results; result.Status != Passed {
		t.Fatalf("final result = %#v, want passed", result)
	}
	if _, ok := <-results; ok {
		t.Fatal("results channel remained open")
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

	skipped := CheckIf(checker, func(Environment) bool { return false }, "skip message").Check(Environment{})
	if skipped.Checker != checker.Name || skipped.Status != Skipped || skipped.Detail == "" {
		t.Fatalf("false condition result = %#v, want named skip with detail", skipped)
	}
	if calls != 0 {
		t.Fatalf("underlying checker called %d times, want 0", calls)
	}

	passed := CheckIf(checker, func(Environment) bool { return true }, "skipped").Check(Environment{})
	if passed.Checker != checker.Name || passed.Status != Passed {
		t.Fatalf("true condition result = %#v, want named pass", passed)
	}
	if calls != 1 {
		t.Fatalf("underlying checker called %d times, want 1", calls)
	}
}

func TestCheckIfHandlesNilFunctions(t *testing.T) {
	if result := CheckIf(Checker{Name: "nil condition"}, nil, "skip message").Check(Environment{}); result.Status != Failed {
		t.Fatalf("nil condition status = %q, want %q", result.Status, Failed)
	}
	if result := CheckIf(Checker{Name: "nil checker"}, func(Environment) bool { return true }, "skip message").Check(Environment{}); result.Status != Failed {
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
	}, "skipped because not enabled")

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
