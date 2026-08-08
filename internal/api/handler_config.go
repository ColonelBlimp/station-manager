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
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
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
	SetupComplete bool `json:"setup_complete"`
	// LoggingStation / Station are the operator-identity blocks (source: config.json).
	// Pointer-typed and **presence-aware on PUT** — like Qsl / Ft8Display below: a body
	// that omits a block leaves the stored one untouched. This closes a data-loss
	// footgun — as value types they were copied unconditionally, so a save carrying
	// only one block (e.g. a Station-tab save omitting logging_station) zeroed the
	// other's operator identity. Always populated (non-nil) on GET.
	LoggingStation *types.LoggingStation `json:"logging_station"`
	DefaultLogbook types.Logbook         `json:"default_logbook"`
	DefaultRig     DefaultRigInfo        `json:"default_rig"`
	Station        *types.StationConfig  `json:"station"`
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
	// Ft8Audio is the RX level meter's classification window (dBFS), always
	// served RESOLVED on GET (defaults + operator overrides) for the FT8 view's
	// level indicator. Read-only over /v1/config like Ft8Frequencies —
	// calibration is a config.json edit + restart (deliberate until the
	// defaults are hardware-calibrated); a PUT never carries it.
	Ft8Audio *types.Ft8AudioLevels `json:"ft8_audio,omitempty"`

	// Ft8Meter is the TX-drive (ALC) display threshold (ADR 0064), always
	// resolved (provisional default until calibrated on hardware).
	Ft8Meter *types.Ft8MeterLevels `json:"ft8_meter,omitempty"`
	// Ft8CallerAnswerMode is the DEFAULT FT8 Call-CQ answerer-selection mode
	// (ft8.tx.caller_answer_mode) — since ADR 0066 the live control is the
	// session's Answer selector in the TX control bar, and this field only
	// seeds it. Served RESOLVED on GET (default operator_pick since
	// 2026-08-08: automation is an explicit opt-in); PUT accepts all three
	// literals (fork 4 retired the ADR 0065 operator_pick fence along with
	// the config-only world it guarded). **Presence-aware** on PUT;
	// pointer-typed so the handler tells "sent" from "absent".
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
	// MapDisplay is the contacts-map display surface (the `map` block —
	// per-band arc colour overrides). No secrets and sparse-by-design, so the
	// canonical types.MapConfig rides the wire RAW like PskReporter: an absent
	// band means "use the SPA's built-in palette" and config.json never
	// materialises the defaults. Pointer = presence-aware on PUT (omit →
	// untouched; carry → replace the whole block); always set on GET. Applied
	// client-side — no daemon restart, the map picks it up on next load.
	MapDisplay *types.MapConfig `json:"map,omitempty"`
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
//   - On PUT: + Password (new value; blank = keep the stored one) and
//     PasswordClear (remove the stored one). PasswordSet is ignored.
//
// Username is shown on GET (a QRZ login/callsign, not a secret); only Password
// is masked. PasswordClear follows the same contract as SmtpInfo's — see
// resolveMaskedPassword, which both merges call — and is likewise a command
// rather than state: never emitted on GET, so echoing a GET body back cannot
// wipe a credential.
type LookupProviderInfo struct {
	Name string `json:"name"`
	// Label is READ-ONLY on this wire: served on GET so the section can display
	// it, ignored on PUT because config.json is the only place it may be set.
	Label         string `json:"label,omitempty"`
	Enabled       bool   `json:"enabled"`
	URL           string `json:"url,omitempty"`
	Username      string `json:"username,omitempty"`
	PasswordSet   bool   `json:"password_set"`
	Password      string `json:"password,omitempty"`
	PasswordClear bool   `json:"password_clear,omitempty"`
	TimeoutSec    int    `json:"timeout_sec,omitempty"`
	ViewURL       string `json:"view_url,omitempty"`
}

// LookupInfo mirrors types.EnrichmentConfig for the wire, with each provider's
// password masked (LookupProviderInfo). The Settings → Enrichment section (and
// the config SPA's Enrichment tab) edits hamnut + the callsign chain (QRZ
// today) + the cache TTLs.
//
// The TTLs carry types.EnrichmentConfig's pointer semantics onto the wire
// unchanged: OMIT one to mean "use the default", send an explicit 0 to mean
// "trust this cache indefinitely". GET always populates them (Normalize has
// resolved nil by then), so a client sees effective values rather than holes.
type LookupInfo struct {
	Hamnut             LookupProviderInfo   `json:"hamnut"`
	Chain              []LookupProviderInfo `json:"chain"`
	CountryTTLDays     *int                 `json:"country_ttl_days,omitempty"`
	StationTTLDays     *int                 `json:"station_ttl_days,omitempty"`
	RefreshMaxInFlight int                  `json:"refresh_max_in_flight"`
}

