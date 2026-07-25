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
	// Pre-setup there is no rig catalogue, so default_rig_id stays 0
	// ("no active rig") rather than dangling at a non-existent rig 1.
	if resp.DefaultRig.ID != 0 {
		t.Errorf("DefaultRig.ID = %d, want 0 (no rig configured pre-setup)", resp.DefaultRig.ID)
	}

	// Bridge timeouts are served RESOLVED even though config.json keeps them
	// sparse (config.md §15 sparse-but-served): a fresh config still reports the
	// effective defaults (liveness 5s) so the config SPA can show them.
	if resp.BridgeTimeouts == nil {
		t.Fatal("bridge_timeouts should be served resolved, got nil")
	}
	if resp.BridgeTimeouts.LivenessMs != 5000 {
		t.Errorf("bridge_timeouts.liveness_ms = %d, want 5000 (resolved default)", resp.BridgeTimeouts.LivenessMs)
	}
	if resp.BridgeTune == nil || resp.BridgeTune.PowerW != 20 {
		t.Errorf("bridge_tune should be served resolved with power_w 20, got %+v", resp.BridgeTune)
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
	cfg.Bridge.Cat = &types.BridgeCatConfig{Driver: "yaesu-ftdx10"}

	info := bridgeInfoFor(cfg)

	want := []string{"set_freq", "set_freq_b", "set_mode", "swap_vfo", "band_up", "band_down", "set_band", "set_power"}
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

	// Second PUT — re-sends the SAME callsign (a post-setup save that doesn't
	// change identity). Must be idempotent: no re-seed, no double-apply. (A
	// callsign CHANGE that would orphan the default logbook is rejected — see
	// TestHandlePutConfig_RejectsOrphaningCallsignChange.)
	second := `{"logging_station": {"station_callsign": "M0XYZ"}}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(second))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second PUT status = %d, body = %s", w2.Code, w2.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp.LoggingStation.StationCallsign != "M0XYZ" {
		t.Errorf("StationCallsign after second PUT = %q, want M0XYZ", resp.LoggingStation.StationCallsign)
	}
	// Logbook callsign was set during seed and is owned by
	// /v1/logbook/{id} now — the second PUT to /v1/config must NOT
	// overwrite it.
	if resp.DefaultLogbook.Callsign != "M0XYZ" {
		t.Errorf("DefaultLogbook.Callsign was rewritten by second PUT; want M0XYZ stays, got %q",
			resp.DefaultLogbook.Callsign)
	}
}

// TestHandlePutConfig_OmittedBlocksPreserved is the regression test for the
// data-loss footgun: LoggingStation and Station were value-typed and copied
// unconditionally, so a PUT that omitted one block zeroed it. Now both are
// pointer-typed and presence-aware — an omitted block must be a no-op, not a wipe.
func TestHandlePutConfig_RejectsOrphaningCallsignChange(t *testing.T) {
	srv := testServer(t)

	// Complete setup with M0XYZ — seeds a default logbook under M0XYZ.
	first := `{"logging_station": {"station_callsign": "M0XYZ"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(first))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	// A callsign CHANGE would orphan the M0XYZ default logbook (live submits gate
	// on STATION_CALLSIGN == logbook callsign) — reject with 409.
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config",
		strings.NewReader(`{"logging_station": {"station_callsign": "G0ABC"}}`))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("callsign-change PUT status = %d, want 409; body = %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "callsign_locked_to_logbook") {
		t.Fatalf("body = %q, want callsign_locked_to_logbook", w2.Body.String())
	}
	// Config unchanged — the rejected write never touched the callsign.
	if got := srv.cfg.Snapshot().LoggingStation.StationCallsign; got != "M0XYZ" {
		t.Errorf("callsign changed despite 409: got %q, want M0XYZ", got)
	}
}

