package types

// MapConfig is the operator's contacts-map display settings, persisted in
// config.json under `map`. Sparse by design: an absent/empty block means the
// SPA's built-in defaults, and only the operator's deviations are stored —
// so a fresh config stays clean and new bands never need a migration.
//
// BandColors maps ADIF band tokens ("20m", "70cm") to CSS hex colours
// ("#22c55e") for the map's QSO arcs; bands without an entry take the SPA's
// default palette. Keys are stored lowercase (the SPA normalises band tokens
// the same way). Edited via the config SPA; the daemon only stores, validates
// and serves it — the colours are applied client-side, so a change needs a
// page reload, not a daemon restart.
type MapConfig struct {
	BandColors map[string]string `json:"band_colors,omitempty"`
}