// ForwarderInfo is the config SPA's view of one forwarding destination. It is
// deliberately asymmetric to keep secrets off the wire (masked-on-GET):
//
//   - On GET: Name/Type/Enabled/ActionFilter + CredentialsSet (the credential
//     keys that currently hold a non-empty value — NEVER the values). Credentials
//     is nil/omitted.
//   - On PUT: Name/Type/Enabled/ActionFilter + Credentials (key→new value, only
//     the fields the operator typed). Omitted or BLANK both keep the stored value,
//     so a client never has to strip empties to avoid destroying a credential —
//     except for fields the type marks CredentialField.Clearable, where empty is a
//     meaningful value the constructor defaults (smcloud's `logbook` → "main").
//     CredentialsSet is ignored.
//
// So there is no per-field clear for anything a forwarder REQUIRES: the masked GET
// means a blank box is "untouched", and Smtp/LookupProvider can't distinguish
// absent from "" at all, so treating "" as a clear only ever fired by accident —
// and on a required field it produced a 200 followed by a daemon that won't
// restart. Removing or replacing the forwarder entry clears its credentials; a
// per-field clear for a required one would need an explicit signal
// (map[string]*string with JSON null), not an empty string.
//
// The advanced knobs (tick_interval_sec / batch_size / retry) are intentionally
// absent: the SPA doesn't edit them, and the PUT merge carries the stored values
// forward by name so a Forwarding-tab save never wipes an operator's hand-set
// values. ActionFilter is sent by the SPA (derived from the type's supported
// actions), so it round-trips without daemon defaulting.
type ForwarderInfo struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Enabled      bool     `json:"enabled"`
	ActionFilter []string `json:"action_filter,omitempty"`
	// Label is READ-ONLY on this wire: served on GET so the SPA can display it,
	// ignored on PUT because config.json is the only place it may be set.
	Label          string            `json:"label,omitempty"`
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
//   - On PUT: + Password (new value; blank = keep the stored one) and
//     PasswordClear (remove the stored one). PasswordSet is ignored.
//
// PasswordClear exists because blank has to go on meaning KEEP — it is what an
// operator editing the host sends every time — so removal needs a signal of its
// own. Deliberately NOT the forwarder Clearable idiom (where blank means reset
// for opted-in fields): that works there because opting in is decided per
// credential, and a password is never a safe field to opt in. It is a command,
// not state — only ever read from a PUT, never emitted on GET, so a client that
// echoes a GET body straight back cannot wipe the secret by accident.
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
	PasswordClear    bool   `json:"password_clear,omitempty"`
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

// errPutValidation is a sentinel the PUT /v1/config commit callback returns when
// the fully-overlaid config fails Validate under the lock. The blocking Finding is
// captured separately (it carries richer fields than an error string) so the
// handler maps it to a 400.
var errPutValidation = stderr.New("api: config validation failed")

// setupLogbookMismatchError is returned by seedDefaultLogbook when a logbook
// already exists at the target default id but under a DIFFERENT callsign than
// the one being set up. Reusing it would seed a default logbook whose callsign
// can never match live submits (review 2026-07-22 #1), so setup is rejected —
// and recovery stays MANUAL by design (operator's Option C on the codex review
// of e5da1945 #1: the API deliberately offers no clear-the-default path). It
// carries the existing callsign so the 409 can tell the operator exactly which
// callsign to set up under (the other recovery is removing that logbook from the
// database directly).
type setupLogbookMismatchError struct{ existingCallsign string }

func (e *setupLogbookMismatchError) Error() string {
	return "api: setup default logbook callsign mismatch (existing callsign " + e.existingCallsign + ")"
}

