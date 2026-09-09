package archetype

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGenericShellScriptDefaultImmutableFiles(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		options map[string]string
		want    []string
		wantErr string
	}{
		"missing command": {
			wantErr: "archetype-options.command is required",
		},
		"command present": {
			options: map[string]string{"command": "./run_tests.sh"},
			want:    []string{"./run_tests.sh"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := genericShellScriptArchetype{}.DefaultImmutableFiles("", test.options)
			if test.wantErr != "" {
				if err == nil || !containsError(err, test.wantErr) {
					t.Fatalf("DefaultImmutableFiles() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultImmutableFiles() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("DefaultImmutableFiles() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGenericShellScriptTests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	script := filepath.Join(root, "run_tests.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'PASS foo'\necho 'FAIL bar'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", script, err)
	}

	checks, err := genericShellScriptArchetype{}.Tests(TestInput{
		Dir:     root,
		Options: map[string]string{"command": "./run_tests.sh"},
	})
	if err != nil {
		t.Fatalf("Tests() error = %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("Tests() = %#v, want 1 check", checks)
	}

	fooResult, err := checks[0].Execute(t.Context())
	if err != nil {
		t.Fatalf("Execute(foo) error = %v", err)
	}
	if fooResult.Passed || fooResult.Name != "unit-tests" || !strings.Contains(fooResult.Reason, "bar") {
		t.Fatalf("suite = %#v, want failure naming bar", fooResult)
	}
}

func TestGenericShellScriptTestsMissingCommand(t *testing.T) {
	t.Parallel()

	checks, err := genericShellScriptArchetype{}.Tests(TestInput{
		Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Tests() error = %v", err)
	}
	if _, err := checks[0].Execute(t.Context()); err == nil || !containsError(err, "archetype-options.command is required") {
		t.Fatalf("Execute() error = %v, want command-required error", err)
	}
}

func TestGenericShellScriptTestsUnrecognizedFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	script := filepath.Join(root, "run_tests.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'oops'\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", script, err)
	}

	checks, err := genericShellScriptArchetype{}.Tests(TestInput{
		Dir:     root,
		Options: map[string]string{"command": "./run_tests.sh"},
	})
	if err != nil {
		t.Fatalf("Tests() error = %v", err)
	}
	if _, err := checks[0].Execute(t.Context()); err == nil || !containsError(err, "oops") {
		t.Fatalf("Execute() error = %v, want unrecognized-output error", err)
	}
}
