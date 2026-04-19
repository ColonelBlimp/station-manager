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
	registryMu      sync.Mutex
	registry        = map[string]Constructor{}
	defaultRetryMap = map[string]types.RetryConfig{}
)

// Register adds a forwarder constructor under the given type name.
// Intended to be called from a forwarder package's init() function.
//
// Panics on empty type, nil constructor, or duplicate registration —
// every one of these is a bug (code error), not a runtime condition.
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

// Build constructs a Forwarder from the given ForwarderConfig. Returns an error
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
// configured forwarder type before building it.
func IsRegistered(typeName string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, ok := registry[typeName]
	return ok
}

// RegisterDefaultRetry records the type-specific default RetryConfig
// for forwarder typeName. Each concrete forwarder package supplies
// its own, tuned to the upstream's tolerances (QRZ web API vs.
// ClubLog daily batches vs. LoTW's slow acknowledgements), so a
// single one-size-fits-all default doesn't have to live in
// cmd/smd/main.go. The daemon looks up the per-type default via
// DefaultRetryFor when the operator's config doesn't override
// `retry` explicitly.
//
// Panics on empty typeName, an obviously-invalid RetryConfig, or
// duplicate registration — all three are bugs in the binary, not
// runtime conditions.
//
// Validation here matches worker.New's constructor checks so an
// invalid default never silently survives to worker-spawn.
func RegisterDefaultRetry(typeName string, retry types.RetryConfig) {
	if typeName == "" {
		panic("forwarding.RegisterDefaultRetry: empty type name")
	}
	if retry.MaxAttempts < 1 {
		panic("forwarding.RegisterDefaultRetry: MaxAttempts must be >= 1 for " + typeName)
	}
	if retry.InitialBackoffSec < 1 {
		panic("forwarding.RegisterDefaultRetry: InitialBackoffSec must be >= 1 for " + typeName)
	}
	if retry.MaxBackoffSec < retry.InitialBackoffSec {
		panic("forwarding.RegisterDefaultRetry: MaxBackoffSec must be >= InitialBackoffSec for " + typeName)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := defaultRetryMap[typeName]; exists {
		panic("forwarding.RegisterDefaultRetry: type already has a default: " + typeName)
	}
	defaultRetryMap[typeName] = retry
}

// DefaultRetryFor returns the registered default RetryConfig for
// typeName, or (zero, false) if none was registered. The daemon
// treats the absence as a config error — forwarder packages should
// always register one, since retry values are upstream-specific.
func DefaultRetryFor(typeName string) (types.RetryConfig, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	retry, ok := defaultRetryMap[typeName]
	return retry, ok
}
