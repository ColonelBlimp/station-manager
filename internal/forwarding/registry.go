package forwarding

import (
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Constructor builds a Forwarder from a ForwarderConfig. Each concrete
// forwarder package (internal/forwarding/qrz, .../stub, etc.) registers
// its constructor under its type name via Register — typically from an
// init() function. The worker layer looks up the constructor via Build
// at daemon startup.
type Constructor func(types.ForwarderConfig) (Forwarder, error)

var (
	registryMu sync.Mutex
	registry   = map[string]Constructor{}
)

// Register adds a forwarder constructor under the given type name.
// Intended to be called from a forwarder package's init() function.
//
// Panics on empty type, nil constructor, or duplicate registration —
// every one of these is a bug in the binary, not a runtime condition.
func Register(typeName string, ctor Constructor) {
	if typeName == "" {
		panic("forwarding.Register: empty type name")
	}
	if ctor == nil {
		panic("forwarding.Register: nil constructor for " + typeName)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[typeName]; exists {
		panic("forwarding.Register: type already registered: " + typeName)
	}
	registry[typeName] = ctor
}

// Build constructs a Forwarder from a ForwarderConfig. Returns an error
// if the type is not registered or if the constructor rejects the
// config (bad credentials, etc.).
func Build(fc types.ForwarderConfig) (Forwarder, error) {
	const op errors.Op = "forwarding.Build"

	registryMu.Lock()
	ctor, ok := registry[fc.Type]
	registryMu.Unlock()

	if !ok {
		return nil, errors.New(op).WithMsgf("unknown forwarder type %q (for %q)", fc.Type, fc.Name)
	}

	fwd, err := ctor(fc)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("build forwarder %q", fc.Name)
	}
	return fwd, nil
}

// IsRegistered reports whether a forwarder type name has a constructor
// registered. Useful at startup when the daemon wants to validate every
// configured forwarder type without actually building them yet.
func IsRegistered(typeName string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, ok := registry[typeName]
	return ok
}
