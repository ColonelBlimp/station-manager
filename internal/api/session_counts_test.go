package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Session exports and emails report exactly what they delivered (ADR 0095;
// W-0021 "alpha.4 hold: fix design", revision 3). In B2-10 a four-row Session
// downloaded a one-record file under a success toast: the daemon skipped the
// three it could not find with only a log line. Every response now says what
// was requested (distinct UUIDs), delivered and omitted; all-missing stays
// no_qsos with no file, backup or mail. Covered independently of any archive
// switch: a row can stop resolving for other reasons too.

func submitDistinctQso(t *testing.T, srv *Server, lbID int64, call string) string {
	t.Helper()
	w := submitQso(t, srv, lbID, strings.Replace(testQsoADIF, "M0CMC", call, 1), false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit %s: status = %d; body = %s", call, w.Code, w.Body.String())
	}
	var res qsoservice.SubmitResult
	if err := unmarshalJSON(w.Body.String(), &res); err != nil || res.UUID == "" {
		t.Fatalf("decode submit %s: %v (%s)", call, err, w.Body.String())
	}
	return res.UUID
}

func uuidsBody(uuids ...string) string {
	return `["` + strings.Join(uuids, `","`) + `"]`
}

func wantExportCounts(t *testing.T, w *httptest.ResponseRecorder, requested, exported, omitted string) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	got := [3]string{
		w.Header().Get("X-SM-Requested-Count"),
		w.Header().Get("X-SM-Exported-Count"),
		w.Header().Get("X-SM-Omitted-Count"),
	}
	if got != [3]string{requested, exported, omitted} {
		t.Fatalf("requested/exported/omitted headers = %q, want [%s %s %s]", got, requested, exported, omitted)
	}
	if n := strings.Count(strings.ToUpper(w.Body.String()), "<EOR>"); strconv.Itoa(n) != exported {
		t.Fatalf("file holds %d records, but X-SM-Exported-Count says %s", n, exported)
	}
}

func TestSessionExport_CountsWhatTheFileHolds(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	a := submitDistinctQso(t, srv, lbID, "M0CMA")
	b := submitDistinctQso(t, srv, lbID, "M0CMB")
	missing := utils.NewUUIDv7()

	t.Run("complete", func(t *testing.T) {
		wantExportCounts(t, postExport(t, srv, `{"uuids":`+uuidsBody(a, b)+`}`), "2", "2", "0")
	})
	t.Run("partial: a row that cannot be found is counted, not hidden", func(t *testing.T) {
		wantExportCounts(t, postExport(t, srv, `{"uuids":`+uuidsBody(a, missing, b)+`}`), "3", "2", "1")
	})
	t.Run("duplicates count once", func(t *testing.T) {
		wantExportCounts(t, postExport(t, srv, `{"uuids":`+uuidsBody(a, a, b)+`}`), "2", "2", "0")
	})
	t.Run("a duplicated missing row is one omission", func(t *testing.T) {
		wantExportCounts(t, postExport(t, srv, `{"uuids":`+uuidsBody(a, missing, missing)+`}`), "2", "1", "1")
	})
}

func TestSessionExport_AllMissingWritesNoBackup(t *testing.T) {
	srv := testServer(t)
	w := postExport(t, srv, `{"uuids":`+uuidsBody(utils.NewUUIDv7(), utils.NewUUIDv7())+`}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no_qsos") {
		t.Fatalf("status = %d %s, want 400 no_qsos", w.Code, w.Body.String())
	}
	if w.Header().Get("X-SM-Exported-Count") != "" {
		t.Fatalf("a refusal carries no export counts")
	}
	if entries := readArchiveDir(t, srv); len(entries) != 0 {
		t.Fatalf("exports/sent-adif holds %d files after a refused export, want 0", len(entries))
	}
}

func TestSessionEmail_ReportsSentAndOmitted(t *testing.T) {
	fake := newAPISmtpFake(t)
	defer fake.close()
	host, port := fake.hostPort()
	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", TimeoutSec: 5,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	a := submitDistinctQso(t, srv, lbID, "M0CMA")
	b := submitDistinctQso(t, srv, lbID, "M0CMB")
	missing := utils.NewUUIDv7()

	body := `{"to":"manager@example.com","uuids":` + uuidsBody(missing, a, missing, b) + `}`
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp SessionEmailResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// sent: the attachment's records in request order; omitted: each distinct
	// missing UUID once; emailed (durably stamped) is a subset of sent.
	if strings.Join(resp.Sent, ",") != a+","+b {
		t.Errorf("sent = %v, want [%s %s]", resp.Sent, a, b)
	}
	if strings.Join(resp.Omitted, ",") != missing {
		t.Errorf("omitted = %v, want [%s] once", resp.Omitted, missing)
	}
	if strings.Join(resp.Emailed, ",") != a+","+b {
		t.Errorf("emailed = %v, want both sent rows stamped", resp.Emailed)
	}
	if n := strings.Count(strings.ToUpper(decodeAdifAttachment(t, fake.capturedBody())), "<EOR>"); n != len(resp.Sent) {
		t.Errorf("attachment holds %d records, sent lists %d", n, len(resp.Sent))
	}
}

// A complete send still reports empty (never null) lists, so a client's exact
// partition check has arrays to compare.
func TestSessionEmail_CompleteSendHasEmptyOmitted(t *testing.T) {
	fake := newAPISmtpFake(t)
	defer fake.close()
	host, port := fake.hostPort()
	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", TimeoutSec: 5,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	a := submitDistinctQso(t, srv, lbID, "M0CMA")
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email",
		strings.NewReader(`{"to":"manager@example.com","uuids":`+uuidsBody(a)+`}`))
	w := httptest.NewRecorder()
	srv.handleSessionEmail(w, req)
	if !strings.Contains(w.Body.String(), `"omitted":[]`) || !strings.Contains(w.Body.String(), `"sent":["`+a+`"]`) {
		t.Fatalf("body = %s, want sent [%s] and an empty omitted array", w.Body.String(), a)
	}
}

// The mail leaves, then the durable stamp fails: still a 200 that accounts for
// the delivery exactly — sent and omitted as composed, emailed an EMPTY ARRAY
// (never null, which a client's partition check must reject) and date "". The
// database is closed while the fake SMTP server holds the send open, so the
// compose succeeded and only the stamp can fail.
func TestSessionEmail_StampFailureAfterSendStillAccountsExactly(t *testing.T) {
	fake := newAPISmtpFake(t)
	defer fake.close()
	fake.paused = make(chan struct{}, 1)
	fake.release = make(chan struct{})
	host, port := fake.hostPort()
	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", TimeoutSec: 5,
	})
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	a := submitDistinctQso(t, srv, lbID, "M0CMA")
	missing := utils.NewUUIDv7()

	req := httptest.NewRequest(http.MethodPost, "/v1/session/email",
		strings.NewReader(`{"to":"manager@example.com","uuids":`+uuidsBody(a, missing)+`}`))
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleSessionEmail(w, req)
	}()
	select {
	case <-fake.paused:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the attachment to reach the mailer")
	}
	if err := srv.db.Close(); err != nil {
		t.Fatalf("close the database mid-send: %v", err)
	}
	close(fake.release)
	<-done

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the mail left); body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"emailed":[]`, `"date":""`, `"sent":["` + a + `"]`, `"omitted":["` + missing + `"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %s; missing %s", body, want)
		}
	}
	if decodeAdifAttachment(t, fake.capturedBody()) == "" {
		t.Errorf("no attachment reached the SMTP fake: the send did not complete")
	}
}
