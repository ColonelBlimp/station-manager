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

/*
	REMOVING A STORED SMTP PASSWORD (operator's call, 2026-08-03).

	Acceptance criterion:

	    When I ask to remove the stored password and save, the daemon's copy is
	    gone and the block stops reporting a password as set — and I can tell
	    that apart from simply leaving the box blank, which KEEPS it.

	Both halves matter, and only the PAIR proves either. TestHandlePutConfig_
	SmtpPasswordKeptWhenBlank above is the other half: without it, "clear works"
	is satisfiable by a merge that drops the password on every save. Read the two
	together — they are one rule about two indistinguishable-looking payloads.

	Why a flag and not the forwarder Clearable idiom (blank means reset): the
	operator ruled blank must go on meaning KEEP. A field whose blank is the
	overwhelmingly common "I didn't retype it" cannot also carry a destructive
	meaning — that is the mistake forwarders avoid by declaring which specific
	fields opt in, and SMTP's single password is not a field that could safely
	opt in. So removal gets its own signal.

	Unauthenticated submission is a legitimate configuration (validateSmtp has
	never required a password), so a cleared password on an ENABLED block must
	still save — S1 uses one deliberately.
*/

// S1: the clear signal reaches the daemon — the stored secret is gone and the
// response reports password_set:false.
func TestHandlePutConfig_SmtpPasswordCleared(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Smtp = types.SmtpConfig{
			Enabled: true, Host: "smtp.example.org", Port: 587,
			Username: "tx@example.org", Password: "stored-pw",
			From: "tx@example.org", StartTLS: true, TimeoutSec: 30,
		}
	})

	body := `{"smtp":{"enabled":true,"host":"smtp.example.org","port":587,` +
		`"username":"tx@example.org","from":"tx@example.org","starttls":true,` +
		`"timeout_sec":30,"password_clear":true}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	if got := srv.cfg.Snapshot().Smtp.Password; got != "" {
		t.Errorf("stored password = %q after an explicit clear, want removed", got)
	}
	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Smtp == nil || resp.Smtp.PasswordSet {
		t.Errorf("response still reports a password as set: %+v", resp.Smtp)
	}
	// The rest of the block is untouched by the clear.
	if got := srv.cfg.Snapshot().Smtp; got.Host != "smtp.example.org" || got.Username != "tx@example.org" {
		t.Errorf("clearing the password disturbed the rest of the block: %+v", got)
	}
}

// S2: clear and a typed value cannot both be honoured, so the destructive
// intent wins and the rule is stated rather than left to whichever the merge
// happens to read last. Clear-wins over a 400 is the OPERATOR'S RULING
// (2026-08-03): fail-safe for secret removal, and sensible against stale form
// state. This test is what stops a later "surely that should be an error"
// rewrite from changing it silently.
//
// Our own SPA never sends this pair (pressing Remove discards any half-typed
// value, the same way forwarding's clear() does), so this is the daemon
// refusing to guess on behalf of a client that got it wrong. Clear wins because
// it is the only one of the two the operator can have expressed DELIBERATELY:
// a stale password field can be left populated by a form bug, whereas the clear
// flag is only ever set by pressing the control.
func TestHandlePutConfig_SmtpClearBeatsTypedPassword(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Smtp = types.SmtpConfig{
			Enabled: true, Host: "smtp.example.org", Port: 587,
			Password: "stored-pw", From: "tx@example.org",
			StartTLS: true, TimeoutSec: 30,
		}
	})

	body := `{"smtp":{"enabled":true,"host":"smtp.example.org","port":587,` +
		`"from":"tx@example.org","starttls":true,"timeout_sec":30,` +
		`"password":"typed-pw","password_clear":true}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	// "typed-pw" is neither the stored value nor empty, so this fixture tells
	// all three outcomes apart: kept-stored, took-typed, cleared.
	if got := srv.cfg.Snapshot().Smtp.Password; got != "" {
		t.Errorf("stored password = %q, want the explicit clear to win", got)
	}
}

// S3: clearing when nothing is stored is a no-op, not a fault — the operator
// pressing Remove on an already-empty account gets a save, not an error.
func TestHandlePutConfig_SmtpClearWithNoStoredPassword(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Smtp = types.SmtpConfig{
			Enabled: true, Host: "smtp.example.org", Port: 587,
			From: "tx@example.org", StartTLS: true, TimeoutSec: 30,
		}
	})

	body := `{"smtp":{"enabled":true,"host":"smtp.example.org","port":587,` +
		`"from":"tx@example.org","starttls":true,"timeout_sec":30,"password_clear":true}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := srv.cfg.Snapshot().Smtp.Password; got != "" {
		t.Errorf("stored password = %q, want still empty", got)
	}
}

// S4: password_clear is GET-side meaningless and must never be echoed — it is a
// command, not state. A GET that carried it would invite a client to round-trip
// it straight back and wipe the secret on the next unrelated save.
func TestHandleGetConfig_SmtpNeverEchoesPasswordClear(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Smtp = types.SmtpConfig{
			Enabled: true, Host: "smtp.example.org", Port: 587,
			Password: "stored-pw", From: "tx@example.org",
			StartTLS: true, TimeoutSec: 30,
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)

	// Assert on the RAW body: a Go-side struct read would report false for both
	// "absent" and "present and false", and absent is the rule.
	if strings.Contains(w.Body.String(), "password_clear") {
		t.Errorf("GET body carries password_clear; it is a PUT-only command: %s", w.Body.String())
	}
}

/*
	BLANK PORT / TIMEOUT RESOLVE TO THE DEFAULTS AT SAVE TIME.

	Acceptance criterion (operator, 2026-08-03 — "blank means default"):

	    When I clear the port or the timeout and save, it resolves to 587 / 30
	    right then, and what I see afterwards is what was stored — not a 400
	    telling me to type a number the daemon already knows, and not a 0 that
	    turns into something else at the next restart.

	The mechanism lives in config.Normalize (see internal/config/smtp_defaults_test.go);
	these two rules pin it at the boundary the operator actually meets, because
	Normalize could be correct while the handler failed to run it, or ran it after
	Validate.
*/

// S5: an enabled block with the numbers omitted saves, and stores the defaults.
// Before this rule that PUT was a 400 (validateSmtp rejects port 0).
func TestHandlePutConfig_SmtpBlankPortAndTimeoutResolveToDefaults(t *testing.T) {
	srv := testServer(t)

	body := `{"smtp":{"enabled":true,"host":"smtp.example.org",` +
		`"from":"tx@example.org","starttls":true}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s; want the defaults to be filled in", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Smtp
	if got.Port != 587 {
		t.Errorf("stored port = %d, want the default 587", got.Port)
	}
	if got.TimeoutSec != 30 {
		t.Errorf("stored timeout_sec = %d, want the default 30", got.TimeoutSec)
	}
}

// S6: and the response says so, so the form can show what was actually stored
// rather than the blank the operator left. Without this the operator's box stays
// empty and the surprise is only deferred.
func TestHandlePutConfig_SmtpResponseEchoesResolvedDefaults(t *testing.T) {
	srv := testServer(t)

	body := `{"smtp":{"enabled":true,"host":"smtp.example.org",` +
		`"from":"tx@example.org","starttls":true}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Smtp == nil || resp.Smtp.Port != 587 || resp.Smtp.TimeoutSec != 30 {
		t.Errorf("response should carry the resolved 587/30; got %+v", resp.Smtp)
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
