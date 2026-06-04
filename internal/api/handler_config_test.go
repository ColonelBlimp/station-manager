package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestHandleGetConfig_PreSetup covers the first-boot shape: setup
// not complete, default IDs in place, no logbook row yet, so the
// joined default_logbook is just the bare {id: N} stub.
func TestHandleGetConfig_PreSetup(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.SetupComplete {
		t.Error("SetupComplete = true on a fresh server, want false")
	}
	if resp.DefaultLogbook.ID != 1 {
		t.Errorf("DefaultLogbook.ID = %d, want 1 (default)", resp.DefaultLogbook.ID)
	}
	if resp.DefaultLogbook.Name != "" || resp.DefaultLogbook.Callsign != "" {
		t.Errorf("Pre-setup default_logbook should have empty name/callsign; got %+v", resp.DefaultLogbook)
	}
	if resp.DefaultRig.ID != 1 {
		t.Errorf("DefaultRig.ID = %d, want 1 (default)", resp.DefaultRig.ID)
	}
	if resp.LoggingStation.StationCallsign != "" {
		t.Errorf("Pre-setup station_callsign should be empty; got %q", resp.LoggingStation.StationCallsign)
	}
}

// TestBridgeInfoFor_Ops pins the capability wiring: a configured FTdx10 driver
// surfaces its exposed ops in BridgeInfo so the SPA can gate rig-control
// surfaces (ADR 0026).
func TestBridgeInfoFor_Ops(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	cfg.Bridge.Enabled = true
	cfg.Bridge.Cat.Driver = "yaesu-ftdx10"

	info := bridgeInfoFor(cfg)

	want := []string{"set_freq", "set_mode", "set_vfo"}
	if len(info.Ops) != len(want) {
		t.Fatalf("BridgeInfo.Ops = %v, want %v", info.Ops, want)
	}
	for i := range want {
		if info.Ops[i] != want[i] {
			t.Errorf("Ops[%d] = %q, want %q", i, info.Ops[i], want[i])
		}
	}
}

// TestHandlePutConfig_FirstSetup_SeedsLogbook is the critical
// transition: a non-empty station_callsign, server flips
// SetupComplete to true, inserts a logbook row at id=1, and the
// follow-up GET reflects all three changes.
func TestHandlePutConfig_FirstSetup_SeedsLogbook(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.SetupComplete {
		t.Error("SetupComplete should be true after a successful first-setup PUT")
	}
	if resp.LoggingStation.StationCallsign != "M0XYZ" {
		t.Errorf("StationCallsign = %q, want M0XYZ", resp.LoggingStation.StationCallsign)
	}
	if resp.DefaultLogbook.ID == 0 {
		t.Error("DefaultLogbook.ID = 0 after setup; expected the seeded logbook's id")
	}
	if resp.DefaultLogbook.Callsign != "M0XYZ" {
		t.Errorf("DefaultLogbook.Callsign = %q, want M0XYZ", resp.DefaultLogbook.Callsign)
	}
	if resp.DefaultLogbook.Name != "Default" {
		t.Errorf("DefaultLogbook.Name = %q, want Default", resp.DefaultLogbook.Name)
	}

	// The logbook row really exists in the DB — fetch it via the
	// other handler to prove the seed wasn't a response-only fiction.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/logbook/1", nil)
	getReq.SetPathValue("id", "1")
	getW := httptest.NewRecorder()
	srv.handleGetLogbook(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("logbook fetch after seed: status = %d, body = %s", getW.Code, getW.Body.String())
	}
}

// TestHandlePutConfig_LowercaseCallsignNormalised confirms the
// handler upper-cases the incoming station_callsign before persisting,
// matching the rule used by /v1/logbook and /v1/qso submission.
func TestHandlePutConfig_LowercaseCallsignNormalised(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "  m0xyz  "}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.LoggingStation.StationCallsign != "M0XYZ" {
		t.Errorf("StationCallsign = %q, want normalised M0XYZ", resp.LoggingStation.StationCallsign)
	}
}

