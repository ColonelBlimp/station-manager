package api

import (
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
// handler testing. Returns the Server and a cleanup function.
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
	// Override the config to use in-memory
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

// ---- Healthz ----

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

// ---- Submit QSO: happy path ----

func TestSubmitQso_Stored(t *testing.T) {
	srv := testServer(t)

	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"stored"`) {
		t.Fatalf("body = %q, want stored", w.Body.String())
	}
}

func TestSubmitQso_Duplicate(t *testing.T) {
	srv := testServer(t)

	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`

	// First submit
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first submit: status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// Second submit — same QSO
	req = httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w = httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second submit: status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"duplicate"`) {
		t.Fatalf("body = %q, want duplicate", w.Body.String())
	}
}

func TestSubmitQso_ForceBypassesDedupe(t *testing.T) {
	srv := testServer(t)

	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`

	// First submit
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first submit: status = %d; body = %s", w.Code, w.Body.String())
	}

	// Second submit with ?force=true — should store, not dedupe
	req = httptest.NewRequest(http.MethodPost, "/v1/qso?force=true", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w = httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("force submit: status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"stored"`) {
		t.Fatalf("body = %q, want stored", w.Body.String())
	}
}

// ---- Error paths ----

func TestSubmitQso_EmptyBody(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/qso", nil)
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "invalid_adif") {
		t.Fatalf("body = %q, want invalid_adif", w.Body.String())
	}
}

func TestSubmitQso_WrongContentType(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestSubmitQso_MissingCall(t *testing.T) {
	srv := testServer(t)

	body := `<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "missing_required_field") {
		t.Fatalf("body = %q, want missing_required_field", w.Body.String())
	}
}

func TestSubmitQso_InvalidBand(t *testing.T) {
	srv := testServer(t)

	body := `<CALL:5>M0CMC<BAND:5>99.9m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Fatalf("body = %q, want invalid_field_value", w.Body.String())
	}
}

func TestSubmitQso_MalformedADIF(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader("this is not adif at all"))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	// Malformed ADIF with no valid tags parses to one empty Record.
	// The handler catches the empty STATION_CALLSIGN and returns 400.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSubmitQso_NoRecords(t *testing.T) {
	srv := testServer(t)

	// Valid ADIF header but no records
	body := `<ADIF_VER:5>3.1.5<EOH>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no QSO records") {
		t.Fatalf("body = %q, want 'no QSO records'", w.Body.String())
	}
}

// ---- Time coherence tests ----

func TestSubmitQso_TimeOnAfterTimeOff_SameDate_Rejected(t *testing.T) {
	srv := testServer(t)

	// TIME_ON=2345 > TIME_OFF=0015, but no QSO_DATE_OFF — invalid.
	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>2345<TIME_OFF:4>0015<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "TIME_ON is after TIME_OFF") {
		t.Fatalf("body = %q, want time coherence error", w.Body.String())
	}
}

func TestSubmitQso_TimeOnAfterTimeOff_NextDay_Accepted(t *testing.T) {
	srv := testServer(t)

	// TIME_ON=2345 > TIME_OFF=0015, QSO_DATE_OFF is the next day — valid midnight crossing.
	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<QSO_DATE_OFF:8>20250509<TIME_ON:4>2345<TIME_OFF:4>0015<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"stored"`) {
		t.Fatalf("body = %q, want stored", w.Body.String())
	}
}

func TestSubmitQso_TimeOnAfterTimeOff_WrongDateOff_Rejected(t *testing.T) {
	srv := testServer(t)

	// TIME_ON=2345 > TIME_OFF=0015, QSO_DATE_OFF is 2 days later — invalid.
	body := `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<QSO_DATE_OFF:8>20250510<TIME_ON:4>2345<TIME_OFF:4>0015<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "QSO_DATE_OFF to be the day after QSO_DATE") {
		t.Fatalf("body = %q, want time coherence error", w.Body.String())
	}
}

func TestSubmitQso_NormalTimeOrder_Accepted(t *testing.T) {
	srv := testServer(t)

	// Normal case: TIME_ON < TIME_OFF, same date — valid.
	body := `<CALL:6>DL1ABC<BAND:3>20m<MODE:2>CW<FREQ:6>14.074<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:3>599<RST_RCVD:3>599<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>Germany<EOR>`
	req := httptest.NewRequest(http.MethodPost, "/v1/qso", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
}
