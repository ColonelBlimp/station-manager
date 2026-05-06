package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// utilsIsValidUUIDv7 is an alias to keep the assertion line in
// TestSubmitQso_UUIDInResponse short.
var utilsIsValidUUIDv7 = utils.IsValidUUIDv7

// unmarshalJSON decodes a response body string into v.
func unmarshalJSON(body string, v any) error {
	return json.Unmarshal([]byte(body), v)
}

// testServer creates a Server wired to an in-memory sqlite database for
// handler testing.
func testServer(t *testing.T) *Server {
	return testServerWithCfg(t, nil)
}

// testServerWithCfg is testServer with an optional config-mutation hook.
// The hook runs against a default Config after the temp data dir is
// set, before any service is constructed — giving tests a chance to
// populate cfg.Forwarders, tweak page limits, etc.
func testServerWithCfg(t *testing.T, mutate func(cfg *config.Config)) *Server {
	t.Helper()

	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"
	if mutate != nil {
		mutate(&cfg)
	}

	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}

	logSvc := &logging.Service{}
	logSvc.ConfigService = cfgSvc
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}

	dbSvc := &sqlite.Service{}
	dbSvc.ConfigService = cfgSvc
	dbSvc.LoggerService = logSvc
	if err := dbSvc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	dbSvc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      ":memory:",
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := dbSvc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := dbSvc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})

	hub := events.NewHub()
	t.Cleanup(func() { hub.Close() })

	qsoSvc := &qsoservice.Service{DB: dbSvc, Logger: logSvc, Config: cfgSvc, Hub: hub}

	// Tests that exercise PUT /v1/config need a Path on cfgSvc so
	// the atomic write target exists; point at a temp file under the
	// already-allocated test working dir. Reads (Snapshot) work
	// without a path, so unit tests that don't touch PUT are
	// unaffected.
	cfgSvc.SetPath(cfgSvc.WorkingDir() + "/config.json")

	return New(cfg, "test", cfgSvc, qsoSvc, dbSvc, logSvc, hub)
}

// createTestLogbook creates a logbook via the handler and returns its ID.
func createTestLogbook(t *testing.T, srv *Server, name, callsign string) int64 {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"callsign":%q}`, name, callsign)
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestLogbook: status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil || resp.ID < 1 {
		t.Fatalf("createTestLogbook: failed to decode id from %s (err=%v)", w.Body.String(), err)
	}
	return resp.ID
}

// submitQso is a test helper that submits a QSO via the handler.
func submitQso(t *testing.T, srv *Server, logbookID int64, adifBody string, force bool) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/v1/qso?logbook=%d", logbookID)
	if force {
		url += "&force=true"
	}
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(adifBody))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	return w
}

const testQsoADIF = `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`

// =============================================================================
// Healthz
// =============================================================================

func TestHealthz_OK(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	w := httptest.NewRecorder()
	srv.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %q, want status ok", w.Body.String())
	}
}

// =============================================================================
// Logbook CRUD
// =============================================================================

func TestCreateLogbook(t *testing.T) {
	srv := testServer(t)

	body := `{"name":"My Logbook","callsign":"G4ABC","description":"Primary log"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":`) {
		t.Fatalf("body = %q, want id field", w.Body.String())
	}
}

