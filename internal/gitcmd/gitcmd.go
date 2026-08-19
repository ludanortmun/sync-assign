package gitcmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes git with a command-scoped working directory.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, err error)
}

// Client performs git operations through a Runner.
type Client struct {
	runner Runner
}

// New returns a Client that invokes the git executable on PATH.
func New() *Client {
	return NewWithRunner(execRunner{})
}

// NewWithRunner returns a Client backed by runner.
func NewWithRunner(runner Runner) *Client {
	return &Client{runner: runner}
}

// CommandError describes a failed git command and includes its captured output.
type CommandError struct {
	Dir    string
	Args   []string
	Stdout string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	location := ""
	if e.Dir != "" {
		location = fmt.Sprintf(" in %q", e.Dir)
	}

	message := fmt.Sprintf("git %s%s failed: %v", strings.Join(e.Args, " "), location, e.Err)
	if e.Stdout != "" {
		message += fmt.Sprintf("; stdout: %q", e.Stdout)
	}
	if e.Stderr != "" {
		message += fmt.Sprintf("; stderr: %q", e.Stderr)
	}
	return message
}

// Unwrap returns the underlying command error.
func (e *CommandError) Unwrap() error {
	return e.Err
}

// Verify checks that git can be executed.
func (c *Client) Verify(ctx context.Context) error {
	_, err := c.run(ctx, "", "--version")
	return err
}

// Clone clones one remote branch into destination.
func (c *Client) Clone(ctx context.Context, repository, destination, branch string) error {
	if err := require("repository", repository); err != nil {
		return err
	}
	if err := require("destination", destination); err != nil {
		return err
	}
	if err := require("branch", branch); err != nil {
		return err
	}

	_, err := c.run(ctx, "", "clone", "--branch", branch, "--single-branch", "--", repository, destination)
	return err
}

// UpdateMirror fetches branch from origin and makes the local branch and
// worktree exactly match the latest remote commit.
func (c *Client) UpdateMirror(ctx context.Context, repository, branch string) error {
	if err := require("repository", repository); err != nil {
		return err
	}
	if err := require("branch", branch); err != nil {
		return err
	}

	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	if _, err := c.run(ctx, repository, "fetch", "--prune", "origin", refspec); err != nil {
		return fmt.Errorf("fetch mirror branch %q: %w", branch, err)
	}
	if _, err := c.run(ctx, repository, "checkout", "--force", "-B", branch, "refs/remotes/origin/"+branch); err != nil {
		return fmt.Errorf("check out mirror branch %q: %w", branch, err)
	}
	if _, err := c.run(ctx, repository, "reset", "--hard", "refs/remotes/origin/"+branch); err != nil {
		return fmt.Errorf("reset mirror branch %q: %w", branch, err)
	}
	if _, err := c.run(ctx, repository, "clean", "-ffdx"); err != nil {
		return fmt.Errorf("clean mirror worktree for branch %q: %w", branch, err)
	}
	return nil
}

// IsDirty reports whether repository has tracked, untracked, or ignored changes
// under path.
func (c *Client) IsDirty(ctx context.Context, repository, path string) (bool, error) {
	if err := require("repository", repository); err != nil {
		return false, err
	}
	if err := require("path", path); err != nil {
		return false, err
	}

	stdout, err := c.run(
		ctx,
		repository,
		"--literal-pathspecs",
		"status",
		"--porcelain=v1",
		"-z",
		"--untracked-files=all",
		"--ignored=matching",
		"--",
		path,
	)
	if err != nil {
		return false, err
	}
	return len(stdout) > 0, nil
}

// StageAssignment stages changes under assignmentDir, including deletions.
func (c *Client) StageAssignment(ctx context.Context, repository, assignmentDir string) error {
	if err := require("repository", repository); err != nil {
		return err
	}
	if err := require("assignment directory", assignmentDir); err != nil {
		return err
	}

	_, err := c.run(ctx, repository, "--literal-pathspecs", "add", "--all", "--", assignmentDir)
	return err
}

// Commit creates a local commit from the staged changes.
func (c *Client) Commit(ctx context.Context, repository, message string) error {
	if err := require("repository", repository); err != nil {
		return err
	}
	if err := require("commit message", message); err != nil {
		return err
	}

	_, err := c.run(ctx, repository, "commit", "--message", message)
	return err
}

// CommitAssignment creates a local commit containing only assignmentDir,
// leaving any unrelated staged changes in the index.
func (c *Client) CommitAssignment(ctx context.Context, repository, assignmentDir, message string) error {
	if err := require("repository", repository); err != nil {
		return err
	}
	if err := require("assignment directory", assignmentDir); err != nil {
		return err
	}
	if err := require("commit message", message); err != nil {
		return err
	}

	_, err := c.run(ctx, repository, "--literal-pathspecs", "commit", "--only", "--message", message, "--", assignmentDir)
	return err
}

func (c *Client) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if c == nil || c.runner == nil {
		return nil, errors.New("git runner is not configured")
	}

	stdout, stderr, err := c.runner.Run(ctx, dir, args...)
	if err != nil {
		return nil, &CommandError{
			Dir:    dir,
			Args:   append([]string(nil), args...),
			Stdout: strings.TrimSpace(string(stdout)),
			Stderr: strings.TrimSpace(string(stderr)),
			Err:    err,
		}
	}
	return stdout, nil
}

func require(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir

	var stdout strings.Builder
	var stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return []byte(stdout.String()), []byte(stderr.String()), err
}
