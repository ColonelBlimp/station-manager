package types

// PskReporterConfig is the operator's PSK Reporter upload settings, persisted in
// config.json under `psk_reporter`. FT8 reception spots are uploaded only when
// Enabled (default false — it publishes your RX to a public service, so it's
// opt-in). Host/Port default to the production server when empty (the daemon's
// pskreporter package fills them); point them at the test server
// (pskreporter.info:14739) to verify without touching the live database.
//
// The receiver identity (callsign, grid) is NOT here — it comes from
// LoggingStation. Not exposed over /v1/config (set-once daemon config, like the
// SMTP block); a config-SPA surface can come later.
type PskReporterConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Host    string `json:"host,omitempty"`    // "" → production report.pskreporter.info
	Port    int    `json:"port,omitempty"`    // 0 → 4739
	Antenna string `json:"antenna,omitempty"` // freeform antennaInformation, shown on the PSK map
}
