package config

// Optional represents a command-line value and whether the flag was explicitly
// provided. This avoids treating a flag's zero value as an override.
type Optional[T any] struct {
	Value T
	Set   bool
}

func Some[T any](value T) Optional[T] {
	return Optional[T]{Value: value, Set: true}
}

// Resolve applies flag > config > default precedence.
func Resolve[T any](flag Optional[T], configured *T, fallback T) T {
	if flag.Set {
		return flag.Value
	}
	if configured != nil {
		return *configured
	}
	return fallback
}
