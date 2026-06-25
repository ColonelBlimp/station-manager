package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// putConfigSmtp PUTs a /v1/config body and returns the recorder — a tiny local
// helper so each SMTP test reads as body→status→assert.
func putConfigSmtp(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	return w
}

// TestHandleGetConfig_SmtpMasked: GET serves the full SMTP block for the Email
// tab, but the password is masked — password_set reflects that a secret is
// stored, and the value itself never crosses the wire.
func TestHandleGetConfig_SmtpMasked(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Smtp = types.SmtpConfig{
			Enabled: true, Host: "smtp.example.org", Port: 587,
			Username: "tx@example.org", Password: "s3cret",
			From: "tx@example.org", DefaultRecipient: "qsl@example.org",
			StartTLS: true, TimeoutSec: 30,
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Smtp == nil {
		t.Fatal("smtp block missing from GET")
	}
	if !resp.Smtp.PasswordSet {
		t.Error("PasswordSet = false with a stored password; want true")
	}
	if resp.Smtp.Password != "" {
		t.Errorf("Password = %q on the wire; the secret must be masked", resp.Smtp.Password)
	}
	if resp.Smtp.Host != "smtp.example.org" || resp.Smtp.From != "tx@example.org" {
		t.Errorf("host/from not round-tripped: %+v", resp.Smtp)
	}
	if resp.Smtp.Username != "tx@example.org" || resp.Smtp.DefaultRecipient != "qsl@example.org" {
		t.Errorf("username/default_recipient not round-tripped: %+v", resp.Smtp)
	}
	if !resp.Smtp.StartTLS || resp.Smtp.Port != 587 || resp.Smtp.TimeoutSec != 30 {
		t.Errorf("starttls/port/timeout not round-tripped: %+v", resp.Smtp)
	}
}

// TestHandlePutConfig_SmtpRoundTrip: a full enabled block persists, and the GET
// that follows shows the new fields with the password masked (set, not echoed).
func TestHandlePutConfig_SmtpRoundTrip(t *testing.T) {
	srv := testServer(t)

	body := `{"smtp":{"enabled":true,"host":"smtp.example.org","port":2525,` +
		`"username":"tx@example.org","password":"newpw","from":"tx@example.org",` +
		`"default_recipient":"qsl@example.org","starttls":false,"timeout_sec":15}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	// The secret landed in the stored config (verified directly — it's masked on
	// the wire, so the snapshot is the only place to confirm it).
	if got := srv.cfg.Snapshot().Smtp.Password; got != "newpw" {
		t.Errorf("stored password = %q, want newpw", got)
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Smtp == nil || !resp.Smtp.Enabled || resp.Smtp.Host != "smtp.example.org" {
		t.Fatalf("smtp not persisted: %+v", resp.Smtp)
	}
	if resp.Smtp.Port != 2525 || resp.Smtp.StartTLS || resp.Smtp.TimeoutSec != 15 {
		t.Errorf("port/starttls/timeout not persisted: %+v", resp.Smtp)
	}
	if !resp.Smtp.PasswordSet || resp.Smtp.Password != "" {
		t.Errorf("response should mask the password (set=true, value=\"\"); got %+v", resp.Smtp)
	}
}

// TestHandlePutConfig_SmtpPasswordKeptWhenBlank: editing other fields without
// re-typing the password keeps the stored secret (masked-on-GET means the SPA
// never had it to send back).
func TestHandlePutConfig_SmtpPasswordKeptWhenBlank(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Smtp = types.SmtpConfig{
			Enabled: true, Host: "old.example.org", Port: 587,
			Password: "stored-pw", From: "tx@example.org",
			StartTLS: true, TimeoutSec: 30,
		}
	})

	// New host, no password field.
	body := `{"smtp":{"enabled":true,"host":"new.example.org","port":587,` +
		`"from":"tx@example.org","starttls":true,"timeout_sec":30}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Smtp
	if got.Host != "new.example.org" {
		t.Errorf("host = %q, want new.example.org", got.Host)
	}
	if got.Password != "stored-pw" {
		t.Errorf("password = %q, want stored-pw preserved across a blank-password edit", got.Password)
	}
}

// TestHandlePutConfig_SmtpEnabledMissingHostReturns400: enabling SMTP without a
// host is rejected by the shared validateSmtp — the SPA never re-implements the
// rule.
func TestHandlePutConfig_SmtpEnabledMissingHostReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"smtp":{"enabled":true,"port":587,"from":"tx@example.org","starttls":true,"timeout_sec":30}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_smtp") {
		t.Errorf("body = %s, want invalid_smtp code", w.Body.String())
	}
}

// TestHandlePutConfig_SmtpPresenceAware: a PUT that omits `smtp` (e.g. a My
// Station save) must leave the stored SMTP block untouched — the pointer field
// is the presence signal.
func TestHandlePutConfig_SmtpPresenceAware(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Smtp = types.SmtpConfig{
			Enabled: true, Host: "keep.example.org", Port: 587,
			Password: "keep-pw", From: "tx@example.org",
			StartTLS: true, TimeoutSec: 30,
		}
	})

	body := `{"logging_station":{"station_callsign":"M0XYZ"}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Smtp
	if got.Host != "keep.example.org" || got.Password != "keep-pw" || !got.Enabled {
		t.Errorf("smtp clobbered by a smtp-less PUT: %+v", got)
	}
}
