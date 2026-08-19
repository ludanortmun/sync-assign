package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	TeacherConfigFilename = "sync-assign.yml"
	StudentConfigFilename = ".sync-assign.yml"
)

// TeacherConfig maps assignment IDs to directories in the teacher repository.
type TeacherConfig struct {
	Assignments map[string]string `yaml:"assignments"`
}

// StudentConfig identifies the teacher repository and optional client defaults.
// Pointer fields distinguish an omitted default from its zero value.
type StudentConfig struct {
	TeacherRepository string  `yaml:"teacher-repository"`
	Commit            *bool   `yaml:"commit,omitempty"`
	Clean             *bool   `yaml:"clean,omitempty"`
	TeacherPath       *string `yaml:"teacher-path,omitempty"`
	Ephemeral         *bool   `yaml:"ephemeral,omitempty"`
	SkipMirror        *bool   `yaml:"skip-mirror,omitempty"`
	Branch            *string `yaml:"branch,omitempty"`
}

func (config TeacherConfig) Validate() error {
	if len(config.Assignments) == 0 {
		return fmt.Errorf("assignments must not be empty")
	}
	for id, directory := range config.Assignments {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("assignment ID must not be empty")
		}
		if id != strings.TrimSpace(id) {
			return fmt.Errorf("assignment ID %q must not have surrounding whitespace", id)
		}
		if err := validateAssignmentDirectory(directory); err != nil {
			return fmt.Errorf("assignment %q: %w", id, err)
		}
	}
	return nil
}

func (config StudentConfig) Validate() error {
	if strings.TrimSpace(config.TeacherRepository) == "" {
		return fmt.Errorf("teacher repository must not be empty")
	}
	if config.TeacherRepository != strings.TrimSpace(config.TeacherRepository) {
		return fmt.Errorf("teacher repository must not have surrounding whitespace")
	}
	if config.TeacherPath != nil && strings.TrimSpace(*config.TeacherPath) == "" {
		return fmt.Errorf("teacher path must not be empty when set")
	}
	if config.TeacherPath != nil && *config.TeacherPath != strings.TrimSpace(*config.TeacherPath) {
		return fmt.Errorf("teacher path must not have surrounding whitespace")
	}
	if config.Branch != nil && strings.TrimSpace(*config.Branch) == "" {
		return fmt.Errorf("branch must not be empty when set")
	}
	if config.Branch != nil && *config.Branch != strings.TrimSpace(*config.Branch) {
		return fmt.Errorf("branch must not have surrounding whitespace")
	}
	if config.Ephemeral != nil && config.SkipMirror != nil && *config.Ephemeral != *config.SkipMirror {
		return fmt.Errorf("ephemeral and skip-mirror must agree when both are set")
	}
	if ephemeral := config.MirrorIsEphemeral(); config.TeacherPath != nil && ephemeral != nil && *ephemeral {
		return fmt.Errorf("teacher path and ephemeral mode cannot be used together")
	}
	return nil
}

// MirrorIsEphemeral returns the configured mirror behavior. Ephemeral is the
// canonical name; skip-mirror is accepted as an equivalent configuration key.
func (config StudentConfig) MirrorIsEphemeral() *bool {
	if config.Ephemeral != nil {
		return config.Ephemeral
	}
	return config.SkipMirror
}

func LoadTeacher(reader io.Reader) (TeacherConfig, error) {
	var config TeacherConfig
	if err := decodeYAML(reader, &config); err != nil {
		return TeacherConfig{}, fmt.Errorf("decode teacher config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return TeacherConfig{}, fmt.Errorf("validate teacher config: %w", err)
	}
	return config, nil
}

func LoadStudent(reader io.Reader) (StudentConfig, error) {
	var config StudentConfig
	if err := decodeYAML(reader, &config); err != nil {
		return StudentConfig{}, fmt.Errorf("decode student config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return StudentConfig{}, fmt.Errorf("validate student config: %w", err)
	}
	return config, nil
}

func LoadTeacherFile(filename string) (TeacherConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		return TeacherConfig{}, fmt.Errorf("open teacher config %q: %w", filename, err)
	}
	defer file.Close()

	config, err := LoadTeacher(file)
	if err != nil {
		return TeacherConfig{}, fmt.Errorf("load teacher config %q: %w", filename, err)
	}
	return config, nil
}

func LoadStudentFile(filename string) (StudentConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		return StudentConfig{}, fmt.Errorf("open student config %q: %w", filename, err)
	}
	defer file.Close()

	config, err := LoadStudent(file)
	if err != nil {
		return StudentConfig{}, fmt.Errorf("load student config %q: %w", filename, err)
	}
	return config, nil
}

func WriteTeacher(writer io.Writer, config TeacherConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate teacher config: %w", err)
	}
	if err := encodeYAML(writer, config); err != nil {
		return fmt.Errorf("encode teacher config: %w", err)
	}
	return nil
}

func WriteStudent(writer io.Writer, config StudentConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate student config: %w", err)
	}
	if err := encodeYAML(writer, config); err != nil {
		return fmt.Errorf("encode student config: %w", err)
	}
	return nil
}

func WriteTeacherFile(filename string, config TeacherConfig) error {
	return writeFile(filename, func(writer io.Writer) error {
		return WriteTeacher(writer, config)
	})
}

func WriteStudentFile(filename string, config StudentConfig) error {
	return writeFile(filename, func(writer io.Writer) error {
		return WriteStudent(writer, config)
	})
}

func validateAssignmentDirectory(directory string) error {
	if strings.TrimSpace(directory) == "" {
		return fmt.Errorf("directory must not be empty")
	}
	if directory != strings.TrimSpace(directory) {
		return fmt.Errorf("directory %q must not have surrounding whitespace", directory)
	}
	if filepath.IsAbs(directory) || path.IsAbs(directory) || filepath.VolumeName(directory) != "" {
		return fmt.Errorf("directory %q must be relative", directory)
	}
	if strings.Contains(directory, `\`) ||
		(len(directory) >= 2 && directory[1] == ':' &&
			((directory[0] >= 'a' && directory[0] <= 'z') ||
				(directory[0] >= 'A' && directory[0] <= 'Z'))) {
		return fmt.Errorf("directory %q must use a single top-level name", directory)
	}
	if directory == "." || directory == ".." || path.Clean(directory) != directory ||
		strings.Contains(directory, "/") || strings.ContainsRune(directory, '\x00') {
		return fmt.Errorf("directory %q must be a single top-level directory", directory)
	}
	if strings.EqualFold(directory, ".git") {
		return fmt.Errorf("directory %q must not target Git metadata", directory)
	}
	return nil
}

func writeFile(filename string, write func(io.Writer) error) error {
	var contents bytes.Buffer
	if err := write(&contents); err != nil {
		return fmt.Errorf("prepare config %q: %w", filename, err)
	}

	directory := filepath.Dir(filename)
	file, err := os.CreateTemp(directory, "."+filepath.Base(filename)+".*")
	if err != nil {
		return fmt.Errorf("create temporary config for %q: %w", filename, err)
	}
	temporaryFilename := file.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryFilename)
		}
	}()

	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		return fmt.Errorf("set permissions on temporary config for %q: %w", filename, err)
	}
	if _, err := file.Write(contents.Bytes()); err != nil {
		_ = file.Close()
		return fmt.Errorf("write config %q: %w", filename, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync config %q: %w", filename, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config %q: %w", filename, err)
	}
	if err := os.Rename(temporaryFilename, filename); err != nil {
		return fmt.Errorf("replace config %q: %w", filename, err)
	}
	removeTemporary = false
	return nil
}
