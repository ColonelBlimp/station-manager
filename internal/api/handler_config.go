package api

import (
	"encoding/json"
	stderr "errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ConfigResponse is the wire shape for GET/PUT /v1/config. It embeds
// types.X for every nested object — no parallel field definitions —
// per the "reuse types.X rather than building parallel structs"
// project idiom in CLAUDE.md.
//
// Source-of-truth split:
//   - SetupComplete, LoggingStation: fields whose source IS config.json.
//     Pass through both directions.
//   - DefaultLogbook: id from config.json; name/callsign/description
//     joined from the DB row at GET time.
//   - DefaultRig: a narrow read-only projection (DefaultRigInfo) of the
//     active rig — id from config.json, model/port joined from cfg.Rigs.
//     NOT the full types.RigConfig: the logging SPA only needs id/model/port,
//     and reusing the write-oriented RigConfig here leaked port/audio/
//     overrides/mode_mappings onto /v1/config and would have auto-widened the
//     wire surface on every future RigConfig field (review 2026-06-19 L1).
//     Full rig profiles (with overrides) live on the config SPA's /v1/rigs.
//   - Mailer: read-only projection of the SMTP block — only the SPA-
//     relevant subset (enabled flag + default recipient). Host / port /
//     username / password are deliberately not on the wire; SMTP creds
//     are operator-side config.json material, not UI-editable.
//
// PUT bodies use the same shape; the handler honours only writable
// fields (the LoggingStation / Station blocks, Qsl, Ft8Display, the
// Bridge mode-mapping overlay, and — for the config SPA's Rigs tab —
// the rig catalogue (`rigs`) + active-rig selector (`default_rig_id`),
// both presence-aware). SetupComplete, Mailer, and the DefaultRig read
// join are server-managed/read-only — the handler ignores them on PUT
// and reasserts the authoritative state in the response.
type ConfigResponse struct {
	SetupComplete  bool                 `json:"setup_complete"`
	LoggingStation types.LoggingStation `json:"logging_station"`
	DefaultLogbook types.Logbook        `json:"default_logbook"`
	DefaultRig     DefaultRigInfo       `json:"default_rig"`
	Station        types.StationConfig  `json:"station"`
	// Qsl is the operator's standing outgoing-QSL defaults (QSL_VIA / QSLMSG /
	// QSL_SENT_VIA). Like Ft8Display it is **presence-aware** on PUT — a body that
	// omits it leaves the stored block untouched; one that includes it replaces it
	// — so a My Station save can't accidentally wipe it. Always populated on GET.
	Qsl    *types.QslDefaults `json:"qsl,omitempty"`
	Bridge BridgeInfo         `json:"bridge"`
	Mailer MailerInfo         `json:"mailer"`
	// Ft8Display is the FT8 Band Activity display preferences (row cap, feed
	// mode, CQ highlight colours) — operator-writable, unlike the read-only
	// Bridge/Mailer projections. On GET it is always populated with the resolved
	// values (defaults filled), so the SPA reads sensible values even on a fresh
	// config. On PUT it is **presence-aware**: a body that omits it (e.g. a My
	// Station save) leaves the stored block untouched; a body that includes it
	// replaces it. Pointer-typed so the handler can tell "sent" from "absent".
	Ft8Display *types.Ft8DisplayConfig `json:"ft8_display,omitempty"`
	// Ft8Frequencies is the per-band FT8 dial frequencies (band→Hz), always served
	// RESOLVED on GET (defaults + operator overrides) for the SPA's Main-Freq band
	// buttons. Read-only over /v1/config for now — overrides are edited in config.json
	// (no Settings control yet) and a PUT never carries it, so it's left untouched on
	// write (it survives in the in-memory cfg, rewritten with the rest).
	Ft8Frequencies map[string]int `json:"ft8_frequencies,omitempty"`
	// Ft8CallerAnswerMode is the FT8 Call-CQ answerer-selection strategy
	// (ft8.tx.caller_answer_mode): "auto_first" (first valid answerer by decode
	// order) or "auto_strongest" (highest-SNR valid answerer in the slot). Always
	// served RESOLVED on GET (default auto_first) for the logging SPA's FT8 Settings
	// tab. Operator-writable; **presence-aware** on PUT (omitting it leaves the
	// stored value untouched). Pointer-typed so the handler tells "sent" from
	// "absent". The config-only "operator_pick" literal is NOT accepted here (it's
	// rejected at Call-CQ start), so the wire surface only ever carries the two
	// attended auto modes.
	Ft8CallerAnswerMode *string `json:"ft8_caller_answer_mode,omitempty"`
	// Ft8MaxRepeats is the FT8 sequencer's unanswered-rung repeat cap
	// (ft8.tx.max_repeats): how many times an unanswered rung is re-sent before the
	// exchange gives up so the operator's Next can advance — the "N calls" readout.
	// Always served RESOLVED on GET (default 6, clamped [1, Ft8MaxRepeatsCeiling]) for
	// the logging SPA's FT8 Settings tab. Operator-writable and applied LIVE: a PUT
	// persists it AND pushes it into the running sequencer (Service.SetMaxRepeats), so
	// lowering it drops a dead contact sooner mid-pile-up without a restart. This is
	// the one /v1/config field with a live side-effect (config.md §11). **Presence-
	// aware** on PUT; pointer-typed so the handler tells "sent" from "absent".
	Ft8MaxRepeats *int `json:"ft8_max_repeats,omitempty"`
	// Ft8FieldDay is the operator's ARRL Field Day exchange (class + ARRL/RAC
	// section), sent when answering a CQ FD over FT8. Served on GET as the stored
	// block (empty `{}` when unset — FD is once a year, so empty is normal).
	// Operator-writable; **presence-aware** on PUT (omitting it leaves the stored
	// block untouched) and normalised to upper-case before the shared Validate
	// checks it (class strict, section loose — go-ft8 owns the canonical section
	// list). Pointer-typed so the handler tells "sent" from "absent".
	Ft8FieldDay *types.Ft8FieldDayConfig `json:"ft8_field_day,omitempty"`
	// BridgeTimeouts / BridgeTune are the resolved (defaults-filled, ceilings
	// applied) bridge supervisor/readLoop timeouts and tune-carrier params.
	// Like Ft8Frequencies, the on-disk config stays SPARSE — zero = "use the
	// built-in default," applied in internal/bridge — and these are served
	// RESOLVED on GET (via bridge.ResolveTimeouts / bridge.ResolveTune, the same
	// resolution the running Service uses) so the config SPA can show effective
	// values without materialising them into config.json (config.md §15
	// sparse-but-served). Read-only over /v1/config: a PUT never carries them
	// (they're edited in config.json while the daemon is stopped), so they're
	// left untouched on write.
	BridgeTimeouts *types.BridgeTimeoutsConfig `json:"bridge_timeouts,omitempty"`
	BridgeTune     *types.BridgeTuneConfig     `json:"bridge_tune,omitempty"`
	// Rigs + DefaultRigID are WRITE-ONLY on this endpoint (the config SPA's Rigs
	// tab). Presence-aware on PUT: a body carrying `rigs` replaces the whole
	// catalogue; one carrying `default_rig_id` sets the active rig. Both are
	// validated through the same Normalize+Validate pipeline as Load
	// (validateRigs — unique positive ids, non-empty model, default_rig_id
	// resolves), so a bad catalogue is a 400. They are NEVER emitted on GET
	// (buildConfigResponse leaves them nil → omitempty drops them): the full
	// catalogue read surface stays on GET /v1/rigs and the active rig's narrow
	// read view stays on the DefaultRig join above. Pointers distinguish "sent"
	// from "absent".
	Rigs         *[]types.RigConfig `json:"rigs,omitempty"`
	DefaultRigID *int64             `json:"default_rig_id,omitempty"`
	// Forwarders is the config SPA's Forwarding-tab surface — masked on GET,
	// merge-on-PUT (see ForwarderInfo). Presence-aware on PUT: a body that omits
	// it leaves the forwarder list untouched (so a Station / FT8 save can't wipe
	// it); a body that carries it REPLACES the whole list. Emitted on GET only
	// when at least one forwarder is configured (omitempty), masked.
	Forwarders []ForwarderInfo `json:"forwarders,omitempty"`
	// Lookup is the config SPA's Enrichment-tab surface (ADR 0017) — masked on
	// GET (provider passwords reported as set/unset, never the value), merge-on-PUT
	// (see LookupProviderInfo). Pointer so the PUT is presence-aware: a body that
	// omits `lookup` leaves the enrichment config untouched; one that carries it
	// REPLACES it (with passwords merged). Always set on GET.
	Lookup *LookupInfo `json:"lookup,omitempty"`
	// Smtp is the config SPA's Email-tab surface — masked on GET (password reported
	// set/unset via password_set, never the value), merge-on-PUT (mergeSmtp: a blank
	// password keeps the stored one). Pointer so the PUT is presence-aware: a body
	// that omits `smtp` leaves the SMTP block untouched (a Station / FT8 save can't
	// wipe it); one that carries it REPLACES the block (password merged). Always set
	// on GET. Distinct from the read-only Mailer projection above (logging SPA,
	// live-mailer state); the handler ignores Mailer on PUT and honours Smtp.
	Smtp *SmtpInfo `json:"smtp,omitempty"`
	// PskReporter is the config SPA's FT8-tab PSK Reporter surface (the psk_reporter
	// block — opt-in public upload of FT8 reception spots). No secrets (receiver
	// identity comes from LoggingStation), so the canonical types.PskReporterConfig
	// rides the wire directly — no masked projection needed. Served RAW (sparse):
	// an empty host/port means "use the production collector default", resolved in
	// internal/pskreporter at runtime, and the SPA renders those as placeholders —
	// so config.json stays sparse rather than materialising the defaults. Pointer =
	// presence-aware on PUT (omit → untouched; carry → replace); always set on GET.
	// Restart-only (the subsystem binds at boot).
	PskReporter *types.PskReporterConfig `json:"psk_reporter,omitempty"`
	// Ft8DecodeLog is the config SPA's FT8-tab decode-log surface (the
	// ft8.decode_log block — a JTDX ALL.TXT-style record of RX decodes + our own
	// TX). No secrets, so the canonical types.Ft8DecodeLogConfig rides the wire
	// directly. Pointer = presence-aware on PUT (omit → untouched; carry →
	// replace); always set on GET (nil-in-config served as a disabled zero block so
	// the form binds). Restart-only: the log file opens at FT8 service start.
	Ft8DecodeLog *types.Ft8DecodeLogConfig `json:"ft8_decode_log,omitempty"`
	// BridgeEnabled / Ft8Enabled are the master on/off switches for the rig CAT
	// bridge and the FT8 subsystem (config SPA's Rigs / FT8 tabs). Read+write:
	// always set on GET (current value) so the toggles show state; presence-aware
	// on PUT (pointer) — a body that omits them leaves the flag untouched, one
	// that carries it sets it. Enabling the bridge requires the active rig to have
	// port+driver (validateBridge) — a 400 otherwise.
	BridgeEnabled *bool `json:"bridge_enabled,omitempty"`
	Ft8Enabled    *bool `json:"ft8_enabled,omitempty"`
	// RestoreRigOnModeSwitch: whether a Phone/CW ↔ FT8 switch auto re-tunes a
	// CAT-live rig to that mode's last freq/mode (SPA behaviour). Always set on GET
	// (resolved — true when unset, so consumers get a definite bool); presence-aware
	// on PUT (omit → untouched).
	RestoreRigOnModeSwitch *bool `json:"restore_rig_on_mode_switch,omitempty"`
}

// LookupProviderInfo is the config SPA's view of one enrichment provider
// (hamnut or a chain entry). Asymmetric like ForwarderInfo to keep the password
// off the wire (masked-on-GET):
//
//   - On GET: name/enabled/url/username/timeout_sec/view_url + PasswordSet (is a
//     password stored). Password is "" (masked).
//   - On PUT: + Password (new value; blank = keep the stored one). PasswordSet is
//     ignored.
//
// Username is shown on GET (a QRZ login/callsign, not a secret); only Password
// is masked.
type LookupProviderInfo struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url,omitempty"`
	Username    string `json:"username,omitempty"`
	PasswordSet bool   `json:"password_set"`
	Password    string `json:"password,omitempty"`
	TimeoutSec  int    `json:"timeout_sec,omitempty"`
	ViewURL     string `json:"view_url,omitempty"`
}

