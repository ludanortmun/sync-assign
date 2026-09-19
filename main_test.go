package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

type contextBindingCommand struct {
	called bool
}

func (command *contextBindingCommand) Run(ctx context.Context) error {
	command.called = ctx != nil
	return nil
}

func TestCLIParsesDefaultSyncCommand(t *testing.T) {
	cli := &cliModel{}
	parser, err := kong.New(cli, kong.Name("sync-assign"))
	if err != nil {
		t.Fatal(err)
	}

	context, err := parser.Parse([]string{"lab-1", "--config", "../.sync-assign.yml", "--no-commit", "--clean", "--branch", "fall"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if context.Command() != "sync <id>" {
		t.Fatalf("command = %q, want default sync command", context.Command())
	}
	if cli.Sync.AssignmentID != "lab-1" || cli.Sync.Commit == nil || *cli.Sync.Commit ||
		cli.Sync.Clean == nil || !*cli.Sync.Clean || *cli.Sync.Branch != "fall" ||
		!filepath.IsAbs(cli.Sync.ConfigPath) {
		t.Fatalf("parsed sync command = %#v", cli.Sync)
	}
}

func TestCLIParsesInitStudentCommand(t *testing.T) {
	cli := &cliModel{}
	parser, err := kong.New(cli, kong.Name("sync-assign"))
	if err != nil {
		t.Fatal(err)
	}

	context, err := parser.Parse([]string{
		"init-student",
		"https://example.com/course.git",
		"--force",
		"--no-ephemeral",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if context.Command() != "init-student <teacher-repo>" {
		t.Fatalf("command = %q", context.Command())
	}
	if cli.InitStudent.TeacherRepository != "https://example.com/course.git" ||
		!cli.InitStudent.Force ||
		cli.InitStudent.Ephemeral == nil ||
		*cli.InitStudent.Ephemeral {
		t.Fatalf("parsed init command = %#v", cli.InitStudent)
	}
}

func TestCLIParsesGradeCommand(t *testing.T) {
	cli := &cliModel{}
	parser, err := kong.New(cli, kong.Name("sync-assign"))
	if err != nil {
		t.Fatal(err)
	}

	context, err := parser.Parse([]string{
		"grade",
		"lab-1",
		"--due", "2026-09-18T23:59:00-07:00",
		"--config", "../.sync-assign.yml",
		"--branch", "student-work",
		"--pull",
		"--mirror-path", ".teacher-mirror",
		"--no-ephemeral",
		"--teacher-branch", "fall",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if context.Command() != "grade <id>" {
		t.Fatalf("command = %q, want grade command", context.Command())
	}
	mirrorPath, err := filepath.Abs(".teacher-mirror")
	if err != nil {
		t.Fatal(err)
	}
	if cli.Grade.AssignmentID != "lab-1" ||
		cli.Grade.Due != "2026-09-18T23:59:00-07:00" ||
		!filepath.IsAbs(cli.Grade.ConfigPath) ||
		cli.Grade.Branch != "student-work" ||
		!cli.Grade.Pull ||
		cli.Grade.MirrorPath == nil || *cli.Grade.MirrorPath != mirrorPath ||
		cli.Grade.Ephemeral == nil || *cli.Grade.Ephemeral ||
		cli.Grade.TeacherBranch == nil || *cli.Grade.TeacherBranch != "fall" {
		t.Fatalf("parsed grade command = %#v", cli.Grade)
	}
}

func TestCLIGradeRequiresDue(t *testing.T) {
	cli := &cliModel{}
	parser, err := kong.New(cli, kong.Name("sync-assign"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.Parse([]string{"grade", "lab-1"})
	if err == nil || !strings.Contains(err.Error(), "--due") {
		t.Fatalf("Parse() error = %v, want missing --due error", err)
	}
}

func TestCLIGradeRequiresAssignmentID(t *testing.T) {
	cli := &cliModel{}
	parser, err := kong.New(cli, kong.Name("sync-assign"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.Parse([]string{"grade", "--due", "2026-09-18"})
	if err == nil || !strings.Contains(err.Error(), "<id>") {
		t.Fatalf("Parse() error = %v, want missing assignment ID error", err)
	}
}

func TestVersionIsInjectable(t *testing.T) {
	original := version
	version = "test-version"
	t.Cleanup(func() { version = original })
	if version != "test-version" {
		t.Fatal("version cannot be assigned")
	}
}

func TestContextBindingRunsCommand(t *testing.T) {
	command := &contextBindingCommand{}
	parser, err := kong.New(
		command,
		kong.BindTo(context.Background(), (*context.Context)(nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !command.called {
		t.Fatal("Run() did not receive the bound context")
	}
}

func TestRootHelpShowsDefaultUsage(t *testing.T) {
	cli := &cliModel{}
	var output bytes.Buffer
	parser, err := kong.New(
		cli,
		kong.Name("sync-assign"),
		kong.Writers(&output, &output),
		kong.Help(helpPrinter),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Kong normally exits immediately after printing help. The no-op exit used
	// by this test lets parsing continue, so only the emitted output matters.
	_, _ = parser.Parse([]string{"--help"})
	for _, want := range []string{
		"Usage: sync-assign <id> [flags]",
		"sync-assign grade <id> --due=<due> [flags]",
		"sync-assign init-student [<teacher-repo>] [flags]",
		"grade",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help %q does not contain %q", output.String(), want)
		}
	}
}

func TestRootShortHelpShowsAllUsages(t *testing.T) {
	cli := &cliModel{}
	var output bytes.Buffer
	parser, err := kong.New(
		cli,
		kong.Name("sync-assign"),
		kong.Writers(&output, &output),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parser.Parse([]string{"--unknown"})
	var parseErr *kong.ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse() error = %v, want *kong.ParseError", err)
	}
	if err := shortHelpPrinter(kong.HelpOptions{}, parseErr.Context); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Usage: sync-assign <id> [flags]",
		"sync-assign grade <id> --due=<due> [flags]",
		"sync-assign init-student [<teacher-repo>] [flags]",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("short help %q does not contain %q", output.String(), want)
		}
	}
}
