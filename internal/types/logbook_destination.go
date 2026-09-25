package types

import (
	"encoding/json"
	"time"
)

// LogbookDestination is one destination binding inside an archive file (ADR
// 0082): which destination a logical logbook uploads to, whether that route is
// on, the logbook-scoped credential fields for it, and the immutable
// forwarder_name that keys its qso_upload rows and its worker. There is no
// archive-level binding; the operator's one switch per destination is an
// aggregate over these rows.
type LogbookDestination struct {
	ID        int64 `json:"id"`
	LogbookID int64 `json:"logbook_id"`
	// Destination is the registered forwarder type (qrz, qrzcq, clublog, smcloud).
	Destination string `json:"destination"`
	// ForwarderName is minted once and never operator-editable: the adopted
	// archive's default logbook keeps the legacy config entry's name so its
	// queue rows and workers are untouched; any other binding is
	// `<destination>.<logbook uuid>`. It is an opaque routing handle on the
	// wire, never a setting.
	ForwarderName string `json:"forwarder_name"`
	Enabled       bool   `json:"enabled"`
	// Credentials is the type-owned JSON of the destination's LOGBOOK-scoped
	// fields (a QRZ key, a ClubLog account and callsign…). It is a secret: it
	// never leaves the archive file unmasked — API projections list keys only.
	Credentials json.RawMessage `json:"credentials,omitempty"`
	// RemoteAdoptedAt records when the remote identity was established (SM
	// Cloud's explicit adoption); nil for every other destination.
	RemoteAdoptedAt *time.Time `json:"remote_adopted_at,omitempty"`
	// LegacyName is the config.json forwarder name a seeded binding derives
	// from — the one name an older, config-driven build drains — so a
	// downgrade collapses to it rather than inferring it. Empty for a binding
	// created after the seed.
	LegacyName string     `json:"legacy_name,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
}
