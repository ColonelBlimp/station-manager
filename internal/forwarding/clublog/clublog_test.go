package clublog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// validCreds returns a minimally-valid credentials blob.
func validCreds(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"email":    "op@example.com",
		"password": "app-password",
		"callsign": "7Q5MLV",
		"api":      "0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33",
	})
	if err != nil {
		t.Fatalf("marshal creds: %v", err)
	}
	return raw
}

// captured records the last request the test server saw.
type captured struct {
	method    string
	form      url.Values
	userAgent string
}

// newTestServer returns an httptest server that records requests into
// *captured and responds with the given status + body. The status line
// reason phrase is set to statusText so the classifier sees ClubLog's
// "200 QSO Duplicate"-style sub-results.
func newTestServer(t *testing.T, status int, statusText, body string, rec *captured) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusInternalServerError)
			return
		}
		rec.method = r.Method
		rec.userAgent = r.Header.Get("User-Agent")
		if parsed, perr := url.ParseQuery(string(raw)); perr == nil {
			rec.form = parsed
		}
		// httptest can't set a custom reason phrase, so encode it in the
		// body too — the classifier falls back to the body when the
		// status line carries no phrase, which is what httptest gives us.
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fwdAt builds a clublog Forwarder pointed at the given URLs with a short
// timeout so accidental hangs fail fast.
func fwdAt(realtime, deleteURL string) *Forwarder {
	return newWithEndpoints(
		credentials{Email: "op@example.com", Password: "pw", Callsign: "7Q5MLV", APIKey: "key"},
		realtime, deleteURL,
		&http.Client{Timeout: 3 * time.Second},
	)
}

// sampleQso returns a minimally-populated QSO.
func sampleQso() types.Qso {
	return types.Qso{
		ContactedStation: types.ContactedStation{Call: "M0TEST"},
		QsoDetails: types.QsoDetails{
			QsoDate: "20260419",
			TimeOn:  "1200",
			Band:    "20m",
			Mode:    "SSB",
		},
		LoggingStation: types.LoggingStation{StationCallsign: "7Q5MLV"},
	}
}

// ---------- registration + construction ----------

func TestInit_RegistersClublogType(t *testing.T) {
	if !forwarding.IsRegistered(Type) {
		t.Fatalf("clublog type %q not registered via init()", Type)
	}
}

func TestInit_RegistersDefaultRetry(t *testing.T) {
	got, ok := forwarding.DefaultRetryFor(Type)
	if !ok {
		t.Fatalf("clublog type %q has no default retry registered", Type)
	}
	if got != DefaultRetry {
		t.Fatalf("registered default = %+v, want %+v", got, DefaultRetry)
	}
}

func TestNew_Valid_ReturnsForwarder(t *testing.T) {
	fwd, err := New(types.ForwarderConfig{Name: "clublog", Type: Type, Credentials: validCreds(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if fwd.Type() != Type {
		t.Fatalf("Type = %q, want %q", fwd.Type(), Type)
	}
	if fwd.AdifPrefix() != AdifFieldPrefix {
		t.Fatalf("AdifPrefix = %q, want %q", fwd.AdifPrefix(), AdifFieldPrefix)
	}
}

func TestNew_MissingCredentials_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{Name: "x", Type: Type})
	if err == nil || !strings.Contains(err.Error(), "credentials required") {
		t.Fatalf("err = %v, want 'credentials required'", err)
	}
}

func TestNew_MissingField_Errors(t *testing.T) {
	cases := map[string]map[string]string{
		"email":    {"password": "p", "callsign": "c", "api": "k"},
		"password": {"email": "e@x", "callsign": "c", "api": "k"},
		"callsign": {"email": "e@x", "password": "p", "api": "k"},
		"api":      {"email": "e@x", "password": "p", "callsign": "c"},
	}
	for missing, creds := range cases {
		raw, _ := json.Marshal(creds)
		_, err := New(types.ForwarderConfig{Name: "x", Type: Type, Credentials: raw})
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Fatalf("missing %s: err = %v, want it named", missing, err)
		}
	}
}

func TestNew_MalformedCredentials_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type, Credentials: json.RawMessage(`{not json`),
	})
	if err == nil {
		t.Fatal("expected error for malformed credentials")
	}
}

// ---------- insert: transport classification ----------

func TestSubmit_Insert_NetworkError_IsTransient(t *testing.T) {
	fwd := fwdAt("http://127.0.0.1:1", "http://127.0.0.1:1")
	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on network error", res.Outcome)
	}
}

