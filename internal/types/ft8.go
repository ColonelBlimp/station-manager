package types

// Ft8Config holds the daemon's FT8 subsystem configuration. The FT8 work
// runs as an in-process subsystem of `cmd/smd` under `internal/ft8`,
// decoding live receive audio into messages.
//
// Enabled gates the whole subsystem. When false (operator not running
// digital modes, no FT8 hardware, or the network-only aggregator
// deployment) the subsystem acquires no audio device and spins up no
// decoder goroutines. Default false — FT8 stays opt-in.
//
// Device selects the audio capture device, a single identifier string.
// Under ADR 0028 the authoritative source is the per-rig
// RigConfig.Audio.Device in the rig catalogue; Config.ActiveFt8() projects
// the active rig's value onto this field, always winning over any value
// left here. So Device is a resolved runtime view, not an on-disk source —
// hand-setting it has no effect when a rig catalogue exists. omitempty so
// the inert loose field doesn't persist in rewritten configs (mirrors the
// empty bridge.serial.port / bridge.cat.driver loose fields). Empty means
// the system default capture device.
//
// EnableOSD turns on go-ft8's OSD-2/MRB fallback decode (ordered-statistics
// decoding after belief-propagation misses) — the analog to WSJT-X/jt9's
// deeper LDPC effort. It recovers weak signals BP alone misses (measured
// 2026-06-02 against jt9 -d 3: ~5 extra of 7 per the live A/B) for ~1.1–1.7×
// decode time, well inside the 15 s slot budget. Pointer-typed so applyDefaults
// can distinguish "absent" (nil → default true) from an explicit operator
// false. Default true — the cost is negligible and the recall gain is real.
//
// The surface is deliberately minimal — it grows (frequency lists, decode
// policy, transmit policy) as the corresponding implementation lands, so
// config schema and behaviour stay in lockstep.
type Ft8Config struct {
	Enabled   bool   `json:"enabled"`
	Device    string `json:"device,omitempty"`
	EnableOSD *bool  `json:"enable_osd,omitempty"`
}
