package types

// StationConfig holds operator-typed station-property preferences
// persisted across daemon restarts. Distinct from LoggingStation,
// which carries ADIF MY_* identity fields that travel with every
// QSO record; StationConfig fields shape the SPA's QSO-emission
// derivations but are not themselves ADIF data.
//
// Currently a small block: the linear-amplifier toggle + multiplier
// that the SPA's displayedState.effectivePower derivation reads when
// computing ADIF TX_PWR. Will grow as more per-station-property
// preferences land in /v1/config — see
// docs/v2-design/frontend-spa.md "Five-tier persistence".
//
// Lives in internal/types alongside the other config-block DTOs
// (DatastoreConfig, LoggingConfig, ForwarderConfig, RigConfig) so
// the handler in internal/api can reference it without importing
// internal/config — same pattern as those others.
type StationConfig struct {
	// AmpEnabled toggles whether AmpMultiplier is applied to the
	// rig's reported TX power on QSO submit. When false, the rig's
	// raw power is logged as-is. When true, the SPA's
	// displayedState.effectivePower multiplies through AmpMultiplier
	// (e.g. 50W rig × 10 multiplier = 500W effective).
	AmpEnabled bool `json:"amp_enabled"`

	// AmpMultiplier is the linear-amplifier gain factor applied to
	// rig power when AmpEnabled is true. 1.0 means "no amp". Defaults
	// to 1.0 so an unset config reads as "no amp", not "10000x amp".
	// Validated >= 0; the handler also rejects values above 1000 as
	// an obvious typo guard.
	AmpMultiplier float64 `json:"amp_multiplier"`

	// DefaultPower is the operator's typical TX power in watts when
	// CAT is unavailable. Used by the SPA's displayedState.rawPower
	// derivation in the CAT-off branch; CAT-reported power overrides
	// this when the bridge is live. 0 means "not set" — the SPA
	// emits ADIF TX_PWR=0 → omitted, matching the existing
	// "TX_PWR omitted when 0" rule. Validated in [0, 2000] (legal
	// max in most jurisdictions ≈ 1500W; the cap allows headroom
	// for amp output before the multiplier is applied).
	DefaultPower float64 `json:"default_power"`

	// OperatingBands is the operator's list of bands they actually work
	// (antenna coverage varies — many stations skip, e.g., 160/60/30).
	// One source for every band surface: the SPA's band-selector grid,
	// the FT8 band buttons, and the keyboard band-jump all read it, so a
	// band change is consistent whichever way the operator operates.
	// Order is preserved as the operator listed it (the SPA renders the
	// grid — and later assigns the digit shortcuts — in this order).
	// Empty/unset means "all bands" (the SPA falls back to its default
	// HF..6m set), so an existing config is unaffected. Each entry must
	// be a known band (enums/bands); validated at load + on PUT.
	OperatingBands []string `json:"operating_bands,omitempty"`
}
