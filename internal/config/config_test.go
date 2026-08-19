package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boolPointer(value bool) *bool {
	return &value
}

func stringPointer(value string) *string {
	return &value
}

func TestLoadTeacher(t *testing.T) {
	config, err := LoadTeacher(strings.NewReader(`
assignments:
  lab-1: assignments/lab-1
`))
	if err == nil {
		t.Fatal("LoadTeacher accepted a nested assignment directory")
	}

	config, err = LoadTeacher(strings.NewReader(`
assignments:
  lab-1: lab-1
  project: final-project
`))
	if err != nil {
		t.Fatalf("LoadTeacher returned an error: %v", err)
	}
	if got := config.Assignments["project"]; got != "final-project" {
		t.Fatalf("Assignments[project] = %q, want final-project", got)
	}
}

func TestTeacherConfigRejectsUnsafeAssignments(t *testing.T) {
	tests := map[string]string{
		"empty":              "",
		"absolute":           "/assignments/lab",
		"traversal":          "../lab",
		"nested":             "assignments/lab",
		"windows nested":     `assignments\lab`,
		"windows drive":      `C:lab`,
		"current":            ".",
		"surrounding space":  " lab",
		"git metadata":       ".git",
		"git metadata upper": ".GIT",
		"git metadata mixed": ".Git",
	}
	for name, directory := range tests {
		t.Run(name, func(t *testing.T) {
			config := TeacherConfig{Assignments: map[string]string{"lab": directory}}
			if err := config.Validate(); err == nil {
				t.Fatalf("Validate accepted %q", directory)
			}
		})
	}
}

func TestLoadStudent(t *testing.T) {
	config, err := LoadStudent(strings.NewReader(`
teacher-repository: git@github.com:school/course.git
commit: false
clean: true
skip-mirror: true
branch: fall-2026
`))
	if err != nil {
		t.Fatalf("LoadStudent returned an error: %v", err)
	}
	if config.Commit == nil || *config.Commit {
		t.Fatalf("Commit = %v, want explicit false", config.Commit)
	}
	if config.MirrorIsEphemeral() == nil || !*config.MirrorIsEphemeral() {
		t.Fatalf("MirrorIsEphemeral() = %v, want true", config.MirrorIsEphemeral())
	}
	if config.Branch == nil || *config.Branch != "fall-2026" {
		t.Fatalf("Branch = %v, want fall-2026", config.Branch)
	}
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	tests := map[string]string{
		"empty repository": "teacher-repository: ''\n",
		"unknown field":    "teacher-repository: repo\nunknown: true\n",
		"multiple docs":    "teacher-repository: repo\n---\nteacher-repository: other\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadStudent(strings.NewReader(input)); err == nil {
				t.Fatal("LoadStudent accepted invalid YAML")
			}
		})
	}
}

func TestStudentConfigRejectsTeacherPathWithEffectiveEphemeralMode(t *testing.T) {
	tests := map[string]StudentConfig{
		"ephemeral alias": {
			TeacherRepository: "repository",
			TeacherPath:       stringPointer("../teacher"),
			Ephemeral:         boolPointer(true),
		},
		"skip-mirror alias": {
			TeacherRepository: "repository",
			TeacherPath:       stringPointer("../teacher"),
			SkipMirror:        boolPointer(true),
		},
		"agreeing aliases": {
			TeacherRepository: "repository",
			TeacherPath:       stringPointer("../teacher"),
			Ephemeral:         boolPointer(true),
			SkipMirror:        boolPointer(true),
		},
	}
	for name, studentConfig := range tests {
		t.Run(name, func(t *testing.T) {
			err := studentConfig.Validate()
			if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
				t.Fatalf("Validate() error = %v, want conflicting mirror mode error", err)
			}
		})
	}
}

func TestStudentConfigAllowsTeacherPathWithoutEffectiveEphemeralMode(t *testing.T) {
	tests := map[string]StudentConfig{
		"nil aliases": {
			TeacherRepository: "repository",
			TeacherPath:       stringPointer("../teacher"),
		},
		"ephemeral false": {
			TeacherRepository: "repository",
			TeacherPath:       stringPointer("../teacher"),
			Ephemeral:         boolPointer(false),
		},
		"skip-mirror false": {
			TeacherRepository: "repository",
			TeacherPath:       stringPointer("../teacher"),
			SkipMirror:        boolPointer(false),
		},
		"nil teacher path": {
			TeacherRepository: "repository",
			Ephemeral:         boolPointer(true),
		},
	}
	for name, studentConfig := range tests {
		t.Run(name, func(t *testing.T) {
			if err := studentConfig.Validate(); err != nil {
				t.Fatalf("Validate() returned an error: %v", err)
			}
		})
	}
}

func TestWriteAndLoadFiles(t *testing.T) {
	directory := t.TempDir()
	teacherFile := filepath.Join(directory, TeacherConfigFilename)
	studentFile := filepath.Join(directory, StudentConfigFilename)

	teacher := TeacherConfig{Assignments: map[string]string{"lab": "lab"}}
	if err := WriteTeacherFile(teacherFile, teacher); err != nil {
		t.Fatalf("WriteTeacherFile returned an error: %v", err)
	}
	if _, err := LoadTeacherFile(teacherFile); err != nil {
		t.Fatalf("LoadTeacherFile returned an error: %v", err)
	}

	student := StudentConfig{
		TeacherRepository: "https://example.com/course.git",
		Commit:            boolPointer(false),
		Clean:             boolPointer(true),
		TeacherPath:       stringPointer("../course"),
		Ephemeral:         boolPointer(false),
		Branch:            stringPointer("main"),
	}
	if err := WriteStudentFile(studentFile, student); err != nil {
		t.Fatalf("WriteStudentFile returned an error: %v", err)
	}
	loaded, err := LoadStudentFile(studentFile)
	if err != nil {
		t.Fatalf("LoadStudentFile returned an error: %v", err)
	}
	if loaded.Commit == nil || *loaded.Commit {
		t.Fatalf("loaded Commit = %v, want explicit false", loaded.Commit)
	}
}

func TestWriteRejectsInvalidConfig(t *testing.T) {
	var output bytes.Buffer
	if err := WriteStudent(&output, StudentConfig{}); err == nil {
		t.Fatal("WriteStudent accepted an invalid config")
	}
	if output.Len() != 0 {
		t.Fatalf("WriteStudent wrote %q before validation", output.String())
	}

	filename := filepath.Join(t.TempDir(), StudentConfigFilename)
	const original = "teacher-repository: original\n"
	if err := os.WriteFile(filename, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile returned an error: %v", err)
	}
	if err := WriteStudentFile(filename, StudentConfig{}); err == nil {
		t.Fatal("WriteStudentFile accepted an invalid config")
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile returned an error: %v", err)
	}
	if string(contents) != original {
		t.Fatalf("invalid write replaced existing config with %q", contents)
	}
}

func TestResolve(t *testing.T) {
	configured := true
	if got := Resolve(Some(false), &configured, true); got {
		t.Fatal("explicit false flag did not override config")
	}
	if got := Resolve(Optional[bool]{}, &configured, false); !got {
		t.Fatal("config did not override fallback")
	}
	if got := Resolve(Optional[string]{}, nil, "default"); got != "default" {
		t.Fatalf("Resolve fallback = %q, want default", got)
	}
}
