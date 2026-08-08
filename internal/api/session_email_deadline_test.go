package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// A successful send must always be REPORTABLE (codex 2026-08-08 P1). The
// server's WriteTimeout (default 30 s) arms the connection write deadline
// before the handler runs, while the SMTP budget (default 30 s, configurable
// HIGHER) plus the pre-send compose/archive work spend from the same clock —
// so SMTP could accept the message and the 200 then hit a dead connection.
// The operator-visible failure is the worst kind: the mail WENT, the client
// saw a transport error, and a retry sends the session twice to a real
// recipient. The confusable to keep distinct: a send that genuinely failed
// (502 — retry is correct) vs a send that succeeded but outlived the response
// window (retry duplicates).
//
// Fixture: a real HTTP server with a 1 s WriteTimeout and a slow-but-WORKING
// SMTP fake (2.5 s pre-banner stall, then a full successful transaction, SMTP
// budget 15 s). The 200 must still reach the client — which requires the
// handler to extend its own response write deadline past the mailer's budget.
func TestSessionEmail_SuccessOutlivesWriteTimeout(t *testing.T) {
	fake := newAPISmtpFake(t)
	fake.stall = 2500 * time.Millisecond
	defer fake.close()
	host, port := fake.hostPort()

	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled:    true,
		Host:       host,
		Port:       port,
		From:       "f@x",
		StartTLS:   false,
		TimeoutSec: 15,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	ts := httptest.NewUnstartedServer(srv.httpServer.Handler)
	ts.Config.WriteTimeout = 1 * time.Second
	ts.Start()
	defer ts.Close()

	body := `{"to":"manager@example.com","uuids":["` + uuid + `"]}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/session/email", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", ts.URL)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("the client never got the response — the mail may have been sent anyway, and a retry would duplicate it: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("response died mid-body — same duplicate-send trap: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, raw)
	}
	var out SessionEmailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Status != "sent" || len(out.Emailed) != 1 {
		t.Errorf("response = %+v, want the sent verdict the operator acts on", out)
	}
}
