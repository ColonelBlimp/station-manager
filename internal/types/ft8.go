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
// Device selects the audio capture device, a single identifier string
// mirroring BridgeSerialConfig.Port (the serial-port end of the rig
// connection). Empty means the system default capture device. The string
// is interpreted by the capture backend; an empty value is the common
// case for a single-radio station.
//
// The surface is deliberately minimal — it grows (frequency lists, decode
// policy, transmit policy) as the corresponding implementation lands, so
// config schema and behaviour stay in lockstep.
type Ft8Config struct {
	Enabled bool   `json:"enabled"`
	Device  string `json:"device"`
}
