package grading

import "context"

// Result is the outcome of executing one Check.
type Result struct {
	Name   string
	Passed bool
	Reason string
}

// Check is a single required, executable unit.
// Implementations are constructed already bound to their inputs (directories,
// spec, expected files, ...), so Execute only needs a context.
type Check interface {
	// Name identifies the check.
	Name() string
	// Execute runs the check and returns its outcome.
	Execute(ctx context.Context) (Result, error)
}

// funcCheck adapts a name and run function into a Check.
type funcCheck struct {
	name string
	run  func(ctx context.Context) (Result, error)
}

func (c *funcCheck) Name() string { return c.name }

func (c *funcCheck) Execute(ctx context.Context) (Result, error) {
	return c.run(ctx)
}

// NewCheck returns a Check named name, backed by run.
func NewCheck(name string, run func(ctx context.Context) (Result, error)) Check {
	return &funcCheck{name: name, run: run}
}
