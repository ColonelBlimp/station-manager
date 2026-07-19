package forwarding

import (
	"regexp"
	"sort"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
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
	registryMu          sync.Mutex
	registry            = map[string]Constructor{}
	defaultRetryMap     = map[string]types.RetryConfig{}
	supportedActions    = map[string][]Action{}
	descriptors         = map[string]TypeDescriptor{}
	defaultEndpointsMap = map[string]map[string]string{}
	adifPrefixMap       = map[string]string{}
	rowMirrorTypes      = map[string]struct{}{}
	noBulkBackfillTypes = map[string]struct{}{}
)

// adifPrefixPattern enforces a safe shape for the ADIF upload-status field prefix
// a type stamps (e.g. "QRZCOM" → qrzcom_qso_upload_status). It mirrors the
// validator in the sqlite stamp writer so a bad prefix is rejected at
// registration, not at first upload. (App-defined APP_SM_* prefixes, which carry
// underscores, are not yet supported by the stamp writer — a future relaxation.)
var adifPrefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

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

// RegisterSupportedActions records which QSO lifecycle actions forwarder
// typeName can actually carry out. QRZ supports insert/update/delete;
// ClubLog's real-time API supports insert/delete only (it cannot edit a
// logged QSO). The daemon consults this at config time in two places:
//
//   - applyDefaults fills an OMITTED action_filter with the supported set
//     (instead of a blanket insert/update/delete), so a natural config
//     never queues rows the forwarder is bound to reject.
//   - validateForwarders rejects an EXPLICIT action_filter that names an
//     unsupported action, surfacing the mistake at load time rather than
//     as a stream of failed upload rows.
//
// Registration is OPTIONAL: a forwarder that registers nothing keeps the
// historical "all three actions" default and is not validated against a
// supported set — so this is additive and never regresses existing
// forwarders.
//
// Panics on the usual programmer errors (empty typeName, empty set,
// unknown or duplicate action, duplicate registration) — all bugs in the
// binary, not runtime conditions.
func RegisterSupportedActions(typeName string, actions []Action) {
	if typeName == "" {
		panic("forwarding.RegisterSupportedActions: empty type name")
	}
	if len(actions) == 0 {
		panic("forwarding.RegisterSupportedActions: empty action set for " + typeName)
	}
	seen := make(map[Action]struct{}, len(actions))
	for _, a := range actions {
		switch a {
		case action.Insert, action.Update, action.Delete:
		default:
			panic("forwarding.RegisterSupportedActions: unknown action " + string(a) + " for " + typeName)
		}
		if _, dup := seen[a]; dup {
			panic("forwarding.RegisterSupportedActions: duplicate action " + string(a) + " for " + typeName)
		}
		seen[a] = struct{}{}
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := supportedActions[typeName]; exists {
		panic("forwarding.RegisterSupportedActions: type already registered: " + typeName)
	}
	supportedActions[typeName] = append([]Action(nil), actions...)
}

// SupportedActionsFor returns the registered supported-action set for
// typeName and whether one was registered. The returned slice is a copy,
// so callers can't mutate the registry's state.
func SupportedActionsFor(typeName string) ([]Action, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	a, ok := supportedActions[typeName]
	if !ok {
		return nil, false
	}
	return append([]Action(nil), a...), true
}

// RegisterDefaultEndpoints records a forwarder type's default upstream URLs,
// keyed by the action they serve ("insert"/"update"/"delete"). The daemon seeds
// these into a forwarder's config (config.applyDefaults) so the endpoints are
// config-driven + overridable without a recompile (ADR 0039), while the package
// const stays the canonical default. From the forwarder package's init().
//
// Panics on the usual programmer errors (empty type/map, empty key or value,
// duplicate registration) — all binary bugs.
func RegisterDefaultEndpoints(typeName string, endpoints map[string]string) {
	if typeName == "" {
		panic("forwarding.RegisterDefaultEndpoints: empty type name")
	}
	if len(endpoints) == 0 {
		panic("forwarding.RegisterDefaultEndpoints: empty endpoint map for " + typeName)
	}
	for k, v := range endpoints {
		if k == "" || v == "" {
			panic("forwarding.RegisterDefaultEndpoints: empty key/value for " + typeName)
		}
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := defaultEndpointsMap[typeName]; exists {
		panic("forwarding.RegisterDefaultEndpoints: type already registered: " + typeName)
	}
	cp := make(map[string]string, len(endpoints))
	for k, v := range endpoints {
		cp[k] = v
	}
	defaultEndpointsMap[typeName] = cp
}

// DefaultEndpointsFor returns a copy of the registered default endpoint map for
// typeName, or (nil, false) if none was registered.
func DefaultEndpointsFor(typeName string) (map[string]string, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	m, ok := defaultEndpointsMap[typeName]
	if !ok {
		return nil, false
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp, true
}

// RegisterAdifPrefix records the ADIF upload-status field prefix forwarder
// typeName stamps on a successfully-uploaded QSO (e.g. qrz → "QRZCOM", whose
// worker writes qrzcom_qso_upload_status). The daemon reads it back via
// AdifPrefixForType to resolve a forwarder to its stamp field WITHOUT building
// the forwarder — used by the manual-backfill skip-check and the
// `missing_from` logbook filter (ADR 0039), which both key "uploaded to X?" on
// the durable ADIF stamp. From the forwarder package's init().
//
// Registration is OPTIONAL: a type that stamps nothing (the dev/test stub,
// custom webhooks) registers none and reads back (\"\", false). Panics on empty
// typeName, a prefix that fails adifPrefixPattern, or duplicate registration —
// all binary bugs.
func RegisterAdifPrefix(typeName, prefix string) {
	if typeName == "" {
		panic("forwarding.RegisterAdifPrefix: empty type name")
	}
	if !adifPrefixPattern.MatchString(prefix) {
		panic("forwarding.RegisterAdifPrefix: invalid prefix " + prefix + " for " + typeName)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := adifPrefixMap[typeName]; exists {
		panic("forwarding.RegisterAdifPrefix: type already registered: " + typeName)
	}
	adifPrefixMap[typeName] = prefix
}

// AdifPrefixForType returns the registered ADIF upload-status prefix for
// typeName and whether one was registered. (\"\", false) means the type stamps
// no upload status — so it has no stamp-based "uploaded to X?" signal.
func AdifPrefixForType(typeName string) (string, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	p, ok := adifPrefixMap[typeName]
	return p, ok
}

// RegisterRowMirror marks forwarder typeName as a ROW MIRROR — a destination
// that holds a full copy of every QSO row (the SM Cloud backup) rather than a
// derived record it extracts once (QRZ, ClubLog). A mirror must be told about
// EVERY row change, including the out-of-band writes that bypass the qsoservice
// ingest paths: the post-upload ADIF stamps and the session-email stamp both
// bump the row's revision AFTER the mirror already received it, and without a
// re-enqueue the mirror's copy stays one revision behind until the hourly
// reconcile heals it via a full-manifest diff (an O(logbook-size) download —
// see docs/backlog.md "smcloud stamp-drift"). qsoservice.EnqueueStampSync
// reads this flag back via IsRowMirror. From the forwarder package's init().
//
// Panics on empty typeName or duplicate registration — binary bugs.
func RegisterRowMirror(typeName string) {
	if typeName == "" {
		panic("forwarding.RegisterRowMirror: empty type name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := rowMirrorTypes[typeName]; exists {
		panic("forwarding.RegisterRowMirror: type already registered: " + typeName)
	}
	rowMirrorTypes[typeName] = struct{}{}
}

// IsRowMirror reports whether typeName registered as a row mirror.
func IsRowMirror(typeName string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, ok := rowMirrorTypes[typeName]
	return ok
}

// RegisterNoBulkBackfill marks forwarder typeName as accepting manual enqueue
// of RETRIES ONLY: its upstream forbids catch-up batches of pre-existing QSOs
// on the realtime endpoint the forwarder uses (ClubLog's realtime.php rule —
// batch catch-up there gets the application's API key BLOCKED; bulk belongs
// to putlogs.php, which SM does not speak yet — see docs/backlog.md).
// qsoservice.EnqueueUploads reads this back via NoBulkBackfill and allows a
// row only when it has queue HISTORY for the forwarder (a failed/re-armed
// live upload — retrying those is legitimate realtime usage, e.g. re-sending
// 403-era rows after a credential fix); history-less rows are refused into
// skipped_no_history. QSOs logged live (enqueued at logging time) are
// unaffected. From the forwarder package's init(). Panics on empty typeName
// or duplicate registration — binary bugs.
func RegisterNoBulkBackfill(typeName string) {
	if typeName == "" {
		panic("forwarding.RegisterNoBulkBackfill: empty type name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := noBulkBackfillTypes[typeName]; exists {
		panic("forwarding.RegisterNoBulkBackfill: type already registered: " + typeName)
	}
	noBulkBackfillTypes[typeName] = struct{}{}
}

// NoBulkBackfill reports whether typeName refuses manual bulk backfill.
func NoBulkBackfill(typeName string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, ok := noBulkBackfillTypes[typeName]
	return ok
}

// ResolveEndpoint returns the first non-empty endpoint among keys in m, falling
// back to def. Forwarder constructors use it to read a config-supplied URL
// (keyed by action) with the package default const as the fallback, so an
// old/sparse config — or any key the operator left unset — never breaks (ADR 0039).
func ResolveEndpoint(m map[string]string, def string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return def
}

// DefaultForwarderConfigs returns one disabled seed entry per registered
// forwarder type that has BOTH an editor descriptor (RegisterForwarderType) AND
// a registered canonical endpoint (RegisterDefaultEndpoints) — i.e. a real
// network destination, sorted by type. A descriptor-only type (the dev/test
// stub, or a future operator-must-supply-URL forwarder) is deliberately
// excluded: it stays addable via its descriptor but is not auto-seeded into the
// non-sparse config. It is the non-sparse default set ADR 0039 calls for: the
// daemon seeds these into config so the operator toggles `enabled` + supplies
// credentials rather than hand-adding an entry. Name defaults to the type
// (single instance per type, per the ham-services-singleton model). Retry/tick/
// batch are left zero so config.applyDefaults fills them; Endpoints carry the
// registered defaults so they're visible + overridable.
func DefaultForwarderConfigs() []types.ForwarderConfig {
	descs := ForwarderTypes() // sorted, deep-copied
	out := make([]types.ForwarderConfig, 0, len(descs))
	for _, d := range descs {
		// Seed only types with a registered canonical endpoint — a real network
		// destination. A type without one (the dev/test stub, or a future
		// operator-must-supply-URL forwarder) stays addable via its descriptor
		// but is not auto-seeded into the non-sparse config.
		eps, ok := DefaultEndpointsFor(d.Type)
		if !ok {
			continue
		}
		out = append(out, types.ForwarderConfig{
			Name:         d.Type,
			Type:         d.Type,
			Enabled:      false,
			ActionFilter: append([]string(nil), d.SupportedActions...),
			Endpoints:    eps,
		})
	}
	return out
}

// CredentialField describes one credential input a forwarder type needs, so the
// config SPA can render its "add forwarder" form data-drivenly (no per-type
// hardcoded forms). Kind drives the input widget + masking: "password" fields
// are never echoed back on GET /v1/config (masked-on-GET); "text" fields are.
type CredentialField struct {
	Key   string `json:"key"`   // matches the forwarder's credentials JSON key (e.g. "api_key")
	Label string `json:"label"` // human label for the input
	Kind  string `json:"kind"`  // "text" | "password"
	Help  string `json:"help,omitempty"`
}

// TypeDescriptor is the editor-facing description of a forwarder type, served by
// GET /v1/forwarder-types. It carries everything the config SPA needs to offer
// the type and collect its credentials; the upload logic itself stays
// per-type-specific in each Forwarder implementation.
type TypeDescriptor struct {
	Type             string            `json:"type"`
	DisplayName      string            `json:"display_name"`
	SupportedActions []string          `json:"supported_actions"`
	CredentialFields []CredentialField `json:"credential_fields"`
}

// RegisterForwarderType records a type's editor descriptor (display name +
// credential fields) AND its supported actions in one call — the canonical
// registration for a real forwarder, from its init(). It delegates the
// type/action validation + recording to RegisterSupportedActions (so the
// supported-action set, action_filter defaulting, and dup checks all behave
// identically), then validates the credential fields and stores the descriptor.
//
// Panics on programmer errors (those from RegisterSupportedActions, plus empty
// display name, bad/duplicate credential field) — all binary bugs.
func RegisterForwarderType(typeName, displayName string, actions []Action, creds []CredentialField) {
	if displayName == "" {
		panic("forwarding.RegisterForwarderType: empty display name for " + typeName)
	}
	seen := make(map[string]struct{}, len(creds))
	for _, c := range creds {
		if c.Key == "" {
			panic("forwarding.RegisterForwarderType: empty credential key for " + typeName)
		}
		if c.Kind != "text" && c.Kind != "password" {
			panic("forwarding.RegisterForwarderType: bad credential kind " + c.Kind + " for " + typeName)
		}
		if _, dup := seen[c.Key]; dup {
			panic("forwarding.RegisterForwarderType: duplicate credential key " + c.Key + " for " + typeName)
		}
		seen[c.Key] = struct{}{}
	}
	// Validates type/actions + dup registration, and records the supported set.
	RegisterSupportedActions(typeName, actions)

	sa := make([]string, len(actions))
	for i, a := range actions {
		sa[i] = string(a)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	descriptors[typeName] = TypeDescriptor{
		Type:             typeName,
		DisplayName:      displayName,
		SupportedActions: sa,
		CredentialFields: append([]CredentialField(nil), creds...),
	}
}

// ForwarderTypes returns every registered type descriptor, sorted by type, as
// deep copies (callers can't mutate registry state). Drives GET
// /v1/forwarder-types. Only types registered via RegisterForwarderType appear —
// a type registered with the bare RegisterSupportedActions (e.g. a test) does
// not, since it has no editor descriptor.
func ForwarderTypes() []TypeDescriptor {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]TypeDescriptor, 0, len(descriptors))
	for _, d := range descriptors {
		out = append(out, TypeDescriptor{
			Type:             d.Type,
			DisplayName:      d.DisplayName,
			SupportedActions: append([]string(nil), d.SupportedActions...),
			CredentialFields: append([]CredentialField(nil), d.CredentialFields...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}
