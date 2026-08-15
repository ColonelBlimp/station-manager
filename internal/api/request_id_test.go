package api

// L6 slice C acceptance — the daemon must carry a request correlation id that joins
// the access record to an inner service failure (mirrors the cloud's request-id
// behaviour: accept a bounded inbound X-Request-Id, else generate one, and echo it).
//
// Confusable states (the finding's own): concurrent requests producing similar
// errors; a handler panic vs a transport disconnect. Without a request id shared by
// the access line and the failure line, two concurrent 5xx cannot be told apart.
//
// Criteria (observable: the access line, the "server error"/panic line, the header):
//   C1 — every request's access line carries a non-empty request_id, echoed in the
//        X-Request-Id response header.
//   C2 — a bounded inbound X-Request-Id is adopted; an unsafe one is replaced.
//   C3 — a 5xx via writeServerError logs a "server error" line whose request_id
//        EQUALS the access line's — a deterministic join.
//   C4 — a panic's line carries the same request_id as the access line.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	stderr "errors"

	apierrors "github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func reqIDServer(t *testing.T) (*Server, *strings.Builder) {
	t.Helper()
	buf := &strings.Builder{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	return srv, buf
}

// exactRecord returns the single log record whose message EQUALS msg (credWarnRecords
// substring-matches, so "http request" would also catch the "http request received"
// debug entry line).
func exactRecord(t *testing.T, buf *strings.Builder, msg string) map[string]any {
	t.Helper()
	var found map[string]any
	n := 0
	for _, rec := range credWarnRecords(t, buf, msg) {
		if m, _ := rec["message"].(string); m == msg {
			found = rec
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 record with message %q, got %d; log:\n%s", msg, n, buf.String())
	}
	return found
}

func TestResolveRequestID_AcceptsSafeInboundRejectsUnsafe(t *testing.T) {
	good := httptest.NewRequest(http.MethodGet, "/x", nil)
	good.Header.Set("X-Request-Id", "req-abcdef0123456789")
	if got := resolveRequestID(good); got != "req-abcdef0123456789" {
		t.Errorf("safe inbound id = %q, want it adopted verbatim", got)
	}

	bad := httptest.NewRequest(http.MethodGet, "/x", nil)
	bad.Header.Set("X-Request-Id", "short") // < 16 chars → rejected
	got := resolveRequestID(bad)
	if got == "" || got == "short" {
		t.Errorf("unsafe inbound id = %q, want a generated replacement", got)
	}
}

func TestLogRequests_AssignsAndEchoesRequestID(t *testing.T) {
	srv, buf := reqIDServer(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	w := httptest.NewRecorder()

	srv.logRequests(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	echoed := w.Header().Get("X-Request-Id")
	if echoed == "" {
		t.Fatal("no X-Request-Id echoed in the response header")
	}
	access := exactRecord(t, buf, "http request")
	if id, _ := access["request_id"].(string); id == "" || id != echoed {
		t.Errorf("access request_id = %q, header = %q; want equal + non-empty", id, echoed)
	}
}

func TestLogRequests_AdoptsSafeInboundID(t *testing.T) {
	srv, buf := reqIDServer(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-Id", "req-abcdef0123456789")

	srv.logRequests(h).ServeHTTP(w, req)

	access := exactRecord(t, buf, "http request")
	if id, _ := access["request_id"].(string); id != "req-abcdef0123456789" {
		t.Errorf("access request_id = %q, want the adopted inbound id", id)
	}
}

func TestWriteServerError_CarriesRequestID_JoinsAccessLine(t *testing.T) {
	srv, buf := reqIDServer(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.writeServerError(w, apierrors.Op("test.op"), stderr.New("boom"), "db_error", "failed")
	})
	w := httptest.NewRecorder()

	srv.logRequests(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	access := exactRecord(t, buf, "http request")
	errln := exactRecord(t, buf, "server error")
	aid, _ := access["request_id"].(string)
	eid, _ := errln["request_id"].(string)
	if aid == "" || aid != eid {
		t.Errorf("server-error request_id %q must equal access request_id %q (joinable)", eid, aid)
	}
}

func TestRecoverPanic_CarriesRequestID_JoinsAccessLine(t *testing.T) {
	srv, buf := reqIDServer(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	w := httptest.NewRecorder()

	srv.logRequests(srv.recoverPanic(h)).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	access := exactRecord(t, buf, "http request")
	panicln := exactRecord(t, buf, "panic in HTTP handler")
	aid, _ := access["request_id"].(string)
	pid, _ := panicln["request_id"].(string)
	if aid == "" || aid != pid {
		t.Errorf("panic request_id %q must equal access request_id %q (joinable)", pid, aid)
	}
}
