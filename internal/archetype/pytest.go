package archetype

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ludanortmun/sync-assign/internal/grading"
)

func init() {
	Register(pythonPytestArchetype{})
}

type pythonPytestArchetype struct{}

func (pythonPytestArchetype) Name() string {
	return "python-pytest"
}

func (pythonPytestArchetype) DefaultImmutableFiles(dir string, options map[string]string) ([]string, error) {
	testRoot := dir
	if path := strings.TrimSpace(options["test-path"]); path != "" {
		if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("test-path must stay within the assignment")
		}
		testRoot = filepath.Join(dir, path)
	}

	files := make([]string, 0)
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk test path %q: %w", testRoot, err)
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		isTest := strings.HasSuffix(name, ".py") && (strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py") || name == "conftest.py")
		inTestPath := testRoot != dir && (path == testRoot || strings.HasPrefix(path, testRoot+string(filepath.Separator)))
		if !inTestPath && !isTest && name != "pytest.ini" && name != ".pytest.ini" && name != "pyproject.toml" && name != "setup.cfg" && name != "tox.ini" {
			return nil
		}

		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("make path %q relative to %q: %w", path, dir, err)
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func (pythonPytestArchetype) Checks(CheckInput) ([]grading.Check, error) {
	return nil, nil
}

// Tests returns one check for the complete pytest suite.
func (pythonPytestArchetype) Tests(input TestInput) ([]grading.Check, error) {
	suite := &pytestSuite{dir: input.Dir, options: input.Options}
	return []grading.Check{newSuiteCheck(suite.results)}, nil
}

// pytestSuite retains the suite outcomes for repeated inspection.
type pytestSuite struct {
	dir     string
	options map[string]string

	once     sync.Once
	outcomes map[string]testOutcome
	err      error
}

func (s *pytestSuite) results(ctx context.Context) (map[string]testOutcome, error) {
	s.once.Do(func() {
		s.outcomes, s.err = s.execute(ctx)
	})
	return s.outcomes, s.err
}

func (s *pytestSuite) execute(ctx context.Context) (map[string]testOutcome, error) {
	dir := s.dir
	options := s.options

	testPath := dir
	if path := strings.TrimSpace(options["test-path"]); path != "" {
		testPath = filepath.Join(dir, path)
	}

	requirementsPath := filepath.Join(dir, "requirements.txt")
	_, err := os.ReadFile(requirementsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("python-pytest archetype: requirements.txt not found in %q", dir)
		}
		return nil, fmt.Errorf("read requirements.txt in %q: %w", dir, err)
	}

	// A fresh environment also handles nested requirements, local packages,
	// interpreter changes and mutable dependency indexes without cache collisions.
	venvPath, err := os.MkdirTemp(dir, ".sync-assign-venv-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(venvPath)
	defer os.Remove(venvPath + ".lock")
	pythonPath := filepath.Join(venvPath, "bin", "python3")
	if err := prepareVenv(ctx, dir, venvPath, requirementsPath, runCommand); err != nil {
		return nil, err
	}

	resultsFile, err := os.CreateTemp(dir, ".sync-assign-pytest-*.xml")
	if err != nil {
		return nil, fmt.Errorf("create pytest junit xml file: %w", err)
	}
	resultsPath := resultsFile.Name()
	if err := resultsFile.Close(); err != nil {
		_ = os.Remove(resultsPath)
		return nil, fmt.Errorf("close pytest junit xml file %q: %w", resultsPath, err)
	}
	defer os.Remove(resultsPath)

	// Do not load pytest configuration or conftest.py from student ancestors.
	pytestConfig := os.DevNull
	for _, name := range []string{"pytest.ini", ".pytest.ini", "pyproject.toml", "tox.ini", "setup.cfg"} {
		filename := filepath.Join(dir, name)
		if _, err := os.Stat(filename); err == nil {
			pytestConfig = filename
			break
		}
	}
	command := exec.CommandContext(
		ctx,
		pythonPath,
		"-m",
		"pytest",
		"--tb=short",
		"-q",
		"-c", pytestConfig,
		"--rootdir="+dir,
		"--confcutdir="+dir,
		"-o", "python_files=test_*.py *_test.py",
		"--junitxml="+resultsPath,
		testPath,
	)
	command.Dir = dir
	boundCommand(command)
	output, runErr := command.CombinedOutput()
	if ctx.Err() != nil {
		runErr = ctx.Err()
	}
	if runErr != nil {
		runErr = fmt.Errorf("run pytest in %q: %w; output: %s", dir, runErr, output)
	}

	data, err := os.ReadFile(resultsPath)
	if err != nil {
		return nil, errors.Join(runErr, fmt.Errorf("read pytest junit xml %q: %w", resultsPath, err))
	}

	outcomes, err := parseJUnitXML(data)
	if err != nil {
		return nil, errors.Join(runErr, fmt.Errorf("parse pytest junit xml %q: %w", resultsPath, err))
	}
	return outcomes, runErr
}

func prepareVenv(ctx context.Context, dir, path, requirementsPath string, run func(context.Context, string, string, ...string) error) error {
	// Keep the lock outside the environment so incomplete environments can be
	// removed without letting another process lock a different inode.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open python venv lock: %w", err)
	}
	defer lock.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return fmt.Errorf("lock python venv: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	ready := filepath.Join(path, ".sync-assign-ready")
	complete := true
	for _, file := range []string{ready, filepath.Join(path, "bin", "python3")} {
		if _, err := os.Stat(file); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect python venv %q: %w", file, err)
			}
			complete = false
		}
	}
	if complete {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove incomplete python venv %q: %w", path, err)
	}
	if err := run(ctx, dir, "python3", "-m", "venv", path); err != nil {
		return fmt.Errorf("create python venv %q: %w", path, err)
	}
	if err := run(ctx, dir, filepath.Join(path, "bin", "pip"), "install", "-r", requirementsPath); err != nil {
		return fmt.Errorf("install python requirements from %q: %w", requirementsPath, err)
	}
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		return fmt.Errorf("mark python venv ready: %w", err)
	}
	return nil
}