func TestHandlePutConfig_SetupRejectsMismatchedDefaultLogbook(t *testing.T) {
	srv := testServer(t)

	// A logbook already exists at the default id (1) under a DIFFERENT callsign
	// than the operator sets up. Reusing it would seed a default whose callsign
	// can never match live submits — reject 409 (review #1).
	createTestLogbook(t, srv, "Preexisting", "G4ABC") // id 1 == default_logbook_id

	req := httptest.NewRequest(http.MethodPut, "/v1/config",
		strings.NewReader(`{"logging_station": {"station_callsign": "M0XYZ"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("setup PUT status = %d, want 409; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "default_logbook_callsign_mismatch") {
		t.Fatalf("body = %q, want default_logbook_callsign_mismatch", w.Body.String())
	}
	// The message must name the existing logbook's callsign so the operator knows
	// exactly which call to set up under (recovery is manual — Option C).
	if !strings.Contains(w.Body.String(), "G4ABC") {
		t.Errorf("message should name the existing callsign G4ABC; got %s", w.Body.String())
	}
}

func TestHandlePutConfig_SetupSeedsOperatorRoster(t *testing.T) {
	srv := testServer(t)
	// First-run setup with a callsign must seed the operator roster (ADR 0055) —
	// setup runs via PUT, which doesn't re-run applyDefaults, so without the
	// setup-time seed the roster stays empty until a restart.
	req := httptest.NewRequest(http.MethodPut, "/v1/config",
		strings.NewReader(`{"logging_station":{"station_callsign":"M0XYZ"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	snap := srv.cfg.Snapshot()
	if len(snap.Operators) != 1 || snap.Operators[0].Callsign != "M0XYZ" {
		t.Errorf("roster = %+v, want a single seeded entry M0XYZ", snap.Operators)
	}
	if snap.DefaultOperator != "M0XYZ" {
		t.Errorf("default_operator = %q, want M0XYZ", snap.DefaultOperator)
	}
}

func TestHandlePutConfig_OmittedBlocksPreserved(t *testing.T) {
	srv := testServer(t)

	// Seed both identity blocks: logging_station (callsign) + station (amp settings).
	seed := `{"logging_station": {"station_callsign": "M0XYZ", "my_gridsquare": "IO91"},` +
		` "station": {"amp_enabled": true, "amp_multiplier": 10}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(seed))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("seed PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	// A PUT that omits BOTH identity blocks (e.g. an FT8-settings save) must leave
	// them untouched — the core of the bug.
	only := `{"ft8_max_repeats": 4}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(only))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("omit-both PUT status = %d, body = %s", w2.Code, w2.Body.String())
	}
	var resp ConfigResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp.LoggingStation.StationCallsign != "M0XYZ" {
		t.Errorf("logging_station zeroed by an FT8-only PUT: StationCallsign = %q, want M0XYZ",
			resp.LoggingStation.StationCallsign)
	}
	if !resp.Station.AmpEnabled || resp.Station.AmpMultiplier != 10 {
		t.Errorf("station zeroed by an FT8-only PUT: got %+v, want amp_enabled + multiplier 10", resp.Station)
	}

	// A logging_station-only PUT must not zero station (the cross-block
	// direction). Keeps the callsign (M0XYZ, matching the default logbook, so no
	// orphan-guard trip) but changes my_gridsquare — proving the block IS applied
	// while station is preserved.
	stationless := `{"logging_station": {"station_callsign": "M0XYZ", "my_gridsquare": "JO22"}}`
	req3 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(stationless))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	srv.handlePutConfig(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("station-less PUT status = %d, body = %s", w3.Code, w3.Body.String())
	}
	var resp2 ConfigResponse
	_ = json.Unmarshal(w3.Body.Bytes(), &resp2)
	if resp2.LoggingStation.StationCallsign != "M0XYZ" {
		t.Errorf("logging_station callsign changed unexpectedly: got %q, want M0XYZ", resp2.LoggingStation.StationCallsign)
	}
	if resp2.LoggingStation.MyGridsquare != "JO22" {
		t.Errorf("logging_station not applied: MyGridsquare = %q, want JO22", resp2.LoggingStation.MyGridsquare)
	}
	if !resp2.Station.AmpEnabled {
		t.Error("station wiped by a logging_station-only PUT; presence-aware apply failed")
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

// TestHandleGetConfig_DefaultRigNarrowShape pins the L1 boundary (review
// 2026-06-19): /v1/config's default_rig is the narrow DefaultRigInfo
// (id/model/port only). The active rig's wider fields — serial overrides,
// per-rig audio devices, mode mappings, ft8_mode, my_rig — are config-SPA
// concerns served on /v1/rigs and must NOT cross to the logging SPA, so a
// future types.RigConfig field can't silently widen this wire surface.
func TestHandleGetConfig_DefaultRigNarrowShape(t *testing.T) {
	ft8Mode := "DATA-U"
	myRig := "Yaesu FTdx10"
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Rigs = []types.RigConfig{{
			ID:    1,
			Model: "yaesu-ftdx10",
			Port:  "/dev/ttyUSB-secret",
			Audio: types.RigAudioConfig{RX: "PCM2901-capture", TX: "PCM2901-playback"},
			Overrides: types.RigOverrides{
				BaudRate:      38400,
				LineDelimiter: "0xFD",
			},
			ModeMappings: map[string]types.ModeMapping{"DATA-U": {Mode: "FT4"}},
			Ft8Mode:      &ft8Mode,
			MyRig:        &myRig,
		}}
		cfg.DefaultRigID = 1
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// Decode just the default_rig object and assert its exact key set.
	var envelope struct {
		DefaultRig map[string]json.RawMessage `json:"default_rig"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	allowed := map[string]bool{"id": true, "model": true, "port": true}
	for k := range envelope.DefaultRig {
		if !allowed[k] {
			t.Errorf("default_rig exposed disallowed field %q (full payload: %s)", k, w.Body.String())
		}
	}

	// The wider values must not appear anywhere in the payload.
	body := w.Body.String()
	for _, leak := range []string{"PCM2901-capture", "PCM2901-playback", "38400", "0xFD", "my_rig"} {
		if strings.Contains(body, leak) {
			t.Errorf("wider rig field leaked into /v1/config: %q in %s", leak, body)
		}
	}

	// And the narrow fields that ARE allowed round-trip correctly.
	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DefaultRig.ID != 1 || resp.DefaultRig.Model != "yaesu-ftdx10" || resp.DefaultRig.Port != "/dev/ttyUSB-secret" {
		t.Errorf("default_rig narrow fields wrong: %+v", resp.DefaultRig)
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

// TestHandlePutConfig_ModeMappingsOverride_RoundTrip plugs the coverage gap for
// the §10 per-rig mode-mapping path: a PUT mode-mappings override is stored on
// the ACTIVE rig (not the removed global block) and the follow-up GET returns it
// merged on top of the rigdef shipped defaults (operator override wins; untouched
// literals stay at the default).
func TestHandlePutConfig_ModeMappingsOverride_RoundTrip(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Bridge.Enabled = true
		// A catalogue with one active rig; ActiveBridge projects its model onto
		// the driver, so bridgeInfoFor resolves the FTdx10 rigdef. Port is set
		// because PUT now runs the whole-config validator (config.md §12), which
		// requires serial.port when the bridge is enabled — an enabled-but-portless
		// bridge couldn't Load anyway.
		cfg.Rigs = []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10", Port: "/dev/ttyUSB0"}}
		cfg.DefaultRigID = 1
	})

	// Override DATA-U → FT4 (the FTdx10 rigdef ships DATA-U → FT8, so this is a
	// genuine operator deviation that must persist).
	body := `{"bridge": {"driver": "yaesu-ftdx10", "mode_mappings": {"DATA-U": {"mode": "FT4"}}}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	// Stored on the active rig, not a global block.
	rc := srv.cfg.Snapshot().RigByID(1)
	if rc == nil || rc.ModeMappings["DATA-U"].Mode != "FT4" {
		t.Fatalf("active rig ModeMappings = %v, want DATA-U→FT4 stored on the rig", rc.ModeMappings)
	}

	// GET returns the merged view: the override on top of rigdef defaults.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	getW := httptest.NewRecorder()
	srv.handleGetConfig(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getW.Code, getW.Body.String())
	}
	var resp ConfigResponse
	if err := unmarshalJSON(getW.Body.String(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp.Bridge.ModeMappings["DATA-U"].Mode; got != "FT4" {
		t.Fatalf("merged DATA-U mode = %q, want FT4 (operator override wins)", got)
	}
	if got := resp.Bridge.ModeMappings["USB"].Mode; got != "SSB" {
		t.Fatalf("merged USB mode = %q, want SSB (untouched rigdef default preserved)", got)
	}
}

// TestHandlePutConfig_WritesRigCatalogue covers the config-SPA Rigs tab write
// path: a PUT carrying `rigs` + `default_rig_id` persists the catalogue and
// selects the active rig — and the catalogue stays OFF the /v1/config GET
// surface (write-only; the full read view is GET /v1/rigs).
func TestHandlePutConfig_WritesRigCatalogue(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station":{},"station":{},` +
		`"rigs":[{"id":1,"model":"yaesu-ftdx10","port":"/dev/ttyUSB0"},` +
		`{"id":2,"model":"icom-ic7300","port":"/dev/ttyUSB1"}],` +
		`"default_rig_id":2}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	// Persisted to the live config.
	cfg := srv.cfg.Snapshot()
	if len(cfg.Rigs) != 2 {
		t.Fatalf("Rigs len = %d, want 2", len(cfg.Rigs))
	}
	if cfg.DefaultRigID != 2 {
		t.Fatalf("DefaultRigID = %d, want 2", cfg.DefaultRigID)
	}
	if cfg.Rigs[0].Model != "yaesu-ftdx10" || cfg.Rigs[1].Model != "icom-ic7300" {
		t.Fatalf("rig models = %q/%q, want yaesu-ftdx10/icom-ic7300",
			cfg.Rigs[0].Model, cfg.Rigs[1].Model)
	}

	// The catalogue is WRITE-ONLY here: GET /v1/config must not emit `rigs`.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	getW := httptest.NewRecorder()
	srv.handleGetConfig(getW, getReq)
	if strings.Contains(getW.Body.String(), `"rigs"`) {
		t.Fatalf("GET /v1/config leaked the rigs catalogue: %s", getW.Body.String())
	}

	// And the full catalogue is readable via GET /v1/rigs.
	rigsReq := httptest.NewRequest(http.MethodGet, "/v1/rigs", nil)
	rigsW := httptest.NewRecorder()
	srv.handleRigs(rigsW, rigsReq)
	if !strings.Contains(rigsW.Body.String(), `"default_rig_id":2`) {
		t.Fatalf("GET /v1/rigs = %s, want default_rig_id 2", rigsW.Body.String())
	}
}

// TestHandlePutConfig_ForwardersMaskedAndMerged covers the config-SPA Forwarding
// tab contract: a forwarder's secret is stored on PUT, MASKED on GET (only the
// set credential keys, never the value), and a later PUT that omits the secret
// MERGES — keeping the stored value — so an enable/disable toggle can't wipe it.
// And a forwarder-less PUT leaves the list untouched (presence-aware).
func TestHandlePutConfig_ForwardersMaskedAndMerged(t *testing.T) {
	srv := testServer(t)

	// 1. Create a forwarder carrying a secret credential.
	body1 := `{"logging_station":{},"station":{},"forwarders":[` +
		`{"name":"qrz-main","type":"qrz","enabled":true,"action_filter":["insert"],` +
		`"credentials":{"api_key":"SECRET123"}}]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body1))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 1 status = %d, body = %s", w.Code, w.Body.String())
	}
	cfg := srv.cfg.Snapshot()
	if len(cfg.Forwarders) != 1 {
		t.Fatalf("Forwarders len = %d, want 1", len(cfg.Forwarders))
	}
	if !strings.Contains(string(cfg.Forwarders[0].Credentials), "SECRET123") {
		t.Fatalf("stored credentials = %s, want the secret persisted", cfg.Forwarders[0].Credentials)
	}

	// 2. GET masks the secret — only the set key, never the value.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	getW := httptest.NewRecorder()
	srv.handleGetConfig(getW, getReq)
	getBody := getW.Body.String()
	if strings.Contains(getBody, "SECRET123") {
		t.Fatalf("GET /v1/config leaked the credential value: %s", getBody)
	}
	if !strings.Contains(getBody, `"credentials_set":["api_key"]`) {
		t.Fatalf("GET did not report the set credential key: %s", getBody)
	}

	// 3. PUT changing only `enabled`, omitting credentials → secret preserved.
	body2 := `{"logging_station":{},"station":{},"forwarders":[` +
		`{"name":"qrz-main","type":"qrz","enabled":false,"action_filter":["insert"]}]}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("PUT 2 status = %d, body = %s", w2.Code, w2.Body.String())
	}
	cfg = srv.cfg.Snapshot()
	if cfg.Forwarders[0].Enabled {
		t.Fatal("enabled not updated to false")
	}
	if !strings.Contains(string(cfg.Forwarders[0].Credentials), "SECRET123") {
		t.Fatalf("merge lost the secret on a credential-less PUT: %s", cfg.Forwarders[0].Credentials)
	}

	// 4. Presence-aware: a forwarder-less PUT leaves the list intact.
	body3 := `{"logging_station":{"station_callsign":"M0ABC"},"station":{}}`
	req3 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	srv.handlePutConfig(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("PUT 3 status = %d, body = %s", w3.Code, w3.Body.String())
	}
	if cfg := srv.cfg.Snapshot(); len(cfg.Forwarders) != 1 {
		t.Fatalf("forwarder-less PUT wiped the list: len = %d, want 1", len(cfg.Forwarders))
	}
}

// TestHandlePutConfig_BlankCredentialKeepsStoredSecret closes the gap the
// omitted-key case above does NOT cover: a credential key present with an EMPTY
// value. The config SPA masks credentials on GET and tells the operator "leave
// blank to keep", but it honoured that CLIENT-side by stripping blanks before the
// PUT — so the daemon still cleared on an empty string, and any other client
// (curl, a script, the app SPA's forwarders section) silently destroyed a stored
// credential. A guarantee about secrets belongs in the daemon, not in one browser.
//
// Blank now means keep, matching mergeSmtp / mergeLookupProvider — where a plain
// string field makes absent and "" indistinguishable, so keep is the only
// behaviour they can express. One rule across all three.
func TestHandlePutConfig_BlankCredentialKeepsStoredSecret(t *testing.T) {
	srv := testServer(t)

	body1 := `{"logging_station":{},"station":{},"forwarders":[` +
		`{"name":"clublog-main","type":"clublog","enabled":true,"action_filter":["insert"],` +
		`"credentials":{"email":"op@example.com","password":"SECRET123"}}]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body1))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 1 status = %d, body = %s", w.Code, w.Body.String())
	}

	// A PUT that sends the key with an empty value — an untouched masked field on a
	// client that doesn't strip blanks, or a hand-rolled request.
	body2 := `{"logging_station":{},"station":{},"forwarders":[` +
		`{"name":"clublog-main","type":"clublog","enabled":true,"action_filter":["insert"],` +
		`"credentials":{"email":"op@example.com","password":""}}]}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("PUT 2 status = %d, body = %s", w2.Code, w2.Body.String())
	}

	cfg := srv.cfg.Snapshot()
	if len(cfg.Forwarders) != 1 {
		t.Fatalf("Forwarders len = %d, want 1", len(cfg.Forwarders))
	}
	if !strings.Contains(string(cfg.Forwarders[0].Credentials), "SECRET123") {
		t.Fatalf("a blank credential wiped the stored secret: %s", cfg.Forwarders[0].Credentials)
	}
	// Whitespace-only is the same "operator typed nothing" case.
	body3 := `{"logging_station":{},"station":{},"forwarders":[` +
		`{"name":"clublog-main","type":"clublog","enabled":true,"action_filter":["insert"],` +
		`"credentials":{"password":"   "}}]}`
	req3 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	srv.handlePutConfig(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("PUT 3 status = %d, body = %s", w3.Code, w3.Body.String())
	}
	cfg = srv.cfg.Snapshot()
	if !strings.Contains(string(cfg.Forwarders[0].Credentials), "SECRET123") {
		t.Fatalf("a whitespace-only credential wiped the stored secret: %s", cfg.Forwarders[0].Credentials)
	}
	// A genuinely supplied value still replaces the stored one.
	body4 := `{"logging_station":{},"station":{},"forwarders":[` +
		`{"name":"clublog-main","type":"clublog","enabled":true,"action_filter":["insert"],` +
		`"credentials":{"password":"NEWSECRET"}}]}`
	req4 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body4))
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	srv.handlePutConfig(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("PUT 4 status = %d, body = %s", w4.Code, w4.Body.String())
	}
	cfg = srv.cfg.Snapshot()
	if !strings.Contains(string(cfg.Forwarders[0].Credentials), "NEWSECRET") {
		t.Fatalf("a supplied credential did not replace the stored one: %s", cfg.Forwarders[0].Credentials)
	}
	if strings.Contains(string(cfg.Forwarders[0].Credentials), "SECRET123") {
		t.Fatalf("the old secret survived a real update: %s", cfg.Forwarders[0].Credentials)
	}
}

// TestHandlePutConfig_EnableToggles covers the master subsystem switches the
// config SPA's Rigs (bridge) + FT8 tabs write: bridge_enabled / ft8_enabled are
// read+write, presence-aware. Enabling the bridge needs the active rig to carry
// port+driver (validateBridge), so they're sent alongside the rig catalogue.
func TestHandlePutConfig_EnableToggles(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station":{},"station":{},` +
		`"rigs":[{"id":1,"model":"yaesu-ftdx10","port":"/dev/ttyUSB0"}],"default_rig_id":1,` +
		`"bridge_enabled":true,"ft8_enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	cfg := srv.cfg.Snapshot()
	if !cfg.Bridge.Enabled || !cfg.Ft8.Enabled {
		t.Fatalf("enables not applied: bridge=%v ft8=%v", cfg.Bridge.Enabled, cfg.Ft8.Enabled)
	}

	// GET reflects them so the toggles show state.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	getW := httptest.NewRecorder()
	srv.handleGetConfig(getW, getReq)
	getBody := getW.Body.String()
	if !strings.Contains(getBody, `"bridge_enabled":true`) || !strings.Contains(getBody, `"ft8_enabled":true`) {
		t.Fatalf("GET did not report the enables: %s", getBody)
	}

	// Presence-aware: a PUT carrying only ft8_enabled=false leaves bridge alone.
	body2 := `{"logging_station":{},"station":{},"ft8_enabled":false}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("PUT 2 status = %d, body = %s", w2.Code, w2.Body.String())
	}
	cfg = srv.cfg.Snapshot()
	if cfg.Ft8.Enabled {
		t.Fatal("ft8_enabled not set to false")
	}
	if !cfg.Bridge.Enabled {
		t.Fatal("bridge.enabled wrongly changed by an ft8-only PUT (presence-aware broken)")
	}
}

// TestHandlePutConfig_LookupMaskedAndMerged covers the Enrichment tab contract:
// a QRZ provider's password is stored on PUT, MASKED on GET (password_set, never
// the value), the QRZ/hamnut URLs are defaulted daemon-side (operator types no
// URL), and a later credential-less PUT MERGES — keeping the stored password.
func TestHandlePutConfig_LookupMaskedAndMerged(t *testing.T) {
	srv := testServer(t)

	// 1. Configure hamnut + a QRZ chain entry with a password, no URLs sent.
	body1 := `{"logging_station":{},"station":{},"lookup":{` +
		`"hamnut":{"name":"hamnutlookupservice","enabled":true},` +
		`"chain":[{"name":"qrzlookupservice","enabled":true,"username":"M0ABC","password":"QRZSECRET"}],` +
		`"country_ttl_days":30,"station_ttl_days":7,"refresh_max_in_flight":4}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body1))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 1 status = %d, body = %s", w.Code, w.Body.String())
	}
	cfg := srv.cfg.Snapshot()
	if len(cfg.Lookup.Chain) != 1 || cfg.Lookup.Chain[0].Password != "QRZSECRET" {
		t.Fatalf("QRZ password not stored: %+v", cfg.Lookup.Chain)
	}
	// URLs defaulted daemon-side (operator sent none).
	if cfg.Lookup.Chain[0].URL != types.QRZLookupDefaultURL {
		t.Fatalf("QRZ URL = %q, want defaulted %q", cfg.Lookup.Chain[0].URL, types.QRZLookupDefaultURL)
	}
	if cfg.Lookup.Hamnut.URL != types.HamNutLookupDefaultURL {
		t.Fatalf("hamnut URL = %q, want defaulted", cfg.Lookup.Hamnut.URL)
	}

	// 2. GET masks the password.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	getW := httptest.NewRecorder()
	srv.handleGetConfig(getW, getReq)
	getBody := getW.Body.String()
	if strings.Contains(getBody, "QRZSECRET") {
		t.Fatalf("GET leaked the QRZ password: %s", getBody)
	}
	if !strings.Contains(getBody, `"password_set":true`) {
		t.Fatalf("GET did not report password_set: %s", getBody)
	}

	// 3. Credential-less PUT (toggle QRZ off) → password preserved by merge.
	body2 := `{"logging_station":{},"station":{},"lookup":{` +
		`"hamnut":{"name":"hamnutlookupservice","enabled":true},` +
		`"chain":[{"name":"qrzlookupservice","enabled":false,"username":"M0ABC"}],` +
		`"country_ttl_days":30,"station_ttl_days":7,"refresh_max_in_flight":4}}`
	req2 := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handlePutConfig(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("PUT 2 status = %d, body = %s", w2.Code, w2.Body.String())
	}
	cfg = srv.cfg.Snapshot()
	if cfg.Lookup.Chain[0].Enabled {
		t.Fatal("QRZ enabled not updated to false")
	}
	if cfg.Lookup.Chain[0].Password != "QRZSECRET" {
		t.Fatalf("merge lost the QRZ password: %q", cfg.Lookup.Chain[0].Password)
	}
}

// TestHandlePutConfig_RejectsBadRigCatalogue confirms the write path runs the
// same validateRigs gate as Load: a dangling default_rig_id is a 400, not a
// persisted bad catalogue.
func TestHandlePutConfig_RejectsBadRigCatalogue(t *testing.T) {
	srv := testServer(t)

	body := `{"logging_station":{},"station":{},` +
		`"rigs":[{"id":1,"model":"yaesu-ftdx10","port":"/dev/ttyUSB0"}],` +
		`"default_rig_id":99}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	// Nothing persisted.
	if cfg := srv.cfg.Snapshot(); len(cfg.Rigs) != 0 {
		t.Fatalf("Rigs len = %d after rejected PUT, want 0 (no partial write)", len(cfg.Rigs))
	}
}