func TestCreateLogbook_MissingName(t *testing.T) {
	srv := testServer(t)

	body := `{"callsign":"G4ABC"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateLogbook_MissingCallsign(t *testing.T) {
	srv := testServer(t)

	body := `{"name":"My Log"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateLogbook_InvalidCallsign(t *testing.T) {
	srv := testServer(t)

	// No digit
	body := `{"name":"Bad Log","callsign":"ABC"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Fatalf("body = %q, want invalid_field_value", w.Body.String())
	}
}

func TestCreateLogbook_CallsignTooShort(t *testing.T) {
	srv := testServer(t)

	body := `{"name":"Bad Log","callsign":"K1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestCreateLogbook_DuplicateName(t *testing.T) {
	srv := testServer(t)

	createTestLogbook(t, srv, "DX Log", "G4ABC")

	body := `{"name":"DX Log","callsign":"M0CMC"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

// TestCreateLogbook_BodyTooLarge covers the M1 fix: JSON handlers now
// honour Server.MaxBodyBytes the same way the QSO handlers do.
func TestCreateLogbook_BodyTooLarge(t *testing.T) {
	srv := testServer(t)
	srv.maxBodyBytes = 50

	body := `{"name":"` + strings.Repeat("X", 200) + `","callsign":"G4ABC"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "body_too_large") {
		t.Fatalf("body = %q, want body_too_large", w.Body.String())
	}
}

func TestUpdateLogbook_BodyTooLarge(t *testing.T) {
	srv := testServer(t)
	id := createTestLogbook(t, srv, "Old Name", "G4ABC")
	srv.maxBodyBytes = 50

	body := `{"name":"` + strings.Repeat("X", 200) + `"}`
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/logbook/%d", id), strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateLogbook(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
}

func TestCreateLogbook_InvalidJSON(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "invalid_json") {
		t.Fatalf("body = %q, want invalid_json", w.Body.String())
	}
}

func TestListLogbooks(t *testing.T) {
	srv := testServer(t)

	createTestLogbook(t, srv, "Log A", "G4ABC")
	createTestLogbook(t, srv, "Log B", "M0CMC")

	req := httptest.NewRequest(http.MethodGet, "/v1/logbook", nil)
	w := httptest.NewRecorder()
	srv.handleListLogbooks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Log A") || !strings.Contains(w.Body.String(), "Log B") {
		t.Fatalf("body = %q, want both logbooks", w.Body.String())
	}
}

func TestGetLogbook(t *testing.T) {
	srv := testServer(t)

	id := createTestLogbook(t, srv, "My Log", "G4ABC")

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/logbook/%d", id), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	w := httptest.NewRecorder()
	srv.handleGetLogbook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "My Log") {
		t.Fatalf("body = %q, want logbook name", w.Body.String())
	}
}

func TestGetLogbook_NotFound(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/logbook/999", nil)
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	srv.handleGetLogbook(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUpdateLogbook(t *testing.T) {
	srv := testServer(t)

	id := createTestLogbook(t, srv, "Old Name", "G4ABC")

	body := `{"name":"New Name"}`
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/logbook/%d", id), strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateLogbook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got types.Logbook
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode logbook: %v", err)
	}
	if got.Name != "New Name" {
		t.Fatalf("Name = %q, want %q", got.Name, "New Name")
	}
	if got.Callsign != "G4ABC" {
		t.Fatalf("Callsign = %q, want unchanged G4ABC", got.Callsign)
	}
}

func TestUpdateLogbook_CallsignIgnored(t *testing.T) {
	srv := testServer(t)

	id := createTestLogbook(t, srv, "My Log", "G4ABC")

	// Attempt to change callsign — should be silently ignored.
	body := `{"name":"Updated","callsign":"M0CMC"}`
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/logbook/%d", id), strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateLogbook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var got types.Logbook
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode logbook: %v", err)
	}
	if got.Name != "Updated" {
		t.Fatalf("Name = %q, want %q", got.Name, "Updated")
	}
	if got.Callsign != "G4ABC" {
		t.Fatalf("Callsign = %q, want unchanged G4ABC (callsign is immutable on PATCH)", got.Callsign)
	}
}

func TestUpdateLogbook_NotFound(t *testing.T) {
	srv := testServer(t)

	body := `{"name":"New Name"}`
	req := httptest.NewRequest(http.MethodPatch, "/v1/logbook/999", strings.NewReader(body))
	req.SetPathValue("id", "999")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateLogbook(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteLogbook(t *testing.T) {
	srv := testServer(t)

	id := createTestLogbook(t, srv, "To Delete", "G4ABC")

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/logbook/%d", id), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", id))
	w := httptest.NewRecorder()
	srv.handleDeleteLogbook(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNoContent, w.Body.String())
	}
}