// forwarderStartupFinding rejects a forwarder list the daemon could not start
// with. It mirrors spawnForwarderWorkers EXACTLY — skip disabled, Build each
// enabled one — because that loop is precisely the failure it exists to pre-empt:
// config.Validate never inspects credentials, so a bad value used to return 200
// here and surface only at the next restart, as a daemon that refuses to come up
// long after the operator believed the save had worked. Three separate review
// findings (a blanked required credential, an unmarked clearable field,
// whitespace persisted verbatim) were all instances of that one gap; validating
// against the real constructors closes the family rather than the symptoms.
//
// Disabled forwarders are skipped because startup skips them — a destination must
// stay saveable while half-configured, before the operator switches it on.
//
// Build is side-effect-free (the constructors assemble a struct and an
// http.Client; no network, no files), so probing is cheap and safe.
//
// The returned Finding carries a STABLE, sanitised message; the constructor's own
// error comes back separately as cause, for the server log only. It must never
// reach the client: constructors format the offending value into their message
// (smcloud.New quotes credentials.url, which can carry userinfo — a token in the
// URL), and the stored value survives merging when an operator enables a
// previously-disabled entry without retyping it. Echoing it would disclose,
// through a 400 and the access log, exactly what GET /v1/config masks. Same split
// the 5xx path already uses: generic on the wire, real cause in the log.
func forwarderStartupFinding(fwds []types.ForwarderConfig) (*config.Finding, error) {
	for _, fc := range fwds {
		if !fc.Enabled {
			continue
		}
		if _, err := forwarding.Build(fc); err != nil {
			return &config.Finding{
				Field: "forwarders",
				Code:  "forwarder_unusable",
				// Name only — operator-chosen and already on GET. No credential
				// values, and no constructor text that might embed one.
				Message: fmt.Sprintf(
					"forwarder %q is enabled but its credentials are incomplete or invalid; "+
						"check its settings (details in the daemon log)", fc.Name),
			}, err
		}
	}
	return nil, nil
}

// firstBlockingFinding returns the first non-warning (fatal) finding, or nil when
// the config produced only advisories. A fatal finding is a 400 at PUT.
func firstBlockingFinding(findings []config.Finding) *config.Finding {
	for i := range findings {
		if !findings[i].Warning {
			return &findings[i]
		}
	}
	return nil
}

