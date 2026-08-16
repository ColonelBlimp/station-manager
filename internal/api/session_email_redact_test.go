package api

// H4 (docs/reviews/internal-codebase-logging-gaps.md) — the session-email handler's
// send-failure path logged the FULL recipient. It must log the recipient domain only
// (email.Domain), never the raw address. smd.log is 0644.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestSessionEmail_SendFailure_LogsToDomainNotRawRecipient(t *testing.T) {
	buf := &bytes.Buffer{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	// Dead SMTP port → Send fails, handler takes the smtp_failure error path.
	srv.mailer = email.New(types.SmtpConfig{
		Enabled: true, Host: "127.0.0.1", Port: 1, From: "daemon@station.example", TimeoutSec: 1,
	}, srv.logger)

	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	body := `{"to":"alice@recipient.example","uuids":["` + uuid + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}

	recs := allMessages(t, buf, "session email send failed")
	if len(recs) != 1 {
		t.Fatalf("send-failure records = %d, want 1\n%s", len(recs), buf.String())
	}
	if recs[0]["to_domain"] != "recipient.example" {
		t.Errorf("to_domain = %v, want recipient.example", recs[0]["to_domain"])
	}
	if _, ok := recs[0]["to"]; ok {
		t.Errorf("raw recipient must not be logged: %v", recs[0]["to"])
	}
	if strings.Contains(buf.String(), "alice@recipient.example") {
		t.Errorf("raw recipient leaked into the log:\n%s", buf.String())
	}
}
