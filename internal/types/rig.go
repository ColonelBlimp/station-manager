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
	// ID is the operator's label for this rig ("rig1", "rig2", "ftdx10-shack").
	// Free-form; used to distinguish rigs within a single operator config.
	ID string `json:"id"`

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
	Overrides RigOverrides `json:"overrides,omitempty"`
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