// TestHandlePutConfig_InvalidCallsignReturns400 covers the validation
// path. The same callsign rule used elsewhere applies here so the
// operator sees one consistent failure mode.
func TestHandlePutConfig_InvalidCallsignReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "AB"}}` // too short
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Errorf("body = %s, want invalid_field_value envelope", w.Body.String())
	}
}

// TestHandlePutConfig_EmptyCallsignAccepted covers the legitimate
// pre-setup state: the operator might issue a PUT before they've
// chosen a callsign (defensive check on the SPA boot path). Empty
// is allowed; SetupComplete stays false; no logbook is seeded.
func TestHandlePutConfig_EmptyCallsignAccepted(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": ""}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.SetupComplete {
		t.Error("SetupComplete flipped to true with empty callsign; should stay false")
	}
}

// TestHandlePutConfig_SetupCompleteFromBodyIgnored is the security
// check on the server-managed flag. A client sending
// setup_complete: true with empty callsign must NOT be able to
// satisfy the setup gate and skip the dialog.
func TestHandlePutConfig_SetupCompleteFromBodyIgnored(t *testing.T) {
	srv := testServer(t)

	body := `{"setup_complete": true, "logging_station": {"station_callsign": ""}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.SetupComplete {
		t.Error("Client-supplied setup_complete=true was honoured; flag must be server-managed only")
	}
}

// TestHandlePutConfig_SetupCompleteIdempotent confirms a re-PUT after
// setup doesn't double-seed the logbook. The second PUT updates the
// callsign block but the original logbook id is preserved (the seed
// is gated on the false→true transition only).
func TestHandlePutConfig_SetupCompleteIdempotent(t *testing.T) {
	srv := testServer(t)

	// First PUT — completes setup.
	first := `{"logging_station": {"station_callsign": "M0XYZ"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(first))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first PUT status = %d", w.Code)
	}

	// Second PUT — operator changes their callsign.
	second := `{"logging_station": {"station_callsign": "G0ABC"}}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(second))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second PUT status = %d, body = %s", w2.Code, w2.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp.LoggingStation.StationCallsign != "G0ABC" {
		t.Errorf("StationCallsign after second PUT = %q, want G0ABC", resp.LoggingStation.StationCallsign)
	}
	// Logbook callsign was set during seed and is owned by
	// /v1/logbook/{id} now — the second PUT to /v1/config must NOT
	// overwrite it.
	if resp.DefaultLogbook.Callsign != "M0XYZ" {
		t.Errorf("DefaultLogbook.Callsign was rewritten by second PUT; want M0XYZ stays, got %q",
			resp.DefaultLogbook.Callsign)
	}
}

// TestHandlePutConfig_FirstSetup_MaterialisesOperatorAndOwner covers
// the ADIF-identity seed: on the false→true setup transition, when the
// request body doesn't supply Operator / OwnerCallsign, the daemon
// fills both with the just-set StationCallsign. Later edits via My
// Station are honoured as-is — only the first setup transition seeds.
func TestHandlePutConfig_FirstSetup_MaterialisesOperatorAndOwner(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.LoggingStation.Operator != "M0XYZ" {
		t.Errorf("Operator = %q, want M0XYZ (seeded from station_callsign)",
			resp.LoggingStation.Operator)
	}
	if resp.LoggingStation.OwnerCallsign != "M0XYZ" {
		t.Errorf("OwnerCallsign = %q, want M0XYZ (seeded from station_callsign)",
			resp.LoggingStation.OwnerCallsign)
	}
}

// TestHandlePutConfig_FirstSetup_RespectsOperatorAndOwnerWhenProvided
// confirms the seed is conditional: a request that supplies non-empty
// Operator / OwnerCallsign is honoured as-is, no overwrite. Covers
// the club-station case where the operator is logging at someone else's
// station and the three callsigns differ.
func TestHandlePutConfig_FirstSetup_RespectsOperatorAndOwnerWhenProvided(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ", "operator": "G0ABC", "owner_callsign": "G7DEF"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.LoggingStation.Operator != "G0ABC" {
		t.Errorf("Operator = %q, want G0ABC (preserved from request)",
			resp.LoggingStation.Operator)
	}
	if resp.LoggingStation.OwnerCallsign != "G7DEF" {
		t.Errorf("OwnerCallsign = %q, want G7DEF (preserved from request)",
			resp.LoggingStation.OwnerCallsign)
	}
}