// overlayConfig applies the request's operator-writable fields onto base,
// presence-aware: a field is touched ONLY when the body carried it, so a save
// scoped to one surface (My Station, or Forwarding) can't zero another. base
// doubles as the merge source for the blank-keep credential merges
// (forwarders/lookup/smtp), so those keep whatever secret is CURRENTLY on base.
//
// Run INSIDE config.Update's callback against the fresh, lock-held clone (config
// review 2026-07-05 finding 1): overlaying onto the live config under the lock —
// rather than wholesale-replacing it with a value derived from a pre-lock
// Snapshot() — is what stops two concurrent saves to different surfaces from
// clobbering each other (the second's replace reverting the first). Assumes the
// request-only field validations (caller_answer_mode, max_repeats, mode-mapping
// driver) already passed in the handler. FT8 display is stored RAW so Validate can
// reject a bad feed_mode; the caller resolves it AFTER validation passes.
func overlayConfig(base *config.Config, req *ConfigResponse) {
	if req.LoggingStation != nil {
		base.LoggingStation = *req.LoggingStation
	}
	if req.Station != nil {
		base.Station = *req.Station
	}
	if req.Ft8Display != nil {
		base.Ft8.Display = req.Ft8Display
	}
	if req.Ft8CallerAnswerMode != nil {
		if base.Ft8.TX == nil {
			base.Ft8.TX = &types.Ft8TXConfig{}
		}
		base.Ft8.TX.CallerAnswerMode = *req.Ft8CallerAnswerMode
	}
	if req.Ft8MaxRepeats != nil {
		if base.Ft8.TX == nil {
			base.Ft8.TX = &types.Ft8TXConfig{}
		}
		base.Ft8.TX.MaxRepeats = *req.Ft8MaxRepeats
	}
	if req.Ft8FieldDay != nil {
		base.Ft8.FieldDay = &types.Ft8FieldDayConfig{
			Class:   strings.ToUpper(strings.TrimSpace(req.Ft8FieldDay.Class)),
			Section: strings.ToUpper(strings.TrimSpace(req.Ft8FieldDay.Section)),
			// RST_RCVD default is an operator-chosen report (e.g. "59", "-15") —
			// trimmed but NOT upper-cased; case is meaningless for a report.
			DefaultRstRcvd: strings.TrimSpace(req.Ft8FieldDay.DefaultRstRcvd),
		}
	}
	if req.Qsl != nil {
		base.Qsl = *req.Qsl
	}
	if req.Rigs != nil {
		base.Rigs = append([]types.RigConfig(nil), (*req.Rigs)...)
	}
	if req.DefaultRigID != nil {
		base.DefaultRigID = *req.DefaultRigID
	}
	if req.Forwarders != nil {
		base.Forwarders = mergeForwarders(req.Forwarders, base.Forwarders)
	}
	if req.Lookup != nil {
		base.Lookup = mergeLookup(*req.Lookup, base.Lookup)
	}
	if req.Smtp != nil {
		base.Smtp = mergeSmtp(*req.Smtp, base.Smtp)
	}
	if req.PskReporter != nil {
		base.PskReporter = *req.PskReporter
	}
	if req.MapDisplay != nil {
		base.Map = *req.MapDisplay
	}
	if req.Ft8DecodeLog != nil {
		base.Ft8.DecodeLog = req.Ft8DecodeLog
	}
	if req.BridgeEnabled != nil {
		base.Bridge.Enabled = *req.BridgeEnabled
	}
	if req.Ft8Enabled != nil {
		base.Ft8.Enabled = *req.Ft8Enabled
	}
	if req.RestoreRigOnModeSwitch != nil {
		base.RestoreRigOnModeSwitch = req.RestoreRigOnModeSwitch
	}
	// Mode-mapping overrides: diff the incoming set against the rigdef's shipped
	// defaults so only operator deviations persist, stored on the active rig
	// (config.md §10). The driver was validated in the handler.
	if req.Bridge.Driver != "" && req.Bridge.ModeMappings != nil {
		def, _ := cat.Lookup(req.Bridge.Driver)
		overrides := make(map[string]types.ModeMapping)
		for lit, mm := range req.Bridge.ModeMappings {
			if shipped, shippedOk := def.ModeMappings[lit]; !shippedOk || shipped != mm {
				overrides[lit] = mm
			}
		}
		// Copy the rig slice before mutating so the active rig's mappings don't
		// alias the live config. Base off base.Rigs (not a snapshot) so a `rigs`
		// block applied just above isn't clobbered by this overlay.
		base.Rigs = append([]types.RigConfig(nil), base.Rigs...)
		if rc := base.RigByID(base.DefaultRigID); rc != nil {
			if len(overrides) > 0 {
				rc.ModeMappings = overrides
			} else {
				rc.ModeMappings = nil
			}
		}
	}
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handlePutConfig"

	var req ConfigResponse
	if !s.readJSONBody(w, r, op, &req) {
		return
	}

	current := s.cfg.Snapshot()

	// Request-only field validations — hoisted ahead of the commit so the in-lock
	// overlay stays pure and a bad field is a loud 400 before we take the config
	// lock. Each checks the request value in isolation (it doesn't consult stored
	// config), matching feed_mode's strict-wire contract (vs Validate, which
	// tolerates a bad value → default).
	// All THREE literals are accepted since ADR 0066 fork 4: this PUT edits
	// the DEFAULT that seeds the session's Answer selector — the selector is
	// the live control, so the old ADR 0065 fence (400 on operator_pick,
	// guarding a world where config WAS the live control) is retired, and
	// with it the GET/PUT asymmetry a codex P1 once probed (5fbc3baa,
	// refuted then, moot now). Junk still 400s — silently resolving a typo
	// to a default the operator never chose is the failure the strict wire
	// check exists to prevent.
	if req.Ft8CallerAnswerMode != nil {
		if !types.Ft8CallerAnswerModeValid(*req.Ft8CallerAnswerMode) {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"ft8_caller_answer_mode must be auto_first, auto_strongest or operator_pick", op)
			return
		}
	}
	if req.Ft8MaxRepeats != nil {
		if n := *req.Ft8MaxRepeats; n < 1 || n > types.Ft8MaxRepeatsCeiling {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				fmt.Sprintf("ft8_max_repeats must be between 1 and %d", types.Ft8MaxRepeatsCeiling), op)
			return
		}
	}
	if req.Bridge.Driver != "" && req.Bridge.ModeMappings != nil {
		if _, ok := cat.Lookup(req.Bridge.Driver); !ok {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"bridge.driver does not match a known rigdef", op)
			return
		}
	}

	// A post-setup station-callsign change must not orphan the default logbook:
	// a live submit requires STATION_CALLSIGN == the logbook's callsign
	// (qsoservice submit gate), so silently changing it here would 200 but then
	// fail every subsequent QSO with callsign_mismatch (review 2026-07-22 #1).
	// Reject and direct the operator to reconcile logbooks first. (The deliberate
	// end-state — operating callsign follows the SELECTED logbook — is the
	// deferred per-logbook-identity feature; until then the callsign stays a
	// config field guarded against this footgun.) Skipped during first-run setup
	// (handled below) and for saves that don't carry My Station.
	if current.SetupComplete && req.LoggingStation != nil {
		newCall := strings.ToUpper(strings.TrimSpace(req.LoggingStation.StationCallsign))
		oldCall := strings.ToUpper(strings.TrimSpace(current.LoggingStation.StationCallsign))
		if newCall != oldCall && current.DefaultLogbookID != 0 {
			lbCall, lerr := s.db.LogbookCallsignByIDWithContext(r.Context(), current.DefaultLogbookID)
			switch {
			case stderr.Is(lerr, errors.ErrNotFound):
				// Default logbook already missing (dangling id) — nothing to
				// orphan, so allow the change. Only reachable from a pre-existing
				// inconsistent config; deleting the default is itself blocked.
			case lerr != nil:
				s.writeServerError(w, op, lerr, "db_error", "database operation failed")
				return
			case !strings.EqualFold(strings.TrimSpace(lbCall), newCall):
				s.writeError(w, http.StatusConflict, "callsign_locked_to_logbook",
					"changing your station callsign would orphan the default logbook; "+
						"create or select a logbook for the new callsign first", op)
				return
			}
		}
	}

	// First-run setup transition (single-operator; the first PUT carrying a
	// callsign). Seed the default logbook OUTSIDE the config lock — DB I/O must not
	// run under the config mutex — gated on a dry-run validation of the overlaid
	// config so an invalid first save doesn't proceed. seedDefaultLogbook is
	// idempotent, so a later retry reuses the row. This one-time path is NOT the
	// concurrent-write race the in-lock overlay below fixes.
	var setupLogbookID int64
	completingSetup := !current.SetupComplete &&
		req.LoggingStation != nil && req.LoggingStation.StationCallsign != ""
	if completingSetup {
		dry := current.Clone()
		overlayConfig(&dry, &req)
		config.Normalize(&dry)
		if f := firstBlockingFinding(config.Validate(dry)); f != nil {
			s.writeError(w, http.StatusBadRequest, f.Code, f.Message, op)
			return
		}
		// The forwarder probe belongs in the dry run too, not only in the commit
		// below: seedDefaultLogbook writes to the DB, and that write is NOT rolled
		// back when the in-lock validation later rejects. A setup PUT carrying an
		// unstartable forwarder would 400 with setup still incomplete but the
		// logbook row already created — and if the operator then corrected the
		// callsign as well, the retry would hit the orphaned row at the default id
		// and fail 409 default_logbook_callsign_mismatch, needing manual DB
		// surgery. Gating here is what the dry run is for.
		if req.Forwarders != nil {
			if f, cause := forwarderStartupFinding(dry.Forwarders); f != nil {
				s.logger.WarnWith().Err(cause).Str("code", f.Code).
					Msg("config PUT rejected during setup: enabled forwarder cannot be started")
				s.writeError(w, http.StatusBadRequest, f.Code, f.Message, op)
				return
			}
		}
		id, err := s.seedDefaultLogbook(r, dry.DefaultLogbookID, dry.LoggingStation.StationCallsign)
		if err != nil {
			var mismatch *setupLogbookMismatchError
			if stderr.As(err, &mismatch) {
				s.writeError(w, http.StatusConflict, "default_logbook_callsign_mismatch",
					fmt.Sprintf("a logbook already exists at the default id under callsign %q; "+
						"set up under that callsign, or remove that logbook from the database before retrying",
						mismatch.existingCallsign), op)
				return
			}
			s.writeServerError(w, op, err, "db_error", "failed to seed default logbook")
			return
		}
		setupLogbookID = id
	}

	// Commit under the config lock. The overlay is applied to the FRESH clone
	// config.Update hands us — NOT a pre-lock Snapshot() — so two SPAs saving
	// different surfaces at once can't clobber each other (the lost-update fix;
	// config review 2026-07-05 finding 1). Validate is authoritative here: a
	// concurrent change to another surface could invalidate this overlay (e.g. a
	// removed rig this body's default_rig_id points at), so it re-checks under the
	// lock and a failure aborts the write → 400, live config untouched.
	var blocking *config.Finding
	// forwarderCause is the constructor's raw error — logged, never sent to the
	// client (it can embed a credential value). Captured here rather than logged
	// inside the closure so nothing writes to the log under the config lock.
	var forwarderCause error
	// before/after bracket the mutation for the save record (SHIP GATE (a)).
	// Both are taken INSIDE the closure, against the fresh clone Update hands
	// us — diffing a pre-lock Snapshot against a post-Update one would
	// attribute a concurrent save's fields to this request, the same
	// lost-update trap the overlay itself avoids above.
	var before, after config.Config
	var setupCompleted bool
	if err := s.cfg.Update(func(cfg *config.Config) error {
		before = cfg.Clone()
		overlayConfig(cfg, &req)
		config.Normalize(cfg)
		if f := firstBlockingFinding(config.Validate(*cfg)); f != nil {
			blocking = f
			return errPutValidation
		}
		// Only when this request actually carried forwarders. A PUT can introduce
		// forwarder breakage ONLY through that block, and checking the merged list
		// unconditionally would let one pre-existing bad destination block every
		// unrelated save (station identity, FT8 settings…) until it was fixed.
		if req.Forwarders != nil {
			if f, cause := forwarderStartupFinding(cfg.Forwarders); f != nil {
				blocking, forwarderCause = f, cause
				return errPutValidation
			}
		}
		// FT8 display passed validation — resolve to the stored shape (clamp
		// colours / row cap; feed_mode already validated raw). Stored normalised so
		// GET serves what's on disk.
		if req.Ft8Display != nil {
			resolved := types.ResolveFt8Display(req.Ft8Display)
			cfg.Ft8.Display = &resolved
		}
		// Complete setup (after validation passes): flip the flag, adopt the seeded
		// logbook id, and materialise OPERATOR / OWNER_CALLSIGN from the callsign
		// when unset. Guarded on cfg.SetupComplete (the fresh value) so a racing
		// setup can't double-apply.
		if completingSetup && !cfg.SetupComplete {
			setupCompleted = true
			cfg.SetupComplete = true
			if setupLogbookID != 0 {
				cfg.DefaultLogbookID = setupLogbookID
			}
			call := cfg.LoggingStation.StationCallsign
			if cfg.LoggingStation.Operator == "" {
				cfg.LoggingStation.Operator = call
			}
			if cfg.LoggingStation.OwnerCallsign == "" {
				cfg.LoggingStation.OwnerCallsign = call
			}
			// Seed the operator roster from the just-established identity (ADR 0055).
			// applyDefaults does this at Load, but setup runs via this PUT — which
			// does not re-run applyDefaults — so without it the roster stays empty
			// until a restart (codex review of 23d2df7a, #3).
			config.SeedOperatorRoster(cfg)
		}
		// LAST, after every mutation above — this is the committed shape.
		after = cfg.Clone()
		return nil
	}); err != nil {
		if stderr.Is(err, errPutValidation) {
			if forwarderCause != nil {
				s.logger.WarnWith().Err(forwarderCause).Str("code", blocking.Code).
					Msg("config PUT rejected: enabled forwarder cannot be started")
			}
			s.writeError(w, http.StatusBadRequest, blocking.Code, blocking.Message, op)
			return
		}
		s.writeServerError(w, op, err, "config_write_error", "failed to persist config update")
		return
	}

	// Emitted here, immediately after the commit and BEFORE the response is
	// built: buildConfigResponse below reads the DB and can fail, turning a
	// change that IS on disk into an HTTP 500 (finding A8). Logging later
	// would leave exactly that case — the one where the operator is most
	// likely to be misled — with no record at all.
	s.logConfigSave(before, after, setupCompleted)

	// Live-apply the FT8 repeat cap to the running sequencer — the one config field
	// that takes effect without a restart, so the operator can dial it down mid-pile-up.
	// After the persist so a rejected/failed write never applies; nil-safe when FT8 is
	// off (s.ft8 == nil). Apply the COMMITTED value from a fresh snapshot, NOT the
	// request's own *req.Ft8MaxRepeats: two interleaved saves could otherwise persist
	// one value but live-apply the other, leaving the sequencer out of step with config
	// until the next restart (review 2026-07-22 #5). The snapshot reflects whichever
	// save committed last under the config lock.
	if req.Ft8MaxRepeats != nil {
		if tx := s.cfg.Snapshot().Ft8.TX; tx != nil {
			s.ft8.SetMaxRepeats(tx.MaxRepeats)
		}
	}

	resp, err := s.buildConfigResponse(r, s.cfg.Snapshot())
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// logConfigSave records a committed config change (SHIP GATE (a)). Silent when
// nothing moved: the SPA saves whole tabs, so a delta-free PUT is routine and a
// line per save would restore the noise this record exists to cut through.
//
// Info, not Warn: a successful save is normal operation. The volume is bounded
// by the delta rather than by how often the operator presses Save.
//
// Values follow config.Diff's policy — never a credential, URLs reduced to
// scheme + host, unrecognised paths redacted. smd.log is 0644 and this data
// came out of a 0600 file, so the allowlist lives with the diff and is not
// re-decided here.
func (s *Server) logConfigSave(before, after config.Config, setupCompleted bool) {
	changes := config.Diff(before, after)
	if len(changes) == 0 {
		return
	}
	ev := s.logger.InfoWith().
		Str("source", "api").
		Int("change_count", len(changes)).
		Interface("changes", changes)
	if setupCompleted {
		ev = ev.Bool("setup_completed", true)
	}
	ev.Msg("config saved")
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
		// Reusing a pre-existing default row is fine ONLY when its callsign
		// matches the callsign being set up — otherwise setup would adopt a
		// default logbook that can never accept this operator's live submits
		// (callsign_mismatch on every QSO). Surface it so the operator picks a
		// clean default id or a matching callsign (review 2026-07-22 #1).
		if !strings.EqualFold(strings.TrimSpace(existing.Callsign), strings.TrimSpace(callsign)) {
			return 0, &setupLogbookMismatchError{existingCallsign: existing.Callsign}
		}
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
		LoggingStation: &cfg.LoggingStation,
		DefaultLogbook: types.Logbook{ID: cfg.DefaultLogbookID},
		DefaultRig:     DefaultRigInfo{ID: cfg.DefaultRigID},
		Station:        &cfg.Station,
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

	// FT8 Call-CQ answerer-selection DEFAULT, resolved (operator_pick since
	// 2026-08-08) — the seed for the session's Answer selector (ADR 0066).
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

	// RX level meter window, resolved for the FT8 view's level indicator.
	audio := types.ResolveFt8Audio(cfg.Ft8.Audio)
	resp.Ft8Audio = &audio

	// TX-drive (ALC) display threshold (ADR 0064), resolved; the default is
	// provisional pending on-hardware calibration.
	meter := types.ResolveFt8Meter(cfg.Ft8.Meter)
	resp.Ft8Meter = &meter

	// Bridge timeouts + tune params, served RESOLVED (defaults filled, ceilings
	// applied) like the FT8 blocks above — config.json stays sparse. Uses the same
	// resolution the running Service applies, so the SPA reads the daemon's
	// effective values even though they aren't materialised on disk.
	bridgeTimeouts := bridge.ResolveTimeouts(cfg.Bridge.Timeouts)
	resp.BridgeTimeouts = &bridgeTimeouts
	bridgeDriver := ""
	if cfg.Bridge.Cat != nil { // nil when the bridge isn't configured (config.md §10)
		bridgeDriver = cfg.Bridge.Cat.Driver
	}
	bridgeTune := bridge.ResolveTune(cfg.Bridge.Tune, bridgeDriver)
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
				Label:          fc.Label,
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

	// Contacts-map display (config SPA Map surface + the app SPA's map view) —
	// served RAW like PskReporter: sparse overrides only, defaults live in the SPA.
	mapDisplay := cfg.Map
	resp.MapDisplay = &mapDisplay

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
		Label:       c.Label,
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
// mergeLookupProvider (masked-on-GET means the SPA never had the secret to echo)
// — unless PasswordClear asks for it to be removed outright.
//
// Clear beats a typed value — OPERATOR'S RULING, 2026-08-03, weighed against
// rejecting the pair with a 400: clear-wins is fail-safe for secret removal and
// handles stale form state sensibly. The two are mutually exclusive intents and
// our own client never sends both (pressing Remove discards any half-typed
// value, as forwarding's clear() does), so this arm only fires for a client
// that got it wrong — and of the two, only the flag can have been set
// deliberately: a stale password field is exactly what a form bug leaves
// populated, whereas the flag is set solely by pressing the control.
func mergeSmtp(in SmtpInfo, ex types.SmtpConfig) types.SmtpConfig {
	pw := resolveMaskedPassword(in.PasswordClear, in.Password, ex.Password)
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

// resolveMaskedPassword is the ONE rule every masked-on-GET password field
// follows, so the SMTP block and the lookup providers cannot drift apart on it:
//
//   - clear set      → removed. The only way to delete a stored secret.
//   - typed value    → replaces the stored one.
//   - blank, no clear→ the stored one is KEPT. This is the common case: it is
//     what an operator editing any OTHER field sends on every save, which is
//     exactly why blank must not be overloaded to mean "erase".
//
// Clear beats a typed value (operator's ruling, 2026-08-03: fail-safe for secret
// removal, and sensible against stale form state). Our own clients never send
// both — pressing Remove discards any half-typed value — so that arm exists for
// a client that got it wrong, and of the two only the flag can have been set
// deliberately: a stale password field is what a form bug leaves populated.
func resolveMaskedPassword(clear bool, typed, stored string) string {
	switch {
	case clear:
		return ""
	case typed != "":
		return typed
	default:
		return stored
	}
}

// mergeLookupProvider rebuilds a provider from the PUT payload, keeping the
// stored password when the operator left the field blank (masked-on-GET means
// the SPA never had it to echo). Matched against the stored entry `ex`.
func mergeLookupProvider(in LookupProviderInfo, ex types.LookupConfig) types.LookupConfig {
	pw := resolveMaskedPassword(in.PasswordClear, in.Password, ex.Password)
	return types.LookupConfig{
		Name: in.Name,
		// Label is config.json-only: no API surface writes it, so it is absent
		// from every PUT and would be DELETED by this rebuild unless carried
		// over explicitly. Same defect class as mergeForwarders' Label and
		// Endpoints (see the note there) — and this rebuild, like that one,
		// keeps only what it names. Taking it from `ex` rather than `in` is
		// also what makes a label sent by a client a no-op rather than a rename.
		Label:          ex.Label,
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

// clearableCredentialKeys reports which of a forwarder type's credential keys
// accept a BLANK value on PUT as "reset to the default" (CredentialField.Clearable).
// Everything not listed — secrets, and required text fields alike — keeps its
// stored value when sent blank, because GET never echoes credential values so a
// blank field means "not retyped" far more often than "erase this".
//
// An unregistered type (a forwarder from a build that doesn't include it) yields
// an empty set, so nothing is clearable: refusing to erase a value we cannot
// classify is the recoverable choice.
func clearableCredentialKeys(typeName string) map[string]bool {
	for _, d := range forwarding.ForwarderTypes() {
		if d.Type != typeName {
			continue
		}
		keys := make(map[string]bool, len(d.CredentialFields))
		for _, f := range d.CredentialFields {
			if f.Clearable {
				keys[f.Key] = true
			}
		}
		return keys
	}
	return nil
}

// mergeForwarders builds the new forwarder list from the SPA's PUT payload,
// preserving secrets the masked-on-GET surface never exposed: credentials are
// merged onto the stored entry (matched by name) — an omitted field always keeps
// its stored value, and a blank one keeps it for password-kind fields — and the
// advanced knobs (tick/batch/retry) carry over from the stored entry too. A
// forwarder with no name match is treated as new (its supplied credentials stand
// alone).
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
			// Label and Endpoints are config.json-only: no API surface writes
			// them, so they are absent from every PUT and would be DELETED by
			// this rebuild unless carried over explicitly.
			//
			// Endpoints is the older of the two and the loss was silent: the
			// save wrote the map out empty and applyDefaults re-seeded the
			// registry DEFAULT at the next Load (config.go:1077), so an
			// operator's override was replaced by the stock URL and only a
			// config diff would have shown it. Pinned by L2/L3 in
			// forwarder_label_test.go.
			//
			// Anything else added to ForwarderConfig that the SPA does not send
			// belongs here too — this rebuild keeps only what it names.
			fc.Label = ex.Label
			fc.Endpoints = ex.Endpoints
		}
		// Merge credentials: stored base overlaid with supplied (typed) fields. A
		// blank KEEPS the stored value unless the field is explicitly Clearable.
		//
		// Keep is the default because GET never echoes credential values, so a blank
		// field overwhelmingly means "not retyped" — and the old clear-on-blank made
		// the safety of every stored secret depend on the client stripping empties
		// before the PUT (the config SPA does; nothing else has to). Destroying a
		// credential must not be the default reading of an empty field.
		//
		// Clearable is a per-field declaration, NOT "any non-password field": most
		// text credentials are REQUIRED (ClubLog email/callsign, SM Cloud url), and
		// emptying one isn't a reset — the forwarder's New() rejects it, which aborts
		// spawnForwarderWorkers and takes the whole daemon down at the next restart,
		// with the PUT having returned 200 because config validation doesn't check
		// credentials. Only a field whose constructor DEFAULTS an empty value
		// (smcloud's `logbook` → "main") may be blanked.
		clearable := clearableCredentialKeys(in.Type)
		base := map[string]json.RawMessage{}
		if matched && len(ex.Credentials) > 0 {
			_ = json.Unmarshal(ex.Credentials, &base)
		}
		for k, v := range in.Credentials {
			if strings.TrimSpace(v) == "" {
				if !clearable[k] {
					continue
				}
				// Store the canonical blank, not the whitespace that was sent. The
				// classification above uses TrimSpace, so persisting the raw value
				// would hand the constructor something it never agreed was empty:
				// smcloud.New trims its logbook and copes, but stub.New compares
				// mode against "" exactly, so a stored " " reaches its unknown-mode
				// branch and the daemon refuses to start. Deciding "this is blank"
				// and then writing something else is the bug — write what we decided.
				v = ""
			}
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
