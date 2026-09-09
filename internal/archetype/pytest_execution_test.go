package archetype

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPytestCollectionDiagnostics(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	// Simulate environment creation without requiring Python or network access.
	creator := `#!/bin/sh
mkdir -p "$3/bin"
printf '#!/bin/sh\necho collection diagnostic >&2\nexit 2\n' > "$3/bin/python3"
printf '#!/bin/sh\nexit 0\n' > "$3/bin/pip"
chmod +x "$3/bin/python3" "$3/bin/pip"
`
	if err := os.WriteFile(filepath.Join(bin, "python3"), []byte(creator), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	suite := pytestSuite{dir: dir}
	if _, err := suite.execute(context.Background()); err == nil || !strings.Contains(err.Error(), "collection diagnostic") {
		t.Fatalf("lost collection failure: %v", err)
	}
}

func TestPytestWholeSuiteReportsUnlistedFailures(t *testing.T) {
	bin := t.TempDir()
	creator := `#!/bin/sh
mkdir -p "$3/bin"
cat > "$3/bin/python3" <<'PYTEST'
#!/bin/sh
for arg in "$@"; do
  case "$arg" in --junitxml=*) results="${arg#--junitxml=}";; esac
done
printf '<testsuite><testcase name="passing"/><testcase classname="tests.math" name="unexpected_failure"><failure message="assertion diagnostic"/></testcase></testsuite>' > "$results"
echo pytest-output-diagnostic
exit 1
PYTEST
printf '#!/bin/sh\nexit 0\n' > "$3/bin/pip"
chmod +x "$3/bin/python3" "$3/bin/pip"
echo created >> environments-created
`
	if err := os.WriteFile(filepath.Join(bin, "python3"), []byte(creator), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		checks, err := (pythonPytestArchetype{}).Tests(TestInput{Dir: dir})
		if err != nil || len(checks) != 1 {
			t.Fatalf("checks=%v err=%v", checks, err)
		}
		result, err := checks[0].Execute(t.Context())
		if result.Passed || !strings.Contains(result.Reason, "tests.math::unexpected_failure: assertion diagnostic") || err == nil || !strings.Contains(err.Error(), "pytest-output-diagnostic") {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "environments-created"))
	if err != nil || string(data) != "created\ncreated\n" {
		t.Fatalf("environments not fresh: %q error=%v", data, err)
	}
	files, err := filepath.Glob(filepath.Join(dir, ".sync-assign-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("execution artifacts leaked: %v error=%v", files, err)
	}
}
