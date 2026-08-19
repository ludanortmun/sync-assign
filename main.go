package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/ludanortmun/sync-assign/internal/commands"
)

var version = "dev"

type syncCLI struct {
	AssignmentID string  `arg:"" name:"id" help:"Assignment ID from the teacher configuration."`
	Commit       *bool   `help:"Create a local commit after syncing." negatable:""`
	Clean        *bool   `help:"Replace an existing assignment." negatable:""`
	Force        bool    `help:"Replace an assignment even when it has uncommitted changes."`
	MirrorPath   *string `name:"mirror-path" type:"path" help:"Override the local teacher mirror path."`
	Ephemeral    *bool   `help:"Use a temporary teacher mirror and remove it afterward." negatable:""`
	Branch       *string `help:"Override the teacher repository branch."`
	Message      string  `short:"m" help:"Commit message (default: Sync assignment <id>)."`
}

func (command *syncCLI) Run(ctx context.Context) error {
	root, err := commands.CurrentDirectory()
	if err != nil {
		return err
	}
	return commands.NewSync().Run(ctx, command.AssignmentID, commands.SyncOptions{
		RepositoryRoot: root,
		Commit:         command.Commit,
		Clean:          command.Clean,
		Force:          command.Force,
		MirrorPath:     command.MirrorPath,
		Ephemeral:      command.Ephemeral,
		Branch:         command.Branch,
		Message:        command.Message,
	})
}

type initStudentCLI struct {
	TeacherRepository string  `arg:"" optional:"" name:"teacher-repo" help:"Teacher Git repository URL or path."`
	Force             bool    `help:"Overwrite an existing student configuration."`
	Commit            *bool   `help:"Set the default local commit behavior." negatable:""`
	Clean             *bool   `help:"Set the default replacement behavior." negatable:""`
	MirrorPath        *string `name:"mirror-path" type:"path" help:"Set a local teacher mirror path."`
	Ephemeral         *bool   `help:"Set whether teacher mirrors are temporary." negatable:""`
	Branch            *string `help:"Set the teacher repository branch."`
}

func (command *initStudentCLI) Run(ctx context.Context) error {
	root, err := commands.CurrentDirectory()
	if err != nil {
		return err
	}
	args := []string(nil)
	if command.TeacherRepository != "" {
		args = append(args, command.TeacherRepository)
	}
	return commands.NewInitStudent(os.Stdin, os.Stdout).Run(ctx, args, commands.InitStudentOptions{
		RepositoryRoot: root,
		Interactive:    stdinIsTerminal(),
		Force:          command.Force,
		Commit:         command.Commit,
		Clean:          command.Clean,
		MirrorPath:     command.MirrorPath,
		Ephemeral:      command.Ephemeral,
		Branch:         command.Branch,
	})
}

type cliModel struct {
	Version     kong.VersionFlag `help:"Print version information and quit."`
	Sync        syncCLI          `cmd:"" default:"withargs" hidden:"" help:"Sync an assignment."`
	InitStudent initStudentCLI   `cmd:"" name:"init-student" help:"Create a student repository configuration."`
}

func main() {
	cli := &cliModel{}
	ctx := kong.Parse(
		cli,
		kong.Name("sync-assign"),
		kong.Description("Sync git-versioned school assignments."),
		kong.Help(helpPrinter),
		kong.ShortHelp(shortHelpPrinter),
		kong.UsageOnError(),
		kong.Vars{"version": version},
		kong.BindTo(context.Background(), (*context.Context)(nil)),
	)
	ctx.FatalIfErrorf(ctx.Run())
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func helpPrinter(options kong.HelpOptions, ctx *kong.Context) error {
	if ctx.Selected() == nil {
		if _, err := fmt.Fprintln(ctx.Stdout, "Usage: sync-assign <id> [flags]\n       sync-assign init-student [<teacher-repo>] [flags]"); err != nil {
			return err
		}
		options.NoAppSummary = true
	}
	return kong.DefaultHelpPrinter(options, ctx)
}

func shortHelpPrinter(options kong.HelpOptions, ctx *kong.Context) error {
	if ctx.Selected() == nil {
		_, err := fmt.Fprintln(ctx.Stdout, "Usage: sync-assign <id> [flags]\n       sync-assign init-student [<teacher-repo>] [flags]")
		return err
	}
	return kong.DefaultShortHelpPrinter(options, ctx)
}
