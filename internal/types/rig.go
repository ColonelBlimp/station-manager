package types

// RigConfig is the operator's DTO for one rig installed at their station —
// what gets written into the logging-app config file (and, later, the
// FT8/4 app config file). It is deliberately minimal: it identifies the
// rig, names the serial port it's attached to, and optionally overrides
// defaults from the rig database. Everything else — CAT command tables,
// state parsers, default baud/parity/timing — comes from the embedded
// rig database in internal/cat, looked up by Model.
//
// See docs/v2-design/cat-serial-reuse.md §3c for the three-type split
// (types.RigConfig ↔ cat.RigDefinition ↔ serial.Config) and §7.5 for the
// open calls on the field set.
//
// RigConfig does NOT embed serial.Config, because types is stdlib-only
// (doc.go "Import constraint") and serial.Config carries go.bug.st/serial
// enum values. Overrides here are primitives; the composition that
// produces a real serial.Config lives in internal/rigconfig (landing when
// the first consumer — the logging app — is built).
type RigConfig struct {
	// ID is a stable numeric identifier for this rig within the
	// operator's config. Matches Logbook.ID's int64 shape so the
	// daemon's "default_<thing>_id" pointers (default_rig_id,
	// default_logbook_id) are uniform across types. Numeric IDs let
	// the daemon assign 1 on first-run with no operator input — part
	// of the "install → run → no errors, only one prompt" UX. Model
	// (below) is the human label; ID doesn't need to double as one.
	//
	// Originally string ("rig1", "ftdx10-shack") to be operator-set,
	// but no consumer materialised — the CAT lib looks up by Model,
	// not ID — so the free-form-label rationale never paid off.
	// Settled to int64 in session 31 when /v1/config landed and a
	// default_rig_id field had to be picked. See cat-serial-reuse.md
	// §7.5.
	ID int64 `json:"id"`

	// Model is the lookup key into the embedded rig database
	// (e.g. "yaesu-ftdx10", "yaesu-ft710"). Must match a cat.Lookup id.
	Model string `json:"model"`

	// Port is the serial device path (e.g. "/dev/ttyUSB0"). Always
	// operator-specific, so it lives at the top level rather than inside
	// Overrides.
	Port string `json:"port"`

	// Overrides is optional; each zero-valued field inherits the rig
	// database default for the given Model. Non-zero values shadow the
	// defaults. Zero-value-means-inherit implies operators cannot
	// distinguish "explicitly set to zero" from "omitted" — acceptable
	// because no realistic override-to-zero case exists today.
	Overrides RigOverrides `json:"overrides,omitzero"`

	// Audio selects this rig's audio interface (ADR 0028). A per-rig
	// resource, NOT FT8-specific: the same physical USB codec carries FT8
	// receive audio and (future) recorded-CQ / voice-keyer transmit audio.
	// Projected onto Ft8Config.Device for the active rig (the one
	// Config.DefaultRigID points at). Empty = the system default device.
	Audio RigAudioConfig `json:"audio,omitzero"`
}

// RigAudioConfig selects a rig's audio interface. Device is a single
// identifier for the one physical codec; the audio layer resolves the
// direction-appropriate handle (capture for decode, playback for a voice
// keyer) at open time, so the operator picks one device, not two (ADR
// 0028 — one physical device, one choice). Empty = the system default.
type RigAudioConfig struct {
	Device string `json:"device,omitempty"`
}

// RigOverrides shadows per-rig defaults from cat.RigDefinition. Field
// types are stdlib primitives so types stays stdlib-only; the composition
// step in internal/rigconfig translates string values ("none", "even",
// "odd" for Parity; a single-character string for LineDelimiter) into
// go.bug.st/serial enum values when constructing the runtime
// serial.Config.
type RigOverrides struct {
	BaudRate      int    `json:"baud_rate,omitempty"`
	DataBits      int    `json:"data_bits,omitempty"`
	StopBits      int    `json:"stop_bits,omitempty"`
	Parity        string `json:"parity,omitempty"`
	LineDelimiter string `json:"line_delimiter,omitempty"`
	ReadTimeoutMS int    `json:"read_timeout_ms,omitempty"`
}