// TestHandlePutConfig_PostSetupNoMaterialisation confirms the seed is
// one-shot: a later PUT (setup already complete) that clears Operator
// and OwnerCallsign does NOT re-seed them from station_callsign. The
// operator's edits via My Station are authoritative.
func TestHandlePutConfig_PostSetupNoMaterialisation(t *testing.T) {
	srv := testServer(t)

	// First PUT completes setup — Operator and OwnerCallsign get seeded.
	first := `{"logging_station": {"station_callsign": "M0XYZ"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(first))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first PUT status = %d", w.Code)
	}

	// Second PUT — operator deliberately blanks Operator / OwnerCallsign.
	// This is post-setup, so the materialisation path must NOT fire.
	second := `{"logging_station": {"station_callsign": "M0XYZ", "operator": "", "owner_callsign": ""}}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(second))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second PUT status = %d, body = %s", w2.Code, w2.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp.LoggingStation.Operator != "" {
		t.Errorf("Operator = %q after post-setup blank; want empty (seed must be one-shot)",
			resp.LoggingStation.Operator)
	}
	if resp.LoggingStation.OwnerCallsign != "" {
		t.Errorf("OwnerCallsign = %q after post-setup blank; want empty (seed must be one-shot)",
			resp.LoggingStation.OwnerCallsign)
	}
}

// TestHandlePutConfig_PersistsToFile verifies the on-disk write
// happens — without it the daemon would forget the callsign on next
// restart even though the in-memory cfg looked right.
func TestHandlePutConfig_PersistsToFile(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// The path was set on cfgSvc by the test helper. Read it back.
	path := srv.cfg.Path
	if path == "" {
		t.Fatal("cfgSvc.Path was not set by the test helper")
	}
	// Just check the file exists and has the new callsign in it.
	// A full re-Load is exercised by config package tests; here we
	// only need to confirm the write happened.
	got := readFileForTest(t, path)
	if !strings.Contains(got, "M0XYZ") {
		t.Errorf("config file does not contain M0XYZ; got: %s", got)
	}
	if !strings.Contains(got, `"setup_complete": true`) {
		t.Errorf("config file should record setup_complete=true; got: %s", got)
	}
}

