package api

import (
	stderr "errors"
	"net/http"

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
// fields (the LoggingStation / Station blocks, Qsl, Ft8Display, and the
// Bridge mode-mapping overlay). SetupComplete, Mailer, and DefaultRig are
// server-managed/read-only — the handler ignores any values the client
// sends (the default rig is selected via PUT /v1/rigs' default_rig_id, not
// here) and reasserts the authoritative state in the response.
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
	// QSL defaults — presence-aware, same rationale as ft8_display: a My Station
	// save omits `qsl` and must leave it alone.
	if req.Qsl != nil {
		candidate.Qsl = *req.Qsl
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
		// alias the live config (candidate := current is a shallow copy).
		candidate.Rigs = append([]types.RigConfig(nil), current.Rigs...)
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

	// FT8 per-band dial frequencies, always resolved (defaults + overrides) for the
	// SPA's Main-Freq band buttons.
	resp.Ft8Frequencies = types.ResolveFt8Frequencies(cfg.Ft8.Frequencies)

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

	return resp, nil
}
