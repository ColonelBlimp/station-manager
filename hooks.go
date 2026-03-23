package iocdi

import (
	"reflect"
	"sync/atomic"
)

// LiteralProvider is a hook invoked when a dependency with a given id is missing.
// - id: the value of the `di.inject` tag for the missing dependency
// - targetType: the type expected for that dependency (e.g., reflect.TypeOf("") for string)
// Returns:
// - value: the literal value to use for injection
// - found: whether a value is available
// - err: any error occurred while sourcing the value (e.g., parsing, I/O)
type LiteralProvider func(id string, targetType reflect.Type) (value any, found bool, err error)

// literalProvider holds the global hook. It is guarded with atomic.Value to allow
// lock-free, race-free reads during injection while supporting concurrent updates.
var literalProvider atomic.Value // stores LiteralProvider

func init() {
	// Initialize with typed nil to fix the stored type for atomic.Value.
	literalProvider.Store(LiteralProvider(nil))
}

// SetLiteralProvider installs a global literal provider hook.
// A typical implementation might read env vars, files, flags, or other configuration sources.
func SetLiteralProvider(p LiteralProvider) {
	literalProvider.Store(p)
}

// loadLiteralProvider returns the currently installed literal provider (may be nil).
func loadLiteralProvider() LiteralProvider {
	if provider, ok := literalProvider.Load().(LiteralProvider); ok {
		return provider
	}
	return nil
}
