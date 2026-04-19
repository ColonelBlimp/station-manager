package qrz

import (
	"context"
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

// captured records the last request the test server saw. Lets tests
// assert on method, form values, and headers without re-wiring each
// time.
type captured struct {
	method    string
	form      url.Values
	userAgent string
	body      string
}

// newTestServer returns an httptest server that records the incoming
// request into *captured and responds with the given status + body.
// Callers close the server in a t.Cleanup.
func newTestServer(t *testing.T, status int, body string, cap *captured) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			http.Error(w, "bad body", http.StatusInternalServerError)
			return
		}
		cap.method = r.Method
		cap.userAgent = r.Header.Get("User-Agent")
		cap.body = string(raw)
		parsed, perr := url.ParseQuery(string(raw))
		if perr == nil {
			cap.form = parsed
		}
		w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fwdAt builds a qrz Forwarder pointed at the given URL. Uses a short
// timeout so any accidental hangs fail fast.
func fwdAt(url string, apiKey string) *Forwarder {
	return newWithEndpoint(apiKey, url, &http.Client{Timeout: 3 * time.Second})
}

// sampleQso returns a minimally-populated QSO. buildForm only
// consumes it through adif.ConvertQsoToAdifNoHeader; the exact
// contents matter less than "it produces a non-empty ADIF".
func sampleQso() types.Qso {
	return types.Qso{
		ContactedStation: types.ContactedStation{Call: "M0TEST"},
		QsoDetails: types.QsoDetails{
			QsoDate: "20260419",
			TimeOn:  "1200",
			Band:    "20m",
			Mode:    "SSB",
		},
		LoggingStation: types.LoggingStation{StationCallsign: "7Q5MLV/T"},
	}
}

// ---------- transport classification ----------

func TestSubmit_Insert_NetworkError_IsTransient(t *testing.T) {
	// Point at a URL that isn't listening — immediate connection refused.
	fwd := fwdAt("http://127.0.0.1:1", "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on network error", res.Outcome)
	}
	if res.Err == nil {
		t.Fatal("Err nil on transient outcome")
	}
}

func TestSubmit_Insert_CtxCancelled_IsTransient(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=OK&LOGID=1", &cap)
	fwd := fwdAt(srv.URL, "key")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := fwd.Submit(ctx, sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on cancelled ctx", res.Outcome)
	}
}

func TestSubmit_Insert_HTTP500_IsTransient(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusInternalServerError, "oops", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on 500", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "500") {
		t.Fatalf("err = %q, want status in message", res.Err.Error())
	}
}

func TestSubmit_Insert_HTTP429_IsTransient(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusTooManyRequests, "rate limited", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on 429", res.Outcome)
	}
}

func TestSubmit_Insert_HTTP408_IsTransient(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusRequestTimeout, "timed out", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on 408", res.Outcome)
	}
}

func TestSubmit_Insert_HTTP400_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusBadRequest, "bad request", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on 400", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "400") {
		t.Fatalf("err = %q, want status in message", res.Err.Error())
	}
}

func TestSubmit_Insert_HTTP401_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusUnauthorized, "unauthorized", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on 401", res.Outcome)
	}
}

// ---------- body classification (insert) ----------

func TestSubmit_Insert_OK_Success(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=OK&LOGID=12345&COUNT=1", &cap)
	fwd := fwdAt(srv.URL, "secret-key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if res.UpstreamID != "12345" {
		t.Fatalf("UpstreamID = %q, want 12345", res.UpstreamID)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil on success", res.Err)
	}
}

func TestSubmit_Insert_OK_MissingLOGID_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=OK&COUNT=1", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on OK-without-LOGID", res.Outcome)
	}
}

func TestSubmit_Insert_FAIL_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=FAIL&REASON=Duplicate+QSO", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on FAIL", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "Duplicate QSO") {
		t.Fatalf("err = %q, want REASON text", res.Err.Error())
	}
}

