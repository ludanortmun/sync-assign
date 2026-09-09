package archetype

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPythonPytestDefaultImmutableFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for path, content := range map[string]string{
		"test_root.py":                "",
		"pkg/test_nested.py":          "",
		"pkg/helper.py":               "",
		"pkg/subdir/test_deep.py":     "",
		"pkg/subdir/spec_test.py":     "",
		".git/test_ignored.py":        "",
		"tests/test_discovered.py":    "",
		"tests/not_a_test.txt":        "",
		"tests/nested/test_more.py":   "",
		"other/not_collected_test.py": "",
	} {
		writeTestFile(t, filepath.Join(root, path), content)
	}

	got, err := pythonPytestArchetype{}.DefaultImmutableFiles(root, nil)
	if err != nil {
		t.Fatalf("DefaultImmutableFiles() error = %v", err)
	}

	want := []string{
		"other/not_collected_test.py",
		"pkg/subdir/spec_test.py",
		"pkg/subdir/test_deep.py",
		"pkg/test_nested.py",
		"test_root.py",
		"tests/nested/test_more.py",
		"tests/test_discovered.py",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultImmutableFiles() = %v, want %v", got, want)
	}
}

func TestPythonPytestDefaultImmutableFilesWithTestPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for path, content := range map[string]string{
		"tests/test_included.py":      "",
		"tests/nested/test_more.py":   "",
		"other/test_not_included.py":  "",
		"tests/nested/helper_test.py": "",
	} {
		writeTestFile(t, filepath.Join(root, path), content)
	}

	got, err := pythonPytestArchetype{}.DefaultImmutableFiles(root, map[string]string{"test-path": "tests"})
	if err != nil {
		t.Fatalf("DefaultImmutableFiles() error = %v", err)
	}

	want := []string{
		"other/test_not_included.py",
		"tests/nested/helper_test.py",
		"tests/nested/test_more.py",
		"tests/test_included.py",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultImmutableFiles() = %v, want %v", got, want)
	}
}

