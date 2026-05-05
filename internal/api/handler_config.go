package api

import (
	stderr "errors"
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/config"
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
//
// PUT bodies use the same shape; the handler honours only writable
// fields (LoggingStation block, DefaultLogbook.ID, DefaultRig.ID).
// SetupComplete is server-managed — the handler ignores any value
// the client sends and decides the new state internally.
type ConfigResponse struct {
	SetupComplete  bool                 `json:"setup_complete"`
	LoggingStation types.LoggingStation `json:"logging_station"`
	DefaultLogbook types.Logbook        `json:"default_logbook"`
	DefaultRig     types.RigConfig      `json:"default_rig"`
	Station        types.StationConfig  `json:"station"`
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
func (s *Server) buildConfigResponse(r *http.Request, cfg config.Config) (ConfigResponse, error) {
	resp := ConfigResponse{
		SetupComplete:  cfg.SetupComplete,
		LoggingStation: cfg.LoggingStation,
		DefaultLogbook: types.Logbook{ID: cfg.DefaultLogbookID},
		DefaultRig:     types.RigConfig{ID: cfg.DefaultRigID},
		Station:        cfg.Station,
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

	// DefaultRig join: deferred until CAT lands and cfg.Rigs is
	// populated. The bare {ID: N} stub stands in for now so the SPA
	// has the field in place.

	return resp, nil
}