func TestDeleteLogbook_NotFound(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/logbook/999", nil)
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	srv.handleDeleteLogbook(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteLogbook_WithQSOs_Rejected(t *testing.T) {
	srv := testServer(t)

	lbID := createTestLogbook(t, srv, "Has QSOs", "G4ABC")
	// Submit a QSO so the logbook is non-empty.
	w := submitQso(t, srv, lbID, testQsoADIF, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup submit: status = %d; body = %s", w.Code, w.Body.String())
	}

	// Attempt to delete — should fail with 409.
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/logbook/%d", lbID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", lbID))
	w = httptest.NewRecorder()
	srv.handleDeleteLogbook(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "has_qsos") {
		t.Fatalf("body = %q, want has_qsos", w.Body.String())
	}
}

func TestSubmitQso_BodyTooLarge(t *testing.T) {
	srv := testServer(t)
	// Create the logbook at default cap, THEN drop the cap — otherwise the
	// M1 fix would also block the test's logbook POST.
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	srv.maxBodyBytes = 10

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/qso?logbook=%d", lbID),
		strings.NewReader(strings.Repeat("X", 100)))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusRequestEntityTooLarge, w.Body.String())
	}
}

func TestSubmitQso_InvalidCallInADIF(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	// CALL has no digit
	body := `<CALL:3>ABC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Fatalf("body = %q, want invalid_field_value", w.Body.String())
	}
}

func TestSubmitQso_InvalidStationCallsign(t *testing.T) {
	srv := testServer(t)
	// Create logbook with valid callsign, but submit ADIF with invalid STATION_CALLSIGN.
	// The handler validates STATION_CALLSIGN before the logbook lookup.
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<STATION_CALLSIGN:2>AB<COUNTRY:7>England<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// =============================================================================
// Submit QSO — happy paths (now require logbook)
// =============================================================================

func TestSubmitQso_Stored(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w := submitQso(t, srv, lbID, testQsoADIF, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"stored"`) {
		t.Fatalf("body = %q, want stored", w.Body.String())
	}
}

func TestSubmitQso_Duplicate(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w1 := submitQso(t, srv, lbID, testQsoADIF, false)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first submit: status = %d; body = %s", w1.Code, w1.Body.String())
	}

	w2 := submitQso(t, srv, lbID, testQsoADIF, false)
	if w2.Code != http.StatusOK {
		t.Fatalf("second submit: status = %d; body = %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"status":"duplicate"`) {
		t.Fatalf("body = %q, want duplicate", w2.Body.String())
	}
}

// TestSubmitQso_UUIDInResponse pins the ADR 0016 prep-item: every
// stored or duplicate response carries a canonical UUIDv7. The store
// path returns the freshly generated UUID; the duplicate path returns
// the UUID of the row that already exists, so re-submits resolve to
// the same external identifier.
func TestSubmitQso_UUIDInResponse(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	first := submitQso(t, srv, lbID, testQsoADIF, false)
	if first.Code != http.StatusCreated {
		t.Fatalf("first submit: status = %d; body = %s", first.Code, first.Body.String())
	}
	var stored qsoservice.SubmitResult
	if err := unmarshalJSON(first.Body.String(), &stored); err != nil {
		t.Fatalf("decode first body: %v", err)
	}
	if stored.UUID == "" {
		t.Fatalf("stored response missing uuid; body = %s", first.Body.String())
	}
	if !utilsIsValidUUIDv7(stored.UUID) {
		t.Fatalf("stored uuid not valid v7: %q", stored.UUID)
	}

	second := submitQso(t, srv, lbID, testQsoADIF, false)
	if second.Code != http.StatusOK {
		t.Fatalf("second submit: status = %d; body = %s", second.Code, second.Body.String())
	}
	var dup qsoservice.SubmitResult
	if err := unmarshalJSON(second.Body.String(), &dup); err != nil {
		t.Fatalf("decode dup body: %v", err)
	}
	if dup.UUID != stored.UUID {
		t.Fatalf("duplicate uuid mismatch: got %q, want %q", dup.UUID, stored.UUID)
	}
}

