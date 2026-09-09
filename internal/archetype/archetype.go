package archetype

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ludanortmun/sync-assign/internal/config"
	"github.com/ludanortmun/sync-assign/internal/grading"
)

// CheckInput supplies assignment context to archetype checks.
type CheckInput struct {
	StudentDir string
	TeacherDir string
	Spec       config.AssignmentSpec
	// CommitTime is zero when there is no historical commit to check against
	// (e.g. when invoked from `check`).
	CommitTime time.Time
}

// TestInput supplies the suite execution directory and options.
type TestInput struct {
	Dir     string
	Options map[string]string
}

// Archetype defines an assignment's checks and file-discovery conventions. Archetypes
// return grading.Check values already bound to their inputs so a caller can
// simply execute every returned Check in a loop.
type Archetype interface {
	// Name returns the archetype's identifier, e.g. "python-pytest".
	Name() string
	// Checks returns archetype-specific checks in addition to the unit suite.
	Checks(input CheckInput) ([]grading.Check, error)
	// Tests returns one check for the entire unit test suite.
	Tests(input TestInput) ([]grading.Check, error)
	// DefaultImmutableFiles returns the archetype's default list of files
	// students must not modify (paths relative to dir), used when the
	// assignment spec's immutable-files is not explicitly set.
	DefaultImmutableFiles(dir string, options map[string]string) ([]string, error)
}

var registry = make(map[string]Archetype)

// Get returns the registered archetype named name.
func Get(name string) (Archetype, bool) {
	archetype, ok := registry[name]
	return archetype, ok
}

// Register adds a built-in archetype to the registry.
func Register(a Archetype) {
	if a == nil {
		panic("archetype: Register(nil)")
	}
	name := strings.TrimSpace(a.Name())
	if name == "" {
		panic("archetype: Register with empty name")
	}
	if name != a.Name() {
		panic(fmt.Sprintf("archetype: Register name %q has surrounding whitespace", a.Name()))
	}

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("archetype: Register duplicate %q", name))
	}
	registry[name] = a
}

// Names returns the sorted names of registered archetypes.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