// LookupInfo mirrors types.EnrichmentConfig for the wire, with each provider's
// password masked (LookupProviderInfo). The config SPA's Enrichment tab edits
// hamnut + the callsign chain (QRZ today) + the cache TTLs.
type LookupInfo struct {
	Hamnut             LookupProviderInfo   `json:"hamnut"`
	Chain              []LookupProviderInfo `json:"chain"`
	CountryTTLDays     int                  `json:"country_ttl_days"`
	StationTTLDays     int                  `json:"station_ttl_days"`
	RefreshMaxInFlight int                  `json:"refresh_max_in_flight"`
}

// ForwarderInfo is the config SPA's view of one forwarding destination. It is
// deliberately asymmetric to keep secrets off the wire (masked-on-GET):
//
//   - On GET: Name/Type/Enabled/ActionFilter + CredentialsSet (the credential
//     keys that currently hold a non-empty value — NEVER the values). Credentials
//     is nil/omitted.
//   - On PUT: Name/Type/Enabled/ActionFilter + Credentials (key→new value, only
//     the fields the operator typed; an omitted field keeps its stored value).
//     CredentialsSet is ignored.
//
// The advanced knobs (tick_interval_sec / batch_size / retry) are intentionally
// absent: the SPA doesn't edit them, and the PUT merge carries the stored values
// forward by name so a Forwarding-tab save never wipes an operator's hand-set
// values. ActionFilter is sent by the SPA (derived from the type's supported
// actions), so it round-trips without daemon defaulting.
type ForwarderInfo struct {
	Name           string            `json:"name"`
	Type           string            `json:"type"`
	Enabled        bool              `json:"enabled"`
	ActionFilter   []string          `json:"action_filter,omitempty"`
	CredentialsSet []string          `json:"credentials_set,omitempty"`
	Credentials    map[string]string `json:"credentials,omitempty"`
}