// TestSubmitQso_ConcurrentDuplicate is the deterministic regression test
// for the H1 race: two concurrent submits with identical dedupe-key
// inputs both pass the pre-transaction FetchQsoByDedupeKey and race
// into InsertQsoTx. The loser used to surface as a 500; with the fix
// it must resolve to the duplicate-outcome shape, matching whichever
// goroutine wrote first.
func TestSubmitQso_ConcurrentDuplicate(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	const workers = 2
	var wg sync.WaitGroup
	results := make([]*httptest.ResponseRecorder, workers)
	start := make(chan struct{})

	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx] = submitQso(t, srv, lbID, testQsoADIF, false)
		}(i)
	}

	close(start)
	wg.Wait()

	var stored, duplicate int
	var storedID, duplicateID int64
	for i, w := range results {
		body := w.Body.String()
		var r qsoservice.SubmitResult
		if err := unmarshalJSON(body, &r); err != nil {
			t.Fatalf("worker %d: decode response: %v; body = %s", i, err, body)
		}
		switch {
		case w.Code == http.StatusCreated && r.Status == "stored":
			stored++
			storedID = r.ID
		case w.Code == http.StatusOK && r.Status == "duplicate":
			duplicate++
			duplicateID = r.ID
		default:
			t.Fatalf("worker %d: unexpected status %d, body = %s", i, w.Code, body)
		}
	}

	if stored != 1 {
		t.Fatalf("stored = %d, want exactly 1", stored)
	}
	if duplicate != 1 {
		t.Fatalf("duplicate = %d, want exactly 1", duplicate)
	}
	if storedID == 0 || storedID != duplicateID {
		t.Fatalf("stored id %d != duplicate id %d — both should name the same row", storedID, duplicateID)
	}
}

func TestSubmitQso_ForceBypassesDedupe(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w1 := submitQso(t, srv, lbID, testQsoADIF, false)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first submit: status = %d; body = %s", w1.Code, w1.Body.String())
	}

	w2 := submitQso(t, srv, lbID, testQsoADIF, true)
	if w2.Code != http.StatusCreated {
		t.Fatalf("force submit: status = %d, want %d; body = %s", w2.Code, http.StatusCreated, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"status":"stored"`) {
		t.Fatalf("body = %q, want stored", w2.Body.String())
	}
}

// TestSubmitQso_ForceParamAcceptsBoolVariants covers the H4 fix: `?force`
// parses through strconv.ParseBool, so "true"/"True"/"TRUE"/"1" all
// trigger the dedupe-bypass and unrecognised values are rejected with a
// 400 instead of silently falling through to dedupe-checked behaviour.
func TestSubmitQso_ForceParamAcceptsBoolVariants(t *testing.T) {
	for _, raw := range []string{"true", "True", "TRUE", "1"} {
		t.Run(raw, func(t *testing.T) {
			srv := testServer(t)
			lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

			// First submit succeeds normally.
			if w := submitQso(t, srv, lbID, testQsoADIF, false); w.Code != http.StatusCreated {
				t.Fatalf("first submit: status = %d; body = %s", w.Code, w.Body.String())
			}

			url := fmt.Sprintf("/v1/qso?logbook=%d&force=%s", lbID, raw)
			req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(testQsoADIF))
			req.Header.Set("Content-Type", "application/x-adif")
			w := httptest.NewRecorder()
			srv.handleSubmitQso(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("force=%s: status = %d, want 201 (force should bypass dedupe); body = %s",
					raw, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `"status":"stored"`) {
				t.Fatalf("force=%s: body = %q, want stored", raw, w.Body.String())
			}
		})
	}
}

func TestSubmitQso_ForceParamRejectsGarbage(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	for _, raw := range []string{"yes", "y", "force", "tru", "2"} {
		t.Run(raw, func(t *testing.T) {
			url := fmt.Sprintf("/v1/qso?logbook=%d&force=%s", lbID, raw)
			req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(testQsoADIF))
			req.Header.Set("Content-Type", "application/x-adif")
			w := httptest.NewRecorder()
			srv.handleSubmitQso(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("force=%s: status = %d, want 400 (unrecognised value should fail loud); body = %s",
					raw, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "invalid_query_param") {
				t.Fatalf("force=%s: body = %q, want invalid_query_param", raw, w.Body.String())
			}
		})
	}
}

// =============================================================================
// Submit QSO — logbook validation
// =============================================================================

func TestSubmitQso_MissingLogbookParam(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(testQsoADIF))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing_required_param") {
		t.Fatalf("body = %q, want missing_required_param", w.Body.String())
	}
}

func TestSubmitQso_LogbookNotFound(t *testing.T) {
	srv := testServer(t)

	w := submitQso(t, srv, 999, testQsoADIF, false)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestSubmitQso_CallsignMismatch(t *testing.T) {
	srv := testServer(t)
	// Logbook has callsign M0CMC, but ADIF has STATION_CALLSIGN=G4ABC
	lbID := createTestLogbook(t, srv, "Wrong Log", "M0CMC")

	w := submitQso(t, srv, lbID, testQsoADIF, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "callsign_mismatch") {
		t.Fatalf("body = %q, want callsign_mismatch", w.Body.String())
	}
}

// =============================================================================
// Submit QSO — error paths
// =============================================================================

func TestSubmitQso_EmptyBody(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/qso?logbook=%d", lbID), nil)
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSubmitQso_WrongContentType(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/qso?logbook=1", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestSubmitQso_MissingCall(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	body := `<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing_required_field") {
		t.Fatalf("body = %q, want missing_required_field", w.Body.String())
	}
}

