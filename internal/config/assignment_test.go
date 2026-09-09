package config

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLoadAssignmentSpec(t *testing.T) {
	base := "archetype: python-pytest\ndue-date: 2026-10-15T23:59:00-07:00\n"
	for _, tc := range []struct{ name, input, want string }{
		{"minimal", base, ""},
		{"full", base + "checks: [file-structure, immutable-files-unmodified, commit-before-due-date]\nimmutable-files: [tests/test.py]\nrequired-files: [solution.py]\narchetype-options:\n  test-path: tests\n", ""},
		{"missing archetype", "due-date: 2026-10-15T23:59:00Z\n", "archetype must not be empty"},
		{"missing due date", "archetype: python-pytest\n", "due date must not be empty"},
		{"date offset", "archetype: python-pytest\ndue-date: 2026-10-15T23:59:00\n", "parse due-date"},
		{"bad date", "archetype: python-pytest\ndue-date: garbage\n", "parse due-date"},
		{"unknown", base + "unexpected: true\n", "field unexpected not found"},
		{"old rubric", base + "rubric: []\n", "field rubric not found"},
		{"old gates", base + "gates: []\n", "field gates not found"},
		{"points", base + "points: 100\n", "field points not found"},
		{"unknown check", base + "checks: [unknown]\n", `check "unknown" is not supported`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := LoadAssignmentSpec(strings.NewReader(tc.input))
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if spec.Archetype != "python-pytest" || spec.DueDate.Format(time.RFC3339) != "2026-10-15T23:59:00-07:00" {
				t.Fatalf("spec = %+v", spec)
			}
		})
	}
	for _, field := range []string{"immutable-files", "required-files"} {
		for _, path := range []string{"/etc/passwd", "../solution.py", `C:\solution.py`, "a/../b", "."} {
			if _, err := LoadAssignmentSpec(strings.NewReader(base + field + ": ['" + path + "']\n")); err == nil {
				t.Fatalf("accepted %s: %s", field, path)
			}
		}
	}
}

func TestAssignmentSpecDueDateRoundTrip(t *testing.T) {
	spec := AssignmentSpec{
		Archetype: "python-pytest",
		DueDate:   time.Date(2026, 10, 15, 23, 59, 0, 0, time.FixedZone("UTC-7", -7*60*60)),
		Checks:    []string{"file-structure"},
	}
	var output bytes.Buffer
	if err := encodeYAML(&output, spec); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"rubric", "points", "gates"} {
		if strings.Contains(output.String(), old) {
			t.Fatalf("obsolete config: %s", &output)
		}
	}
	loaded, err := LoadAssignmentSpec(strings.NewReader(output.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.DueDate.Equal(spec.DueDate) || len(loaded.Checks) != 1 {
		t.Fatalf("roundtrip: %+v", loaded)
	}
}
