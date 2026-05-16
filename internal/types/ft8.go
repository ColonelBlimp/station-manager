package types

// Ft8Config holds the daemon's FT8 subsystem configuration. Per ADR
// 0021 the FT8 work runs as an in-process subsystem of `cmd/smd`
// under `internal/ft8` — same lifecycle + package-boundary pattern as
// `internal/bridge`.
//
// Enabled gates the whole subsystem. When false (operator without
// FT8 hardware / not running digital modes today, or the network-only
// "master smd" deployment), the FT8 subsystem does not acquire audio
// devices, spin up the decoder pool, or register any future endpoints.
// Default false — FT8 stays opt-in for v1 while the Fortran port
// matures against the WSJT-X v.3.0.0.1 reference at
// /home/mveary/Development/wsjtx.
//
// **v1 surface is deliberately minimal.** This scaffold ships only
// the Enabled flag. Audio device selection, frequency lists, decode
// policy, callsign / grid (likely shared with LoggingStation),
// transmit policy, etc. land as their corresponding implementation
// pieces do — keeping config schema and implementation in lockstep
// avoids the v1 trap where the config grew capabilities the daemon
// didn't actually have.
type Ft8Config struct {
	Enabled bool `json:"enabled"`
}