func TestSubmitQso_InvalidBand(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	body := `<CALL:5>M0CMC<BAND:5>99.9m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Fatalf("body = %q, want invalid_field_value", w.Body.String())
	}
}

func TestSubmitQso_MalformedADIF(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/qso?logbook=%d", lbID), strings.NewReader("this is not adif at all"))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSubmitQso_NoRecords(t *testing.T) {
	srv := testServer(t)

	body := `<ADIF_VER:5>3.1.5<EOH>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso?logbook=1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// =============================================================================
// Time coherence
// =============================================================================

func TestSubmitQso_TimeOnAfterTimeOff_SameDate_Rejected(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>2345<TIME_OFF:4>0015<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	var envelope ErrorResponse
	if err := unmarshalJSON(w.Body.String(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Code != "invalid_time_range" {
		t.Fatalf("code = %q, want invalid_time_range", envelope.Code)
	}
}

func TestSubmitQso_TimeOnAfterTimeOff_NextDay_Accepted(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<QSO_DATE_OFF:8>20250509<TIME_ON:4>2345<TIME_OFF:4>0015<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestSubmitQso_TimeOnAfterTimeOff_WrongDateOff_Rejected(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<QSO_DATE_OFF:8>20250510<TIME_ON:4>2345<TIME_OFF:4>0015<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	var envelope ErrorResponse
	if err := unmarshalJSON(w.Body.String(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Code != "invalid_time_range" {
		t.Fatalf("code = %q, want invalid_time_range", envelope.Code)
	}
}

func TestSubmitQso_NormalTimeOrder_Accepted(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	body := `<CALL:6>DL1ABC<BAND:3>20m<MODE:2>CW<FREQ:6>14.074<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:3>599<RST_RCVD:3>599<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>Germany<EOR>`
	w := submitQso(t, srv, lbID, body, false)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

// =============================================================================
// Get QSO
// =============================================================================

func TestGetQso(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	submitW := submitQso(t, srv, lbID, testQsoADIF, false)
	if submitW.Code != http.StatusCreated {
		t.Fatalf("submit: status = %d; body = %s", submitW.Code, submitW.Body.String())
	}
	var r qsoservice.SubmitResult
	if err := unmarshalJSON(submitW.Body.String(), &r); err != nil || r.ID < 1 {
		t.Fatalf("decode QSO id from %s (err=%v)", submitW.Body.String(), err)
	}
	qsoID := r.ID

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d", qsoID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", qsoID))
	w := httptest.NewRecorder()
	srv.handleGetQso(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"call":"M0CMC"`) {
		t.Fatalf("body = %q, want call M0CMC", body)
	}
	if !strings.Contains(body, fmt.Sprintf(`"logbook_id":%d`, lbID)) {
		t.Fatalf("body = %q, want logbook_id %d", body, lbID)
	}
	if !strings.Contains(body, fmt.Sprintf(`"uuid":%q`, r.UUID)) {
		t.Fatalf("body = %q, want uuid %q", body, r.UUID)
	}
}

func TestGetQso_NotFound(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/qso/999", nil)
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	srv.handleGetQso(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestGetQso_InvalidID(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/qso/abc", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	srv.handleGetQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// =============================================================================
// Update QSO (PATCH)
// =============================================================================

// submitAndGetID is a test helper that submits testQsoADIF and returns the
// resulting QSO ID.
func submitAndGetID(t *testing.T, srv *Server, lbID int64, body string) int64 {
	t.Helper()
	w := submitQso(t, srv, lbID, body, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status = %d; body = %s", w.Code, w.Body.String())
	}
	var r qsoservice.SubmitResult
	if err := unmarshalJSON(w.Body.String(), &r); err != nil || r.ID < 1 {
		t.Fatalf("decode QSO id from %s (err=%v)", w.Body.String(), err)
	}
	return r.ID
}

// patchQso is a test helper that sends a PATCH to /v1/qso/{id}.
func patchQso(t *testing.T, srv *Server, qsoID int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/qso/%d", qsoID), strings.NewReader(body))
	req.SetPathValue("id", fmt.Sprintf("%d", qsoID))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateQso(w, req)
	return w
}

func TestUpdateQso_PatchComment(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := patchQso(t, srv, id, `{"comment":"nice chat"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"comment":"nice chat"`) {
		t.Fatalf("body = %q, want comment updated", w.Body.String())
	}
}

func TestUpdateQso_RecomputesDedupeKey(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	// Fetch the pre-edit dedupe key.
	getW := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d", id), nil)
	getReq.SetPathValue("id", fmt.Sprintf("%d", id))
	srv.handleGetQso(getW, getReq)
	var before struct {
		DedupeKey string `json:"dedupe_key"`
	}
	if err := unmarshalJSON(getW.Body.String(), &before); err != nil {
		t.Fatalf("decode pre-edit: %v", err)
	}

	// Change CALL — a dedupe input — and confirm the key changed.
	w := patchQso(t, srv, id, `{"call":"M0XYZ"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var after struct {
		Call      string `json:"call"`
		DedupeKey string `json:"dedupe_key"`
	}
	if err := unmarshalJSON(w.Body.String(), &after); err != nil {
		t.Fatalf("decode post-edit: %v", err)
	}
	if after.Call != "M0XYZ" {
		t.Fatalf("call = %q, want M0XYZ", after.Call)
	}
	if after.DedupeKey == before.DedupeKey {
		t.Fatalf("dedupe key unchanged after CALL edit: %s", after.DedupeKey)
	}
}

func TestUpdateQso_FreqChangeRecomputesDedupe(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	// Pre-edit dedupe key.
	getW := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d", id), nil)
	getReq.SetPathValue("id", fmt.Sprintf("%d", id))
	srv.handleGetQso(getW, getReq)
	var before struct {
		DedupeKey string `json:"dedupe_key"`
	}
	if err := unmarshalJSON(getW.Body.String(), &before); err != nil {
		t.Fatalf("decode pre-edit: %v", err)
	}

	// Change FREQ — a dedupe input — and confirm the key changed.
	w := patchQso(t, srv, id, `{"freq":"7.120"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var after struct {
		Freq      string `json:"freq"`
		DedupeKey string `json:"dedupe_key"`
	}
	if err := unmarshalJSON(w.Body.String(), &after); err != nil {
		t.Fatalf("decode post-edit: %v", err)
	}
	if after.Freq != "7.120" {
		t.Fatalf("freq = %q, want canonical MHz 7.120", after.Freq)
	}
	if after.DedupeKey == before.DedupeKey {
		t.Fatalf("dedupe key unchanged after FREQ edit: %s", after.DedupeKey)
	}
}

func TestSubmitQso_SameCallSameTimeDifferentFreq(t *testing.T) {
	// Regression test for the dedupe-key expansion: two QSOs with
	// identical CALL/BAND/MODE/DATE/TIME but different FREQ are legitimate
	// distinct QSOs (net ops, split, frequency hopping) and must both
	// store. Before FREQ was added to the dedupe key, the second would
	// have been flagged as a duplicate.
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	first := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w1 := submitQso(t, srv, lbID, first, false)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first: status = %d; body = %s", w1.Code, w1.Body.String())
	}

	second := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.120<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	w2 := submitQso(t, srv, lbID, second, false)
	if w2.Code != http.StatusCreated {
		t.Fatalf("second: status = %d; body = %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"status":"stored"`) {
		t.Fatalf("second body = %q, want stored", w2.Body.String())
	}
}

func TestUpdateQso_NonDedupeFieldLeavesKeyAlone(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	getW := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d", id), nil)
	getReq.SetPathValue("id", fmt.Sprintf("%d", id))
	srv.handleGetQso(getW, getReq)
	var before struct {
		DedupeKey string `json:"dedupe_key"`
	}
	if err := unmarshalJSON(getW.Body.String(), &before); err != nil {
		t.Fatalf("decode pre-edit: %v", err)
	}

	w := patchQso(t, srv, id, `{"rst_sent":"57","comment":"late report"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	var after struct {
		DedupeKey string `json:"dedupe_key"`
	}
	if err := unmarshalJSON(w.Body.String(), &after); err != nil {
		t.Fatalf("decode post-edit: %v", err)
	}
	if after.DedupeKey != before.DedupeKey {
		t.Fatalf("dedupe key changed on non-dedupe edit: %s -> %s", before.DedupeKey, after.DedupeKey)
	}
}

func TestUpdateQso_DuplicateKeyConflict(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	// Two distinct QSOs in the same logbook.
	id1 := submitAndGetID(t, srv, lbID, testQsoADIF)
	second := `<CALL:6>DL1ABC<BAND:3>20m<MODE:2>CW<FREQ:6>14.074<QSO_DATE:8>20250508<TIME_ON:4>1000<TIME_OFF:4>1005<RST_SENT:3>599<RST_RCVD:3>599<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>Germany<EOR>`
	_ = submitAndGetID(t, srv, lbID, second)

	// Edit QSO 1 so it would collide with QSO 2. Must match all dedupe
	// inputs, including freq. Include time_off so the merged result doesn't
	// trip the time_on > time_off coherence check first.
	body := `{"call":"DL1ABC","band":"20m","mode":"CW","freq":"14.074","qso_date":"20250508","time_on":"1000","time_off":"1005"}`
	w := patchQso(t, srv, id1, body)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "duplicate_key") {
		t.Fatalf("body = %q, want duplicate_key", w.Body.String())
	}
}

func TestUpdateQso_ImmutableFieldsIgnored(t *testing.T) {
	srv := testServer(t)
	lbID1 := createTestLogbook(t, srv, "Primary", "G4ABC")
	lbID2 := createTestLogbook(t, srv, "Secondary", "M0CMC")
	id := submitAndGetID(t, srv, lbID1, testQsoADIF)

	// Attempt to rewrite structural fields. They should be silently ignored:
	// the PATCH succeeds, but station_callsign and logbook_id stay the same.
	body := fmt.Sprintf(`{"station_callsign":"M0CMC","logbook_id":%d,"id":9999,"comment":"poke"}`, lbID2)
	w := patchQso(t, srv, id, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), fmt.Sprintf(`"logbook_id":%d`, lbID1)) {
		t.Fatalf("logbook_id changed: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"station_callsign":"G4ABC"`) {
		t.Fatalf("station_callsign changed: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), fmt.Sprintf(`"id":%d`, id)) {
		t.Fatalf("id changed: %s", w.Body.String())
	}
}

func TestUpdateQso_InvalidBand(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := patchQso(t, srv, id, `{"band":"not-a-band"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Fatalf("body = %q, want invalid_field_value", w.Body.String())
	}
}

func TestUpdateQso_InvalidCall(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	// No digit — fails callsign validation.
	w := patchQso(t, srv, id, `{"call":"ABC"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestUpdateQso_TimeCoherenceViolation(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	// TIME_ON after TIME_OFF with no QSO_DATE_OFF set.
	w := patchQso(t, srv, id, `{"time_on":"2300","time_off":"0100"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_time_range") {
		t.Fatalf("body = %q, want invalid_time_range", w.Body.String())
	}
}

func TestUpdateQso_NotFound(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPatch, "/v1/qso/999", strings.NewReader(`{"comment":"x"}`))
	req.SetPathValue("id", "999")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateQso(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUpdateQso_InvalidJSON(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := patchQso(t, srv, id, `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUpdateQso_EmptyPatchIsNoOp(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := patchQso(t, srv, id, `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"call":"M0CMC"`) {
		t.Fatalf("body = %q, want original call preserved", w.Body.String())
	}
}

// =============================================================================
// Delete QSO (DELETE)
// =============================================================================

func deleteQso(t *testing.T, srv *Server, qsoID int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/qso/%d", qsoID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", qsoID))
	w := httptest.NewRecorder()
	srv.handleDeleteQso(w, req)
	return w
}

func TestDeleteQso(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := deleteQso(t, srv, id)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// GET should now return 404 — soft-deleted rows are filtered by FindQso.
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d", id), nil)
	getReq.SetPathValue("id", fmt.Sprintf("%d", id))
	getW := httptest.NewRecorder()
	srv.handleGetQso(getW, getReq)
	if getW.Code != http.StatusNotFound {
		t.Fatalf("GET after delete: status = %d, want %d", getW.Code, http.StatusNotFound)
	}
}

func TestDeleteQso_NotFound(t *testing.T) {
	srv := testServer(t)

	w := deleteQso(t, srv, 999)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteQso_Twice(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	id := submitAndGetID(t, srv, lbID, testQsoADIF)

	if w := deleteQso(t, srv, id); w.Code != http.StatusNoContent {
		t.Fatalf("first delete: status = %d", w.Code)
	}
	// Second delete sees no non-deleted row with that ID and returns 404.
	if w := deleteQso(t, srv, id); w.Code != http.StatusNotFound {
		t.Fatalf("second delete: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteQso_InvalidID(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/qso/abc", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	srv.handleDeleteQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeleteQso_FreesDedupeKey(t *testing.T) {
	// Soft-delete should free the dedupe key so the same (call, band,
	// mode, freq, date, time) can be logged again. The partial unique
	// index on dedupe_key is scoped WHERE deleted_at IS NULL, so a
	// soft-deleted row does not block re-submission.
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	id := submitAndGetID(t, srv, lbID, testQsoADIF)
	if w := deleteQso(t, srv, id); w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", w.Code)
	}

	// Resubmit the same QSO — should store, not be rejected as duplicate.
	w := submitQso(t, srv, lbID, testQsoADIF, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("resubmit: status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"stored"`) {
		t.Fatalf("resubmit body = %q, want stored", w.Body.String())
	}
}
