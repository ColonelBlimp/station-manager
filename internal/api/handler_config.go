package api

import (
	stderr "errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
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
//   - DefaultRig: id from config.json; model/port joined from
//     cfg.Rigs (when CAT lands; for now the joined fields stay zero).
//   - Mailer: read-only projection of the SMTP block — only the SPA-
//     relevant subset (enabled flag + default recipient). Host / port /
//     username / password are deliberately not on the wire; SMTP creds
//     are operator-side config.json material, not UI-editable.
//
// PUT bodies use the same shape; the handler honours only writable
// fields (LoggingStation block, DefaultLogbook.ID, DefaultRig.ID).
// SetupComplete and Mailer are server-managed — the handler ignores
// any values the client sends and reasserts the authoritative state
// in the response.
type ConfigResponse struct {
	SetupComplete  bool                 `json:"setup_complete"`
	LoggingStation types.LoggingStation `json:"logging_station"`
	DefaultLogbook types.Logbook        `json:"default_logbook"`
	DefaultRig     types.RigConfig      `json:"default_rig"`
	Station        types.StationConfig  `json:"station"`
	Bridge         BridgeInfo           `json:"bridge"`
	Mailer         MailerInfo           `json:"mailer"`
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
// only when the rig exposes the ops it needs. Empty when the rig defines no
// exposed commands (e.g. the FT-710 today).
//
// Tune reports whether the connected rig can run the tune-carrier feature
// (ADR 0027) — the rigdef defines the set_mode/set_power/tx_on/tx_off
// commands the controller needs. The SPA shows the Tune button only when
// true; false (omitted) for a rig that can't tune (e.g. the FT-710 today).
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

	// Normalise + validate the incoming station_callsign. Empty is
	// allowed (pre-setup state); non-empty must satisfy the project's
	// callsign rules — same shape used by /v1/logbook and /v1/qso so
	// the operator gets one consistent failure mode regardless of
	// where they're entering a callsign.
	incomingCall := strings.ToUpper(strings.TrimSpace(req.LoggingStation.StationCallsign))
	req.LoggingStation.StationCallsign = incomingCall
	if incomingCall != "" && !isValidCallsign(incomingCall) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"station_callsign must be 3-32 characters and contain at least one digit", op)
		return
	}

	// Validate + normalise MyGridsquare and derive MyLat/MyLon. Empty
	// gridsquare clears the derived coordinates; an invalid gridsquare
	// is a 400 (operator types it via the My Station panel and gets a
	// validator there too — daemon-side check is the backstop). Zones
	// + DXCC are operator-typed strings; not derived from the grid.
	incomingGrid := utils.NormalizeMaidenhead(req.LoggingStation.MyGridsquare)
	if incomingGrid != "" && !utils.IsValidMaidenhead(incomingGrid) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"my_gridsquare must be a 4, 6, or 8 character Maidenhead locator", op)
		return
	}
	req.LoggingStation.MyGridsquare = incomingGrid
	if incomingGrid == "" {
		req.LoggingStation.MyLat = ""
		req.LoggingStation.MyLon = ""
	} else if lat, lon, ok := utils.MaidenheadToADIFLatLon(incomingGrid); ok {
		req.LoggingStation.MyLat = lat
		req.LoggingStation.MyLon = lon
	}

	// Validate MyCqZone (1-40 per CQWW), MyITUZone (1-90 per ITU
	// R-REC-V.6), and MyDXCC (0-522 per current ARRL DXCC list; 0 is
	// the "None" entity for maritime-mobile / non-DXCC QSOs). All
	// three are operator-typed strings; empty stays empty (pre-setup
	// or operator hasn't filled them in). When non-empty, malformed
	// values must not land in config.json — they'd be emitted on
	// every subsequent QSO's MY_* tags and rejected by ClubLog / LoTW.
	// Daemon-side enforcement is the backstop; the My Station panel
	// can pre-empt with a SPA-side validator for snappy feedback. The
	// 522 cap on DXCC is the current ARRL maximum at time of writing
	// — bump when ARRL adds a new entity (rare; once every few years).
	req.LoggingStation.MyCqZone = strings.TrimSpace(req.LoggingStation.MyCqZone)
	if req.LoggingStation.MyCqZone != "" {
		if !isValidZone(req.LoggingStation.MyCqZone, 1, 40) {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"my_cq_zone must be a number between 1 and 40", op)
			return
		}
	}
	req.LoggingStation.MyITUZone = strings.TrimSpace(req.LoggingStation.MyITUZone)
	if req.LoggingStation.MyITUZone != "" {
		if !isValidZone(req.LoggingStation.MyITUZone, 1, 90) {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"my_itu_zone must be a number between 1 and 90", op)
			return
		}
	}
	req.LoggingStation.MyDXCC = strings.TrimSpace(req.LoggingStation.MyDXCC)
	if req.LoggingStation.MyDXCC != "" {
		if !isValidZone(req.LoggingStation.MyDXCC, 0, 522) {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"my_dxcc must be a number between 0 and 522 (ARRL DXCC entity code; 0 = None)", op)
			return
		}
	}

	// Validate the amp multiplier. Negative values are nonsense; a
	// 1000x cap is a typo guard (real linear amps top out around 50x;
	// 1000 is well into "operator typed two extra zeros" territory).
	if req.Station.AmpMultiplier < 0 || req.Station.AmpMultiplier > 1000 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"station.amp_multiplier must be between 0 and 1000", op)
		return
	}

	// Validate the CAT-off default power. 0 means "not set" (ADIF
	// TX_PWR will be omitted). 2000W cap is a typo guard — legal
	// limits in most jurisdictions are ≈ 1500W, the headroom allows
	// for operators describing amp output before the multiplier
	// applies.
	if req.Station.DefaultPower < 0 || req.Station.DefaultPower > 2000 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"station.default_power must be between 0 and 2000", op)
		return
	}

	// Setup transition: if SetupComplete is currently false AND the
	// incoming callsign is non-empty, this PUT completes setup. Seed
	// the default logbook row before persisting config so the file
	// can record default_logbook_id immediately. Idempotent: if a
	// row at default_logbook_id already exists, no insert is done.
	current := s.cfg.Snapshot()
	completingSetup := !current.SetupComplete && incomingCall != ""

	var seededLogbookID int64
	if completingSetup {
		id, err := s.seedDefaultLogbook(r, current.DefaultLogbookID, incomingCall)
		if err != nil {
			s.writeServerError(w, op, err, "db_error", "failed to seed default logbook")
			return
		}
		seededLogbookID = id
	}

	// Mode-mapping overrides: diff against the rigdef's shipped
	// defaults so only operator-deviated entries get persisted.
	// Future rigdef updates can change defaults the operator never
	// touched without their overrides locking in the old values.
	// Skipped entirely when the request body doesn't carry a bridge
	// block (req.Bridge.Driver == "" — operator's PUT was about
	// something else like logging_station).
	var nextBridgeOverrides map[string]types.ModeMapping
	if req.Bridge.Driver != "" && req.Bridge.ModeMappings != nil {
		def, ok := cat.Lookup(req.Bridge.Driver)
		if !ok {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"bridge.driver does not match a known rigdef", op)
			return
		}
		nextBridgeOverrides = make(map[string]types.ModeMapping)
		for rigStr, mm := range req.Bridge.ModeMappings {
			// Validate first — both sides of the pair must be
			// catalogue-known. validateBridge re-checks on persist
			// (defence in depth) but we surface a 400 here with
			// the offending key for a friendlier error path.
			if mm.Mode == "" || !modes.IsValidMode(mm.Mode) {
				s.writeError(w, http.StatusBadRequest, "invalid_field_value",
					fmt.Sprintf("bridge.mode_mappings[%s].mode %q is not a known ADIF main mode", rigStr, mm.Mode), op)
				return
			}
			if mm.SubMode != "" && !modes.IsValidSubMode(mm.SubMode) {
				s.writeError(w, http.StatusBadRequest, "invalid_field_value",
					fmt.Sprintf("bridge.mode_mappings[%s].submode %q is not a known ADIF submode", rigStr, mm.SubMode), op)
				return
			}
			shipped, shippedOk := def.ModeMappings[rigStr]
			if !shippedOk || shipped != mm {
				nextBridgeOverrides[rigStr] = mm
			}
		}
	}

	if err := s.cfg.Update(func(cfg *config.Config) error {
		// Operator-writable fields. Server-managed fields
		// (SetupComplete, DefaultLogbookID/RigID — except via the
		// setup transition) are NOT touched from the request body.
		cfg.LoggingStation = req.LoggingStation
		cfg.Station = req.Station
		if completingSetup {
			cfg.SetupComplete = true
			if seededLogbookID != 0 {
				cfg.DefaultLogbookID = seededLogbookID
			}
			// Materialise ADIF identity on first setup: copy the
			// just-set callsign into Operator and OwnerCallsign when
			// the request didn't already provide them. One-shot — later
			// edits via the My Station panel are honoured as-is, no
			// re-sync.
			if cfg.LoggingStation.Operator == "" {
				cfg.LoggingStation.Operator = incomingCall
			}
			if cfg.LoggingStation.OwnerCallsign == "" {
				cfg.LoggingStation.OwnerCallsign = incomingCall
			}
		}
		if req.Bridge.Driver != "" && req.Bridge.ModeMappings != nil {
			if cfg.Bridge.ModeMappings == nil {
				cfg.Bridge.ModeMappings = make(map[string]map[string]types.ModeMapping)
			}
			if len(nextBridgeOverrides) > 0 {
				cfg.Bridge.ModeMappings[req.Bridge.Driver] = nextBridgeOverrides
			} else {
				delete(cfg.Bridge.ModeMappings, req.Bridge.Driver)
			}
		}
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
// Joins the default_logbook DB row when one exists; leaves the
// default_rig empty until CAT lands and cfg.Rigs is populated.
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

	// Merge mode mappings: rigdef defaults first, then operator
	// overrides on top. Operator's entry wins per-rig-string on
	// collision; entries the operator hasn't touched stay at the
	// shipped default.
	merged := make(map[string]types.ModeMapping, len(def.ModeMappings))
	for k, v := range def.ModeMappings {
		merged[k] = v
	}
	if perDriver, ok := b.ModeMappings[b.Cat.Driver]; ok {
		for k, v := range perDriver {
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
		DefaultRig:     types.RigConfig{ID: cfg.DefaultRigID},
		Station:        cfg.Station,
		Bridge:         bridgeInfoFor(cfg),
		Mailer: MailerInfo{
			Enabled:          s.mailer.Enabled(),
			DefaultRecipient: s.mailer.DefaultRecipient(),
		},
	}

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
	// active rig's display fields. The bare {ID: N} stub stands in when no
	// rig matches (a catalogue-less / bridge-disabled host).
	if rc := cfg.RigByID(cfg.DefaultRigID); rc != nil {
		resp.DefaultRig = *rc
	}

	return resp, nil
}