// DefaultRigInfo is the SPA-visible subset of the active rig for GET
// /v1/config. The logging SPA needs only the rig's identity and serial
// port (the My Station Equipment readout); it must NOT receive the full
// types.RigConfig (audio devices, serial overrides, per-rig mode mappings,
// ft8_mode, my_rig) — those are the config SPA's concern and stay on
// /v1/rigs. Keeping this a deliberate narrow type means a future field on
// types.RigConfig can't silently widen the /v1/config wire surface (review
// 2026-06-19 L1). Read-only: the default-rig selection is written via PUT
// /v1/rigs' default_rig_id, not /v1/config.
type DefaultRigInfo struct {
	ID    int64  `json:"id"`
	Model string `json:"model,omitempty"`
	Port  string `json:"port,omitempty"`
}

// MailerInfo is the SPA-visible subset of the SMTP config. Enabled
// drives whether the SessionPanel renders its email controls;
// DefaultRecipient pre-fills the recipient input. Host / port /
// username / password / from are intentionally absent — exposing them
// would either leak the SMTP password or invite the SPA to edit creds
// it has no business editing.
type MailerInfo struct {
	Enabled          bool   `json:"enabled"`
	DefaultRecipient string `json:"default_recipient,omitempty"`
}

// SmtpInfo is the config SPA's editable view of the SMTP block (Email tab). It is
// the persisted-intent EDIT surface (cfg.Smtp-backed, presence-aware on PUT),
// distinct from the read-only MailerInfo above — which stays the logging SPA's
// RUNNING-state projection (enabled + default recipient from the live mailer).
// Same split the codebase uses for DefaultRig (narrow read) vs /v1/rigs (full
// edit): the two can diverge until the daemon restarts to pick up a saved change,
// which is the config-SPA-requires-restart model the Bridge/FT8 toggles also use.
//
// Asymmetric to keep the password off the wire (masked-on-GET, merge-on-PUT, like
// ForwarderInfo / LookupProviderInfo):
//   - On GET: enabled/host/port/username/from/default_recipient/starttls/
//     timeout_sec + PasswordSet (is a password stored). Password is "" (masked).
//   - On PUT: + Password (new value; blank = keep the stored one). PasswordSet is
//     ignored.
//
// Username is shown on GET (an SMTP login, not masked the way the password is) —
// the same call LookupProviderInfo makes. validateSmtp (config pipeline) gates the
// merged result, so an enabled block missing host/from or with a bad address is a
// 400 — the SPA never has to re-implement the rules.
type SmtpInfo struct {
	Enabled          bool   `json:"enabled"`
	Host             string `json:"host,omitempty"`
	Port             int    `json:"port,omitempty"`
	Username         string `json:"username,omitempty"`
	From             string `json:"from,omitempty"`
	DefaultRecipient string `json:"default_recipient,omitempty"`
	StartTLS         bool   `json:"starttls"`
	TimeoutSec       int    `json:"timeout_sec,omitempty"`
	PasswordSet      bool   `json:"password_set"`
	Password         string `json:"password,omitempty"`
}

