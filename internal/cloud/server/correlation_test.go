package server

// L6 slice B acceptance — every authenticated application-failure line must carry
// the tenant and request correlation, so the same failure for two tenants (or two
// concurrent requests) is distinguishable and joinable to its access record.
//
// Confusable states (the finding's own): concurrent requests producing similar
// errors; the same error for different tenants. Without tenant_id + request_id on
// the failure line, they are indistinguishable in the log.
//
// These lines fire only on a store error. The store is a concrete *store.Store, so
// there is no fake to inject; instead the server is built over a *sql.DB pointing at
// a dead address — sql.Open is lazy, and the first query fails fast with
// connection-refused, exercising the real handler → store-error → log path through
// the full chain without a live Postgres.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// deadDBServer builds a server whose store points at an unreachable Postgres, so any
// store call returns a connection error rather than data.
func deadDBServer(t *testing.T, buf *bytes.Buffer) *httptest.Server {
	t.Helper()
	db, err := sql.Open("postgres", "host=127.0.0.1 port=1 user=x dbname=x sslmode=disable connect_timeout=1")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := New(store.New(db), db, log, map[string]int64{"tok": 1}, "test-version", 0)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func assertCorrelated(t *testing.T, buf *bytes.Buffer, msg string, wantTenant int64) {
	t.Helper()
	m := findLog(t, buf, msg)
	if tid, _ := m["tenant_id"].(float64); int64(tid) != wantTenant {
		t.Errorf("%q tenant_id = %v, want %d", msg, m["tenant_id"], wantTenant)
	}
	if id, _ := m["request_id"].(string); id == "" {
		t.Errorf("%q missing request_id: %v", msg, m)
	}
}

func TestManifest_LogbookLookupFailure_LogsCorrelation(t *testing.T) {
	var buf bytes.Buffer
	ts := deadDBServer(t, &buf)

	resp := do(t, http.MethodGet, ts.URL+"/v1/logbooks/5/manifest", "tok", nil, nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (store connection error)", resp.StatusCode)
	}
	assertCorrelated(t, &buf, "logbook lookup failed", 1)
}

func TestPutQsos_EnsureLogbookFailure_LogsCorrelation(t *testing.T) {
	var buf bytes.Buffer
	ts := deadDBServer(t, &buf)

	payload, err := json.Marshal(fixtureQso("0197f9a0-0000-7000-8000-000000000001"))
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	body := PutQsosRequest{
		Logbook: "test",
		Qsos:    []QsoUpload{{ModifiedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Qso: payload}},
	}
	resp := do(t, http.MethodPut, ts.URL+"/v1/qsos", "tok", body, nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (store connection error)", resp.StatusCode)
	}
	assertCorrelated(t, &buf, "ensure logbook failed", 1)
}
