package archetype

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ludanortmun/sync-assign/internal/grading"
)

func init() {
	Register(genericShellScriptArchetype{})
}

type genericShellScriptArchetype struct{}

func (genericShellScriptArchetype) Name() string {
	return "generic-shell-script"
}

func (genericShellScriptArchetype) DefaultImmutableFiles(_ string, options map[string]string) ([]string, error) {
	command := strings.TrimSpace(options["command"])
	if command == "" {
		return nil, fmt.Errorf("generic-shell-script archetype: archetype-options.command is required")
	}
	if filepath.IsAbs(command) || strings.ContainsAny(command, " \t\n;&|$`<>") || command == ".." || strings.HasPrefix(filepath.Clean(command), "../") {
		return nil, fmt.Errorf("archetype-options.command must be a relative script path without shell arguments")
	}
	return []string{command}, nil
}

func (genericShellScriptArchetype) Checks(CheckInput) ([]grading.Check, error) {
	return nil, nil
}

// Tests returns one check for the complete script suite.
func (genericShellScriptArchetype) Tests(input TestInput) ([]grading.Check, error) {
	suite := &shellScriptSuite{dir: input.Dir, options: input.Options}
	return []grading.Check{newSuiteCheck(suite.results)}, nil
}

// shellScriptSuite runs the configured shell script at most once (via
// sync.Once), retaining its parsed PASS/FAIL outcomes.
type shellScriptSuite struct {
	dir     string
	options map[string]string

	once     sync.Once
	outcomes map[string]testOutcome
	err      error
}

func (s *shellScriptSuite) results(ctx context.Context) (map[string]testOutcome, error) {
	s.once.Do(func() {
		s.outcomes, s.err = s.execute(ctx)
	})
	return s.outcomes, s.err
}

func (s *shellScriptSuite) execute(ctx context.Context) (map[string]testOutcome, error) {
	commandText := strings.TrimSpace(s.options["command"])
	if commandText == "" {
		return nil, fmt.Errorf("generic-shell-script archetype: archetype-options.command is required")
	}

	command := exec.CommandContext(ctx, "/bin/sh", "-c", commandText)
	command.Dir = s.dir
	boundCommand(command)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}

	outcomes := make(map[string]testOutcome)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r \t")
		switch {
		case strings.HasPrefix(line, "PASS "):
			name := strings.TrimSpace(line[len("PASS "):])
			if previous, exists := outcomes[name]; !exists || previous.Passed {
				outcomes[name] = testOutcome{Passed: name != "", Message: ""}
			}
		case strings.HasPrefix(line, "FAIL "):
			outcomes[strings.TrimSpace(line[len("FAIL "):])] = testOutcome{Passed: false}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return outcomes, fmt.Errorf("scan shell-script output: %w; output: %s; execution error: %v", scanErr, output, err)
	}

	if err != nil {
		return outcomes, fmt.Errorf("run shell-script command %q in %q: %w; output: %s", commandText, s.dir, err, output)
	}
	if len(outcomes) == 0 {
		return outcomes, fmt.Errorf("script produced no recognized PASS/FAIL cases; output: %s", output)
	}

	return outcomes, nil
}