func TestSubmit_Insert_CtxCancelled_IsTransient(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusOK, "", "QSO OK", &rec)
	fwd := fwdAt(srv.URL, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res := fwd.Submit(ctx, sampleQso(), action.Insert, ""); res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on cancelled ctx", res.Outcome)
	}
}

func TestSubmit_Insert_HTTP500_IsTransient(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusInternalServerError, "", "oops", &rec)
	fwd := fwdAt(srv.URL, srv.URL)
	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on 500", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "500") {
		t.Fatalf("err = %q, want status in message", res.Err.Error())
	}
}

func TestSubmit_Insert_HTTP429_IsTransient(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusTooManyRequests, "", "throttled", &rec)
	fwd := fwdAt(srv.URL, srv.URL)
	if res := fwd.Submit(context.Background(), sampleQso(), action.Insert, ""); res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on 429", res.Outcome)
	}
}

func TestSubmit_Insert_HTTP400_IsTerminal(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusBadRequest, "", "QSO Rejected: bad ADIF", &rec)
	fwd := fwdAt(srv.URL, srv.URL)
	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on 400", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "400") {
		t.Fatalf("err = %q, want status in message", res.Err.Error())
	}
}

// ---------- insert: success variants ----------

func TestSubmit_Insert_Success_Variants(t *testing.T) {
	for _, body := range []string{"QSO OK", "QSO Modified", "QSO Duplicate"} {
		var rec captured
		srv := newTestServer(t, http.StatusOK, "", body, &rec)
		fwd := fwdAt(srv.URL, srv.URL)
		res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
		if res.Outcome != forwarding.OutcomeSuccess {
			t.Fatalf("body %q: outcome = %q, want success", body, res.Outcome)
		}
		if res.UpstreamID != "" {
			t.Fatalf("body %q: UpstreamID = %q, want empty (ClubLog returns none)", body, res.UpstreamID)
		}
		if res.Err != nil {
			t.Fatalf("body %q: Err = %v, want nil on success", body, res.Err)
		}
	}
}

func TestSubmit_Insert_RequestShape(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusOK, "", "QSO OK", &rec)
	fwd := fwdAt(srv.URL, srv.URL)

	if res := fwd.Submit(context.Background(), sampleQso(), action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if rec.method != http.MethodPost {
		t.Fatalf("method = %q, want POST", rec.method)
	}
	for _, k := range []string{"email", "password", "callsign", "api"} {
		if rec.form.Get(k) == "" {
			t.Fatalf("%s missing from insert request", k)
		}
	}
	if adif := rec.form.Get("adif"); adif == "" {
		t.Fatal("adif field empty in request")
	} else if !strings.Contains(adif, "M0TEST") {
		t.Fatalf("adif = %q, want CALL embedded", adif)
	}
	if !strings.HasPrefix(rec.userAgent, "station-manager/") {
		t.Fatalf("User-Agent = %q, want 'station-manager/*'", rec.userAgent)
	}
}

// ---------- 403 circuit breaker ----------

func TestSubmit_HTTP403_TripsBreaker_AndShortCircuits(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "Forbidden")
	}))
	t.Cleanup(srv.Close)
	fwd := fwdAt(srv.URL, srv.URL)

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on 403", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "403") {
		t.Fatalf("err = %q, want '403' substring", res.Err.Error())
	}

	// Second Submit must NOT reach the network — the breaker is tripped.
	res2 := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res2.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("second outcome = %q, want terminal (breaker tripped)", res2.Outcome)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server saw %d calls, want 1 — breaker must suppress further requests", got)
	}
	if !strings.Contains(res2.Err.Error(), "refusing further requests") {
		t.Fatalf("err = %q, want breaker message", res2.Err.Error())
	}
}

// ---------- update ----------

func TestSubmit_Update_IsTerminal_NoNetwork(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "QSO OK")
	}))
	t.Cleanup(srv.Close)
	fwd := fwdAt(srv.URL, srv.URL)

	res := fwd.Submit(context.Background(), sampleQso(), action.Update, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal (no update support)", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "cannot edit") {
		t.Fatalf("err = %q, want 'cannot edit' substring", res.Err.Error())
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("server saw %d calls, want 0 — update must not hit the network", got)
	}
}

// ---------- delete ----------

func TestSubmit_Delete_OK_Success(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusOK, "", "QSO OK", &rec)
	fwd := fwdAt(srv.URL, srv.URL)
	res := fwd.Submit(context.Background(), sampleQso(), action.Delete, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success on delete OK", res.Outcome)
	}
}