func TestSubmit_Insert_AUTH_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=AUTH&REASON=Invalid+API+key", &cap)
	fwd := fwdAt(srv.URL, "bogus-key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on AUTH", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "authentication rejected") {
		t.Fatalf("err = %q, want 'authentication rejected' substring", res.Err.Error())
	}
}

// ---------- body classification (update) ----------

func TestSubmit_Update_REPLACE_Success(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=REPLACE&LOGID=999&COUNT=1", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Update, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success on REPLACE", res.Outcome)
	}
	if res.UpstreamID != "999" {
		t.Fatalf("UpstreamID = %q, want 999", res.UpstreamID)
	}
}

func TestSubmit_Update_OK_Success(t *testing.T) {
	// OK on an update means QRZ inserted the record because it didn't
	// already exist. End state matches intent — treat as success.
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=OK&LOGID=100", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Update, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
}

// ---------- malformed body ----------

func TestSubmit_Insert_MalformedBody_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "%ZZ not a real body", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on malformed body", res.Outcome)
	}
}

func TestSubmit_Insert_EmptyBody_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Insert, "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on empty body", res.Outcome)
	}
}

// ---------- request shape assertions ----------

func TestSubmit_Insert_RequestShape(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=OK&LOGID=1", &cap)
	fwd := fwdAt(srv.URL, "the-api-key")

	if res := fwd.Submit(context.Background(), sampleQso(), action.Insert, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}

	if cap.method != http.MethodPost {
		t.Fatalf("method = %q, want POST", cap.method)
	}
	if got := cap.form.Get("KEY"); got != "the-api-key" {
		t.Fatalf("KEY = %q, want 'the-api-key'", got)
	}
	if got := cap.form.Get("ACTION"); got != "INSERT" {
		t.Fatalf("ACTION = %q, want INSERT", got)
	}
	if cap.form.Get("OPTION") != "" {
		t.Fatalf("OPTION = %q, want empty for insert", cap.form.Get("OPTION"))
	}
	if adif := cap.form.Get("ADIF"); adif == "" {
		t.Fatal("ADIF field empty in request")
	} else if !strings.Contains(adif, "7Q5MLV/T") {
		t.Fatalf("ADIF = %q, want STATION_CALLSIGN embedded", adif)
	}
	if !strings.HasPrefix(cap.userAgent, "station-manager/") {
		t.Fatalf("User-Agent = %q, want 'station-manager/*'", cap.userAgent)
	}
}

func TestSubmit_Update_SendsOptionReplace(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=REPLACE&LOGID=1", &cap)
	fwd := fwdAt(srv.URL, "key")

	if res := fwd.Submit(context.Background(), sampleQso(), action.Update, ""); res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
	if got := cap.form.Get("ACTION"); got != "INSERT" {
		t.Fatalf("ACTION = %q, want INSERT (update uses INSERT+REPLACE)", got)
	}
	if got := cap.form.Get("OPTION"); got != "REPLACE" {
		t.Fatalf("OPTION = %q, want REPLACE", got)
	}
}

// ---------- action=delete still deferred ----------

func TestSubmit_Delete_DeferredToStage5(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "RESULT=OK&COUNT=1")
	}))
	t.Cleanup(srv.Close)

	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), action.Delete, "prior-logid-123")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal (delete deferred)", res.Outcome)
	}
	if !strings.Contains(res.Err.Error(), "deferred to stage 5") {
		t.Fatalf("err = %q, want 'deferred to stage 5' substring", res.Err.Error())
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("server saw %d calls, want 0 — delete must not fire HTTP yet", got)
	}
}

// ---------- unknown action ----------

func TestSubmit_UnknownAction_IsTerminal(t *testing.T) {
	var cap captured
	srv := newTestServer(t, http.StatusOK, "RESULT=OK&LOGID=1", &cap)
	fwd := fwdAt(srv.URL, "key")

	res := fwd.Submit(context.Background(), sampleQso(), "hovercraft", "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal on unknown action", res.Outcome)
	}
}
