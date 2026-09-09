package config

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

const AssignmentSpecFilename = "sync-assign.yml"

var validAssignmentChecks = map[string]struct{}{
	"file-structure":             {},
	"immutable-files-unmodified": {},
	"commit-before-due-date":     {},
}

var validAssignmentSpecFields = map[string]struct{}{
	"timeout":           {},
	"archetype":         {},
	"due-date":          {},
	"immutable-files":   {},
	"required-files":    {},
	"checks":            {},
	"archetype-options": {},
}

// AssignmentSpec defines per-assignment metadata loaded from an assignment
// directory in the teacher repository.
type AssignmentSpec struct {
	Timeout        *time.Duration `yaml:"timeout,omitempty"`
	Archetype      string         `yaml:"archetype"`
	DueDate        time.Time      `yaml:"due-date"`
	ImmutableFiles []string       `yaml:"immutable-files,omitempty"`
	RequiredFiles  []string       `yaml:"required-files,omitempty"`
	// Checks overrides the default optional checks. Mandatory checks always run.
	Checks           []string          `yaml:"checks,omitempty"`
	ArchetypeOptions map[string]string `yaml:"archetype-options,omitempty"`
}

func (spec AssignmentSpec) Validate() error {
	if spec.Timeout != nil && *spec.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if strings.TrimSpace(spec.Archetype) == "" {
		return fmt.Errorf("archetype must not be empty")
	}
	if spec.Archetype != strings.TrimSpace(spec.Archetype) {
		return fmt.Errorf("archetype must not have surrounding whitespace")
	}
	if spec.DueDate.IsZero() {
		return fmt.Errorf("due date must not be empty")
	}
	for _, filename := range spec.ImmutableFiles {
		if err := validateAssignmentSpecPath(filename); err != nil {
			return fmt.Errorf("immutable file %q: %w", filename, err)
		}
	}
	for _, filename := range spec.RequiredFiles {
		if err := validateAssignmentSpecPath(filename); err != nil {
			return fmt.Errorf("required file %q: %w", filename, err)
		}
	}
	for _, check := range spec.Checks {
		if _, ok := validAssignmentChecks[check]; !ok {
			return fmt.Errorf("check %q is not supported", check)
		}
	}
	return nil
}

func (spec *AssignmentSpec) UnmarshalYAML(node *yaml.Node) error {
	for index := 0; index+1 < len(node.Content); index += 2 {
		field := node.Content[index].Value
		if _, ok := validAssignmentSpecFields[field]; !ok {
			return fmt.Errorf("field %s not found", field)
		}
	}

	type rawAssignmentSpec struct {
		Timeout          *time.Duration    `yaml:"timeout,omitempty"`
		Archetype        string            `yaml:"archetype"`
		DueDate          yaml.Node         `yaml:"due-date"`
		ImmutableFiles   []string          `yaml:"immutable-files,omitempty"`
		RequiredFiles    []string          `yaml:"required-files,omitempty"`
		Checks           []string          `yaml:"checks,omitempty"`
		ArchetypeOptions map[string]string `yaml:"archetype-options,omitempty"`
	}

	var raw rawAssignmentSpec
	if err := node.Decode(&raw); err != nil {
		return err
	}

	spec.Archetype = raw.Archetype
	spec.Timeout = raw.Timeout
	spec.ImmutableFiles = raw.ImmutableFiles
	spec.RequiredFiles = raw.RequiredFiles
	spec.Checks = raw.Checks
	spec.ArchetypeOptions = raw.ArchetypeOptions

	if strings.TrimSpace(raw.DueDate.Value) == "" {
		spec.DueDate = time.Time{}
		return nil
	}
	dueDate, err := time.Parse(time.RFC3339, raw.DueDate.Value)
	if err != nil {
		return fmt.Errorf("parse due-date: %w", err)
	}
	spec.DueDate = dueDate
	return nil
}

func (spec AssignmentSpec) MarshalYAML() (any, error) {
	type rawAssignmentSpec struct {
		Timeout          *time.Duration    `yaml:"timeout,omitempty"`
		Archetype        string            `yaml:"archetype"`
		DueDate          string            `yaml:"due-date"`
		ImmutableFiles   []string          `yaml:"immutable-files,omitempty"`
		RequiredFiles    []string          `yaml:"required-files,omitempty"`
		Checks           []string          `yaml:"checks,omitempty"`
		ArchetypeOptions map[string]string `yaml:"archetype-options,omitempty"`
	}

	raw := rawAssignmentSpec{
		Timeout:          spec.Timeout,
		Archetype:        spec.Archetype,
		ImmutableFiles:   spec.ImmutableFiles,
		RequiredFiles:    spec.RequiredFiles,
		Checks:           spec.Checks,
		ArchetypeOptions: spec.ArchetypeOptions,
	}
	if !spec.DueDate.IsZero() {
		raw.DueDate = spec.DueDate.Format(time.RFC3339)
	}
	return raw, nil
}

func LoadAssignmentSpec(reader io.Reader) (AssignmentSpec, error) {
	var spec AssignmentSpec
	if err := decodeYAML(reader, &spec); err != nil {
		return AssignmentSpec{}, fmt.Errorf("decode assignment spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return AssignmentSpec{}, fmt.Errorf("validate assignment spec: %w", err)
	}
	return spec, nil
}

func LoadAssignmentSpecFile(filename string) (AssignmentSpec, error) {
	file, err := os.Open(filename)
	if err != nil {
		return AssignmentSpec{}, fmt.Errorf("open assignment spec %q: %w", filename, err)
	}
	defer file.Close()

	spec, err := LoadAssignmentSpec(file)
	if err != nil {
		return AssignmentSpec{}, fmt.Errorf("load assignment spec %q: %w", filename, err)
	}
	return spec, nil
}

func validateAssignmentSpecPath(filename string) error {
	if strings.TrimSpace(filename) == "" {
		return fmt.Errorf("path must not be empty")
	}
	if filename != strings.TrimSpace(filename) {
		return fmt.Errorf("path %q must not have surrounding whitespace", filename)
	}
	if filepath.IsAbs(filename) || path.IsAbs(filename) || filepath.VolumeName(filename) != "" {
		return fmt.Errorf("path %q must be relative", filename)
	}
	if strings.Contains(filename, `\`) ||
		(len(filename) >= 2 && filename[1] == ':' &&
			((filename[0] >= 'a' && filename[0] <= 'z') ||
				(filename[0] >= 'A' && filename[0] <= 'Z'))) {
		return fmt.Errorf("path %q must use slash-separated relative paths", filename)
	}
	if path.Clean(filename) != filename || filename == "." || strings.ContainsRune(filename, '\x00') {
		return fmt.Errorf("path %q must be a clean relative path", filename)
	}
	for _, segment := range strings.Split(filename, "/") {
		if segment == ".." {
			return fmt.Errorf("path %q must not contain parent-directory traversal", filename)
		}
	}
	return nil
}
