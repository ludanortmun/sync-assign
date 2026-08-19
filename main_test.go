package main

import (
	"bytes"
	"context"
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

	context, err := parser.Parse([]string{"lab-1", "--no-commit", "--clean", "--branch", "fall"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if context.Command() != "sync <id>" {
		t.Fatalf("command = %q, want default sync command", context.Command())
	}
	if cli.Sync.AssignmentID != "lab-1" || cli.Sync.Commit == nil || *cli.Sync.Commit ||
		cli.Sync.Clean == nil || !*cli.Sync.Clean || *cli.Sync.Branch != "fall" {
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
		"sync-assign init-student [<teacher-repo>] [flags]",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help %q does not contain %q", output.String(), want)
		}
	}
}
