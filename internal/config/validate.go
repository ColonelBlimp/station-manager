package config

import (
	"fmt"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Field-rule bounds (config.md §12). Mirrors the values the PUT handler used
// before validation was unified here. The callsign rule mirrors
// qsoservice.IsValidCallsign (config can't import qsoservice — cycle — so the
// rule is reimplemented; option (a): api keeps its own copy for QSO/logbook).
const (
	minCallsignLen   = 3
	maxCallsignLen   = 32
	minCQZone        = 1
	maxCQZone        = 40
	minITUZone       = 1
	maxITUZone       = 90
	minDXCCEntity    = 0
	maxDXCCEntity    = 522
	maxAmpMultiplier = 1000 // real linear amps top out ~50x; 1000 = two extra zeros
	maxDefaultPowerW = 2000 // legal limits ≈ 1500W; headroom for pre-multiplier amp output
)

// Finding is one result from Validate — a config rule outcome carrying a stable
// code (an i18n-key candidate, ADR 0010), a human message, and a severity. The
// CALLER decides disposition (config.md §12): Load aborts fatally on any error
// finding; PUT /v1/config maps error findings to 400; warnings are advisory
// (logged at Load, returned in the PUT response) and never block.
//
// Field is the offending config path when known (e.g. "smtp"); whole-block
// checks may leave it coarse, since Message already carries the context.
type Finding struct {
	Field   string
	Code    string
	Message string
	Warning bool // false = error (fatal at Load / 400 at PUT); true = advisory
}

// Validate is the single source of truth for config rules (config.md §12), run
// by both Load (errors → fatal) and PUT /v1/config (errors → 400). Pure: no
// mutation. Returns errors and advisory warnings as Findings; the caller filters
// by severity.
//
// It consolidates the four standalone validators (forwarders, lookup, smtp,
// bridge), the rig catalogue + per-rig mode-mapping checks, the operator-identity
// field rules (callsign, grid, zones, amp/power, ft8_display feed-mode), and the
// non-loopback-bind advisory into this one entry point. The PUT handler builds a
// candidate, runs Normalize then this, and 400s on the first error finding —
// validating the whole candidate, so a value rejected here also fails at Load.
func Validate(cfg Config) []Finding {
	var out []Finding
	if err := validateForwarders(cfg.Forwarders); err != nil {
		out = append(out, Finding{Field: "forwarders", Code: "invalid_forwarder", Message: err.Error()})
	}
	if err := validateLookup(cfg.Lookup); err != nil {
		out = append(out, Finding{Field: "lookup", Code: "invalid_lookup", Message: err.Error()})
	}
	if err := validateSmtp(cfg.Smtp); err != nil {
		out = append(out, Finding{Field: "smtp", Code: "invalid_smtp", Message: err.Error()})
	}
	if err := validateBridge(cfg.ActiveBridge()); err != nil {
		out = append(out, Finding{Field: "bridge", Code: "invalid_bridge", Message: err.Error()})
	}
	out = append(out, validateRigs(cfg)...)
	out = append(out, validateLoggingStation(cfg.LoggingStation)...)
	out = append(out, validateStationPrefs(cfg.Station)...)
	out = append(out, validateFt8Display(cfg.Ft8.Display)...)
	// Advisory findings (non-fatal) — currently just the non-loopback-bind notice.
	for _, w := range Warnings(cfg) {
		out = append(out, Finding{Field: "socket_path", Code: "insecure_bind", Message: w, Warning: true})
	}
	return out
}

// validateRigs checks the rig catalogue (config.md §10): positive unique ids,
// non-empty model, default_rig_id resolves, and per-rig mode-mapping ADIF
// validity. Moved here from applyRigProfiles (which now only folds legacy loose
// fields into the catalogue) so the rules live in the one validator and run at
// both Load and PUT.
func validateRigs(cfg Config) []Finding {
	var out []Finding
	rigErr := func(field, msg string) Finding {
		return Finding{Field: field, Code: "invalid_field_value", Message: msg}
	}
	seen := make(map[int64]struct{}, len(cfg.Rigs))
	for i := range cfg.Rigs {
		rc := cfg.Rigs[i]
		if rc.ID <= 0 {
			out = append(out, rigErr(fmt.Sprintf("rigs[%d]", i),
				fmt.Sprintf("rigs[%d]: id must be a positive integer", i)))
		} else if _, dup := seen[rc.ID]; dup {
			out = append(out, rigErr(fmt.Sprintf("rigs[%d]", i),
				fmt.Sprintf("rigs: duplicate id %d", rc.ID)))
		} else {
			seen[rc.ID] = struct{}{}
		}
		if rc.Model == "" {
			out = append(out, rigErr(fmt.Sprintf("rigs[id=%d]", rc.ID),
				fmt.Sprintf("rigs[id=%d]: model must not be empty", rc.ID)))
		}
		for lit, mm := range rc.ModeMappings {
			field := fmt.Sprintf("rigs[id=%d].mode_mappings[%s]", rc.ID, lit)
			if mm.Mode == "" || !modes.IsValidMode(mm.Mode) {
				out = append(out, rigErr(field,
					fmt.Sprintf("%s: mode %q is not a known ADIF main mode", field, mm.Mode)))
			}
			if mm.SubMode != "" && !modes.IsValidSubMode(mm.SubMode) {
				out = append(out, rigErr(field,
					fmt.Sprintf("%s: submode %q is not a known ADIF submode", field, mm.SubMode)))
			}
		}
	}
	if len(cfg.Rigs) > 0 && cfg.RigByID(cfg.DefaultRigID) == nil {
		out = append(out, rigErr("default_rig_id",
			fmt.Sprintf("default_rig_id %d does not match any defined rig", cfg.DefaultRigID)))
	}
	return out
}

// validateLoggingStation checks the operator-identity field rules (config.md §12;
// moved from the PUT handler). Assumes Normalize has run (callsign uppercased,
// grid normalised, zones trimmed). Empty fields pass (pre-setup / not filled in).
func validateLoggingStation(ls types.LoggingStation) []Finding {
	var out []Finding
	add := func(field, msg string) {
		out = append(out, Finding{Field: field, Code: "invalid_field_value", Message: msg})
	}
	if ls.StationCallsign != "" && !isValidCallsign(ls.StationCallsign) {
		add("logging_station.station_callsign", "station_callsign must be 3-32 characters and contain at least one digit")
	}
	if ls.MyGridsquare != "" && !utils.IsValidMaidenhead(ls.MyGridsquare) {
		add("logging_station.my_gridsquare", "my_gridsquare must be a 4, 6, or 8 character Maidenhead locator")
	}
	if ls.MyCqZone != "" && !zoneInRange(ls.MyCqZone, minCQZone, maxCQZone) {
		add("logging_station.my_cq_zone", fmt.Sprintf("my_cq_zone must be a number between %d and %d", minCQZone, maxCQZone))
	}
	if ls.MyITUZone != "" && !zoneInRange(ls.MyITUZone, minITUZone, maxITUZone) {
		add("logging_station.my_itu_zone", fmt.Sprintf("my_itu_zone must be a number between %d and %d", minITUZone, maxITUZone))
	}
	if ls.MyDXCC != "" && !zoneInRange(ls.MyDXCC, minDXCCEntity, maxDXCCEntity) {
		add("logging_station.my_dxcc", fmt.Sprintf("my_dxcc must be a number between %d and %d (ARRL DXCC entity code; 0 = None)", minDXCCEntity, maxDXCCEntity))
	}
	return out
}

// validateStationPrefs checks the amp / default-power typo guards (config.md §12).
func validateStationPrefs(s types.StationConfig) []Finding {
	var out []Finding
	if s.AmpMultiplier < 0 || s.AmpMultiplier > maxAmpMultiplier {
		out = append(out, Finding{Field: "station.amp_multiplier", Code: "invalid_field_value",
			Message: fmt.Sprintf("station.amp_multiplier must be between 0 and %d", maxAmpMultiplier)})
	}
	if s.DefaultPower < 0 || s.DefaultPower > maxDefaultPowerW {
		out = append(out, Finding{Field: "station.default_power", Code: "invalid_field_value",
			Message: fmt.Sprintf("station.default_power must be between 0 and %d", maxDefaultPowerW)})
	}
	return out
}

// validateFt8Display checks the one ft8_display enum (the rest is normalised by
// ResolveFt8Display, not rejected).
func validateFt8Display(d *types.Ft8DisplayConfig) []Finding {
	if d != nil && d.FeedMode != "" && !types.Ft8FeedModeValid(d.FeedMode) {
		return []Finding{{Field: "ft8_display.feed_mode", Code: "invalid_field_value",
			Message: fmt.Sprintf("ft8_display.feed_mode %q must be \"accumulate\" or \"single\"", d.FeedMode)}}
	}
	return nil
}

// isValidCallsign mirrors qsoservice.IsValidCallsign (3-32 chars, only
// [0-9A-Za-z/-], at least one digit). config can't import qsoservice (cycle), so
// the rule is reimplemented here for the config station_callsign; api keeps its
// own copy for the QSO/logbook surfaces (config.md §12, option a).
func isValidCallsign(s string) bool {
	if len(s) < minCallsignLen || len(s) > maxCallsignLen {
		return false
	}
	hasDigit := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '/' || c == '-':
		default:
			return false
		}
	}
	return hasDigit
}

// zoneInRange reports whether s parses as a base-10 int in [lo, hi].
func zoneInRange(s string, lo, hi int) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= lo && n <= hi
}
