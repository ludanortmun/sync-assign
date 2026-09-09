package archetype

import (
	"reflect"
	"sort"
	"testing"

	"github.com/ludanortmun/sync-assign/internal/grading"
)

func TestBuiltInArchetypesRegistered(t *testing.T) {
	// Not run in parallel: sibling tests in this file mutate the shared
	// package-level registry without synchronization.
	names := Names()
	for _, want := range []string{"generic-shell-script", "python-pytest"} {
		if _, ok := Get(want); !ok {
			t.Fatalf("Get(%q) ok = false, want true", want)
		}
		if !containsString(names, want) {
			t.Fatalf("Names() = %v, want containing %q", names, want)
		}
	}
}

func TestRegisterAndGet(t *testing.T) {
	originalRegistry := registry
	registry = make(map[string]Archetype)
	defer func() {
		registry = originalRegistry
	}()

	custom := stubArchetype{name: "custom"}
	Register(custom)

	got, ok := Get("custom")
	if !ok {
		t.Fatal("Get(\"custom\") ok = false, want true")
	}
	if got.Name() != custom.Name() {
		t.Fatalf("Get(\"custom\").Name() = %q, want %q", got.Name(), custom.Name())
	}
}

func TestNamesSorted(t *testing.T) {
	originalRegistry := registry
	registry = map[string]Archetype{
		"zeta":  stubArchetype{name: "zeta"},
		"alpha": stubArchetype{name: "alpha"},
		"beta":  stubArchetype{name: "beta"},
	}
	defer func() {
		registry = originalRegistry
	}()

	got := Names()
	want := []string{"alpha", "beta", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestRegisterPanics(t *testing.T) {
	tests := map[string]struct {
		archetype Archetype
	}{
		"nil":                    {archetype: nil},
		"empty name":             {archetype: stubArchetype{}},
		"surrounding whitespace": {archetype: stubArchetype{name: " custom "}},
		"duplicate":              {archetype: stubArchetype{name: "python-pytest"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			originalRegistry := registry
			registry = make(map[string]Archetype)
			if name == "duplicate" {
				registry["python-pytest"] = stubArchetype{name: "python-pytest"}
			}
			defer func() {
				registry = originalRegistry
			}()

			defer func() {
				if recover() == nil {
					t.Fatal("Register() did not panic")
				}
			}()
			Register(test.archetype)
		})
	}
}

func containsString(values []string, target string) bool {
	values = append([]string(nil), values...)
	sort.Strings(values)
	index := sort.SearchStrings(values, target)
	return index < len(values) && values[index] == target
}

type stubArchetype struct {
	name string
}

func (s stubArchetype) Name() string {
	return s.name
}

func (stubArchetype) Checks(CheckInput) ([]grading.Check, error) {
	return nil, nil
}

func (stubArchetype) Tests(TestInput) ([]grading.Check, error) {
	return nil, nil
}

func (stubArchetype) DefaultImmutableFiles(string, map[string]string) ([]string, error) {
	return nil, nil
}
