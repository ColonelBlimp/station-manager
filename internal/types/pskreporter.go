package types

// PskReporterConfig is the operator's PSK Reporter upload settings, persisted in
// config.json under `psk_reporter`. FT8 reception spots are uploaded only when
// Enabled (default false — it publishes your RX to a public service, so it's
// opt-in). Host/Port default to the production collector when empty (the daemon's
// pskreporter package fills them: report.pskreporter.info:4739). To test, keep the
// host and set Port=14739 (the test port on the same collector — parses without
// writing the live database); NOT pskreporter.info, which is the website + drops UDP.
//
// The receiver identity is NOT here — callsign and grid come from LoggingStation,
// and the antennaInformation is sourced from its MY_ANTENNA (single source of
// truth, same as the antenna stamped on logged QSOs). Editable over /v1/config via
// the config SPA's FT8 tab (unmasked — no secrets here; served raw/sparse so empty
// host/port round-trip to the runtime default). Restart-only — the subsystem binds
// at boot.
type PskReporterConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Host    string `json:"host,omitempty"` // "" → production report.pskreporter.info
	Port    int    `json:"port,omitempty"` // 0 → 4739
}