// BridgeInfo is the SPA-visible subset of the bridge subsystem config.
//
// Enabled mirrors the operator's persisted intent (drives the SPA's
// configState.station.enabled and the three-flag isLive rule per ADR
// 0009).
//
// Driver is the configured rig-driver id (e.g. "yaesu-ftdx10") —
// resolved from cfg.Bridge.Cat.Driver, used by the SPA to key into
// per-rig sub-maps (Mode Mappings).
//
// RigName is the rigdef's human-readable name (e.g. "Yaesu FTdx10")
// resolved from cat.Lookup(Driver) — the SPA shows it in the My
// Station Equipment panel and uses it as the ADIF MY_RIG fallback.
//
// RigModes is the set of unique mode strings the configured rigdef's
// MAINMODE parser can produce (e.g. ["LSB","USB","CW-U","DATA-U",...])
// — used by the SPA's My Station → Mode Mappings sub-tab to render
// one row per rig mode.
//
// Ops is the connected rig's advertised inbound-command vocabulary — the
// exposed command names from the rigdef (e.g. ["set_freq","set_mode"]) per
// ADR 0026. The SPA gates rig-control surfaces on this set: a feature shows
// only when the rig exposes the ops it needs. Empty when the rigdef defines
// no exposed commands (a read-only / display-only rig); both shipped Yaesu
// rigs (FTdx10, FT-710) expose the same write ops.
//
// Tune reports whether the connected rig can run the tune-carrier feature
// (ADR 0027) — the rigdef defines the set_mode/set_power/tx_on/tx_off
// commands the controller needs. The SPA shows the Tune button only when
// true; false (omitted) for a rig whose rigdef lacks those commands. Both
// shipped Yaesu rigs advertise tune (FT-710 live-confirmed 2026-06-06).
//
// ModeMappings is the merged view (rigdef defaults + operator
// overrides from cfg.Bridge.ModeMappings) for the configured driver
// only — the SPA sees a single keyed-by-rig-string table without
// needing to know about other drivers' mappings.
//
// Empty / nil values mean either the bridge is disabled, the
// configured driver is unknown, or no overrides are set — all
// reachable states; the SPA handles them gracefully.
//
// Port stays off the wire because it's a hardware-config concern
// the SPA has no business reading or editing; the operator owns it
// via config.json directly (matching the SMTP-creds-not-on-the-wire
// decision above). Baud + other serial parameters live in the
// rigdef and aren't surfaceable anyway.
type BridgeInfo struct {
	Enabled      bool                         `json:"enabled"`
	Driver       string                       `json:"driver,omitempty"`
	RigName      string                       `json:"rig_name,omitempty"`
	RigModes     []string                     `json:"rig_modes,omitempty"`
	Ops          []string                     `json:"ops,omitempty"`
	Tune         bool                         `json:"tune,omitempty"`
	ModeMappings map[string]types.ModeMapping `json:"mode_mappings,omitempty"`

	// Ft8Mode is the rig CAT mode literal for FT8 (e.g. "USB-D" on the IC-7300,
	// "DATA-U" on the FTdx10) — rigdef default, overridable per-rig. The SPA's
	// FT8 Main-Freq band buttons drive set_mode with it so picking an FT8 band
	// also puts the rig in data mode. Empty when no driver is configured.
	Ft8Mode string `json:"ft8_mode,omitempty"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleGetConfig"

	resp, err := s.buildConfigResponse(r, s.cfg.Snapshot())
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handlePutConfig"

	var req ConfigResponse
	if !s.readJSONBody(w, r, op, &req) {
		return
	}

	current := s.cfg.Snapshot()

	// Build the candidate config: the current snapshot with the request's
	// operator-writable fields overlaid, then run it through the ONE config
	// pipeline — Normalize + Validate (config.md §12), the same Load runs — before
	// persisting. A value rejected here would also be rejected at Load.
	//
	// Clone (deep copy), not the shallow Snapshot value: Normalize mutates the
	// candidate in place (e.g. normalizeRigOverrides nils per-rig overrides), and
	// a shallow copy still aliases the live config's Rigs/slice backing arrays —
	// so editing the candidate would corrupt the running config before the change
	// is even validated or committed.
	candidate := current.Clone()
	candidate.LoggingStation = req.LoggingStation
	candidate.Station = req.Station
	// FT8 display prefs — presence-aware: only touched when the body carried
	// `ft8_display` (a My Station save omits it, leaving it alone). Stored RAW
	// here so Validate can reject an invalid feed_mode (config.md §12 option A);
	// resolution (clamp colours/cap, default feed_mode) happens after validation.
	if req.Ft8Display != nil {
		candidate.Ft8.Display = req.Ft8Display
	}
	// FT8 caller-answer mode — presence-aware (logging SPA FT8 Settings tab). Only
	// the two attended auto modes are accepted over the wire; operator_pick is a
	// config.json-only literal (rejected at Call-CQ start), so the SPA can't set it.
	// Validated here rather than via config.Validate (which tolerates an invalid
	// value → default) so a bad value is a loud 400, matching feed_mode's contract.
	if req.Ft8CallerAnswerMode != nil {
		mode := *req.Ft8CallerAnswerMode
		if mode != types.Ft8CallerAnswerAutoFirst && mode != types.Ft8CallerAnswerAutoStrongest {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"ft8_caller_answer_mode must be auto_first or auto_strongest", op)
			return
		}
		if candidate.Ft8.TX == nil {
			candidate.Ft8.TX = &types.Ft8TXConfig{}
		}
		candidate.Ft8.TX.CallerAnswerMode = mode
	}
	// FT8 unanswered-rung repeat cap — presence-aware (logging SPA FT8 Settings tab).
	// Validated to [1, Ft8MaxRepeatsCeiling] here as a loud 400 rather than leaning on
	// the resolver's silent clamp, matching caller_answer_mode's strict-wire contract.
	// Applied LIVE after the persist commits (below), so the change reaches the running
	// sequencer without a restart.
	if req.Ft8MaxRepeats != nil {
		n := *req.Ft8MaxRepeats
		if n < 1 || n > types.Ft8MaxRepeatsCeiling {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				fmt.Sprintf("ft8_max_repeats must be between 1 and %d", types.Ft8MaxRepeatsCeiling), op)
			return
		}
		if candidate.Ft8.TX == nil {
			candidate.Ft8.TX = &types.Ft8TXConfig{}
		}
		candidate.Ft8.TX.MaxRepeats = n
	}
	// FT8 Field Day exchange — presence-aware (config SPA FT8 tab). Normalised to
	// upper-case (TrimSpace+ToUpper) here so "2a"/"dx" store canonically; the shared
	// Validate then rejects a malformed class/section as a 400 (validateFt8FieldDay).
	// An empty block (both fields blank) is the valid "FD identity not set" state.
	if req.Ft8FieldDay != nil {
		candidate.Ft8.FieldDay = &types.Ft8FieldDayConfig{
			Class:   strings.ToUpper(strings.TrimSpace(req.Ft8FieldDay.Class)),
			Section: strings.ToUpper(strings.TrimSpace(req.Ft8FieldDay.Section)),
			// RST_RCVD default is an operator-chosen report value (e.g. "59", "-15") —
			// trimmed but NOT upper-cased; case is meaningless for a report.
			DefaultRstRcvd: strings.TrimSpace(req.Ft8FieldDay.DefaultRstRcvd),
		}
	}
	// QSL defaults — presence-aware, same rationale as ft8_display: a My Station
	// save omits `qsl` and must leave it alone.
	if req.Qsl != nil {
		candidate.Qsl = *req.Qsl
	}
	// Rig catalogue + active-rig selector — presence-aware (config SPA Rigs tab).
	// Applied BEFORE the mode-mapping overlay (which bases off candidate.Rigs)
	// and before Normalize+Validate, so validateRigs catches a bad catalogue
	// (duplicate/non-positive ids, empty model, dangling default_rig_id) as a 400.
	// A fresh slice so the candidate never aliases the request's backing array.
	if req.Rigs != nil {
		candidate.Rigs = append([]types.RigConfig(nil), (*req.Rigs)...)
	}
	if req.DefaultRigID != nil {
		candidate.DefaultRigID = *req.DefaultRigID
	}
	// Forwarders — presence-aware (config SPA Forwarding tab). A body carrying
	// `forwarders` REPLACES the whole list; each entry's credentials are MERGED
	// onto the stored values (matched by name) so a field the operator left blank
	// keeps its secret (masked-on-GET means the SPA never had it to echo). The
	// advanced knobs (tick/batch/retry) carry over from the matched existing entry
	// too. Validated through validateForwarders below (dup names / unknown type /
	// unsupported action).
	if req.Forwarders != nil {
		candidate.Forwarders = mergeForwarders(req.Forwarders, current.Forwarders)
	}
	// Enrichment (config SPA Enrichment tab) — presence-aware; provider passwords
	// merged onto the stored values by name (blank = keep). Normalize stamps the
	// default provider URLs; validateLookup gates the result.
	if req.Lookup != nil {
		candidate.Lookup = mergeLookup(*req.Lookup, current.Lookup)
	}
	// SMTP (config SPA Email tab) — presence-aware; the password is merged onto the
	// stored value (blank = keep, masked-on-GET means the SPA never had it to echo).
	// validateSmtp (in the pipeline below) gates the merged block: an enabled block
	// missing host/from or with a malformed address is a 400.
	if req.Smtp != nil {
		candidate.Smtp = mergeSmtp(*req.Smtp, current.Smtp)
	}
	// PSK Reporter (config SPA FT8 tab) — presence-aware; no secrets, so the block
	// is taken as-sent (validatePskReporter gates the port below). Stored sparse:
	// an empty host/port round-trips to the runtime default.
	if req.PskReporter != nil {
		candidate.PskReporter = *req.PskReporter
	}
	// FT8 decode log (config SPA FT8 tab) — presence-aware; no secrets, stored as
	// sent. Pointer block, so a disabled log can persist as {enabled:false} or be
	// dropped — either reads as "no file" at service start.
	if req.Ft8DecodeLog != nil {
		candidate.Ft8.DecodeLog = req.Ft8DecodeLog
	}
	// Master subsystem switches — presence-aware (config SPA Rigs / FT8 tabs).
	// validateBridge below enforces port+driver when bridge is enabled.
	if req.BridgeEnabled != nil {
		candidate.Bridge.Enabled = *req.BridgeEnabled
	}
	if req.Ft8Enabled != nil {
		candidate.Ft8.Enabled = *req.Ft8Enabled
	}
	// Mode-switch rig-restore preference — presence-aware (stored as-sent; the SPA
	// reads it and gates the live re-tune). Omit → untouched.
	if req.RestoreRigOnModeSwitch != nil {
		candidate.RestoreRigOnModeSwitch = req.RestoreRigOnModeSwitch
	}
	// Mode-mapping overrides: diff the incoming set against the rigdef's shipped
	// defaults so only operator deviations persist, stored on the active rig
	// (config.md §10). Bad ADIF in the result is caught by Validate below.
	if req.Bridge.Driver != "" && req.Bridge.ModeMappings != nil {
		def, ok := cat.Lookup(req.Bridge.Driver)
		if !ok {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"bridge.driver does not match a known rigdef", op)
			return
		}
		overrides := make(map[string]types.ModeMapping)
		for lit, mm := range req.Bridge.ModeMappings {
			if shipped, shippedOk := def.ModeMappings[lit]; !shippedOk || shipped != mm {
				overrides[lit] = mm
			}
		}
		// Copy the rig slice before mutating so the active rig's mappings don't
		// alias the live config. Base off candidate.Rigs (not current.Rigs) so a
		// `rigs` block applied just above isn't clobbered by the mode-mapping overlay.
		candidate.Rigs = append([]types.RigConfig(nil), candidate.Rigs...)
		if rc := candidate.RigByID(candidate.DefaultRigID); rc != nil {
			if len(overrides) > 0 {
				rc.ModeMappings = overrides
			} else {
				rc.ModeMappings = nil
			}
		}
	}

	// Normalize + validate through the single config pipeline. The first error
	// finding becomes a 400 carrying its stable code + message (ADR 0010).
	config.Normalize(&candidate)
	for _, f := range config.Validate(candidate) {
		if !f.Warning {
			s.writeError(w, http.StatusBadRequest, f.Code, f.Message, op)
			return
		}
	}

	// FT8 display passed validation — now resolve to the stored shape (clamps
	// colours / row cap; feed_mode was already validated raw above). Stored
	// normalised so the on-disk value matches what GET would serve.
	if req.Ft8Display != nil {
		resolved := types.ResolveFt8Display(req.Ft8Display)
		candidate.Ft8.Display = &resolved
	}

	// Setup transition (after validation passes): the first PUT with a non-empty
	// callsign completes setup — seed the default logbook row, flip SetupComplete,
	// and materialise OPERATOR / OWNER_CALLSIGN from the callsign when unset.
	if !current.SetupComplete && candidate.LoggingStation.StationCallsign != "" {
		id, err := s.seedDefaultLogbook(r, candidate.DefaultLogbookID, candidate.LoggingStation.StationCallsign)
		if err != nil {
			s.writeServerError(w, op, err, "db_error", "failed to seed default logbook")
			return
		}
		candidate.SetupComplete = true
		if id != 0 {
			candidate.DefaultLogbookID = id
		}
		call := candidate.LoggingStation.StationCallsign
		if candidate.LoggingStation.Operator == "" {
			candidate.LoggingStation.Operator = call
		}
		if candidate.LoggingStation.OwnerCallsign == "" {
			candidate.LoggingStation.OwnerCallsign = call
		}
	}

	if err := s.cfg.Update(func(cfg *config.Config) error {
		*cfg = candidate
		return nil
	}); err != nil {
		s.writeServerError(w, op, err, "config_write_error", "failed to persist config update")
		return
	}

	// Live-apply the FT8 repeat cap to the running sequencer — the one config field
	// that takes effect without a restart, so the operator can dial it down mid-pile-up.
	// After the persist so a rejected/failed write never applies; nil-safe when FT8 is
	// off (s.ft8 == nil).
	if req.Ft8MaxRepeats != nil {
		s.ft8.SetMaxRepeats(*req.Ft8MaxRepeats)
	}

	resp, err := s.buildConfigResponse(r, s.cfg.Snapshot())
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// seedDefaultLogbook ensures a logbook row exists at the configured
// default_logbook_id. If a row already exists it is returned as-is
// (idempotent — operator may have created one manually). Otherwise a
// new row is inserted using the operator's just-set callsign. Returns
// the resolved logbook ID — which equals defaultID on the existing
// path and the newly-inserted ID otherwise.
//
// The "Default" name is intentionally generic: the operator can
// rename it via the My Station card / future PUT /v1/logbook/{id}.
// Description seeded with a hint so first-time operators know what
// they're looking at when they open the logbook list.
func (s *Server) seedDefaultLogbook(r *http.Request, defaultID int64, callsign string) (int64, error) {
	if existing, err := s.db.FetchLogbookByIDWithContext(r.Context(), defaultID); err == nil {
		return existing.ID, nil
	} else if !stderr.Is(err, errors.ErrNotFound) {
		return 0, err
	}

	id, err := s.db.InsertLogbookWithContext(r.Context(), types.Logbook{
		Name:        "Default",
		Callsign:    callsign,
		Description: "Default logbook (auto-created during first-run setup)",
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// buildConfigResponse projects a Config snapshot into the wire shape.
// Joins the default_logbook DB row when one exists and the active rig's
// id/model/port into the narrow DefaultRigInfo (L1).
//
// The Mailer block is sourced from the live mailer Service rather
// than the cfg snapshot — Enabled() and DefaultRecipient() are
// nil-safe (test wiring passes mailer=nil) and tracking the actual
// service state means a future "reload SMTP without restart" flow
// stays correct without a parallel branch here.
// bridgeInfoFor builds the BridgeInfo response block. Resolves the
// configured driver's rigdef (when present) to populate RigName,
// RigModes, and the merged ModeMappings (rigdef shipped defaults +
// operator overrides from cfg.Bridge.ModeMappings — operator's value
// wins per-rig-string on collision).
//
// Pure construction; safe to call with any Config snapshot.
func bridgeInfoFor(cfg config.Config) BridgeInfo {
	// Resolve the ACTIVE rig's driver (ADR 0028): the loose bridge.cat /
	// bridge.serial fields are superseded by the catalogue, so read the
	// projected active values, not cfg.Bridge directly.
	b := cfg.ActiveBridge()
	info := BridgeInfo{
		Enabled: b.Enabled,
		Driver:  b.Cat.Driver,
	}
	if b.Cat.Driver == "" {
		return info
	}
	def, ok := cat.Lookup(b.Cat.Driver)
	if !ok {
		// Unknown driver — leave Driver set so the SPA can flag a
		// config issue, but skip the rigdef-derived fields.
		return info
	}
	info.RigName = def.Name
	info.RigModes = cat.RigModes(def)
	info.Ops = cat.ExposedCommands(def)
	info.Tune = bridge.TuneSupported(def)
	info.Ft8Mode = def.Ft8Mode

	rc := cfg.RigByID(cfg.DefaultRigID)
	// FT8 mode: rigdef default, overridden by the active rig's per-rig value.
	if rc != nil && rc.Ft8Mode != nil {
		info.Ft8Mode = *rc.Ft8Mode
	}

	// Merge mode mappings: rigdef defaults first, then operator
	// overrides on top. Operator's entry wins per-rig-string on
	// collision; entries the operator hasn't touched stay at the
	// shipped default.
	merged := make(map[string]types.ModeMapping, len(def.ModeMappings))
	for k, v := range def.ModeMappings {
		merged[k] = v
	}
	// Operator overrides now live on the active rig (config.md §10), not a global
	// driver-keyed block. Layer the active rig's per-rig overrides on top.
	if rc != nil {
		for k, v := range rc.ModeMappings {
			merged[k] = v
		}
	}
	if len(merged) > 0 {
		info.ModeMappings = merged
	}
	return info
}

func (s *Server) buildConfigResponse(r *http.Request, cfg config.Config) (ConfigResponse, error) {
	resp := ConfigResponse{
		SetupComplete:  cfg.SetupComplete,
		LoggingStation: cfg.LoggingStation,
		DefaultLogbook: types.Logbook{ID: cfg.DefaultLogbookID},
		DefaultRig:     DefaultRigInfo{ID: cfg.DefaultRigID},
		Station:        cfg.Station,
		Bridge:         bridgeInfoFor(cfg),
		Mailer: MailerInfo{
			Enabled:          s.mailer.Enabled(),
			DefaultRecipient: s.mailer.DefaultRecipient(),
		},
	}

	// QSL defaults — served as-is (empty fields just omit). Copied to a local so
	// the response carries a pointer that doesn't alias the snapshot.
	qsl := cfg.Qsl
	resp.Qsl = &qsl

	// FT8 display prefs, always resolved (defaults filled) so a fresh config
	// still yields sensible values for the SPA's Settings tab.
	ft8Display := types.ResolveFt8Display(cfg.Ft8.Display)
	resp.Ft8Display = &ft8Display

	// FT8 Call-CQ answerer-selection mode, resolved (default auto_first) for the
	// Settings tab. A config holding operator_pick still reads back as-is here, but
	// the SPA dropdown only offers the two auto modes and a PUT rejects anything else.
	callerMode := types.ResolveFt8CallerAnswerMode(cfg.Ft8.TX)
	resp.Ft8CallerAnswerMode = &callerMode

	// FT8 unanswered-rung repeat cap, resolved (default 6, clamp [1, Ft8MaxRepeatsCeiling])
	// so the Settings-tab field shows the effective value even on a fresh config.
	maxRepeats := types.ResolveFt8MaxRepeats(cfg.Ft8.TX)
	resp.Ft8MaxRepeats = &maxRepeats

	// FT8 Field Day exchange — served as the stored block, or an empty `{}` when
	// unset, so the SPA always reads a stable {class, section} shape (no defaults to
	// resolve: empty means "FD identity not set").
	if cfg.Ft8.FieldDay != nil {
		fd := *cfg.Ft8.FieldDay
		resp.Ft8FieldDay = &fd
	} else {
		resp.Ft8FieldDay = &types.Ft8FieldDayConfig{}
	}

	// FT8 per-band dial frequencies, always resolved (defaults + overrides) for the
	// SPA's Main-Freq band buttons.
	resp.Ft8Frequencies = types.ResolveFt8Frequencies(cfg.Ft8.Frequencies)

	// Bridge timeouts + tune params, served RESOLVED (defaults filled, ceilings
	// applied) like the FT8 blocks above — config.json stays sparse. Uses the same
	// resolution the running Service applies, so the SPA reads the daemon's
	// effective values even though they aren't materialised on disk.
	bridgeTimeouts := bridge.ResolveTimeouts(cfg.Bridge.Timeouts)
	resp.BridgeTimeouts = &bridgeTimeouts
	bridgeTune := bridge.ResolveTune(cfg.Bridge.Tune)
	resp.BridgeTune = &bridgeTune

	if cfg.DefaultLogbookID > 0 {
		row, err := s.db.FetchLogbookByIDWithContext(r.Context(), cfg.DefaultLogbookID)
		if err == nil {
			resp.DefaultLogbook = row
		} else if !stderr.Is(err, errors.ErrNotFound) {
			return ConfigResponse{}, err
		}
		// ErrNotFound: pre-setup state — keep the bare {ID: N} stub.
	}

	// DefaultRig join (ADR 0028): cfg.Rigs is now populated (a single-rig
	// config is migrated into a one-entry catalogue at Load), so resolve the
	// active rig's display fields. Only id/model/port cross to the logging SPA
	// (L1); the bare {ID: N} stub stands in when no rig matches (a
	// catalogue-less / bridge-disabled host).
	if rc := cfg.RigByID(cfg.DefaultRigID); rc != nil {
		resp.DefaultRig = DefaultRigInfo{ID: rc.ID, Model: rc.Model, Port: rc.Port}
	}

	// Forwarders — masked: name/type/enabled/action_filter + which credential
	// keys are set (never the values). omitempty drops the field when none, so
	// the logging SPA's /v1/config payload is unaffected on a forwarder-less host.
	if len(cfg.Forwarders) > 0 {
		fwds := make([]ForwarderInfo, 0, len(cfg.Forwarders))
		for _, fc := range cfg.Forwarders {
			fwds = append(fwds, ForwarderInfo{
				Name:           fc.Name,
				Type:           fc.Type,
				Enabled:        fc.Enabled,
				ActionFilter:   fc.ActionFilter,
				CredentialsSet: credentialKeysSet(fc.Credentials),
			})
		}
		resp.Forwarders = fwds
	}

	// Enrichment — masked (provider passwords reported set/unset, never the value).
	lookupInfo := lookupInfoFrom(cfg.Lookup)
	resp.Lookup = &lookupInfo

	// SMTP (config SPA Email tab) — masked: the password is reported set/unset via
	// password_set, never the value. Always served (the form needs the full shape).
	smtpInfo := smtpInfoFrom(cfg.Smtp)
	resp.Smtp = &smtpInfo

	// PSK Reporter (config SPA FT8 tab) — served RAW (no secrets to mask, sparse so
	// empty host/port show as defaults in the SPA). Always set so the form binds.
	psk := cfg.PskReporter
	resp.PskReporter = &psk

	// FT8 decode log (config SPA FT8 tab) — served so the form binds. A nil block in
	// config (never enabled) is served as a disabled zero value; an empty path means
	// "use the default" (resolved in internal/ft8 at open), shown as a placeholder.
	if cfg.Ft8.DecodeLog != nil {
		dl := *cfg.Ft8.DecodeLog
		resp.Ft8DecodeLog = &dl
	} else {
		resp.Ft8DecodeLog = &types.Ft8DecodeLogConfig{}
	}

	// Master subsystem switches — current values so the SPA toggles show state.
	bridgeEnabled := cfg.Bridge.Enabled
	ft8Enabled := cfg.Ft8.Enabled
	resp.BridgeEnabled = &bridgeEnabled
	resp.Ft8Enabled = &ft8Enabled

	// Mode-switch rig-restore preference — served RESOLVED (true when unset) so the
	// SPA gets a definite bool. Default is ON; only an explicit false disables it.
	restoreOnSwitch := cfg.RestoreRigOnModeSwitch == nil || *cfg.RestoreRigOnModeSwitch
	resp.RestoreRigOnModeSwitch = &restoreOnSwitch

	return resp, nil
}

// lookupProviderInfoFrom masks one provider for GET: the password becomes a
// set/unset flag, the value is dropped.
func lookupProviderInfoFrom(c types.LookupConfig) LookupProviderInfo {
	return LookupProviderInfo{
		Name:        c.Name,
		Enabled:     c.Enabled,
		URL:         c.URL,
		Username:    c.Username,
		PasswordSet: c.Password != "",
		TimeoutSec:  c.HttpTimeoutSec,
		ViewURL:     c.ViewURL,
	}
}

// smtpInfoFrom masks the SMTP block for GET: the password becomes a set/unset
// flag, the value is dropped. Every other field round-trips (none are secret in
// the way the password is).
func smtpInfoFrom(s types.SmtpConfig) SmtpInfo {
	return SmtpInfo{
		Enabled:          s.Enabled,
		Host:             s.Host,
		Port:             s.Port,
		Username:         s.Username,
		From:             s.From,
		DefaultRecipient: s.DefaultRecipient,
		StartTLS:         s.StartTLS,
		TimeoutSec:       s.TimeoutSec,
		PasswordSet:      s.Password != "",
	}
}

// mergeSmtp rebuilds the SMTP block from the PUT payload, keeping the stored
// password when the operator left the field blank — same keep-on-blank rule as
// mergeLookupProvider (masked-on-GET means the SPA never had the secret to echo).
func mergeSmtp(in SmtpInfo, ex types.SmtpConfig) types.SmtpConfig {
	pw := ex.Password
	if in.Password != "" {
		pw = in.Password
	}
	return types.SmtpConfig{
		Enabled:          in.Enabled,
		Host:             in.Host,
		Port:             in.Port,
		Username:         in.Username,
		Password:         pw,
		From:             in.From,
		DefaultRecipient: in.DefaultRecipient,
		StartTLS:         in.StartTLS,
		TimeoutSec:       in.TimeoutSec,
	}
}

func lookupInfoFrom(lc types.EnrichmentConfig) LookupInfo {
	chain := make([]LookupProviderInfo, 0, len(lc.Chain))
	for _, c := range lc.Chain {
		chain = append(chain, lookupProviderInfoFrom(c))
	}
	return LookupInfo{
		Hamnut:             lookupProviderInfoFrom(lc.Hamnut),
		Chain:              chain,
		CountryTTLDays:     lc.CountryTTLDays,
		StationTTLDays:     lc.StationTTLDays,
		RefreshMaxInFlight: lc.RefreshMaxInFlight,
	}
}

// mergeLookupProvider rebuilds a provider from the PUT payload, keeping the
// stored password when the operator left the field blank (masked-on-GET means
// the SPA never had it to echo). Matched against the stored entry `ex`.
func mergeLookupProvider(in LookupProviderInfo, ex types.LookupConfig) types.LookupConfig {
	pw := ex.Password
	if in.Password != "" {
		pw = in.Password
	}
	return types.LookupConfig{
		Name:           in.Name,
		Enabled:        in.Enabled,
		URL:            in.URL,
		Username:       in.Username,
		Password:       pw,
		HttpTimeoutSec: in.TimeoutSec,
		ViewURL:        in.ViewURL,
	}
}

// mergeLookup rebuilds the enrichment config from the PUT payload, merging each
// provider's password onto the stored value by name (hamnut + each chain entry).
func mergeLookup(in LookupInfo, existing types.EnrichmentConfig) types.EnrichmentConfig {
	exByName := make(map[string]types.LookupConfig, len(existing.Chain))
	for _, c := range existing.Chain {
		exByName[c.Name] = c
	}
	chain := make([]types.LookupConfig, 0, len(in.Chain))
	for _, p := range in.Chain {
		chain = append(chain, mergeLookupProvider(p, exByName[p.Name]))
	}
	return types.EnrichmentConfig{
		Hamnut:             mergeLookupProvider(in.Hamnut, existing.Hamnut),
		Chain:              chain,
		CountryTTLDays:     in.CountryTTLDays,
		StationTTLDays:     in.StationTTLDays,
		RefreshMaxInFlight: in.RefreshMaxInFlight,
	}
}

// credentialKeysSet returns the credential keys that currently hold a non-empty
// value, sorted — the masked GET view (the values themselves never leave the
// daemon). A non-string value is reported as set when non-null (all current
// forwarder creds are strings, but this stays robust to a future shape).
func credentialKeysSet(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			if s != "" {
				keys = append(keys, k)
			}
			continue
		}
		if v != nil {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// mergeForwarders builds the new forwarder list from the SPA's PUT payload,
// preserving secrets the masked-on-GET surface never exposed: credentials are
// merged onto the stored entry (matched by name) — a field the operator didn't
// type keeps its stored value — and the advanced knobs (tick/batch/retry) carry
// over from the stored entry too. A forwarder with no name match is treated as
// new (its supplied credentials stand alone).
func mergeForwarders(incoming []ForwarderInfo, existing []types.ForwarderConfig) []types.ForwarderConfig {
	byName := make(map[string]types.ForwarderConfig, len(existing))
	for _, fc := range existing {
		byName[fc.Name] = fc
	}
	out := make([]types.ForwarderConfig, 0, len(incoming))
	for _, in := range incoming {
		fc := types.ForwarderConfig{
			Name:         in.Name,
			Type:         in.Type,
			Enabled:      in.Enabled,
			ActionFilter: in.ActionFilter,
		}
		ex, matched := byName[in.Name]
		if matched {
			fc.TickIntervalSec = ex.TickIntervalSec
			fc.BatchSize = ex.BatchSize
			fc.Retry = ex.Retry
		}
		// Merge credentials: stored base overlaid with supplied (typed) fields.
		base := map[string]json.RawMessage{}
		if matched && len(ex.Credentials) > 0 {
			_ = json.Unmarshal(ex.Credentials, &base)
		}
		for k, v := range in.Credentials {
			if b, err := json.Marshal(v); err == nil {
				base[k] = b
			}
		}
		if len(base) > 0 {
			if rawCreds, err := json.Marshal(base); err == nil {
				fc.Credentials = rawCreds
			}
		}
		out = append(out, fc)
	}
	return out
}