func TestParseJUnitXML(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input   string
		want    map[string]testOutcome
		wantErr string
	}{
		"testsuites wrapper all pass": {
			input: `<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="pytest">
    <testcase classname="tests.test_math" name="test_addition"></testcase>
    <testcase classname="tests.test_math" name="test_subtraction"></testcase>
  </testsuite>
</testsuites>`,
			want: map[string]testOutcome{
				"tests.test_math::test_addition":    {Passed: true},
				"tests.test_math::test_subtraction": {Passed: true},
			},
		},
		"bare testsuite mixed results": {
			input: `<?xml version="1.0" encoding="utf-8"?>
<testsuite name="pytest">
  <testcase classname="tests.test_math" name="test_addition"></testcase>
  <testcase classname="tests.test_math" name="test_division">
    <failure message="assertion failed">expected 2
got 3</failure>
  </testcase>
  <testcase name="test_runtime">
    <error message="boom">traceback</error>
  </testcase>
</testsuite>`,
			want: map[string]testOutcome{
				"tests.test_math::test_addition": {Passed: true},
				"tests.test_math::test_division": {Passed: false, Message: "expected 2\ngot 3"},
				"test_runtime":                   {Passed: false, Message: "traceback"},
			},
		},
		"nested suites": {
			input: `<?xml version="1.0" encoding="utf-8"?>
<testsuites>
  <testsuite name="outer">
    <testsuite name="inner">
      <testcase classname="tests.test_inner" name="test_ok"></testcase>
    </testsuite>
  </testsuite>
</testsuites>`,
			want: map[string]testOutcome{
				"tests.test_inner::test_ok": {Passed: true},
			},
		},
		"invalid xml": {
			input:   `<not-xml`,
			wantErr: "decode junit xml",
		},
		"no testcases": {
			input:   `<testsuite name="empty"></testsuite>`,
			wantErr: "no testcases found",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parseJUnitXML([]byte(test.input))
			if test.wantErr != "" {
				if err == nil || !containsError(err, test.wantErr) {
					t.Fatalf("parseJUnitXML() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseJUnitXML() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseJUnitXML() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestPythonPytestSuiteExecuteMissingRequirements(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	suite := &pytestSuite{dir: root}
	_, err := suite.execute(t.Context())
	if err == nil || !containsError(err, "requirements.txt not found") {
		t.Fatalf("execute() error = %v, want requirements error", err)
	}
}

func TestPythonPytestTestsPropagatesExecuteError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	checks, err := pythonPytestArchetype{}.Tests(TestInput{
		Dir: root,
	})
	if err != nil {
		t.Fatalf("Tests() error = %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("Tests() = %#v, want 1 check", checks)
	}
	if _, err := checks[0].Execute(t.Context()); err == nil || !containsError(err, "requirements.txt not found") {
		t.Fatalf("Execute() error = %v, want requirements error", err)
	}
}

func TestJUnitSkippedTestsFailSuite(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, element, reason string
	}{
		{"skip", `<skipped message="not available">location: not available</skipped>`, "location: not available"},
		{"xfail", `<skipped type="pytest.xfail" message="known bug"/>`, "known bug"},
		{"empty skip", `<skipped/>`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			outcomes, err := parseJUnitXML([]byte(`<testsuite><testcase name="test_example">` + test.element + `</testcase></testsuite>`))
			if err != nil {
				t.Fatal(err)
			}
			if got := outcomes["test_example"]; got.Passed || got.Message != "skipped: "+test.reason {
				t.Fatalf("outcome = %#v, want non-passing with reason %q", got, test.reason)
			}
			check := newSuiteCheck(func(context.Context) (map[string]testOutcome, error) {
				return outcomes, nil
			})
			result, err := check.Execute(t.Context())
			if err != nil || result.Passed || !strings.Contains(result.Reason, "test_example") {
				t.Fatalf("Execute() = %#v, %v", result, err)
			}
		})
	}
}

func TestPrepareVenvRecoversFailedInstall(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "venv")
	ready := filepath.Join(path, ".sync-assign-ready")
	var creates, installs int
	installErr := errors.New("pip failed")
	run := func(_ context.Context, _, name string, _ ...string) error {
		if name == "python3" {
			creates++
			if _, err := os.Stat(filepath.Join(path, "stale")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("incomplete environment was not removed: %v", err)
			}
			writeTestFile(t, filepath.Join(path, "bin", "python3"), "")
			return nil
		}
		installs++
		return installErr
	}
	writeTestFile(t, filepath.Join(path, "bin", "python3"), "")
	writeTestFile(t, filepath.Join(path, "stale"), "")
	if err := prepareVenv(t.Context(), "", path, "requirements.txt", run); !errors.Is(err, installErr) {
		t.Fatalf("prepareVenv() = %v", err)
	}
	if _, err := os.Stat(ready); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed install marked ready: %v", err)
	}
	installErr = nil
	for range 2 {
		if err := prepareVenv(t.Context(), "", path, "requirements.txt", run); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 2 || installs != 2 {
		t.Fatalf("creates = %d, installs = %d; want 2 each", creates, installs)
	}
}

func TestPrepareVenvSerializesInitialization(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "venv")
	started, release := make(chan struct{}), make(chan struct{})
	var installs atomic.Int32
	run := func(_ context.Context, _, name string, _ ...string) error {
		if name == "python3" {
			return os.MkdirAll(filepath.Join(path, "bin"), 0o755)
		}
		installs.Add(1)
		close(started)
		<-release
		return os.WriteFile(filepath.Join(path, "bin", "python3"), nil, 0o600)
	}
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			results <- prepareVenv(t.Context(), "", path, "requirements.txt", run)
		})
	}
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := prepareVenv(ctx, "", path, "requirements.txt", run)
	close(release)
	workers.Wait()
	close(results)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting initializer = %v, want deadline exceeded", err)
	}
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := installs.Load(); got != 1 {
		t.Fatalf("install count = %d, want 1", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func containsError(err error, want string) bool {
	text := err.Error()
	return text != "" && strings.Contains(text, want)
}
