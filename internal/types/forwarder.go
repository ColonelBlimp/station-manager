package types

import "encoding/json"

// ForwarderConfig is one entry in config.Forwarders — the daemon-facing
// description of a forwarding destination. Shape driven by
// docs/v2-design/forwarding.md §2.
//
// Name is the per-instance handle the operator picks (e.g. "qrz-primary").
// Type is the plugin identifier (e.g. "qrz") and must match a registered
// forwarder type. The Credentials blob is opaque at this layer — each
// forwarder package parses it with its own type-specific schema.
//
// ActionFilter lists the QSO lifecycle actions this destination cares about.
// Valid values are "insert", "update", "delete". An empty filter means
// "all actions" after applyDefaults runs.
//
// TickIntervalSec and BatchSize are operator-environment tunables; their
// zero-value defaults (120 s and 5) are conservative to match a slow /
// unreliable internet link — see docs/v2-design/forwarding.md §4.
//
// Retry is optional; when nil, the forwarder package supplies its own
// type-specific retry defaults (see §5 of the same doc).
//
// Endpoints carries the destination's upstream URLs, keyed by the action they
// serve ("insert" / "update" / "delete") — so a forwarder with per-action URLs
// (ClubLog: insert→realtime, delete→delete) and one with a single shared URL
// (QRZ: all actions → the same API URL) use the same shape (ADR 0039). Config
// is the runtime source so an endpoint can change without a recompile;
// applyDefaults seeds the per-type defaults daemon-side, and a forwarder
// constructor falls back to its package default const for any key the operator
// left unset. The operator never needs to type a URL for the common case.
type ForwarderConfig struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Label is the operator's own display name for this destination, settable
	// ONLY by hand in config.json — no API surface writes it and the SPA has no
	// control for it. Empty means "use the type's built-in DisplayName".
	//
	// It exists because that built-in name is a string in the binary
	// (smcloud.go's "SM Cloud backup"), so renaming is a build and a deploy, and
	// an operator cannot do it at all. Label is deliberately NOT Name: Name is
	// the durable key that qso_upload's UNIQUE (qso_id, forwarder_name, action)
	// is built on, so renaming THAT would make the daemon forget which QSOs it
	// had already sent and re-upload them upstream. Nothing joins on Label.
	Label           string            `json:"label,omitempty"`
	Enabled         bool              `json:"enabled"`
	Credentials     json.RawMessage   `json:"credentials,omitempty"`
	ActionFilter    []string          `json:"action_filter,omitempty"`
	Endpoints       map[string]string `json:"endpoints,omitempty"`
	TickIntervalSec int               `json:"tick_interval_sec,omitempty"`
	BatchSize       int               `json:"batch_size,omitempty"`
	Retry           *RetryConfig      `json:"retry,omitempty"`
}

// RetryConfig overrides the forwarder package's built-in retry defaults.
// When set, all three fields are expected to be populated. See
// docs/v2-design/forwarding.md §5.
type RetryConfig struct {
	MaxAttempts       int `json:"max_attempts"`
	InitialBackoffSec int `json:"initial_backoff_sec"`
	MaxBackoffSec     int `json:"max_backoff_sec"`
}
