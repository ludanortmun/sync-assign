package config

import (
	"strings"
	"testing"
	"time"
)

func TestAssignmentTimeout(t *testing.T) {
	base := "archetype: generic-shell-script\ndue-date: 2026-01-10T12:00:00Z\n"
	for _, value := range []string{"45s", "2m", "0s", "-1s", "nonsense"} {
		spec, err := LoadAssignmentSpec(strings.NewReader(base + "timeout: " + value + "\n"))
		want, parseErr := time.ParseDuration(value)
		if parseErr != nil || want <= 0 {
			if err == nil {
				t.Fatalf("accepted %q", value)
			}
		} else if err != nil || spec.Timeout == nil || *spec.Timeout != want {
			t.Fatalf("%q: %+v %v", value, spec, err)
		}
	}
}
