package commands

import (
	"path/filepath"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/config"
)

func TestResolveStudentConfigPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "students", "alan")

	tests := []struct {
		name     string
		override string
		want     string
	}{
		{
			name: "default in repository",
			want: filepath.Join(root, config.StudentConfigFilename),
		},
		{
			name:     "relative to repository",
			override: filepath.Join("..", config.StudentConfigFilename),
			want:     filepath.Join(filepath.Dir(root), config.StudentConfigFilename),
		},
		{
			name:     "absolute",
			override: filepath.Join(t.TempDir(), "shared.yml"),
		},
	}
	tests[2].want = tests[2].override

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveStudentConfigPath(root, test.override)
			if err != nil {
				t.Fatalf("resolveStudentConfigPath() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("resolveStudentConfigPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveStudentConfigPathRejectsWhitespace(t *testing.T) {
	if _, err := resolveStudentConfigPath(t.TempDir(), "  "); err == nil {
		t.Fatal("resolveStudentConfigPath() accepted a whitespace-only override")
	}
}
