// Package grader evaluates student assignments with an ordered set of checks.
package grader

// Environment identifies the student assignment and teacher template directories.
type Environment struct {
	StudentDir string
	TeacherDir string
}

// Status describes the outcome of a checker.
type Status string

const (
	Passed  Status = "passed"
	Failed  Status = "failed"
	Skipped Status = "skipped"
)

// Result is the outcome of one checker.
type Result struct {
	Checker string
	Status  Status
	Detail  string
}

// Checker is a named grading check.
type Checker struct {
	Name  string
	Check func(Environment) Result
}

// Report contains checker results in execution order.
type Report struct {
	Results []Result
}

// Passed reports whether no checker failed.
func (report Report) Passed() bool {
	for _, result := range report.Results {
		if result.Status == Failed {
			return false
		}
	}
	return true
}

// Run executes each checker in order.
func Run(environment Environment, checkers []Checker) Report {
	report := Report{Results: make([]Result, 0, len(checkers))}
	for _, checker := range checkers {
		if checker.Check == nil {
			report.Results = append(report.Results, Result{
				Checker: checker.Name,
				Status:  Failed,
				Detail:  "checker has no check function",
			})
			continue
		}

		result := checker.Check(environment)
		result.Checker = checker.Name
		report.Results = append(report.Results, result)
	}
	return report
}

// CheckIf conditionally runs checker. A false or nil condition skips it.
func CheckIf(checker Checker, condition func(Environment) bool) Checker {
	return Checker{
		Name: checker.Name,
		Check: func(environment Environment) Result {
			if condition == nil {
				return Result{
					Checker: checker.Name,
					Status:  Skipped,
					Detail:  "condition is not configured",
				}
			}
			if !condition(environment) {
				return Result{
					Checker: checker.Name,
					Status:  Skipped,
					Detail:  "condition not met",
				}
			}
			if checker.Check == nil {
				return Result{
					Checker: checker.Name,
					Status:  Failed,
					Detail:  "checker has no check function",
				}
			}

			result := checker.Check(environment)
			result.Checker = checker.Name
			return result
		},
	}
}
