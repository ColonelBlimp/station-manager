package types

// RequiredConfigs holds the small set of "required" runtime tunables that
// the kept v1 code still references. This struct has been pruned from its
// v1 shape — most of the original fields were Wails-app concerns (default
// logbook ID, default rig ID, power multiplier, pagination page size, etc.)
// that don't apply to the v2 daemon.
//
// What's left is the single field the sqlite service reads during batch
// upload reservation. When the v2 forwarder redesign lands
// (docs/v1-analysis/design-decisions-log.md → "Hardcoded QRZ forwarder"
// and docs/v2-design/structure.md → "Open items deferred"), this field is
// a candidate to move into a forwarder-specific config struct and this
// file may go away entirely.
type RequiredConfigs struct {
	// QsoForwardingRowLimit caps how many QSO rows the sqlite upload
	// reservation query will lock in a single batch. Zero means use the
	// sqlite package's compiled-in default.
	QsoForwardingRowLimit int `json:"qso_forwarding_row_limit"`
}
