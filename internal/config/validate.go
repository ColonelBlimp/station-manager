package config

import (
	"fmt"
	"math"
	"strconv"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/cat"
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
	if err := validatePskReporter(cfg.PskReporter); err != nil {
		out = append(out, Finding{Field: "psk_reporter", Code: "invalid_psk_reporter", Message: err.Error()})
	}
	if err := validateBridge(cfg.ActiveBridge()); err != nil {
		out = append(out, Finding{Field: "bridge", Code: "invalid_bridge", Message: err.Error()})
	}
	out = append(out, validateServer(cfg)...)
	out = append(out, validateRigs(cfg)...)
	out = append(out, validateLoggingStation(cfg.LoggingStation)...)
	out = append(out, validateStationPrefs(cfg.Station)...)
	out = append(out, validateFt8Display(cfg.Ft8.Display)...)
	out = append(out, validateFt8Occupancy(cfg.Ft8.TX)...)
	out = append(out, validateFt8FieldDay(cfg.Ft8.FieldDay)...)
	// Advisory findings (non-fatal) — currently just the non-loopback-bind notice.
	for _, w := range Warnings(cfg) {
		out = append(out, Finding{Field: "socket_path", Code: "insecure_bind", Message: w, Warning: true})
	}
	return out
}

// validateServer checks the ServerConfig block (review 2026-06-19 M1).
// applyDefaults only fills zero-valued fields, so a hand-edited config can still
// carry a negative or otherwise nonsensical value that reaches runtime: a
// negative max_concurrent_requests panics newLoadLimiter (a buffered channel of
// negative size), a non-positive HTTP timeout is read by net/http as "disabled"
// (silently dropping the slow-header guard), and a non-positive submit bucket
// can wedge submits. These are config errors, caught here before api.New rather
// than as a runtime panic or a quietly-weakened guard.
//
// Lower bounds + the page-limit ordering + the profiling advisory are enforced;
// deliberately no arbitrary upper ceilings — a large-but-valid operator value
// (a generous body cap, a big page limit) is not a safety hazard, and capping it
// would just reject legitimate tuning.
func validateServer(cfg Config) []Finding {
	var out []Finding
	s := cfg.Server

	if s.Protocol != "tcp" && s.Protocol != "unix" {
		out = append(out, Finding{Field: "server.protocol", Code: "invalid_field_value",
			Message: fmt.Sprintf("server.protocol %q must be \"tcp\" or \"unix\"", s.Protocol)})
	}

	// Positive-required integer knobs. Each maps a config path to its value; the
	// loop keeps the rule list readable and the messages uniform.
	type posRule struct {
		field string
		val   int
	}
	for _, r := range []posRule{
		{"server.read_header_timeout_sec", s.ReadHeaderTimeoutSec},
		{"server.read_timeout_sec", s.ReadTimeoutSec},
		{"server.write_timeout_sec", s.WriteTimeoutSec},
		{"server.idle_timeout_sec", s.IdleTimeoutSec},
		{"server.shutdown_timeout_sec", s.ShutdownTimeoutSec},
		{"server.default_page_limit", s.DefaultPageLimit},
		{"server.max_page_limit", s.MaxPageLimit},
		{"server.max_contact_history_results", s.MaxContactHistoryResults},
		{"server.max_concurrent_requests", s.MaxConcurrentRequests},
		{"server.max_event_subscribers", s.MaxEventSubscribers},
		{"server.submit_rate_per_sec", s.SubmitRatePerSec},
		{"server.submit_rate_burst", s.SubmitRateBurst},
	} {
		if r.val <= 0 {
			out = append(out, Finding{Field: r.field, Code: "invalid_field_value",
				Message: fmt.Sprintf("%s must be a positive value (got %d)", r.field, r.val)})
		}
	}
	if s.MaxBodyBytes <= 0 {
		out = append(out, Finding{Field: "server.max_body_bytes", Code: "invalid_field_value",
			Message: fmt.Sprintf("server.max_body_bytes must be a positive value (got %d)", s.MaxBodyBytes)})
	}

	// Page-limit ordering: a default above the ceiling would clamp every
	// unspecified-limit request below its own default.
	if s.DefaultPageLimit > 0 && s.MaxPageLimit > 0 && s.DefaultPageLimit > s.MaxPageLimit {
		out = append(out, Finding{Field: "server.default_page_limit", Code: "invalid_field_value",
			Message: fmt.Sprintf("server.default_page_limit (%d) must not exceed server.max_page_limit (%d)",
				s.DefaultPageLimit, s.MaxPageLimit)})
	}

	// Profiling on a non-loopback TCP bind exposes heap/goroutine/CPU forensics
	// (and a /debug/pprof/profile DoS vector) to the LAN. Advisory, like the
	// existing insecure-bind warning.
	if s.EnableProfiling && s.Protocol == "tcp" && !isLoopbackBind(cfg.SocketPath) {
		out = append(out, Finding{Field: "server.enable_profiling", Code: "insecure_profiling", Warning: true,
			Message: "server.enable_profiling=true with a non-loopback TCP bind exposes /debug/pprof " +
				"(heap/goroutine/CPU dumps + a profiling DoS vector) to any host that can reach this address"})
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
		} else if _, ok := cat.Lookup(rc.Model); !ok {
			// Model is a cat.Lookup driver id (types.RigConfig.Model). Reject an
			// unknown one HERE, at the single validation boundary, rather than
			// letting it pass to a runtime unknown_driver bridge error after the
			// daemon has already started (review 2026-06-19 M2).
			out = append(out, rigErr(fmt.Sprintf("rigs[id=%d]", rc.ID),
				fmt.Sprintf("rigs[id=%d]: model %q is not a known rig driver", rc.ID, rc.Model)))
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
	// default_rig_id must resolve to a defined rig. The sole exception is a
	// rig-less config (fresh install before CAT is set up): id 0 with no rigs
	// means "no active rig" and is valid. A non-zero id resolving to nothing —
	// or any id once rigs exist — is a dangling pointer.
	if cfg.RigByID(cfg.DefaultRigID) == nil && !(len(cfg.Rigs) == 0 && cfg.DefaultRigID == 0) {
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
	// Operator and OwnerCallsign follow the same callsign rule (empty passes —
	// they're optional and fall back to station_callsign). Catches a malformed
	// value from a direct API client or hand-edited config before it lands in
	// ADIF (OPERATOR / OWNER_CALLSIGN) or the FT8-fallback identity path.
	if ls.Operator != "" && !isValidCallsign(ls.Operator) {
		add("logging_station.operator", "operator must be 3-32 characters and contain at least one digit")
	}
	if ls.OwnerCallsign != "" && !isValidCallsign(ls.OwnerCallsign) {
		add("logging_station.owner_callsign", "owner_callsign must be 3-32 characters and contain at least one digit")
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

// validateFt8FieldDay checks the operator's Field Day exchange. Both fields are
// optional (empty = FD identity not set). Class is validated by the syntactic rule in
// types; Section is checked against go-ft8's canonical ARRL/RAC list
// (ValidARRLFieldDaySection, which trims + upper-cases internally) — go-ft8 owns that
// enumeration because it encodes the section into the FD frame.
func validateFt8FieldDay(d *types.Ft8FieldDayConfig) []Finding {
	if d == nil {
		return nil
	}
	var out []Finding
	if d.Class != "" && !types.Ft8FieldDayClassValid(d.Class) {
		out = append(out, Finding{Field: "ft8_field_day.class", Code: "invalid_field_value",
			Message: fmt.Sprintf("ft8_field_day.class %q must be a transmitter count 1–99 "+
				"followed by a category A–F (e.g. \"2A\")", d.Class)})
	}
	if d.Section != "" && !goft8.ValidARRLFieldDaySection(d.Section) {
		out = append(out, Finding{Field: "ft8_field_day.section", Code: "invalid_field_value",
			Message: fmt.Sprintf("ft8_field_day.section %q is not a valid ARRL/RAC Field Day "+
				"section (e.g. \"EMA\", or \"DX\" outside US/Canada)", d.Section)})
	}
	return out
}

// validateFt8Occupancy bounds the ft8.tx.occupancy knobs (review 2026-06-19 M3).
// They shape the clear-offset picker AND — once a suggestion is selected — a real
// TX offset, so a bad passband must be rejected, not silently yield negative or
// alias-prone suggestions. Each field is a sparse override (0/nil = use the
// built-in default, resolved in internal/ft8), so only NON-zero values are
// checked. Each passband edge is validated independently; the low<high + width
// cross-checks fire only when BOTH edges are overridden (a single-edge override
// resolves against the known-good default, so we needn't duplicate that default
// here). The 50 Hz signal width + 6 kHz Nyquist (12 kHz/2) mirror internal/ft8.
func validateFt8Occupancy(tx *types.Ft8TXConfig) []Finding {
	if tx == nil || tx.Occupancy == nil {
		return nil
	}
	const (
		ft8SignalWidthHz = 50
		ft8NyquistHz     = 6000
	)
	o := tx.Occupancy
	var out []Finding
	fld := func(f, msg string) {
		out = append(out, Finding{Field: "ft8.tx.occupancy." + f, Code: "invalid_field_value", Message: msg})
	}

	if o.PassbandLowHz != 0 && (o.PassbandLowHz < 0 || o.PassbandLowHz >= ft8NyquistHz) {
		fld("passband_low_hz", fmt.Sprintf("must be in 1..%d", ft8NyquistHz-1))
	}
	if o.PassbandHighHz != 0 && (o.PassbandHighHz <= 0 || o.PassbandHighHz > ft8NyquistHz) {
		fld("passband_high_hz", fmt.Sprintf("must be in 1..%d (Nyquist for the 12 kHz FT8 sample rate)", ft8NyquistHz))
	}
	if o.PassbandLowHz > 0 && o.PassbandHighHz > 0 {
		if o.PassbandLowHz >= o.PassbandHighHz {
			fld("passband", fmt.Sprintf("passband_low_hz (%d) must be < passband_high_hz (%d)", o.PassbandLowHz, o.PassbandHighHz))
		} else if o.PassbandHighHz-o.PassbandLowHz < ft8SignalWidthHz {
			fld("passband", fmt.Sprintf("width must be >= %d Hz (one FT8 signal)", ft8SignalWidthHz))
		}
	}

	if o.ThresholdFactor != 0 && (o.ThresholdFactor <= 0 || math.IsNaN(o.ThresholdFactor) || math.IsInf(o.ThresholdFactor, 0)) {
		fld("threshold_factor", "must be a finite positive number")
	}
	checkWeight := func(name string, w float64) {
		if w != 0 && (w < 0 || math.IsNaN(w) || math.IsInf(w, 0)) {
			fld(name, "must be a finite non-negative number")
		}
	}
	checkWeight("weight_margin", o.WeightMargin)
	checkWeight("weight_edge", o.WeightEdge)
	checkWeight("weight_centered", o.WeightCentered)
	if o.GuardMarginHz != nil && *o.GuardMarginHz < 0 {
		fld("guard_margin_hz", "must be >= 0")
	}
	return out
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