func TestSubmit_Delete_NotDeleted_IsIdempotentSuccess(t *testing.T) {
	// "QSO Not Deleted" (no match) — the record's absence matches intent.
	var rec captured
	srv := newTestServer(t, http.StatusOK, "", "QSO Not Deleted", &rec)
	fwd := fwdAt(srv.URL, srv.URL)
	res := fwd.Submit(context.Background(), sampleQso(), action.Delete, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want idempotent success on Not Deleted", res.Outcome)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
}

func TestSubmit_Delete_RequestShape(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusOK, "", "QSO OK", &rec)
	fwd := fwdAt(srv.URL, srv.URL)

	if res := fwd.Submit(context.Background(), sampleQso(), action.Delete, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if got := rec.form.Get("dxcall"); got != "M0TEST" {
		t.Fatalf("dxcall = %q, want M0TEST", got)
	}
	if got := rec.form.Get("datetime"); got != "2026-04-19 12:00:00" {
		t.Fatalf("datetime = %q, want '2026-04-19 12:00:00'", got)
	}
	if got := rec.form.Get("bandid"); got != "20" {
		t.Fatalf("bandid = %q, want 20", got)
	}
	// No ADIF on a structured delete.
	if rec.form.Get("adif") != "" {
		t.Fatalf("adif = %q, want empty on delete", rec.form.Get("adif"))
	}
}

func TestSubmit_Delete_MissingFields_AreTerminal_NoNetwork(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "QSO OK")
	}))
	t.Cleanup(srv.Close)
	fwd := fwdAt(srv.URL, srv.URL)

	cases := map[string]types.Qso{
		"no call":       {QsoDetails: types.QsoDetails{QsoDate: "20260419", TimeOn: "1200", Band: "20m"}},
		"bad date":      {ContactedStation: types.ContactedStation{Call: "M0TEST"}, QsoDetails: types.QsoDetails{QsoDate: "2026", TimeOn: "1200", Band: "20m"}},
		"unmapped band": {ContactedStation: types.ContactedStation{Call: "M0TEST"}, QsoDetails: types.QsoDetails{QsoDate: "20260419", TimeOn: "1200", Band: "1.25m"}},
	}
	for name, qso := range cases {
		res := fwd.Submit(context.Background(), qso, action.Delete, "")
		if res.Outcome != forwarding.OutcomeTerminal {
			t.Fatalf("%s: outcome = %q, want terminal", name, res.Outcome)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("server saw %d calls, want 0 — unresolvable deletes must not hit the network", got)
	}
}

// ---------- unknown action ----------

func TestSubmit_UnknownAction_IsTerminal(t *testing.T) {
	var rec captured
	srv := newTestServer(t, http.StatusOK, "", "QSO OK", &rec)
	fwd := fwdAt(srv.URL, srv.URL)
	if res := fwd.Submit(context.Background(), sampleQso(), "hovercraft", ""); res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on unknown action", res.Outcome)
	}
}

// ---------- pure helpers ----------

func TestFormatDateTime(t *testing.T) {
	cases := []struct {
		date, time, want string
		wantErr          bool
	}{
		{"20260419", "1200", "2026-04-19 12:00:00", false},
		{"20070903", "213300", "2007-09-03 21:33:00", false},
		{"2026", "1200", "", true},      // short date
		{"20260419", "12", "", true},    // short time
		{"20260419", "12000", "", true}, // 5 digits
		{"abcd0419", "1200", "", true},  // non-numeric date
		{"20260419", "12ab", "", true},  // non-numeric time
	}
	for _, c := range cases {
		got, err := formatDateTime(c.date, c.time)
		if c.wantErr {
			if err == nil {
				t.Errorf("formatDateTime(%q,%q) err = nil, want error", c.date, c.time)
			}
			continue
		}
		if err != nil {
			t.Errorf("formatDateTime(%q,%q) err = %v", c.date, c.time, err)
		}
		if got != c.want {
			t.Errorf("formatDateTime(%q,%q) = %q, want %q", c.date, c.time, got, c.want)
		}
	}
}

func TestBandID(t *testing.T) {
	cases := map[string]string{"20m": "20", "160m": "160", "70cm": "70", "23cm": "23", "6m": "6"}
	for band, want := range cases {
		if got, ok := bandID(band); !ok || got != want {
			t.Errorf("bandID(%q) = %q,%v want %q,true", band, got, ok, want)
		}
	}
	// Case/whitespace tolerated; unmapped bands rejected.
	if got, ok := bandID(" 20M "); !ok || got != "20" {
		t.Errorf("bandID(' 20M ') = %q,%v want 20,true", got, ok)
	}
	for _, band := range []string{"1.25m", "33cm", "", "garbage"} {
		if _, ok := bandID(band); ok {
			t.Errorf("bandID(%q) ok = true, want false", band)
		}
	}
}
