package config

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/enums/bands"
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
	if err := validateMap(cfg.Map); err != nil {
		out = append(out, Finding{Field: "map", Code: "invalid_map", Message: err.Error()})
	}
	if err := validateBridge(cfg.ActiveBridge()); err != nil {
		out = append(out, Finding{Field: "bridge", Code: "invalid_bridge", Message: err.Error()})
	}
	out = append(out, validateServer(cfg)...)
	out = append(out, validateRigs(cfg)...)
	out = append(out, validateLoggingStation(cfg.LoggingStation)...)
	out = append(out, validateOperators(cfg)...)
	out = append(out, validateStationPrefs(cfg.Station)...)
	out = append(out, validateFt8Display(cfg.Ft8.Display)...)
	out = append(out, validateFt8Audio(cfg.Ft8.Audio)...)
	out = append(out, validateFt8Occupancy(cfg.Ft8.TX)...)
	out = append(out, validateFt8FieldDay(cfg.Ft8.FieldDay)...)
	out = append(out, validateEvidence(cfg.Evidence)...)
	out = append(out, validateEvidenceSync(cfg)...)
	out = append(out, validateDatastore(cfg.Datastore)...)
	out = append(out, validateLogging(cfg.Logging)...)
	// Advisory findings (non-fatal). Currently just the ACKNOWLEDGED non-loopback
	// bind notice — an unacknowledged bind is fatal (validateServer above) and
	// Warnings() returns nothing for it, so the two paths never both fire (ST-3a).
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

	// A unix listener with no resolvable private socket path is fatal (ST-5): applyDefaults
	// leaves SocketPath empty when it cannot resolve a private runtime directory, and the
	// daemon must not fall back to a world-writable location like /tmp.
	if s.Protocol == "unix" && cfg.SocketPath == "" {
		out = append(out, Finding{Field: "socket_path", Code: "unix_socket_unresolved",
			Message: "server.protocol=unix but no private socket_path could be resolved; set " +
				"server.socket_path explicitly, or set $XDG_RUNTIME_DIR / $XDG_STATE_HOME / $HOME"})
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

	// A non-loopback TCP bind exposes the entire unauthenticated, unencrypted API +
	// RF control to the network (ST-3a). FATAL unless the operator acknowledges the
	// posture with server.allow_insecure_network: true, at which point Warnings()
	// emits a standing advisory instead (wrapped as an advisory Finding by Validate).
	// isLoopbackBind conservatively classifies wildcard (0.0.0.0 / :: / empty host)
	// and hostname binds as non-loopback; loopback-Host trust in requireSameOrigin is
	// a DNS-rebinding defence, not peer authentication, so a wildcard bind requires
	// the acknowledgement too (operator ruling 2).
	if s.Protocol == "tcp" && !isLoopbackBind(cfg.SocketPath) && !s.AllowInsecureNetwork {
		out = append(out, Finding{Field: "server.allow_insecure_network",
			Code: "insecure_bind_unacknowledged", Message: insecureNetworkFatalMsg(cfg.SocketPath)})
	}

	// Profiling on a non-loopback TCP bind exposes heap/goroutine/CPU forensics to
	// the LAN. Advisory even on an acknowledged bind: allow_insecure_network=true +
	// enable_profiling=true are already two deliberate switches, so no third override
	// is required (ST-3a, operator ruling 6). On an unacknowledged bind the fatal
	// finding above blocks startup regardless.
	if s.EnableProfiling && s.Protocol == "tcp" && !isLoopbackBind(cfg.SocketPath) {
		out = append(out, Finding{Field: "server.enable_profiling", Code: "insecure_profiling", Warning: true,
			Message: "server.enable_profiling=true with a non-loopback TCP bind exposes /debug/pprof " +
				"to any host that can reach this address: heap and goroutine dumps can disclose " +
				"in-memory data (including secrets), and CPU/trace profiling is resource-intensive " +
				"and a DoS vector"})
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

// validateOperators checks the operator roster (ADR 0055): each entry needs a
// valid, unique callsign, and default_operator must resolve to a roster entry.
// Assumes Normalize has run (callsigns uppercased/trimmed). An empty roster
// passes — applyDefaults seeds it at Load, but a client that clears it is a
// legitimate (if unusual) pre-setup state.
func validateOperators(cfg Config) []Finding {
	var out []Finding
	add := func(field, msg string) {
		out = append(out, Finding{Field: field, Code: "invalid_field_value", Message: msg})
	}
	seen := make(map[string]struct{}, len(cfg.Operators))
	for i := range cfg.Operators {
		call := cfg.Operators[i].Callsign
		if call == "" {
			add(fmt.Sprintf("operators[%d]", i), fmt.Sprintf("operators[%d]: callsign must not be empty", i))
			continue
		}
		if !isValidCallsign(call) {
			add(fmt.Sprintf("operators[%d]", i),
				fmt.Sprintf("operators[%d]: callsign %q must be 3-32 characters and contain at least one digit", i, call))
		}
		if _, dup := seen[call]; dup {
			add(fmt.Sprintf("operators[%d]", i), fmt.Sprintf("operators: duplicate callsign %q", call))
		}
		seen[call] = struct{}{}
	}
	// default_operator must point at a roster entry (empty passes — a roster-less
	// pre-seed config, or a client that cleared both together).
	if cfg.DefaultOperator != "" {
		if _, ok := seen[cfg.DefaultOperator]; !ok {
			add("default_operator",
				fmt.Sprintf("default_operator %q does not match any roster entry", cfg.DefaultOperator))
		}
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
	// The operator's own position, checked against the locator they declared.
	// Refused rather than corrected: unlike third-party coordinates there is a
	// human here to tell, and silently moving them to their cell centre would
	// ignore the input and explain nothing. The cell IS the tolerance — a
	// locator declares an extent, so no distance threshold is invented.
	if ls.MyLat != "" || ls.MyLon != "" {
		switch {
		case !utils.CoordsValid(ls.MyLat, ls.MyLon):
			add("logging_station.my_lat",
				"my_lat and my_lon must both be decimal degrees within ±90 / ±180 (e.g. -11.443917)")
		case ls.MyGridsquare != "" && utils.IsValidMaidenhead(ls.MyGridsquare) &&
			!utils.CoordsInsideGrid(ls.MyGridsquare, ls.MyLat, ls.MyLon):
			add("logging_station.my_lat",
				fmt.Sprintf("my_lat / my_lon are outside grid %s — correct them, or change my_gridsquare", ls.MyGridsquare))
		}
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
	// operating_bands: every entry must be a known band. An empty list means
	// "all bands" (the SPA defaults), so only non-empty entries are checked;
	// an unknown band is a config typo that would render a dead selector button.
	seen := make(map[string]bool, len(s.OperatingBands))
	for _, b := range s.OperatingBands {
		if !bands.IsValidBand(b) {
			out = append(out, Finding{Field: "station.operating_bands", Code: "invalid_field_value",
				Message: fmt.Sprintf("station.operating_bands contains an unknown band %q", b)})
			continue
		}
		if seen[b] {
			out = append(out, Finding{Field: "station.operating_bands", Code: "invalid_field_value",
				Message: fmt.Sprintf("station.operating_bands lists %q more than once", b)})
		}
		seen[b] = true
	}
	return out
}

// validateFt8Audio checks the RX level meter's window bounds against the
// RESOLVED values (defaults + overrides): a lone override can invert the
// window against a default, and the resolved pair is exactly what the SPA
// classifies with. Bounds [-120, 0] dBFS (the meter's silence floor to full
// scale), low strictly below high.
func validateFt8Audio(a *types.Ft8AudioConfig) []Finding {
	r := types.ResolveFt8Audio(a)
	inBounds := func(v float64) bool { return v >= -120 && v <= 0 }
	if !inBounds(r.LowDbfs) || !inBounds(r.HighDbfs) {
		return []Finding{{Field: "ft8.audio", Code: "invalid_field_value",
			Message: fmt.Sprintf("ft8.audio dBFS bounds (%g, %g) must be within [-120, 0]",
				r.LowDbfs, r.HighDbfs)}}
	}
	if r.LowDbfs >= r.HighDbfs {
		return []Finding{{Field: "ft8.audio", Code: "invalid_field_value",
			Message: fmt.Sprintf("ft8.audio low_dbfs %g must be below high_dbfs %g (resolved values)",
				r.LowDbfs, r.HighDbfs)}}
	}
	return nil
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
// validateEvidence checks the evidence block. The cap floor fires only when
// capture is enabled (a disabled block is inert; its cap starts mattering at
// the moment of consent) — the floor is types.EvidenceMinCapBytes, shared
// with the evidence package through the one place both can import, so the
// operator-facing finding and the writer cannot drift. The antenna
// declaration (§4.2) validates REGARDLESS of capture: it is declarative
// data — broken is broken — and O4 has it validate at load while pinning
// only when the evidence store opens.
func validateEvidence(e types.EvidenceConfig) []Finding {
	out := validateAntennas(e.Antennas)
	if e.Capture && e.CapBytes < types.EvidenceMinCapBytes {
		out = append(out, Finding{
			Field: "evidence.cap_bytes",
			Code:  "evidence_cap_too_small",
			Message: fmt.Sprintf("evidence cap %d bytes is below the minimum %d (reserved headroom + working floor); capture would drop immediately",
				e.CapBytes, types.EvidenceMinCapBytes),
		})
	}
	return out
}

// maxAntennaFieldRunes bounds the free-text declaration fields (name, type,
// feedline) — an engineering constant like the evidence headroom, not an
// operator threshold. The evidence activation gate reserves 16 MiB of
// headroom on the premise that one activation writes a few KB; bands are
// exclusive valid tokens (≤ 17 activatable entries) and the locator ≤ 8
// chars, so these three fields were the only unbounded inputs. 128 runes is
// far past any real antenna description and keeps the worst valid
// declaration around 30 KB.
const maxAntennaFieldRunes = 128

// EvidenceSyncCredentials resolves the §5 evidence-sync destination from
// the enabled smcloud forwarder's credentials (operator ruling 2026-08-10:
// one boolean, no second account or token surface). cmd/smd calls it to
// fill evidence.Config; validateEvidenceSync refuses configs where it
// errors, so a running daemon never reaches the error path.
func EvidenceSyncCredentials(cfg Config) (url, token string, err error) {
	for _, fc := range cfg.Forwarders {
		if fc.Type != "smcloud" || !fc.Enabled {
			continue
		}
		var creds struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		}
		if len(fc.Credentials) > 0 {
			if jerr := json.Unmarshal(fc.Credentials, &creds); jerr != nil {
				return "", "", fmt.Errorf("smcloud forwarder %q credentials are not valid JSON", fc.Name)
			}
		}
		if creds.URL == "" || creds.Token == "" {
			return "", "", fmt.Errorf("smcloud forwarder %q is missing url or token in its credentials", fc.Name)
		}
		return creds.URL, creds.Token, nil
	}
	return "", "", fmt.Errorf("no enabled smcloud forwarder is configured")
}

// validateEvidenceSync is SY1's validation half: evidence.sync reuses the
// smcloud forwarder's channel, so enabling it without that forwarder
// (absent, disabled, or credential-less) is refused rather than left to
// fail silently at runtime. Consent-inert when sync is off.
func validateEvidenceSync(cfg Config) []Finding {
	if !cfg.Evidence.Sync {
		return nil
	}
	if _, _, err := EvidenceSyncCredentials(cfg); err != nil {
		return []Finding{{
			Field: "evidence.sync",
			Code:  "evidence_sync_needs_smcloud",
			Message: fmt.Sprintf(
				"evidence.sync requires the smcloud forwarder's credentials (%s); sync reuses that channel — there is no separate evidence account", err),
		}}
	}
	return nil
}

// validateAntennas enforces the §4.2 declaration rules (operator rulings
// 2026-08-10; acceptance in validate_antennas_test.go). The trimmed name is
// the lineage identity, one band maps to one antenna, and nothing is
// silently normalized away — a duplicate band or name is a typo the
// operator must see, not a shape validation may repair.
func validateAntennas(antennas []types.AntennaDecl) []Finding {
	var out []Finding
	seenNames := map[string]int{}    // trimmed name → first declaring index
	bandOwner := map[string]string{} // band token → owning antenna name
	for i, a := range antennas {
		field := func(sub string) string { return fmt.Sprintf("evidence.antennas[%d].%s", i, sub) }
		name := strings.TrimSpace(a.Name)
		switch {
		case name == "":
			out = append(out, Finding{Field: field("name"), Code: "evidence_antenna_name_empty",
				Message: fmt.Sprintf("antenna entry %d has an empty name; the trimmed name is the lineage identity", i)})
			name = fmt.Sprintf("antennas[%d]", i) // placeholder so later messages stay readable
		case utf8.RuneCountInString(name) > maxAntennaFieldRunes:
			// Report the LENGTH, never the value: findings land in logs and
			// PUT 400 bodies, and echoing would amplify a megabyte input.
			// The placeholder also keeps every later message for this entry
			// from embedding it.
			out = append(out, Finding{Field: field("name"), Code: "evidence_antenna_field_too_long",
				Message: fmt.Sprintf("antenna entry %d name is %d characters; the limit is %d",
					i, utf8.RuneCountInString(name), maxAntennaFieldRunes)})
			name = fmt.Sprintf("antennas[%d]", i)
		default:
			if j, dup := seenNames[name]; dup {
				out = append(out, Finding{Field: field("name"), Code: "evidence_antenna_name_duplicate",
					Message: fmt.Sprintf("antenna name %q is declared twice (entries %d and %d); the trimmed name is the lineage identity and must be unique", name, j, i)})
			} else {
				seenNames[name] = i
			}
		}
		for _, fc := range []struct{ sub, val string }{
			{"type", strings.TrimSpace(a.Type)}, {"feedline", strings.TrimSpace(a.Feedline)},
		} {
			if n := utf8.RuneCountInString(fc.val); n > maxAntennaFieldRunes {
				out = append(out, Finding{Field: field(fc.sub), Code: "evidence_antenna_field_too_long",
					Message: fmt.Sprintf("antenna %q %s is %d characters; the limit is %d",
						name, fc.sub, n, maxAntennaFieldRunes)})
			}
		}
		if len(a.Bands) == 0 {
			out = append(out, Finding{Field: field("bands"), Code: "evidence_antenna_bands_empty",
				Message: fmt.Sprintf("antenna %q claims no bands and so declares nothing; retire an antenna by removing its entry", name)})
		}
		seenBands := map[string]bool{}
		for _, b := range a.Bands {
			if !bands.IsValidBand(b) {
				out = append(out, Finding{Field: field("bands"), Code: "evidence_antenna_band_unknown",
					Message: fmt.Sprintf("antenna %q claims unknown band token %q (use ADIF band names, e.g. \"20m\")", name, b)})
				continue
			}
			if seenBands[b] {
				out = append(out, Finding{Field: field("bands"), Code: "evidence_antenna_band_repeated",
					Message: fmt.Sprintf("antenna %q claims band %q more than once; silent de-duplication would conceal a typo", name, b)})
				continue
			}
			seenBands[b] = true
			if owner, taken := bandOwner[b]; taken {
				out = append(out, Finding{Field: field("bands"), Code: "evidence_antenna_band_conflict",
					Message: fmt.Sprintf("band %q is claimed by both %q and %q; one band maps to one antenna (the same-band override is a deferred feature)", b, owner, name)})
			} else {
				bandOwner[b] = name
			}
		}
		if a.HeightM != nil && (*a.HeightM < 0 || math.IsNaN(*a.HeightM) || math.IsInf(*a.HeightM, 0)) {
			out = append(out, Finding{Field: field("height_m"), Code: "evidence_antenna_height_invalid",
				Message: fmt.Sprintf("antenna %q height_m %v must be a finite value ≥ 0 (feedpoint metres above ground; 0 = ground-mounted; no upper bound)", name, *a.HeightM)})
		}
		if l := strings.TrimSpace(a.Locator); l != "" && !utils.IsValidMaidenhead(l) {
			out = append(out, Finding{Field: field("locator"), Code: "evidence_antenna_locator_invalid",
				Message: fmt.Sprintf("antenna %q locator %q is not a valid Maidenhead locator", name, l)})
		}
	}
	return out
}

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

// validateDatastore mirrors the SQLite consumer's rules (the struct tags on
// types.DatastoreConfig that internal/database/sqlite/validation.go enforces) at the
// config boundary, so a hand-edited datastore block fails at Load with a field-
// specific code instead of later as a generic dependency-injection error (CC-3).
// Structural only — no I/O; filesystem viability and the actual DB open stay a
// startup concern. The consumer is kept as a defensive boundary; a parity test in
// internal/database/sqlite guards the two against drift.
func validateDatastore(ds types.DatastoreConfig) []Finding {
	var out []Finding
	add := func(field, msg string) {
		out = append(out, Finding{Field: field, Code: "invalid_datastore", Message: msg})
	}
	if ds.Driver != types.SqliteDriverName {
		add("datastore.driver", fmt.Sprintf("driver must be %q", types.SqliteDriverName))
	}
	if ds.Path == "" {
		add("datastore.path", "path is required")
	}
	if ds.MaxOpenConns < 1 {
		add("datastore.max_open_conns", "max_open_conns must be at least 1")
	}
	if ds.MaxIdleConns < 1 {
		add("datastore.max_idle_conns", "max_idle_conns must be at least 1")
	}
	if ds.ConnMaxLifetime < 0 {
		add("datastore.conn_max_lifetime", "conn_max_lifetime must be 0 or greater")
	}
	if ds.ConnMaxIdleTime < 0 {
		add("datastore.conn_max_idle_time", "conn_max_idle_time must be 0 or greater")
	}
	if ds.ContextTimeout < 5 {
		add("datastore.context_timeout", "context_timeout must be at least 5 seconds")
	}
	if ds.TransactionContextTimeout < 5 {
		add("datastore.transaction_context_timeout", "transaction_context_timeout must be at least 5 seconds")
	}
	return out
}

// validateLogging mirrors the logging consumer's rules (the struct tags on
// types.LoggingConfig plus internal/logging/validation.go's level, skip-frame, and
// relative-path semantic checks) at the config boundary (CC-3). Structural/semantic
// only — no I/O. Without this an invalid logging block passes Load and surfaces only
// when the structured logger itself cannot start. A parity test in internal/logging
// guards against drift. `omitempty` size/timeout fields treat 0 as "use the default".
func validateLogging(lg types.LoggingConfig) []Finding {
	var out []Finding
	add := func(field, msg string) {
		out = append(out, Finding{Field: field, Code: "invalid_logging", Message: msg})
	}
	switch lg.Level {
	case "trace", "debug", "info", "warn", "error", "fatal", "panic":
	default:
		add("logging.level", "level must be one of trace, debug, info, warn, error, fatal, panic")
	}
	if lg.SkipFrameCount < 0 || lg.SkipFrameCount > 20 {
		add("logging.skip_frame_count", "skip_frame_count must be between 0 and 20")
	}
	if lg.RelLogFileDir == "" {
		add("logging.rel_log_file_dir", "rel_log_file_dir is required")
	} else if clean := filepath.Clean(lg.RelLogFileDir); strings.Contains(clean, "..") {
		add("logging.rel_log_file_dir", "rel_log_file_dir must not contain '..' (directory traversal)")
	} else if filepath.IsAbs(clean) {
		add("logging.rel_log_file_dir", "rel_log_file_dir must be a relative path")
	}
	if lg.LogFileMaxBackups < 0 {
		add("logging.log_file_max_backups", "log_file_max_backups must be 0 or greater")
	}
	if lg.LogFileMaxAgeDays < 0 {
		add("logging.log_file_max_age_days", "log_file_max_age_days must be 0 or greater")
	}
	if lg.LogFileMaxSizeMB < 0 {
		add("logging.log_file_max_size_mb", "log_file_max_size_mb must be 0 (default) or at least 1")
	}
	if lg.ShutdownTimeoutMS != 0 && (lg.ShutdownTimeoutMS < 10 || lg.ShutdownTimeoutMS > 10000) {
		add("logging.shutdown_timeout_ms", "shutdown_timeout_ms must be 0 (default) or between 10 and 10000")
	}
	return out
}

// ValidationError wraps the first blocking Finding rejected by the service update
// boundary's normalize→validate pipeline (CC-4). Callers can errors.As it to recover
// the structured finding; its message matches Load's fatal form.
type ValidationError struct {
	Finding Finding
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid config (%s): %s", e.Finding.Code, e.Finding.Message)
}

// normalizeAndValidate runs the canonical normalize→validate pipeline on an update
// candidate and returns a *ValidationError for the first blocking finding, so the
// service update boundary is authoritative regardless of caller discipline (CC-4). It
// is idempotent for a Load-derived config, which Load already normalized and validated.
func normalizeAndValidate(next *Config) error {
	Normalize(next)
	for _, f := range Validate(*next) {
		if !f.Warning {
			return &ValidationError{Finding: f}
		}
	}
	return nil
}
