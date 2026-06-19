package api

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
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

	body := `{"to":"a@b","uuids":["x"]}`
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

	body := `{"to":"a@b","uuids":["x"]}`
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

	body := `{"uuids":["x"]}`
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

func TestSessionEmail_MissingUuids_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"to":"a@b"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing_required_field") {
		t.Errorf("body should carry code missing_required_field; got %s", w.Body.String())
	}
}

func TestSessionEmail_ToWithoutAt_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"to":"not-an-email","uuids":["x"]}`
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

// TestSessionEmail_HeaderInjectionRecipient_Returns400 covers review M1: a
// recipient carrying CR/LF (the "a@b\r\nBcc: x@y" header-injection shape) is
// rejected at the API boundary as a 400 — it must never reach the
// compose/archive/send path. The body is a raw string literal so the \r\n stays
// a JSON escape that decodes to actual control characters.
func TestSessionEmail_HeaderInjectionRecipient_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"to":"a@b\r\nBcc: x@y","uuids":["x"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Errorf("body should carry code invalid_field_value; got %s", w.Body.String())
	}
	// No side effect: a rejected recipient must not have written an archive.
	if entries := readArchiveDir(t, srv); len(entries) != 0 {
		t.Errorf("invalid recipient wrote %d archive file(s); want 0", len(entries))
	}
}

// TestSessionEmail_MultipleRecipients_Returns400 covers review M1: a
// comma-separated address list is not a single mailbox and is rejected.
func TestSessionEmail_MultipleRecipients_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"to":"a@b.com, c@d.com","uuids":["x"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Errorf("status = %d body = %s, want 400 invalid_field_value", w.Code, w.Body.String())
	}
}

// TestSessionEmail_DisplayNameRecipient_Sends covers review M1: a display-name
// address ("Name" <addr>) is a valid single mailbox — it's accepted and
// normalized to the bare address, so the full send path succeeds (200).
func TestSessionEmail_DisplayNameRecipient_Sends(t *testing.T) {
	fake := newAPISmtpFake(t)
	defer fake.close()
	host, port := fake.hostPort()

	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", StartTLS: false, TimeoutSec: 5,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	body := `{"to":"\"QSL Manager\" <manager@example.com>","uuids":["` + uuid + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
}

// TestSessionEmail_DuplicateUuids_EmitsOnce covers review L1: a repeated UUID is
// deduplicated, so the QSO appears once in the ADIF attachment and once in the
// emitted list — not twice.
func TestSessionEmail_DuplicateUuids_EmitsOnce(t *testing.T) {
	fake := newAPISmtpFake(t)
	defer fake.close()
	host, port := fake.hostPort()

	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", StartTLS: false, TimeoutSec: 5,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	body := `{"to":"manager@example.com","uuids":["` + uuid + `","` + uuid + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp SessionEmailResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Emailed) != 1 {
		t.Errorf("emailed = %v, want exactly one UUID", resp.Emailed)
	}

	// The archived ADIF (same bytes as the attachment) must carry one record.
	entries := readArchiveDir(t, srv)
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived ADIF, got %d", len(entries))
	}
	saved, err := os.ReadFile(filepath.Join(srv.cfg.WorkingDir(), "exports", "sent-adif", entries[0].Name()))
	if err != nil {
		t.Fatalf("read archived ADIF: %v", err)
	}
	if n := strings.Count(strings.ToUpper(string(saved)), "<EOR>"); n != 1 {
		t.Errorf("archived ADIF has %d records (<EOR>), want 1:\n%s", n, saved)
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

// TestSessionEmail_NoQsosFound_Returns400 covers the case where every
// supplied UUID fails to resolve (e.g. the rows were soft-deleted since
// the SPA snapshotted the session). No mail is sent.
func TestSessionEmail_NoQsosFound_Returns400(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5})

	body := `{"to":"a@b","uuids":["nonexistent-uuid"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no_qsos") {
		t.Errorf("body should carry code no_qsos; got %s", w.Body.String())
	}
}

// ---- transport failure path ----

func TestSessionEmail_SmtpDialFailure_Returns502(t *testing.T) {
	// Point at a closed port — Dial will fail, handler routes the
	// transport error to 502 smtp_failure. A real QSO is logged first
	// so the handler gets past the fetch + ADIF-compose stages and
	// actually attempts the send.
	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled:    true,
		Host:       "127.0.0.1",
		Port:       1, // privileged + nothing listening
		From:       "f@x",
		TimeoutSec: 1,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	body := `{"to":"a@b","uuids":["` + uuid + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "smtp_failure") {
		t.Errorf("body should carry code smtp_failure; got %s", w.Body.String())
	}

	// Save-on-compose contract: the local ADIF archive is written even
	// though the SMTP send failed, so a flaky network never loses the
	// export.
	entries := readArchiveDir(t, srv)
	if len(entries) != 1 {
		t.Errorf("expected 1 archived ADIF despite send failure, got %d", len(entries))
	}
}

// ---- success path: send + durable stamp ----

// TestSessionEmail_Success_SendsAndStamps is the end-to-end pin for the
// feature: a successful send marks each emailed QSO with the durable
// "forwarded by email" ADIF fields, which a subsequent fetch surfaces.
func TestSessionEmail_Success_SendsAndStamps(t *testing.T) {
	fake := newAPISmtpFake(t)
	defer fake.close()
	host, port := fake.hostPort()

	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled:    true,
		Host:       host,
		Port:       port,
		From:       "f@x",
		StartTLS:   false,
		TimeoutSec: 5,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	body := `{"to":"manager@example.com","uuids":["` + uuid + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp SessionEmailResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "sent" {
		t.Errorf("status = %q, want sent", resp.Status)
	}
	if len(resp.Emailed) != 1 || resp.Emailed[0] != uuid {
		t.Errorf("emailed = %v, want [%s]", resp.Emailed, uuid)
	}
	if resp.Date == "" {
		t.Errorf("date should be stamped on success")
	}

	// The QSO row now carries the durable forwarded-by-email stamp.
	stored, err := srv.db.FetchQsoByUUIDWithContext(req.Context(), uuid)
	if err != nil {
		t.Fatalf("refetch qso: %v", err)
	}
	if stored.SmFwrdByEmailStatus != "Y" {
		t.Errorf("SmFwrdByEmailStatus = %q, want Y", stored.SmFwrdByEmailStatus)
	}
	if stored.SmFwrdByEmailDate != resp.Date {
		t.Errorf("SmFwrdByEmailDate = %q, want %q", stored.SmFwrdByEmailDate, resp.Date)
	}
}

// TestSessionEmail_ComposedAdifHasHeaderBeforeRecords pins the fix for
// the "remote callsign missing" report: the emailed ADIF must carry a
// proper header terminated by <EOH>, with the record (and its <CALL>)
// appearing AFTER it. A headerless file that opens directly with
// <CALL...> causes lenient QSL/logger importers to swallow the first
// record as header text — dropping the contacted callsign. Exercises
// the real daemon path: submit → fetch from DB → ComposeToAdifString.
func TestSessionEmail_ComposedAdifHasHeaderBeforeRecords(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	q, err := srv.db.FetchQsoByUUIDWithContext(context.Background(), uuid)
	if err != nil {
		t.Fatalf("refetch qso: %v", err)
	}
	body, err := adif.ComposeToAdifString(types.QsoSlice{q})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	up := strings.ToUpper(body)
	eoh := strings.Index(up, "<EOH>")
	call := strings.Index(up, "<CALL:")
	if eoh < 0 {
		t.Fatalf("composed ADIF has no <EOH> header terminator:\n%s", body)
	}
	if call < 0 {
		t.Fatalf("composed ADIF missing remote callsign CALL:\n%s", body)
	}
	if call < eoh {
		t.Fatalf("CALL at %d precedes <EOH> at %d — importers parse the record as header:\n%s", call, eoh, body)
	}
}

// TestSessionEmail_ArchivesLocalAdifCopy pins the local-archive feature:
// a successful send writes the composed ADIF under
// <workingdir>/exports/sent-adif/, and the saved file carries the
// header + remote callsign (i.e. it's the same well-formed ADIF that
// was emailed).
func TestSessionEmail_ArchivesLocalAdifCopy(t *testing.T) {
	fake := newAPISmtpFake(t)
	defer fake.close()
	host, port := fake.hostPort()

	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", StartTLS: false, TimeoutSec: 5,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	body := `{"to":"manager@example.com","uuids":["` + uuid + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}

	entries := readArchiveDir(t, srv)
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived ADIF, got %d", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".adi") {
		t.Errorf("archived filename %q should end in .adi", entries[0].Name())
	}
	saved, err := os.ReadFile(filepath.Join(srv.cfg.WorkingDir(), "exports", "sent-adif", entries[0].Name()))
	if err != nil {
		t.Fatalf("read archived ADIF: %v", err)
	}
	if !strings.Contains(string(saved), "<CALL:5>M0CMC") {
		t.Errorf("archived ADIF missing remote callsign:\n%s", saved)
	}
	if !strings.Contains(strings.ToUpper(string(saved)), "<EOH>") {
		t.Errorf("archived ADIF missing <EOH> header:\n%s", saved)
	}
}

// ---- helpers ----

// readArchiveDir returns the entries in the session-ADIF archive dir,
// or an empty slice if the dir doesn't exist yet.
func readArchiveDir(t *testing.T, srv *Server) []os.DirEntry {
	t.Helper()
	dir := filepath.Join(srv.cfg.WorkingDir(), "exports", "sent-adif")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read archive dir %s: %v", dir, err)
	}
	return entries
}

// submitTestQsoUUID logs testQsoADIF into the given logbook and returns
// the stored QSO's canonical UUID.
func submitTestQsoUUID(t *testing.T, srv *Server, lbID int64) string {
	t.Helper()
	w := submitQso(t, srv, lbID, testQsoADIF, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit qso: status = %d; body = %s", w.Code, w.Body.String())
	}
	var stored qsoservice.SubmitResult
	if err := unmarshalJSON(w.Body.String(), &stored); err != nil {
		t.Fatalf("decode submit body: %v", err)
	}
	if stored.UUID == "" {
		t.Fatalf("submit response missing uuid; body = %s", w.Body.String())
	}
	return stored.UUID
}

func TestSessionEmailSubject_CallsignPrefix(t *testing.T) {
	now := time.Date(2026, 6, 17, 14, 30, 0, 0, time.UTC)
	// Default subject (none supplied), prefixed with the logbook callsign + colon.
	def := sessionEmailSubject("G4ABC", "", now)
	if !strings.HasPrefix(def, "G4ABC: ") {
		t.Errorf("subject = %q, want it prefixed with 'G4ABC: '", def)
	}
	if !strings.Contains(def, "Station Manager session ADIF") {
		t.Errorf("default subject body missing; got %q", def)
	}
	// Operator-supplied subject is also prefixed with the callsign + colon.
	if got := sessionEmailSubject("G4ABC", "My QSOs", now); got != "G4ABC: My QSOs" {
		t.Errorf("supplied subject = %q, want 'G4ABC: My QSOs'", got)
	}
	// No callsign → no prefix (just the supplied/default subject).
	if got := sessionEmailSubject("", "My QSOs", now); got != "My QSOs" {
		t.Errorf("no-callsign subject = %q, want 'My QSOs'", got)
	}
	// Whitespace-only callsign is treated as absent.
	if got := sessionEmailSubject("  ", "My QSOs", now); got != "My QSOs" {
		t.Errorf("blank-callsign subject = %q, want 'My QSOs'", got)
	}
}

func TestSessionEmailBody_QsoCountAndPluralisation(t *testing.T) {
	now := time.Date(2026, 6, 17, 14, 30, 0, 0, time.UTC)
	one := sessionEmailBody(1, now)
	if !strings.Contains(one, "ADIF for this session attached.") || !strings.Contains(one, "Contains 1 QSO.") {
		t.Errorf("1-QSO body = %q, want the attached line + 'Contains 1 QSO.'", one)
	}
	if strings.Contains(one, "QSOs") {
		t.Errorf("1-QSO body should say 'QSO' (singular), got %q", one)
	}
	many := sessionEmailBody(5, now)
	if !strings.Contains(many, "Contains 5 QSOs.") {
		t.Errorf("5-QSO body = %q, want 'Contains 5 QSOs.'", many)
	}
}

// apiSmtpFake is a minimal SMTP server that accepts any message and
// records nothing beyond the fact that a full DATA exchange completed.
// It mirrors the unexported smtpFake in internal/email (which the api
// package can't import); kept deliberately small — just enough to drive
// the handler's success path. StartTLS is not offered, so the client
// must connect with StartTLS=false.
type apiSmtpFake struct {
	listener net.Listener
	wg       sync.WaitGroup
}

func newAPISmtpFake(t *testing.T) *apiSmtpFake {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &apiSmtpFake{listener: l}
	f.wg.Add(1)
	go f.acceptLoop()
	return f
}

func (f *apiSmtpFake) hostPort() (string, int) {
	host, portStr, _ := net.SplitHostPort(f.listener.Addr().String())
	port, _ := net.LookupPort("tcp", portStr)
	return host, port
}

func (f *apiSmtpFake) close() {
	_ = f.listener.Close()
	f.wg.Wait()
}

func (f *apiSmtpFake) acceptLoop() {
	defer f.wg.Done()
	for {
		c, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			f.handle(c)
		}()
	}
}

func (f *apiSmtpFake) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	flush := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}
	flush("220 fake.localhost ESMTP")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		upper := strings.ToUpper(strings.TrimRight(line, "\r\n"))
		switch {
		case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
			flush("250-fake.localhost")
			flush("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH"):
			flush("235 OK")
		case strings.HasPrefix(upper, "MAIL FROM:"), strings.HasPrefix(upper, "RCPT TO:"):
			flush("250 OK")
		case upper == "DATA":
			flush("354 send body")
			for {
				bl, berr := r.ReadString('\n')
				if berr != nil {
					return
				}
				if bl == ".\r\n" || bl == ".\n" {
					break
				}
			}
			flush("250 OK")
		case upper == "QUIT":
			flush("221 bye")
			return
		case upper == "RSET", upper == "NOOP":
			flush("250 OK")
		default:
			flush("250 OK")
		}
	}
}