// TestHandlePutConfig_DerivesLatLonFromGridsquare confirms the daemon
// fills MY_LAT / MY_LON from MY_GRIDSQUARE on PUT and ignores any
// client-supplied values for those derived fields. The wire format is
// ADIF "XDDD MM.MMM" — the same shape used elsewhere for coordinates.
func TestHandlePutConfig_DerivesLatLonFromGridsquare(t *testing.T) {
	srv := testServer(t)

	// Client sends bogus lat/lon alongside a valid grid. The daemon
	// must overwrite both with the centre of the IO91 cell.
	body := `{"logging_station": {"station_callsign": "M0XYZ", "my_gridsquare": "IO91", "my_lat": "N000 00.000", "my_lon": "W000 00.000"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.LoggingStation.MyGridsquare != "IO91" {
		t.Errorf("MyGridsquare = %q, want IO91", resp.LoggingStation.MyGridsquare)
	}
	if resp.LoggingStation.MyLat != "N051 30.000" {
		t.Errorf("MyLat = %q, want N051 30.000 (centre of IO91)", resp.LoggingStation.MyLat)
	}
	if resp.LoggingStation.MyLon != "W001 00.000" {
		t.Errorf("MyLon = %q, want W001 00.000 (centre of IO91)", resp.LoggingStation.MyLon)
	}
}

// TestHandlePutConfig_NormalisesGridsquareCase confirms the on-the-wire
// canonical form: upper field, lower subsquare. Operator types in any
// case; daemon stores the canonical form so subsequent GETs and ADIF
// emissions are consistent.
func TestHandlePutConfig_NormalisesGridsquareCase(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ", "my_gridsquare": "io91VL"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.LoggingStation.MyGridsquare != "IO91vl" {
		t.Errorf("MyGridsquare = %q, want canonical IO91vl", resp.LoggingStation.MyGridsquare)
	}
}

// TestHandlePutConfig_EmptyGridsquareClearsLatLon confirms blanking the
// gridsquare also clears the derived coordinates — the alternative
// (stale lat/lon hanging around from a previous grid) would leak into
// QSO submissions and country-panel bearing math.
func TestHandlePutConfig_EmptyGridsquareClearsLatLon(t *testing.T) {
	srv := testServer(t)

	// First PUT — derives lat/lon from IO91.
	first := `{"logging_station": {"station_callsign": "M0XYZ", "my_gridsquare": "IO91"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(first))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first PUT status = %d", w.Code)
	}

	// Second PUT — operator clears the grid.
	second := `{"logging_station": {"station_callsign": "M0XYZ", "my_gridsquare": ""}}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(second))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second PUT status = %d, body = %s", w2.Code, w2.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp.LoggingStation.MyGridsquare != "" {
		t.Errorf("MyGridsquare = %q after blank, want empty", resp.LoggingStation.MyGridsquare)
	}
	if resp.LoggingStation.MyLat != "" {
		t.Errorf("MyLat = %q after blanking grid, want empty (stale-coord leak)", resp.LoggingStation.MyLat)
	}
	if resp.LoggingStation.MyLon != "" {
		t.Errorf("MyLon = %q after blanking grid, want empty (stale-coord leak)", resp.LoggingStation.MyLon)
	}
}

// TestHandlePutConfig_StationAmpRoundTrip confirms the amp pair
// (amp_enabled / amp_multiplier) survives a PUT and shows up on the
// next GET. The SPA's effectivePower derivation reads these directly
// so they must round-trip exactly.
func TestHandlePutConfig_StationAmpRoundTrip(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}, "station": {"amp_enabled": true, "amp_multiplier": 10}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Station.AmpEnabled {
		t.Errorf("Station.AmpEnabled = false, want true")
	}
	if resp.Station.AmpMultiplier != 10 {
		t.Errorf("Station.AmpMultiplier = %v, want 10", resp.Station.AmpMultiplier)
	}
}

// TestHandlePutConfig_NegativeAmpMultiplierReturns400 — negative gain
// is nonsense. Reject at the daemon as the authoritative validator;
// the SPA can defend the same boundary client-side later if needed.
func TestHandlePutConfig_NegativeAmpMultiplierReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}, "station": {"amp_enabled": true, "amp_multiplier": -1}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "amp_multiplier") {
		t.Errorf("body = %s, want amp_multiplier in error", w.Body.String())
	}
}

// TestHandlePutConfig_DefaultPowerRoundTrip confirms the CAT-off
// default power persists across a PUT/GET cycle. The SPA reads this
// directly to populate displayedState.rawPower when CAT is unavailable.
func TestHandlePutConfig_DefaultPowerRoundTrip(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}, "station": {"default_power": 100}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Station.DefaultPower != 100 {
		t.Errorf("Station.DefaultPower = %v, want 100", resp.Station.DefaultPower)
	}
}

// TestHandlePutConfig_NegativeDefaultPowerReturns400 — sanity guard.
func TestHandlePutConfig_NegativeDefaultPowerReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}, "station": {"default_power": -10}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "default_power") {
		t.Errorf("body = %s, want default_power in error", w.Body.String())
	}
}

// TestHandlePutConfig_AbsurdDefaultPowerReturns400 — typo guard at 2000W cap.
func TestHandlePutConfig_AbsurdDefaultPowerReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}, "station": {"default_power": 9999}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
}

// TestHandlePutConfig_AbsurdAmpMultiplierReturns400 — the typo guard.
// Reject values above 1000 as obvious operator error.
func TestHandlePutConfig_AbsurdAmpMultiplierReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ"}, "station": {"amp_enabled": true, "amp_multiplier": 9999}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
}

// TestHandlePutConfig_InvalidGridsquareReturns400 is the validation
// backstop. The SPA validates client-side too, but the daemon is the
// authority: a malformed grid never reaches persistence.
func TestHandlePutConfig_InvalidGridsquareReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"station_callsign": "M0XYZ", "my_gridsquare": "ZZ99xx"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "my_gridsquare") {
		t.Errorf("body = %s, want my_gridsquare in error message", w.Body.String())
	}
}

// TestHandlePutConfig_LoggingStationZoneValidation covers the
// CQ Zone (1-40), ITU Zone (1-90), and DXCC (0-522) range checks
// that block malformed values from landing in config.json — they'd
// otherwise be emitted on every QSO's MY_* tags and rejected by
// downstream services (ClubLog, LoTW). Empty stays empty; non-empty
// must satisfy the range.
func TestHandlePutConfig_LoggingStationZoneValidation(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantSubstr string // substring expected in the error envelope
	}{
		// Valid cases — all bounds + interior values pass.
		{name: "empty zones accepted", body: `{"logging_station": {}}`, wantStatus: http.StatusOK},
		{name: "cq=1 accepted", body: `{"logging_station": {"my_cq_zone": "1"}}`, wantStatus: http.StatusOK},
		{name: "cq=40 accepted", body: `{"logging_station": {"my_cq_zone": "40"}}`, wantStatus: http.StatusOK},
		{name: "itu=1 accepted", body: `{"logging_station": {"my_itu_zone": "1"}}`, wantStatus: http.StatusOK},
		{name: "itu=90 accepted", body: `{"logging_station": {"my_itu_zone": "90"}}`, wantStatus: http.StatusOK},
		{name: "dxcc=0 accepted (None / maritime)", body: `{"logging_station": {"my_dxcc": "0"}}`, wantStatus: http.StatusOK},
		{name: "dxcc=522 accepted (current ARRL max)", body: `{"logging_station": {"my_dxcc": "522"}}`, wantStatus: http.StatusOK},
		{name: "all three valid", body: `{"logging_station": {"my_cq_zone": "14", "my_itu_zone": "27", "my_dxcc": "223"}}`, wantStatus: http.StatusOK},

		// CQ Zone — out of range / malformed.
		{name: "cq=0 rejected", body: `{"logging_station": {"my_cq_zone": "0"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_cq_zone"},
		{name: "cq=41 rejected", body: `{"logging_station": {"my_cq_zone": "41"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_cq_zone"},
		{name: "cq=non-numeric rejected", body: `{"logging_station": {"my_cq_zone": "37x"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_cq_zone"},
		{name: "cq=negative rejected", body: `{"logging_station": {"my_cq_zone": "-3"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_cq_zone"},

		// ITU Zone — out of range / malformed.
		{name: "itu=0 rejected", body: `{"logging_station": {"my_itu_zone": "0"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_itu_zone"},
		{name: "itu=91 rejected", body: `{"logging_station": {"my_itu_zone": "91"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_itu_zone"},
		{name: "itu=non-numeric rejected", body: `{"logging_station": {"my_itu_zone": "abc"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_itu_zone"},

		// DXCC — out of range / malformed.
		{name: "dxcc=-1 rejected", body: `{"logging_station": {"my_dxcc": "-1"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_dxcc"},
		{name: "dxcc=523 rejected", body: `{"logging_station": {"my_dxcc": "523"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_dxcc"},
		{name: "dxcc=non-numeric rejected", body: `{"logging_station": {"my_dxcc": "USA"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_dxcc"},
		{name: "dxcc=fractional rejected", body: `{"logging_station": {"my_dxcc": "291.5"}}`, wantStatus: http.StatusBadRequest, wantSubstr: "my_dxcc"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServer(t)
			req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.handlePutConfig(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, body = %s; want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			if tc.wantSubstr != "" && !strings.Contains(w.Body.String(), tc.wantSubstr) {
				t.Errorf("body = %s, want substring %q", w.Body.String(), tc.wantSubstr)
			}
		})
	}
}

// TestHandlePutConfig_ZoneTrimming confirms the validators trim
// whitespace before checking — operators copy/paste values and
// accidental spaces shouldn't reject as "non-numeric".
func TestHandlePutConfig_ZoneTrimming(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station": {"my_cq_zone": "  37  ", "my_itu_zone": " 53 ", "my_dxcc": "\t291\t"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want 200", w.Code, w.Body.String())
	}
	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LoggingStation.MyCqZone != "37" {
		t.Errorf("MyCqZone = %q, want %q (whitespace must be trimmed)", resp.LoggingStation.MyCqZone, "37")
	}
	if resp.LoggingStation.MyITUZone != "53" {
		t.Errorf("MyITUZone = %q, want %q", resp.LoggingStation.MyITUZone, "53")
	}
	if resp.LoggingStation.MyDXCC != "291" {
		t.Errorf("MyDXCC = %q, want %q", resp.LoggingStation.MyDXCC, "291")
	}
}

// TestHandleGetConfig_MailerDisabled covers the default test wiring:
// nil mailer → Enabled() reports false, no recipient on the wire.
// The SessionPanel uses this flag to hide its email controls when
// SMTP isn't configured.
func TestHandleGetConfig_MailerDisabled(t *testing.T) {
	srv := testServer(t) // nil mailer

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Mailer.Enabled {
		t.Error("Mailer.Enabled = true with nil mailer; want false")
	}
	if resp.Mailer.DefaultRecipient != "" {
		t.Errorf("Mailer.DefaultRecipient = %q with nil mailer; want empty",
			resp.Mailer.DefaultRecipient)
	}
}

// TestHandleGetConfig_MailerEnabled exercises the populated path: a
// real Service with Host set surfaces enabled=true and the configured
// default recipient. SMTP creds (host, port, username, password, from)
// must NOT appear anywhere in the wire payload — exposing them would
// either leak the password or invite SPA edits to fields it doesn't own.
func TestHandleGetConfig_MailerEnabled(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled:          true,
		Host:             "smtp.example.org",
		Port:             587,
		Username:         "operator",
		Password:         "secret-token-do-not-leak",
		From:             "operator@example.org",
		DefaultRecipient: "qsl@example.org",
		StartTLS:         true,
		TimeoutSec:       30,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "secret-token-do-not-leak") {
		t.Fatalf("password leaked into wire payload: %s", body)
	}
	if strings.Contains(body, "smtp.example.org") {
		t.Fatalf("host leaked into wire payload: %s", body)
	}
	if strings.Contains(body, "operator@example.org") {
		t.Fatalf("from-address leaked into wire payload: %s", body)
	}

	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.Mailer.Enabled {
		t.Error("Mailer.Enabled = false with a configured mailer; want true")
	}
	if resp.Mailer.DefaultRecipient != "qsl@example.org" {
		t.Errorf("Mailer.DefaultRecipient = %q, want qsl@example.org",
			resp.Mailer.DefaultRecipient)
	}
}

// TestHandlePutConfig_MailerBlockIgnored confirms the Mailer field is
// server-managed: a client sending mailer.enabled=true with a recipient
// must NOT mutate the SMTP config (which lives in config.json, not the
// SPA-writable surface) and the response must reflect the actual mailer
// state, not the request body.
func TestHandlePutConfig_MailerBlockIgnored(t *testing.T) {
	srv := testServer(t) // nil mailer → Enabled() = false

	body := `{"logging_station": {"station_callsign": "M0XYZ"}, "mailer": {"enabled": true, "default_recipient": "spoofed@example.com"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Mailer.Enabled {
		t.Error("Mailer.Enabled was set from PUT body; the field must be server-managed")
	}
	if resp.Mailer.DefaultRecipient == "spoofed@example.com" {
		t.Errorf("Mailer.DefaultRecipient was honoured from PUT body (%q); must be ignored",
			resp.Mailer.DefaultRecipient)
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
