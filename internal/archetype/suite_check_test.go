package archetype

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuiteCheckStrictPassAndDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outcomes map[string]testOutcome
		err      error
		passed   bool
		reason   string
	}{
		{name: "no tests", reason: "no tests ran"},
		{name: "all pass", outcomes: map[string]testOutcome{"one": {Passed: true}, "two": {Passed: true}}, passed: true},
		{name: "unlisted failure", outcomes: map[string]testOutcome{"one": {Passed: true}, "surprise_case": {Message: "assertion"}}, reason: "surprise_case: assertion"},
		{name: "partial results and error", outcomes: map[string]testOutcome{"failed_case": {}}, err: errors.New("execution"), reason: "failed_case"},
		{name: "exit error despite pass", outcomes: map[string]testOutcome{"one": {Passed: true}}, err: errors.New("execution")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := newSuiteCheck(func(context.Context) (map[string]testOutcome, error) { return tc.outcomes, tc.err })
			result, err := check.Execute(t.Context())
			if result.Name != "unit-tests" || result.Passed != tc.passed || !errors.Is(err, tc.err) || !strings.Contains(result.Reason, tc.reason) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestShellSuiteStrictExitAndDuplicateFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script, reason string
		executionError       bool
	}{
		{"duplicate", "echo FAIL named_case\necho PASS named_case", "named_case", false},
		{"exit", "echo PASS named_case\nexit 7", "", true},
		{"partial error", "echo FAIL named_case\nexit 2", "named_case", true},
		{"empty", "echo diagnostics", "no tests ran", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			checks, err := (genericShellScriptArchetype{}).Tests(TestInput{Dir: dir, Options: map[string]string{"command": "./run.sh"}})
			if err != nil || len(checks) != 1 {
				t.Fatalf("checks=%v error=%v", checks, err)
			}
			result, err := checks[0].Execute(t.Context())
			if result.Passed || (err != nil) != tc.executionError || !strings.Contains(result.Reason, tc.reason) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestJUnitDuplicateFailureCannotBeOverwritten(t *testing.T) {
	outcomes, err := parseJUnitXML([]byte(`<testsuite><testcase name="same"><failure message="first failure"/></testcase><testcase name="same"/></testsuite>`))
	if err != nil || outcomes["same"].Passed || outcomes["same"].Message != "first failure" {
		t.Fatalf("outcomes=%v error=%v", outcomes, err)
	}
}
