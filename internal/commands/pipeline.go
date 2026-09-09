package commands

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ludanortmun/sync-assign/internal/archetype"
	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/grading"
)

// assignmentPipelineInput bundles everything needed to run one assignment's
// checks, regardless of whether the target is the
// current working tree (check) or a checked-out worktree at a historical
// commit (grade).
type assignmentPipelineInput struct {
	Timeout              *time.Duration
	AssignmentID         string
	StudentAssignmentDir string
	TeacherAssignmentDir string
	Spec                 config.AssignmentSpec
	Authoritative        bool
	// IsCheck distinguishes a `check` run (no historical commit, so
	// commit-before-due-date is not applicable and is omitted entirely) from
	// a `grade` run. It usually moves together with Authoritative, but is
	// tracked separately since they mean different things.
	IsCheck    bool
	CommitTime time.Time
}

// Assignment resolves an assignment's archetype and file lists once, then
// exposes its applicable checks as plain, composable grading.Check
// values that a caller can execute in a simple loop.
type Assignment struct {
	id          string
	archetype   archetype.Archetype
	spec        config.AssignmentSpec
	studentDir  string
	teacherDir  string
	immutable   []string
	required    []string
	commitTime  time.Time
	setupErrors []grading.Check
}

// NewAssignment resolves the archetype named by spec.Archetype and the
// effective file lists: immutable additions supplement archetype protection;
// required files use the explicit list or filesystem defaults.
func NewAssignment(
	id string,
	studentAssignmentDir string,
	teacherAssignmentDir string,
	spec config.AssignmentSpec,
	commitTime time.Time,
) *Assignment {
	arch, ok := archetype.Get(spec.Archetype)
	var immutableFiles []string
	var setupErrors []grading.Check
	if !ok {
		setupErrors = append(setupErrors, failedCheck("archetype", fmt.Errorf(
			"unsupported archetype %q (supported: %s)",
			spec.Archetype,
			strings.Join(archetype.Names(), ", "),
		)))
	} else {
		var err error
		immutableFiles, err = arch.DefaultImmutableFiles(teacherAssignmentDir, spec.ArchetypeOptions)
		if err != nil {
			setupErrors = append(setupErrors, failedCheck("immutable-files-discovery", err))
		}
	}
	immutableFiles = append(immutableFiles, spec.ImmutableFiles...)

	requiredFiles := spec.RequiredFiles
	if len(requiredFiles) == 0 {
		var err error
		requiredFiles, err = defaultRequiredFiles(teacherAssignmentDir)
		if err != nil {
			setupErrors = append(setupErrors, failedCheck("required-files-discovery", err))
		}
	}

	return &Assignment{
		id:          id,
		archetype:   arch,
		spec:        spec,
		studentDir:  studentAssignmentDir,
		teacherDir:  teacherAssignmentDir,
		immutable:   immutableFiles,
		required:    requiredFiles,
		commitTime:  commitTime,
		setupErrors: setupErrors,
	}
}

// Checks returns configured and mandatory checks. When isCheck is true, commit-before-due-date is
// omitted entirely, since there is no historical commit to check against.
func (a *Assignment) Checks(isCheck bool) ([]grading.Check, error) {
	checkNames := grading.EffectiveChecks(a.spec)
	if !slices.Contains(checkNames, grading.CheckImmutableFilesUnmodified) {
		checkNames = append(checkNames, grading.CheckImmutableFilesUnmodified)
	}
	if isCheck {
		checkNames = removeCheck(checkNames, grading.CheckCommitBeforeDueDate)
	}

	checks, err := grading.BuildChecks(checkNames, grading.CheckContext{
		AssignmentDir:        a.studentDir,
		TeacherAssignmentDir: a.teacherDir,
		ImmutableFiles:       a.immutable,
		RequiredFiles:        a.required,
		DueDate:              a.spec.DueDate,
		CommitTime:           a.commitTime,
	})
	if err != nil {
		checks = append(checks, failedCheck("assignment-checks", err))
	}

	var archetypeChecks []grading.Check
	if a.archetype != nil {
		archetypeChecks, err = a.archetype.Checks(archetype.CheckInput{
			StudentDir: a.studentDir,
			TeacherDir: a.teacherDir,
			Spec:       a.spec,
			CommitTime: a.commitTime,
		})
		if err != nil {
			archetypeChecks = append(archetypeChecks, failedCheck("archetype-checks", err))
		}
	}
	if a.spec.Archetype == "python-pytest" {
		checks = append(checks, grading.NewCheck("test-file-coverage", func(ctx context.Context) (grading.Result, error) {
			files, err := a.archetype.DefaultImmutableFiles(a.studentDir, a.spec.ArchetypeOptions)
			if err != nil {
				return grading.Result{}, err
			}
			for _, file := range files {
				if !slices.Contains(a.immutable, file) {
					return grading.Result{Name: "test-file-coverage", Reason: "unexpected test or pytest configuration file: " + file}, nil
				}
			}
			return grading.Result{Name: "test-file-coverage", Passed: true}, nil
		}))
	}

	checks = append(checks, a.setupErrors...)
	return append(checks, archetypeChecks...), nil
}

