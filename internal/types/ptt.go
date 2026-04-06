package types

// PTTConfig holds configuration for the push-to-talk serial interface.
type PTTConfig struct {
	Enabled  bool   `json:"enabled"`
	PortName string `json:"port_name"`
	// Line selects the control pin: "rts" (default) or "dtr".
	Line string `json:"line"`
}
