package smcloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func testConfig(url string) types.ForwarderConfig {
	creds, _ := json.Marshal(map[string]string{"url": url, "token": "tok-123"})
	return types.ForwarderConfig{Name: "smcloud", Type: Type, Credentials: creds}
}

func testQso(uuid string) types.Qso {
	q := types.Qso{UUID: uuid, ModifiedAt: time.Date(2026, 7, 17, 7, 0, 0, 123456000, time.UTC)}
	q.Call = "DL9UW"
	q.Band = "20m"
	q.Mode = "SSB"
	q.QsoDate = "20260717"
	q.TimeOn = "070015"
	return q
}

func TestNew_Validation(t *testing.T) {
	cases := []struct {
		name  string
		creds string
	}{
		{"no credentials", ``},
		{"missing url", `{"token":"x"}`},
		{"missing token", `{"url":"https://c.example.org"}`},
		{"non-http url", `{"url":"ftp://c.example.org","token":"x"}`},
		{"garbage url", `{"url":"::not a url","token":"x"}`},
	}
	for _, c := range cases {
		fc := types.ForwarderConfig{Name: "smcloud", Type: Type}
		if c.creds != "" {
			fc.Credentials = json.RawMessage(c.creds)
		}
		if _, err := New(fc); err == nil {
			t.Errorf("%s: New accepted bad credentials", c.name)
		}
	}
}

// TestNew_RejectionDoesNotEchoCredentials: a constructor error is logged as a
// startup fatal by spawnForwarderWorkers and raised again by the config PUT's
// startup probe, so it must never quote the URL — a URL credential can hide
// userinfo (https://user:token@host) that a %q looks harmless around.
func TestNew_RejectionDoesNotEchoCredentials(t *testing.T) {
	const (
		user  = "alice"
		token = "s3cr3t-token"
		bad   = "ftp://" + user + ":" + token + "@cloud.example.org"
	)
	fc := types.ForwarderConfig{Name: "smcloud", Type: Type}
	fc.Credentials = json.RawMessage(`{"url":"` + bad + `","token":"tok-123"}`)

	_, err := New(fc)
	if err == nil {
		t.Fatal("New accepted a non-http(s) URL")
	}
	for _, leak := range []string{bad, token, user, "tok-123"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("constructor error leaked credential material (%q): %v", leak, err)
		}
	}
	// It must still say something useful.
	if !strings.Contains(err.Error(), "credentials.url") {
		t.Fatalf("error should still name the offending field: %v", err)
	}
}

func TestNew_LogbookDefaultsToMain(t *testing.T) {
	f, err := New(testConfig("https://cloud.example.org/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fwd := f.(*Forwarder)
	if fwd.logbook != DefaultLogbook {
		t.Errorf("logbook = %q, want %q", fwd.logbook, DefaultLogbook)
	}
	// Trailing slash on the base URL must not double up.
	if fwd.putURL != "https://cloud.example.org/v1/qsos" {
		t.Errorf("putURL = %q", fwd.putURL)
	}
}

// The forwarder deliberately stamps NO ADIF prefix: the worker then takes the
// plain success path (qso_upload only, no QSO-row write), so a backup push
// can never bump the row's modified_at — the property S4 reconcile stands on.
func TestAdifPrefix_EmptyByDesign(t *testing.T) {
	f, err := New(testConfig("https://cloud.example.org"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p := f.AdifPrefix(); p != "" {
		t.Fatalf("AdifPrefix = %q, want empty (modified_at protection)", p)
	}
}

// Submit sends the documented wire shape: bearer auth, the logbook name, and
// a modified_at envelope BESIDE the payload (never inside it).
func TestSubmit_InsertWireShape(t *testing.T) {
	var got struct {
		auth, ua string
		body     putRequest
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		got.ua = r.Header.Get("User-Agent")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.body)
		// The payload must NOT carry modified_at/deleted_at keys (json:"-").
		var raw struct {
			Qsos []struct {
				Qso map[string]any `json:"qso"`
			} `json:"qsos"`
		}
		_ = json.Unmarshal(b, &raw)
		if _, has := raw.Qsos[0].Qso["modified_at"]; has {
			t.Error("payload leaked modified_at into the qso body")
		}
		_ = json.NewEncoder(w).Encode(putResponse{Received: 1, Applied: 1})
	}))
	defer ts.Close()

	f, _ := New(testConfig(ts.URL))
	q := testQso("0197f9a0-0000-7000-8000-000000000001")
	res := f.Submit(context.Background(), q, action.Insert, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %s (%v)", res.Outcome, res.Err)
	}
	if res.UpstreamID != q.UUID {
		t.Errorf("upstream id = %q, want the uuid", res.UpstreamID)
	}
	if got.auth != "Bearer tok-123" {
		t.Errorf("auth = %q", got.auth)
	}
	if got.ua == "" {
		t.Error("no User-Agent sent")
	}
	if got.body.Logbook != DefaultLogbook || len(got.body.Qsos) != 1 {
		t.Fatalf("body = %+v", got.body)
	}
	up := got.body.Qsos[0]
	if !up.ModifiedAt.Equal(q.ModifiedAt) || up.DeletedAt != nil {
		t.Errorf("envelope = modified %v deleted %v", up.ModifiedAt, up.DeletedAt)
	}
	if up.Qso.UUID != q.UUID || up.Qso.Call != "DL9UW" {
		t.Errorf("payload qso = %+v", up.Qso)
	}
}

// Delete sends the tombstone: the same record with deleted_at set; the
// prior upstream id is ignored (the UUID is the key).
func TestSubmit_DeleteSendsTombstone(t *testing.T) {
	var got putRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_ = json.NewEncoder(w).Encode(putResponse{Received: 1, Applied: 1})
	}))
	defer ts.Close()

	f, _ := New(testConfig(ts.URL))
	q := testQso("0197f9a0-0000-7000-8000-000000000002")
	q.DeletedAt = q.ModifiedAt.Add(time.Minute)
	res := f.Submit(context.Background(), q, action.Delete, "ignored-prior-id")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %s (%v)", res.Outcome, res.Err)
	}
	up := got.Qsos[0]
	if up.DeletedAt == nil || !up.DeletedAt.Equal(q.DeletedAt) {
		t.Fatalf("tombstone deleted_at = %v, want %v", up.DeletedAt, q.DeletedAt)
	}
}