func runCommand(ctx context.Context, dir, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	boundCommand(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %w; output: %q", name, strings.Join(args, " "), err, string(output))
	}

	return nil
}

// Kill descendants as well as the shell/installer, and bound pipe draining.
func boundCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitTestCase struct {
	ClassName string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Failure   *junitFailure `xml:"failure"`
	Error     *junitFailure `xml:"error"`
	Skipped   *junitFailure `xml:"skipped"`
}

type junitTestSuite struct {
	TestCases []junitTestCase  `xml:"testcase"`
	Suites    []junitTestSuite `xml:"testsuite"`
}

type junitTestSuites struct {
	Suites []junitTestSuite `xml:"testsuite"`
}

func parseJUnitXML(data []byte) (map[string]testOutcome, error) {
	var suites junitTestSuites
	if err := xml.Unmarshal(data, &suites); err == nil && len(suites.Suites) > 0 {
		return outcomesFromSuites(suites.Suites), nil
	}

	var suite junitTestSuite
	if err := xml.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("decode junit xml: %w", err)
	}
	if len(suite.TestCases) == 0 && len(suite.Suites) == 0 {
		return nil, fmt.Errorf("decode junit xml: no testcases found")
	}
	return outcomesFromSuites([]junitTestSuite{suite}), nil
}

func outcomesFromSuites(suites []junitTestSuite) map[string]testOutcome {
	outcomes := make(map[string]testOutcome)
	for _, suite := range suites {
		collectJUnitSuiteOutcomes(suite, outcomes)
	}
	return outcomes
}

func collectJUnitSuiteOutcomes(suite junitTestSuite, outcomes map[string]testOutcome) {
	for _, testCase := range suite.TestCases {
		id, outcome := junitTestOutcome(testCase)
		if previous, exists := outcomes[id]; exists && !previous.Passed {
			continue
		}
		outcomes[id] = outcome
	}
	for _, child := range suite.Suites {
		collectJUnitSuiteOutcomes(child, outcomes)
	}
}

func junitTestOutcome(testCase junitTestCase) (string, testOutcome) {
	outcome := testOutcome{Passed: true}

	if testCase.Skipped != nil {
		outcome.Passed = false
		outcome.Message = "skipped: " + junitFailureMessage(*testCase.Skipped)
	}
	if testCase.Failure != nil {
		outcome.Passed = false
		outcome.Message = junitFailureMessage(*testCase.Failure)
	}
	if testCase.Error != nil {
		outcome.Passed = false
		outcome.Message = junitFailureMessage(*testCase.Error)
	}
	return junitTestID(testCase), outcome
}

func junitTestID(testCase junitTestCase) string {
	if strings.TrimSpace(testCase.ClassName) == "" {
		return testCase.Name
	}
	return testCase.ClassName + "::" + testCase.Name
}

func junitFailureMessage(failure junitFailure) string {
	if message := strings.TrimSpace(failure.Text); message != "" {
		return message
	}
	return strings.TrimSpace(failure.Message)
}
