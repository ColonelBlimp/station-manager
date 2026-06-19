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

	// Audio selects this rig's audio devices, per direction (ADR 0028;
	// per-direction model config.md §10.4 #1, revised 2026-06-16). A per-rig
	// resource, NOT FT8-specific: the rig's codec carries FT8 receive audio
	// and (future) recorded-CQ / voice-keyer transmit audio. Projected onto
	// Ft8Config.Device (RX) / Ft8Config.TX.Device (TX) for the active rig (the
	// one Config.DefaultRigID points at), so switching the active rig re-binds
	// audio along with the CAT port + driver.
	Audio RigAudioConfig `json:"audio,omitzero"`

	// Ft8Mode is the operator's optional per-rig override of the FT8 operating
	// mode literal (config.md §10, scope B2). nil = use the rigdef default for
	// this rig's Model (cat.RigDefinition.Ft8Mode); a non-nil value (including
	// "") overrides it — "" meaning "leave the rig's current mode". Pointer so
	// "unset → use rigdef default" is distinguishable from an explicit ""
	// override. Resolved + projected onto Ft8Config.TX.Mode by Config.ActiveFt8.
	Ft8Mode *string `json:"ft8_mode,omitempty"`

	// ModeMappings is this rig's operator-override layer (config.md §10, B2) for
	// the rig-literal → ADIF (MODE, SUBMODE) translation, keyed by the rig's mode
	// literal (e.g. "DATA-U"). The rigdef for this rig's Model ships the defaults
	// (cat.RigDefinition.ModeMappings); they're merged with these overrides at
	// /v1/config GET time (operator's entry wins on collision). Moved here from
	// the global bridge.mode_mappings — the rig knows its Model, so no driver key.
	ModeMappings map[string]ModeMapping `json:"mode_mappings,omitempty"`

	// MyRig is the operator's optional per-rig override of the ADIF MY_RIG value
	// (config.md §10, B2). nil = derive from the rigdef Name for this rig's Model
	// (e.g. "Yaesu FTdx10"); a non-nil value (including "") overrides it — ""
	// meaning "don't publish MY_RIG" (suppress). Pointer so "unset → derive" is
	// distinguishable from an explicit "" suppress. Resolved by Config.ResolveMyRig
	// and stamped onto each live QSO at submit time (not stored on the QSO's
	// LoggingStation in config).
	MyRig *string `json:"my_rig,omitempty"`
}

// RigAudioConfig selects a rig's audio devices by NAME, per direction
// (config.md §10.4 #1, revised 2026-06-16 from the original single-field
// model). RX is the capture device (FT8 decode, future recording); TX is the
// playback device (FT8 transmit, future voice keyer).
//
// Names — not indices — because an index drifts across replug/reboot and
// differs between a codec's capture and playback enumerations (the borrowed
// IC-7300's USB codec "PCM2901 …" is capture index 4 but playback index 2). The
// audio layer resolves each name to a live device index at acquire time by
// matching the enumerated capture/playback lists (the same enumeration
// GET /v1/hardware exposes), fail-soft: no match → that direction's device is
// treated as absent (the FT8 subsystem stays idle rather than grabbing the
// wrong default). Per direction rather than one field because RX and TX are not
// guaranteed to be the same physical device. Empty = the system default for
// that direction.
type RigAudioConfig struct {
	RX string `json:"rx,omitempty"`
	TX string `json:"tx,omitempty"`
}

// RigOverrides shadows per-rig defaults from cat.RigDefinition. Field
// types are stdlib primitives so types stays stdlib-only; the composition
// step in internal/rigserial translates string values ("none", "even",
// "odd" for Parity; a single byte OR the "0xNN" hex form for LineDelimiter —
// e.g. CI-V's "0xFD") into go.bug.st/serial enum values when constructing the
// runtime serial.Config, and validates the numeric fields.
type RigOverrides struct {
	BaudRate      int    `json:"baud_rate,omitempty"`
	DataBits      int    `json:"data_bits,omitempty"`
	StopBits      int    `json:"stop_bits,omitempty"`
	Parity        string `json:"parity,omitempty"`
	LineDelimiter string `json:"line_delimiter,omitempty"`
	ReadTimeoutMS int    `json:"read_timeout_ms,omitempty"`
}
