package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// testServer creates a Server wired to an in-memory sqlite database for
// handler testing.
func testServer(t *testing.T) *Server {
	t.Helper()

	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"

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

	qsoSvc := &qsoservice.Service{DB: dbSvc, Logger: logSvc}

	return New(cfg, qsoSvc, dbSvc, logSvc)
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
	// Extract ID from {"id":N}
	var id int64
	_, _ = fmt.Sscanf(w.Body.String(), `{"id":%d}`, &id)
	if id < 1 {
		t.Fatalf("createTestLogbook: failed to parse ID from %s", w.Body.String())
	}
	return id
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
	if !strings.Contains(w.Body.String(), "New Name") {
		t.Fatalf("body = %q, want updated name", w.Body.String())
	}
	// Callsign should be unchanged
	if !strings.Contains(w.Body.String(), "G4ABC") {
		t.Fatalf("body = %q, want unchanged callsign", w.Body.String())
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
	if !strings.Contains(w.Body.String(), "TIME_ON is after TIME_OFF") {
		t.Fatalf("body = %q, want time coherence error", w.Body.String())
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
	if !strings.Contains(w.Body.String(), "must be the day after") {
		t.Fatalf("body = %q, want time coherence error", w.Body.String())
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