// A stale re-push (cloud kept a newer copy → applied 0) is still success:
// the backup already holds this QSO's future.
func TestSubmit_StaleAppliedZeroIsSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(putResponse{Received: 1, Applied: 0})
	}))
	defer ts.Close()
	f, _ := New(testConfig(ts.URL))
	res := f.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-000000000003"), action.Update, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %s (%v)", res.Outcome, res.Err)
	}
}

func TestSubmit_OutcomeClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   forwarding.Outcome
	}{
		{"bad token", http.StatusUnauthorized, forwarding.OutcomeTerminal},
		{"malformed", http.StatusBadRequest, forwarding.OutcomeTerminal},
		{"server error", http.StatusInternalServerError, forwarding.OutcomeTransient},
		{"rate limited", http.StatusTooManyRequests, forwarding.OutcomeTransient},
		{"degraded", http.StatusServiceUnavailable, forwarding.OutcomeTransient},
	}
	for _, c := range cases {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(`{"code":"x","message":"y"}`))
		}))
		f, _ := New(testConfig(ts.URL))
		res := f.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-000000000004"), action.Insert, "")
		ts.Close()
		if res.Outcome != c.want {
			t.Errorf("%s: outcome = %s, want %s", c.name, res.Outcome, c.want)
		}
		if res.Err == nil {
			t.Errorf("%s: no error recorded for last_error", c.name)
		}
	}
}

// No response at all → unreachable → the worker retries forever (ADR 0038).
func TestSubmit_ConnectionRefusedIsUnreachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	ts.Close() // deliberately dead
	f, _ := New(testConfig(ts.URL))
	res := f.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-000000000005"), action.Insert, "")
	if res.Outcome != forwarding.OutcomeUnreachable {
		t.Fatalf("outcome = %s, want unreachable", res.Outcome)
	}
}

// Guard rails: a QSO without UUID or without the modified_at overlay is a
// caller/fetch-path bug — terminal, never silently defaulted (a now()
// substitute would poison the reconcile hash forever).
func TestSubmit_GuardRails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("request must not reach the wire")
	}))
	defer ts.Close()
	f, _ := New(testConfig(ts.URL))

	noUUID := testQso("")
	if res := f.Submit(context.Background(), noUUID, action.Insert, ""); res.Outcome != forwarding.OutcomeTerminal {
		t.Errorf("no-uuid outcome = %s, want terminal", res.Outcome)
	}
	noMod := testQso("0197f9a0-0000-7000-8000-000000000006")
	noMod.ModifiedAt = time.Time{}
	if res := f.Submit(context.Background(), noMod, action.Insert, ""); res.Outcome != forwarding.OutcomeTerminal {
		t.Errorf("no-modified_at outcome = %s, want terminal", res.Outcome)
	}
}

func TestRegistry_Registered(t *testing.T) {
	if !forwarding.IsRegistered(Type) {
		t.Fatal("smcloud not registered")
	}
	if _, ok := forwarding.DefaultRetryFor(Type); !ok {
		t.Fatal("no default retry registered")
	}
	if _, ok := forwarding.AdifPrefixForType(Type); ok {
		t.Fatal("smcloud must register NO adif prefix")
	}
	if _, ok := forwarding.DefaultEndpointsFor(Type); ok {
		t.Fatal("smcloud must register NO default endpoints (operator-supplied URL)")
	}
	acts, ok := forwarding.SupportedActionsFor(Type)
	if !ok || len(acts) != 3 {
		t.Fatalf("supported actions = %v", acts)
	}
}

// TestSubmit_InvalidAckIsTransient: a protocol-invalid 2xx acknowledgement (the
// cloud did NOT take our single record — received != 1, or an out-of-range
// applied) must be TRANSIENT, so the backup retries rather than being silently
// marked uploaded and dropped (review 2026-07-20 internal/forwarding #3).
func TestSubmit_InvalidAckIsTransient(t *testing.T) {
	cases := []struct {
		name string
		body putResponse
	}{
		{"received zero (e.g. {})", putResponse{Received: 0, Applied: 0}},
		{"received two", putResponse{Received: 2, Applied: 1}},
		{"applied out of range", putResponse{Received: 1, Applied: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			defer ts.Close()
			f, _ := New(testConfig(ts.URL))
			res := f.Submit(context.Background(), testQso("0197f9a0-0000-7000-8000-0000000000f1"),
				action.Insert, "")
			if res.Outcome != forwarding.OutcomeTransient {
				t.Fatalf("outcome = %s (%v), want Transient", res.Outcome, res.Err)
			}
		})
	}
}
