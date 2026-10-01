package api

// Recovered-submission attribution on the wire (ADR 0085, operator ruling
// 2026-10-01).
//
//   W1  GET /v1/submit-attribution reports what a Phone / CW submit is stored
//       with: MY_RIG, and the effective OPERATOR and MY_NAME for the
//       logging_station.operator the SPA sends.
//   W2  POST /v1/qso with all three expect_* parameters matching stores.
//   W3  Any mismatch is 409 attribution_changed, and nothing is stored.
//   W4  Some but not all expect_* parameters is a 400; nothing is stored.
//   W5  A present-but-empty parameter is a known-empty value, not "absent".
//   W6  A malformed query (e.g. an unescaped ';') is a 400 and stores nothing:
//       a parser that drops what it cannot read would silently turn a guarded
//       recovered submit into an ordinary one (review 2026-10-01).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func attributionServer(t *testing.T, mutate func(cfg *config.Config)) (*Server, int64) {
	t.Helper()
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		override := "FTdx10 (home)"
		cfg.Rigs = []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10", MyRig: &override}}
		cfg.DefaultRigID = 1
		cfg.Operators = []types.Operator{{Callsign: "G4ABC", Name: "Marc"}}
		cfg.LoggingStation.Operator = "G4ABC"
		if mutate != nil {
			mutate(cfg)
		}
	})
	return srv, createTestLogbook(t, srv, "Main", "G4ABC")
}

func getAttribution(t *testing.T, srv *Server) types.SubmitAttribution {
	t.Helper()
	w := httptest.NewRecorder()
	srv.handleGetSubmitAttribution(w, httptest.NewRequest(http.MethodGet, "/v1/submit-attribution", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got types.SubmitAttribution
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// The SPA's Phone / CW record: it carries logging_station.operator.
const attributionADIF = `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<OPERATOR:5>G4ABC<EOR>`

func submitExpecting(t *testing.T, srv *Server, lbID int64, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	q.Set("logbook", fmt.Sprint(lbID))
	for k, v := range params {
		q.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/qso?"+q.Encode(), strings.NewReader(attributionADIF))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	return w
}

func expectParams(a types.SubmitAttribution) map[string]string {
	return map[string]string{
		"expect_my_rig":   a.MyRig,
		"expect_operator": a.Operator,
		"expect_my_name":  a.MyName,
	}
}

func storedCount(t *testing.T, srv *Server, lbID int64) int64 {
	t.Helper()
	n, err := srv.db.FetchQsoCountByLogbookIdWithContext(context.Background(), lbID, "", false)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestSubmitAttribution_ReportsWhatTheQsoIsStoredWith(t *testing.T) {
	srv, lbID := attributionServer(t, nil)
	got := getAttribution(t, srv)
	if got != (types.SubmitAttribution{MyRig: "FTdx10 (home)", Operator: "G4ABC", MyName: "Marc"}) {
		t.Fatalf("attribution = %+v", got)
	}
	w := submitQso(t, srv, lbID, attributionADIF, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, body = %s", w.Code, w.Body.String())
	}
	var res struct {
		UUID string `json:"uuid"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	stored, err := srv.db.FetchQsoByUUIDWithContext(context.Background(), res.UUID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	want := types.SubmitAttribution{
		MyRig:    stored.LoggingStation.MyRig,
		Operator: stored.LoggingStation.Operator,
		MyName:   stored.LoggingStation.MyName,
	}
	if got != want {
		t.Fatalf("exposed %+v, stored %+v", got, want)
	}
}

func TestSubmitQso_MatchingExpectationStores(t *testing.T) {
	srv, lbID := attributionServer(t, nil)
	w := submitExpecting(t, srv, lbID, expectParams(getAttribution(t, srv)))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestSubmitQso_MismatchedExpectationIs409AndStoresNothing(t *testing.T) {
	srv, lbID := attributionServer(t, nil)
	stale := getAttribution(t, srv)
	stale.MyRig = "Yaesu FTdx10" // the rig this QSO was really made on
	w := submitExpecting(t, srv, lbID, expectParams(stale))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"attribution_changed"`) {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if n := storedCount(t, srv, lbID); n != 0 {
		t.Fatalf("stored %d QSOs, want 0", n)
	}
}

func TestSubmitQso_PartialExpectationIs400(t *testing.T) {
	srv, lbID := attributionServer(t, nil)
	a := getAttribution(t, srv)
	w := submitExpecting(t, srv, lbID, map[string]string{
		"expect_my_rig":   a.MyRig,
		"expect_operator": a.Operator,
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"invalid_query_param"`) {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if n := storedCount(t, srv, lbID); n != 0 {
		t.Fatalf("stored %d QSOs, want 0", n)
	}
}

func TestSubmitQso_PresentEmptyExpectationIsKnownEmpty(t *testing.T) {
	srv, lbID := attributionServer(t, func(cfg *config.Config) {
		suppress := ""
		cfg.Rigs[0].MyRig = &suppress
		cfg.Operators[0].Name = ""
	})
	a := getAttribution(t, srv)
	if a.MyRig != "" || a.MyName != "" {
		t.Fatalf("attribution = %+v, want known-empty MY_RIG and MY_NAME", a)
	}
	w := submitExpecting(t, srv, lbID, expectParams(a))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// Empty is a value: expecting "" where the station now stamps a name refuses.
	srv2, lb2 := attributionServer(t, nil)
	w2 := submitExpecting(t, srv2, lb2, map[string]string{
		"expect_my_rig": "FTdx10 (home)", "expect_operator": "G4ABC", "expect_my_name": "",
	})
	if w2.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", w2.Code, w2.Body.String())
	}
}

func TestSubmitQso_MalformedQueryIs400AndStoresNothing(t *testing.T) {
	srv, lbID := attributionServer(t, nil)
	raw := fmt.Sprintf("logbook=%d&expect_my_rig=a;b&expect_operator=G4;ABC&expect_my_name=M;arc", lbID)
	req := httptest.NewRequest(http.MethodPost, "/v1/qso?"+raw, strings.NewReader(attributionADIF))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"invalid_query_param"`) {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if n := storedCount(t, srv, lbID); n != 0 {
		t.Fatalf("stored %d QSOs, want 0", n)
	}
}
