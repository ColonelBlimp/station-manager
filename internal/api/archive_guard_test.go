package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The archive request guard (ADR 0094 update; W-0021 "alpha.4 hold: fix design",
// revision 3). The fixture is the daemon a stale tab reaches after a switch: it
// serves Contest, while the tab is still bound to Home. Contest has the same
// logbook ID and station callsign as Home, and holds a QSO whose UUID Home also
// holds (`smd import` preserves UUIDs), with contents only Contest's copy has.
// Without the guard every stale request below would succeed against Contest.
const (
	guardServedArchive = "01a1640f-815a-75e4-bae2-b9ef8d86f8c3" // Contest: what this generation serves
	guardStaleArchive  = "01a163a7-0f04-7f69-85e9-255664b58d11" // Home: the tab's stale binding
	guardContestMark   = "contest-copy"
)

const guardFixtureADIF = `<CALL:5>JA8EP<BAND:3>20m<MODE:3>SSB<FREQ:6>14.200<QSO_DATE:8>20261023<TIME_ON:4>1234<TIME_OFF:4>1235<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COMMENT:12>contest-copy<EOR>`

const guardSubmitADIF = `<CALL:5>VK0YY<BAND:3>20m<MODE:3>SSB<FREQ:6>14.210<QSO_DATE:8>20261023<TIME_ON:4>1240<TIME_OFF:4>1241<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<EOR>`

type guardFixture struct {
	srv  *Server
	lbID int64
	uuid string
	smtp *apiSmtpFake
}

func newGuardFixture(t *testing.T) guardFixture {
	t.Helper()
	fake := newAPISmtpFake(t)
	t.Cleanup(fake.close)
	host, port := fake.hostPort()

	srv := testServerWithFt8(t, nil, ft8.NewService(types.Ft8Config{Enabled: true}, &logging.Service{}, ""))
	srv.SetServedArchive(guardServedArchive)
	srv.mailer = email.New(types.SmtpConfig{
		Enabled: true, Host: host, Port: port, From: "f@x", TimeoutSec: 5,
	}, srv.logger)

	lbID := createTestLogbook(t, srv, "Main", "G4ABC")
	srv.qso.SetDestinationRoutes([]forwarding.BoundForwarder{{
		LogbookID: lbID,
		Config: types.ForwarderConfig{
			Name: reloadForwarder, Type: "qrz", Enabled: true, ActionFilter: []string{"insert"},
		},
	}})
	w := submitQso(t, srv, lbID, guardFixtureADIF, false) // direct handler: the fixture write
	if w.Code != http.StatusCreated {
		t.Fatalf("fixture submit: status = %d; body = %s", w.Code, w.Body.String())
	}
	var stored struct {
		UUID string `json:"uuid"`
	}
	if err := unmarshalJSON(w.Body.String(), &stored); err != nil || stored.UUID == "" {
		t.Fatalf("fixture uuid from %s (err=%v)", w.Body.String(), err)
	}
	return guardFixture{srv: srv, lbID: lbID, uuid: stored.UUID, smtp: fake}
}

// guardHeader is how a request presents the archive header: absent, malformed,
// or naming an archive.
type guardHeader struct {
	name  string
	value string
	set   bool
}

var (
	headerAbsent    = guardHeader{name: "absent"}
	headerMalformed = guardHeader{name: "malformed", value: "not-a-uuid", set: true}
	headerStale     = guardHeader{name: "stale", value: guardStaleArchive, set: true}
	headerServed    = guardHeader{name: "served", value: guardServedArchive, set: true}
)

// serveGuarded drives the REAL handler chain (mux and wrappers), as production does.
func serveGuarded(srv *Server, method, path, contentType, body string, h guardHeader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8080" // loopback: passes the Host allowlist that wraps the mux
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if h.set {
		req.Header.Set(archiveIDHeader, h.value)
	}
	// The tab's default is the served one: these tests isolate the archive guard.
	if _, def, err := srv.effectiveDefault(context.Background()); err == nil && def != "" {
		req.Header.Set(defaultLogbookHeader, def)
	}
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	return w
}

type guardedOp struct {
	name        string
	method      string
	path        func(f guardFixture) string
	contentType string
	body        func(f guardFixture) string
}

const ctJSON = "application/json"

func fixed(s string) func(guardFixture) string { return func(guardFixture) string { return s } }

// protectedOps is every operation the guard must refuse for a stale tab. GET of a
// QSO is listed separately: without a header it stays open to plain readers.
func protectedOps() []guardedOp {
	return []guardedOp{
		{"submit", http.MethodPost, func(f guardFixture) string { return fmt.Sprintf("/v1/qso?logbook=%d", f.lbID) }, "application/x-adif", fixed(guardSubmitADIF)},
		{"patch", http.MethodPatch, func(f guardFixture) string { return "/v1/qso/" + f.uuid }, ctJSON, fixed(`{"comment":"home-edit"}`)},
		{"delete", http.MethodDelete, func(f guardFixture) string { return "/v1/qso/" + f.uuid }, "", fixed("")},
		{"export", http.MethodPost, fixed("/v1/session/export"), ctJSON, func(f guardFixture) string { return `{"uuids":["` + f.uuid + `"]}` }},
		{"email", http.MethodPost, fixed("/v1/session/email"), ctJSON, func(f guardFixture) string {
			return `{"to":"manager@example.com","uuids":["` + f.uuid + `"]}`
		}},
		{"ft8 arm on", http.MethodPost, fixed("/v1/ft8/tx/arm"), ctJSON, fixed(`{"armed":true}`)},
		{"ft8 send", http.MethodPost, fixed("/v1/ft8/tx/send"), ctJSON, fixed(`{"message":"CQ G4ABC IO91","offset_hz":1500}`)},
		{"ft8 qso start", http.MethodPost, fixed("/v1/ft8/qso/start"), ctJSON, fixed(`{"their_call":"K1ABC"}`)},
		{"ft8 qso work", http.MethodPost, fixed("/v1/ft8/qso/work"), ctJSON, fixed(`{"their_call":"K1ABC"}`)},
		{"ft8 cq start", http.MethodPost, fixed("/v1/ft8/cq/start"), ctJSON, fixed(`{"offset_hz":1500,"operating_freq_mhz":14.074}`)},
		{"ft8 cq pick", http.MethodPost, fixed("/v1/ft8/cq/pick"), ctJSON, fixed(`{"call":"K1ABC"}`)},
		{"ft8 cq bag", http.MethodPost, fixed("/v1/ft8/cq/bag"), ctJSON, fixed(`{"call":"K1ABC"}`)},
		{"ft8 cq resume", http.MethodPost, fixed("/v1/ft8/cq/resume"), ctJSON, fixed("")},
		{"ft8 qso next", http.MethodPost, fixed("/v1/ft8/qso/next"), ctJSON, fixed("")},
		{"ft8 skip off", http.MethodPost, fixed("/v1/ft8/qso/skip"), ctJSON, fixed(`{"armed":false}`)},
		{"ft8 path", http.MethodPost, fixed("/v1/ft8/qso/path"), ctJSON, fixed(`{"path":"S"}`)},
	}
}

func (op guardedOp) serve(f guardFixture, h guardHeader) *httptest.ResponseRecorder {
	return serveGuarded(f.srv, op.method, op.path(f), op.contentType, op.body(f), h)
}

// wantGuardRefusal pins a refusal BY THE GUARD. archive_mismatch is the guard's own
// code; its 400s share codes with ordinary body validation, so they must also name
// the header — otherwise an unrelated refusal (a body field the handler rejects)
// could pass for one.
func wantGuardRefusal(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	body := w.Body.String()
	byGuard := code == "archive_mismatch" || strings.Contains(body, archiveIDHeader)
	if w.Code != status || !strings.Contains(body, `"code":"`+code+`"`) || !byGuard {
		t.Fatalf("got %d %s; want %d with code %q from the %s guard", w.Code, body, status, code, archiveIDHeader)
	}
}

// assertContestUntouched pins "no trace": the fixture row keeps Contest's contents,
// nothing new was stored or queued, no export backup was written, no email sent
// or stamped.
func assertContestUntouched(t *testing.T, f guardFixture) {
	t.Helper()
	q, err := f.srv.db.FetchQsoByUUIDWithContext(context.Background(), f.uuid)
	if err != nil {
		t.Fatalf("fixture row is gone (a stale DELETE reached Contest): %v", err)
	}
	if q.Comment != guardContestMark {
		t.Fatalf("fixture comment = %q, want %q (a stale PATCH reached Contest)", q.Comment, guardContestMark)
	}
	if q.SmFwrdByEmailStatus == "Y" {
		t.Fatalf("fixture row is stamped as emailed (a stale email reached Contest)")
	}
	if n := storedQsos(t, f.srv, f.lbID); n != 1 {
		t.Fatalf("Contest logbook holds %d QSOs, want 1 (a stale submit stored)", n)
	}
	if n := queuedUploads(t, f.srv); n != 1 {
		t.Fatalf("upload queue holds %d, want the fixture's 1 (a stale submit queued)", n)
	}
	if entries := readArchiveDir(t, f.srv); len(entries) != 0 {
		t.Fatalf("exports/sent-adif holds %d files, want 0 (a stale export or email wrote a backup)", len(entries))
	}
	if body := f.smtp.capturedBody(); body != "" {
		t.Fatalf("the SMTP fake received a message (a stale email was sent)")
	}
}

func TestArchiveGuard_StaleRequestIsRefusedAndLeavesNoTrace(t *testing.T) {
	for _, op := range protectedOps() {
		t.Run(op.name, func(t *testing.T) {
			f := newGuardFixture(t)
			w := op.serve(f, headerStale)
			wantGuardRefusal(t, w, http.StatusConflict, "archive_mismatch")
			assertContestUntouched(t, f)
		})
	}
}

func TestArchiveGuard_StaleGetRevealsNothing(t *testing.T) {
	f := newGuardFixture(t)
	w := serveGuarded(f.srv, http.MethodGet, "/v1/qso/"+f.uuid, "", "", headerStale)
	wantGuardRefusal(t, w, http.StatusConflict, "archive_mismatch")
	if strings.Contains(w.Body.String(), guardContestMark) {
		t.Fatalf("a stale GET returned Contest's row: %s", w.Body.String())
	}
}

func TestArchiveGuard_MissingOrMalformedHeaderIsRefused(t *testing.T) {
	for _, op := range protectedOps() {
		t.Run(op.name+"/absent", func(t *testing.T) {
			f := newGuardFixture(t)
			wantGuardRefusal(t, op.serve(f, headerAbsent), http.StatusBadRequest, "missing_required_field")
			assertContestUntouched(t, f)
		})
		t.Run(op.name+"/malformed", func(t *testing.T) {
			f := newGuardFixture(t)
			wantGuardRefusal(t, op.serve(f, headerMalformed), http.StatusBadRequest, "invalid_field_value")
			assertContestUntouched(t, f)
		})
	}
}

// GET of a QSO: no header stays open (plain readers), but a supplied header is
// enforced — a malformed one too.
func TestArchiveGuard_GetEnforcesOnlyASuppliedHeader(t *testing.T) {
	f := newGuardFixture(t)
	if w := serveGuarded(f.srv, http.MethodGet, "/v1/qso/"+f.uuid, "", "", headerAbsent); w.Code != http.StatusOK {
		t.Fatalf("headerless GET: %d %s; want 200", w.Code, w.Body.String())
	}
	if w := serveGuarded(f.srv, http.MethodGet, "/v1/qso/"+f.uuid, "", "", headerServed); w.Code != http.StatusOK {
		t.Fatalf("matching GET: %d %s; want 200", w.Code, w.Body.String())
	}
	wantGuardRefusal(t, serveGuarded(f.srv, http.MethodGet, "/v1/qso/"+f.uuid, "", "", headerMalformed),
		http.StatusBadRequest, "invalid_field_value")
}

// The served archive's own requests pass, and the submit response names the
// archive that stored the QSO (provenance for the Session list).
func TestArchiveGuard_MatchingHeaderPassesAndSubmitNamesItsArchive(t *testing.T) {
	f := newGuardFixture(t)
	w := serveGuarded(f.srv, http.MethodPost, fmt.Sprintf("/v1/qso?logbook=%d", f.lbID), "application/x-adif", guardSubmitADIF, headerServed)
	if w.Code != http.StatusCreated {
		t.Fatalf("matching submit: %d %s; want 201", w.Code, w.Body.String())
	}
	var resp struct {
		ArchiveID string `json:"archive_id"`
	}
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if resp.ArchiveID != guardServedArchive {
		t.Fatalf("submit response archive_id = %q, want %q", resp.ArchiveID, guardServedArchive)
	}
	p := serveGuarded(f.srv, http.MethodPatch, "/v1/qso/"+f.uuid, ctJSON, `{"comment":"contest-edit"}`, headerServed)
	if p.Code != http.StatusOK {
		t.Fatalf("matching patch: %d %s; want 200", p.Code, p.Body.String())
	}
}

// Stop and reduce controls are exempt from the archive check: in a state where
// each is valid it behaves identically whatever the header, and its existing
// refusals (rung, run, profile) are unchanged. A blanket guard would fail this.
func TestArchiveGuard_StopControlsAreExempt(t *testing.T) {
	exempt := []guardedOp{
		{"ft8 arm off", http.MethodPost, fixed("/v1/ft8/tx/arm"), ctJSON, fixed(`{"armed":false}`)},
		{"ft8 abandon", http.MethodPost, fixed("/v1/ft8/qso/abandon"), ctJSON, fixed("")},
		{"ft8 autowork stop", http.MethodPost, fixed("/v1/ft8/autowork/stop"), ctJSON, fixed("")},
		{"ft8 cq unbag", http.MethodPost, fixed("/v1/ft8/cq/unbag"), ctJSON, fixed(`{"call":"K1ABC"}`)},
		{"ft8 skip on", http.MethodPost, fixed("/v1/ft8/qso/skip"), ctJSON, fixed(`{"armed":true}`)},
		{"ft8 claim", http.MethodPost, fixed("/v1/ft8/claim"), ctJSON, fixed(`{"mode":"ft8"}`)},
	}
	for _, op := range exempt {
		t.Run(op.name, func(t *testing.T) {
			ref := op.serve(newGuardFixture(t), headerAbsent)
			if ref.Code == http.StatusNotFound {
				t.Fatalf("route not served (404): the fixture must register FT8 routes")
			}
			for _, h := range []guardHeader{headerMalformed, headerStale, headerServed} {
				got := op.serve(newGuardFixture(t), h)
				if got.Code != ref.Code || got.Body.String() != ref.Body.String() {
					t.Errorf("header %s: %d %s; want the headerless %d %s",
						h.name, got.Code, got.Body.String(), ref.Code, ref.Body.String())
				}
			}
		})
	}
}

// Two header values are malformed even when one of them matches: the guard must
// not pick whichever value suits it.
func TestArchiveGuard_RepeatedHeaderIsMalformed(t *testing.T) {
	f := newGuardFixture(t)
	req := httptest.NewRequest(http.MethodDelete, "/v1/qso/"+f.uuid, nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Add(archiveIDHeader, guardServedArchive)
	req.Header.Add(archiveIDHeader, guardStaleArchive)
	w := httptest.NewRecorder()
	f.srv.httpServer.Handler.ServeHTTP(w, req)
	wantGuardRefusal(t, w, http.StatusBadRequest, "invalid_field_value")
	assertContestUntouched(t, f)
}

// A daemon that never recorded its served archive can prove no match: every
// supplied header is refused (fail closed), rather than any being accepted.
func TestArchiveGuard_UnsetServedArchiveRefusesEveryHeader(t *testing.T) {
	f := newGuardFixture(t)
	f.srv.SetServedArchive("")
	w := serveGuarded(f.srv, http.MethodDelete, "/v1/qso/"+f.uuid, "", "", headerServed)
	wantGuardRefusal(t, w, http.StatusConflict, "archive_mismatch")
	assertContestUntouched(t, f)
}