// Tests returns the assignment's suite check.
func (a *Assignment) Tests() ([]grading.Check, error) {
	if a.archetype == nil {
		return nil, fmt.Errorf("cannot construct unit suite for unsupported archetype %q", a.spec.Archetype)
	}
	tests, err := a.archetype.Tests(archetype.TestInput{
		Dir:     a.studentDir,
		Options: a.spec.ArchetypeOptions,
	})
	if err != nil {
		return tests, fmt.Errorf("build %s archetype tests: %w", a.spec.Archetype, err)
	}
	return tests, nil
}

func removeCheck(checkNames []string, name string) []string {
	filtered := make([]string, 0, len(checkNames))
	for _, checkName := range checkNames {
		if checkName == name {
			continue
		}
		filtered = append(filtered, checkName)
	}
	return filtered
}

func checkTimeout(spec config.AssignmentSpec, override *time.Duration) (time.Duration, error) {
	timeout := 120 * time.Second
	if spec.Timeout != nil {
		timeout = *spec.Timeout
	}
	if override != nil {
		timeout = *override
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("timeout must be positive")
	}
	return timeout, nil
}

func failedCheck(name string, err error) grading.Check {
	return grading.NewCheck(name, func(context.Context) (grading.Result, error) {
		return grading.Result{}, err
	})
}

// runAssignmentPipeline executes every check, preserving diagnostics on errors.
func runAssignmentPipeline(ctx context.Context, in assignmentPipelineInput) (grading.Report, error) {
	timeout, err := checkTimeout(in.Spec, in.Timeout)
	if err != nil {
		return grading.Report{}, err
	}
	assignment := NewAssignment(
		in.AssignmentID,
		in.StudentAssignmentDir,
		in.TeacherAssignmentDir,
		in.Spec,
		in.CommitTime,
	)
	checks, err := assignment.Checks(in.IsCheck)
	if err != nil {
		checks = append(checks, failedCheck("assignment-checks", err))
	}
	tests, err := assignment.Tests()
	if err != nil {
		tests = append(tests, failedCheck("unit-tests", err))
	}
	if len(tests) == 0 {
		tests = append(tests, failedCheck("unit-tests", fmt.Errorf("archetype supplied no unit test check")))
	}
	checks = append(checks, tests...)

	results := make([]grading.Result, 0, len(checks))
	for _, check := range checks {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		result, err := check.Execute(checkCtx)
		if checkCtx.Err() != nil {
			err = errors.Join(err, checkCtx.Err())
		}
		cancel()
		if err != nil {
			result.Passed = false
			if result.Reason != "" {
				result.Reason += "\n"
			}
			result.Reason += err.Error()
		}
		result.Name = check.Name()
		results = append(results, result)
	}

	return grading.BuildReport(in.AssignmentID, in.Authoritative, results), nil
}

func defaultRequiredFiles(teacherAssignmentDir string) ([]string, error) {
	requiredFiles := make([]string, 0)
	err := filepath.WalkDir(teacherAssignmentDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %q: %w", path, err)
		}
		if entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(teacherAssignmentDir, path)
		if err != nil {
			return fmt.Errorf("relativize %q: %w", path, err)
		}
		relativePath = filepath.ToSlash(relativePath)
		if relativePath == config.AssignmentSpecFilename {
			return nil
		}
		requiredFiles = append(requiredFiles, relativePath)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(requiredFiles)
	return requiredFiles, nil
}
