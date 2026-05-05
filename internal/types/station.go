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
}
