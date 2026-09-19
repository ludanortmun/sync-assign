package commands

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ludanortmun/sync-assign/internal/config"
)

func resolveStudentConfigPath(repositoryRoot, override string) (string, error) {
	if strings.TrimSpace(override) == "" {
		if override != "" {
			return "", fmt.Errorf("student config path must not be empty")
		}
		return filepath.Join(repositoryRoot, config.StudentConfigFilename), nil
	}
	if filepath.IsAbs(override) {
		return filepath.Clean(override), nil
	}
	return filepath.Clean(filepath.Join(repositoryRoot, override)), nil
}
