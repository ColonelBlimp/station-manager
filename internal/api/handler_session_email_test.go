package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// testServerWithMailer wraps testServer and replaces the nil mailer
// with one pointed at the supplied SMTP config — a real
// email.Service. Pair with a fake SMTP server (or one with Host set
// to "" to test the disabled path) to exercise the handler.
func testServerWithMailer(t *testing.T, cfg types.SmtpConfig) *Server {
	t.Helper()
	srv := testServer(t)
	srv.mailer = email.New(cfg, srv.logger)
	return srv
}

// ---- mailer disabled ----

func TestSessionEmail_MailerDisabled_Returns503(t *testing.T) {
	// Default testServer constructs Server with a nil mailer →
	// Enabled() returns false → 503 mailer_disabled.
	srv := testServer(t)

	body := `{"to":"a@b","adif":"<call:5>K1ABC<eor>"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "mailer_disabled") {
		t.Errorf("body should carry code mailer_disabled; got %s", w.Body.String())
	}
}

func TestSessionEmail_MailerConfiguredButDisabled_Returns503(t *testing.T) {
	// Service constructed with a real mailer but Enabled=false → 503
	// (distinct from the nil-Service path above which exercises the
	// same response from a different code branch).
	srv := testServerWithMailer(t, types.SmtpConfig{From: "f@x", Port: 587, TimeoutSec: 5})

	body := `{"to":"a@b","adif":"<call:5>K1ABC<eor>"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// ---- input validation ----

func TestSessionEmail_MissingTo_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"adif":"<call:5>K1ABC<eor>"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing_required_field") {
		t.Errorf("body missing code; got %s", w.Body.String())
	}
}

func TestSessionEmail_MissingAdif_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"to":"a@b"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSessionEmail_ToWithoutAt_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"to":"not-an-email","adif":"<call:5>K1ABC<eor>"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Errorf("body should carry code invalid_field_value; got %s", w.Body.String())
	}
}

func TestSessionEmail_MalformedJson_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(`{not-json`))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_json") {
		t.Errorf("body should carry code invalid_json; got %s", w.Body.String())
	}
}

// ---- transport failure path ----

func TestSessionEmail_SmtpDialFailure_Returns502(t *testing.T) {
	// Point at a closed port — Dial will fail, handler routes the
	// transport error to 502 smtp_failure.
	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled:    true,
		Host:       "127.0.0.1",
		Port:       1, // privileged + nothing listening
		From:       "f@x",
		TimeoutSec: 1,
	})

	body := `{"to":"a@b","adif":"<call:5>K1ABC<eor>"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
	if !strings.Contains(w.Body.String(), "smtp_failure") {
		t.Errorf("body should carry code smtp_failure; got %s", w.Body.String())
	}
}
