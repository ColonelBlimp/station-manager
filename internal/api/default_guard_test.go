package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0024 slice 4: the stale-tab default guard (ADR 0097 §4). A tab names the
   logbook it treats as the default; the daemon checks it against its effective
   default before any side effect, beside the archive guard.

   The fixture is the archive guard's (Contest served, one QSO in it) with a
   second logbook: A is the effective default, B is another logbook. A tab still
   showing B as the default is the stale tab.

     DG1  every live route refuses a missing, malformed or repeated header
          (400, naming the header), leaving no trace.
     DG2  FT8 live routes refuse a stale default (409 default_logbook_mismatch).
     DG3  POST /v1/qso naming the believed default (B) while A is effective is
          refused, and stores nothing in A or B: never retargeted.
     DG4  POST /v1/qso naming another logbook than the believed default is a
          deliberate operation, logged where it was named.
     DG5  the matching default passes (any letter case); a live submit logs to A.
     DG6  exempt routes behave identically whatever the default header: stop and
          reduce controls, the claim, and qso/path (archive guard only).
     DG7  order: the archive guard decides first; a default refusal does not
          spend a submit-rate token.
     DG8  an effective default that names no logbook proves no match: every
          supplied header is refused on FT8.
     DG9  GET /v1/version reports the effective default's UUID beside the
          archive, for a reconnecting tab to compare.
     DG10 with live entry sealed (a change pending), submits are classified by
          the tab's assertion, not by the effective default: a stale tab naming
          A and any tab naming a third logbook are deliberate and logged; a
          matching live submit is refused.
     DG11 FT8 refusal precedence: with a matching default each route reaches its
          own FT8 service answer (offline: the admission seal, the route's own
          refusal, or skip-off's 202); with a stale default the same fixture
          answers the guard's refusal. This proves the guard's answer wins; it
          cannot exclude "ask the service, then replace its answer" — DG13 does,
          for arming and the starts.
     DG13 FT8 non-admission: a recording keyer (never ready, keying fails the
          test) is consulted by arming and by every start; a matching default
          reaches it, a missing or stale one leaves its call count at zero.
     DG12 a datastore failure while resolving the effective default is a 503
          db_unavailable, never reported as a proven mismatch.
*/

const defaultHeaderName = "X-SM-Default-Logbook"

type defaultFixture struct {
	guardFixture
	lbB   int64
	uuidA string
	uuidB string
}

func newDefaultFixture(t *testing.T) defaultFixture {
	t.Helper()
	f := newGuardFixture(t)
	if f.lbID != f.srv.cfg.Snapshot().DefaultLogbookID {
		t.Fatalf("fixture: logbook %d is not the default %d", f.lbID, f.srv.cfg.Snapshot().DefaultLogbookID)
	}
	lbB := createTestLogbook(t, f.srv, "Other", "G4ABC")
	return defaultFixture{guardFixture: f, lbB: lbB, uuidA: logbookUUID(t, f.srv, f.lbID), uuidB: logbookUUID(t, f.srv, lbB)}
}

func logbookUUID(t *testing.T, srv *Server, id int64) string {
	t.Helper()
	lb, err := srv.db.FetchLogbookByIDWithContext(context.Background(), id)
	if err != nil || lb.UUID == "" {
		t.Fatalf("logbook %d uuid: %q (err=%v)", id, lb.UUID, err)
	}
	return lb.UUID
}

// defaultHeader is how a request presents X-SM-Default-Logbook: no values
// (absent), one, or several.
type defaultHeader struct {
	name   string
	values []string
}

func (f defaultFixture) headers() (absent, malformed, repeated, stale, current defaultHeader) {
	return defaultHeader{name: "absent"},
		defaultHeader{name: "malformed", values: []string{"not-a-uuid"}},
		defaultHeader{name: "repeated", values: []string{f.uuidA, f.uuidA}},
		defaultHeader{name: "stale", values: []string{f.uuidB}},
		defaultHeader{name: "current", values: []string{f.uuidA}}
}

// serveDefault drives the real handler chain with the served archive and the
// given default header.
func serveDefault(f defaultFixture, op guardedOp, archive guardHeader, d defaultHeader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(op.method, op.path(f.guardFixture), strings.NewReader(op.body(f.guardFixture)))
	req.Host = "127.0.0.1:8080"
	if op.contentType != "" {
		req.Header.Set("Content-Type", op.contentType)
	}
	if archive.set {
		req.Header.Set(archiveIDHeader, archive.value)
	}
	for _, v := range d.values {
		req.Header.Add(defaultHeaderName, v)
	}
	w := httptest.NewRecorder()
	f.srv.httpServer.Handler.ServeHTTP(w, req)
	return w
}

// liveFt8Ops is every FT8 route that admits a new session or transmission
// (operator ruling 2026-10-29): the three starts, send, arming, pick, bag,
// resume, next and disabling skip.
func liveFt8Ops() []guardedOp {
	return []guardedOp{
		{"ft8 arm on", http.MethodPost, fixed("/v1/ft8/tx/arm"), ctJSON, fixed(`{"armed":true}`)},
		{"ft8 send", http.MethodPost, fixed("/v1/ft8/tx/send"), ctJSON, fixed(`{"message":"CQ G4ABC IO91","offset_hz":1500}`)},
		{"ft8 qso start", http.MethodPost, fixed("/v1/ft8/qso/start"), ctJSON,
			fixed(`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`)},
		{"ft8 qso work", http.MethodPost, fixed("/v1/ft8/qso/work"), ctJSON,
			fixed(`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-5,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`)},
		{"ft8 cq start", http.MethodPost, fixed("/v1/ft8/cq/start"), ctJSON, fixed(`{"offset_hz":1500,"operating_freq_mhz":14.074}`)},
		{"ft8 cq pick", http.MethodPost, fixed("/v1/ft8/cq/pick"), ctJSON, fixed(`{"call":"K1ABC"}`)},
		{"ft8 cq bag", http.MethodPost, fixed("/v1/ft8/cq/bag"), ctJSON, fixed(`{"call":"K1ABC"}`)},
		{"ft8 cq resume", http.MethodPost, fixed("/v1/ft8/cq/resume"), ctJSON, fixed(`{}`)},
		{"ft8 qso next", http.MethodPost, fixed("/v1/ft8/qso/next"), ctJSON, fixed(`{}`)},
		{"ft8 skip off", http.MethodPost, fixed("/v1/ft8/qso/skip"), ctJSON, fixed(`{"armed":false}`)},
	}
}

// serviceAnswer is the answer each live FT8 route gives once it reaches the FT8
// service in the offline fixture. sealed routes reach the admission seal
// (SealTxAdmission, as an archive switch takes it): their answer proves the
// service's admission was asked. The others need a run or a session, so their
// own refusal (or skip-off's acceptance) is the service's answer.
type serviceAnswer struct {
	sealed bool
	status int
	code   string // "" = no body (202)
	op     string
}

var ft8ServiceAnswers = map[string]serviceAnswer{
	"ft8 arm on":    {true, http.StatusConflict, "archive_switch_pending", "api.handleFt8TxArm"},
	"ft8 send":      {true, http.StatusConflict, "archive_switch_pending", "api.handleFt8TxSend"},
	"ft8 qso start": {true, http.StatusConflict, "archive_switch_pending", "api.handleFt8QsoStart"},
	"ft8 qso work":  {true, http.StatusConflict, "archive_switch_pending", "api.handleFt8QsoWork"},
	"ft8 cq start":  {true, http.StatusConflict, "archive_switch_pending", "api.handleFt8CqStart"},
	"ft8 cq pick":   {false, http.StatusConflict, "ft8_no_cq_pick_run", "api.handleFt8CqPick"},
	"ft8 cq bag":    {false, http.StatusConflict, "ft8_no_cq_pick_run", "api.handleFt8CqBag"},
	"ft8 cq resume": {false, http.StatusConflict, "ft8_no_cq_pick_run", "api.handleFt8CqResume"},
	"ft8 qso next":  {false, http.StatusConflict, "ft8_no_answerer", "api.handleFt8QsoNext"},
	"ft8 skip off":  {false, http.StatusAccepted, "", ""},
}

func wantServiceAnswer(t *testing.T, w *httptest.ResponseRecorder, a serviceAnswer) {
	t.Helper()
	if w.Code != a.status {
		t.Fatalf("got %d %s; want the FT8 service's %d %q", w.Code, w.Body.String(), a.status, a.code)
	}
	if a.code == "" {
		if w.Body.Len() != 0 {
			t.Fatalf("got body %s; want the service's empty 202", w.Body.String())
		}
		return
	}
	var e struct{ Code, Op string }
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e.Code != a.code || e.Op != a.op {
		t.Fatalf("got %s; want code %q from %s (the FT8 service's answer)", w.Body.String(), a.code, a.op)
	}
}

// liveSubmit is POST /v1/qso naming logbook id.
func liveSubmit(name string, id int64) guardedOp {
	return guardedOp{name, http.MethodPost, fixed(fmt.Sprintf("/v1/qso?logbook=%d", id)), "application/x-adif", fixed(guardSubmitADIF)}
}

// wantDefaultRefusal pins a refusal BY THE DEFAULT GUARD: its 400s share codes
// with the archive guard and body validation, so they must name the header.
func wantDefaultRefusal(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	body := w.Body.String()
	byGuard := code == "default_logbook_mismatch" || strings.Contains(body, defaultHeaderName)
	if w.Code != status || !strings.Contains(body, `"code":"`+code+`"`) || !byGuard {
		t.Fatalf("got %d %s; want %d with code %q from the %s guard", w.Code, body, status, code, defaultHeaderName)
	}
}

// assertNothingLogged pins that neither logbook gained a QSO and nothing was
// queued beyond the fixture's own row.
func assertNothingLogged(t *testing.T, f defaultFixture) {
	t.Helper()
	assertContestUntouched(t, f.guardFixture)
	if n := storedQsos(t, f.srv, f.lbB); n != 0 {
		t.Fatalf("logbook B holds %d QSOs, want 0", n)
	}
}

func TestDefaultGuard_LiveRoutesRefuseAMissingOrMalformedHeader(t *testing.T) {
	probe := newDefaultFixture(t)
	ops := append(liveFt8Ops(), liveSubmit("submit to A", probe.lbID), liveSubmit("submit to B", probe.lbB))
	for _, op := range ops {
		for _, tc := range []struct {
			pick func(defaultFixture) defaultHeader
			code string
		}{
			{func(f defaultFixture) defaultHeader { h, _, _, _, _ := f.headers(); return h }, "missing_required_field"},
			{func(f defaultFixture) defaultHeader { _, h, _, _, _ := f.headers(); return h }, "invalid_field_value"},
			{func(f defaultFixture) defaultHeader { _, _, h, _, _ := f.headers(); return h }, "invalid_field_value"},
		} {
			f := newDefaultFixture(t)
			h := tc.pick(f)
			t.Run(op.name+"/"+h.name, func(t *testing.T) {
				wantDefaultRefusal(t, serveDefault(f, op, headerServed, h), http.StatusBadRequest, tc.code)
				assertNothingLogged(t, f)
			})
		}
	}
}

func TestDefaultGuard_Ft8RefusesAStaleDefault(t *testing.T) {
	for _, op := range liveFt8Ops() {
		t.Run(op.name, func(t *testing.T) {
			f := newDefaultFixture(t)
			_, _, _, stale, _ := f.headers()
			wantDefaultRefusal(t, serveDefault(f, op, headerServed, stale), http.StatusConflict, "default_logbook_mismatch")
			assertNothingLogged(t, f)
		})
	}
}

// The stale tab believes B is the default and submits there as a live entry;
// A is effective. Refused, and never redirected to A.
func TestDefaultGuard_StaleLiveSubmitIsRefusedAndNeverRetargeted(t *testing.T) {
	f := newDefaultFixture(t)
	_, _, _, stale, _ := f.headers()
	wantDefaultRefusal(t, serveDefault(f, liveSubmit("submit to B", f.lbB), headerServed, stale),
		http.StatusConflict, "default_logbook_mismatch")
	assertNothingLogged(t, f)
}

// A submit naming a logbook other than the believed default is an explicit
// operation on that logbook: it logs there, whatever the tab believes.
func TestDefaultGuard_DeliberateSubmitToAnotherLogbookIsLogged(t *testing.T) {
	t.Run("current tab names B", func(t *testing.T) {
		f := newDefaultFixture(t)
		_, _, _, _, current := f.headers()
		w := serveDefault(f, liveSubmit("submit to B", f.lbB), headerServed, current)
		if w.Code != http.StatusCreated {
			t.Fatalf("deliberate submit to B: %d %s; want 201", w.Code, w.Body.String())
		}
		if n := storedQsos(t, f.srv, f.lbB); n != 1 {
			t.Fatalf("logbook B holds %d QSOs, want 1", n)
		}
		if n := storedQsos(t, f.srv, f.lbID); n != 1 {
			t.Fatalf("logbook A holds %d QSOs, want only the fixture's 1", n)
		}
	})
	t.Run("stale tab names A", func(t *testing.T) {
		f := newDefaultFixture(t)
		_, _, _, stale, _ := f.headers()
		w := serveDefault(f, liveSubmit("submit to A", f.lbID), headerServed, stale)
		if w.Code != http.StatusCreated {
			t.Fatalf("deliberate submit to A from a tab believing B: %d %s; want 201", w.Code, w.Body.String())
		}
		if n := storedQsos(t, f.srv, f.lbID); n != 2 {
			t.Fatalf("logbook A holds %d QSOs, want 2", n)
		}
	})
}

func TestDefaultGuard_MatchingDefaultPasses(t *testing.T) {
	t.Run("live submit logs to A", func(t *testing.T) {
		f := newDefaultFixture(t)
		_, _, _, _, current := f.headers()
		w := serveDefault(f, liveSubmit("submit to A", f.lbID), headerServed, current)
		if w.Code != http.StatusCreated {
			t.Fatalf("live submit: %d %s; want 201", w.Code, w.Body.String())
		}
		if n := storedQsos(t, f.srv, f.lbID); n != 2 {
			t.Fatalf("logbook A holds %d QSOs, want 2", n)
		}
	})
	t.Run("letter case", func(t *testing.T) {
		f := newDefaultFixture(t)
		upper := defaultHeader{name: "upper", values: []string{strings.ToUpper(f.uuidA)}}
		w := serveDefault(f, liveSubmit("submit to A", f.lbID), headerServed, upper)
		if w.Code != http.StatusCreated {
			t.Fatalf("upper-case default: %d %s; want 201", w.Code, w.Body.String())
		}
	})
}

// DG11. The matching default reaches each route's own FT8 service answer; the
// stale default, in the same fixture, is refused by the guard instead — so a
// guard whose answer came after the service's refusal would show the service's
// code and fail. Precedence only: DG13 proves the service is never asked.
func TestDefaultGuard_Ft8GuardRunsBeforeTheService(t *testing.T) {
	for _, op := range liveFt8Ops() {
		a, ok := ft8ServiceAnswers[op.name]
		if !ok {
			t.Fatalf("no service answer recorded for %s", op.name)
		}
		fixture := func(t *testing.T) defaultFixture {
			f := newDefaultFixture(t)
			if a.sealed {
				if err := f.srv.ft8.SealTxAdmission(); err != nil {
					t.Fatalf("seal: %v", err)
				}
			}
			return f
		}
		t.Run(op.name+"/current reaches the service", func(t *testing.T) {
			f := fixture(t)
			_, _, _, _, current := f.headers()
			wantServiceAnswer(t, serveDefault(f, op, headerServed, current), a)
		})
		t.Run(op.name+"/stale never reaches it", func(t *testing.T) {
			f := fixture(t)
			_, _, _, stale, _ := f.headers()
			wantDefaultRefusal(t, serveDefault(f, op, headerServed, stale), http.StatusConflict, "default_logbook_mismatch")
		})
	}
}

func TestDefaultGuard_ExemptRoutesIgnoreTheHeader(t *testing.T) {
	exempt := []guardedOp{
		{"ft8 arm off", http.MethodPost, fixed("/v1/ft8/tx/arm"), ctJSON, fixed(`{"armed":false}`)},
		{"ft8 abandon", http.MethodPost, fixed("/v1/ft8/qso/abandon"), ctJSON, fixed("")},
		{"ft8 autowork stop", http.MethodPost, fixed("/v1/ft8/autowork/stop"), ctJSON, fixed("")},
		{"ft8 cq unbag", http.MethodPost, fixed("/v1/ft8/cq/unbag"), ctJSON, fixed(`{"call":"K1ABC"}`)},
		{"ft8 skip on", http.MethodPost, fixed("/v1/ft8/qso/skip"), ctJSON, fixed(`{"armed":true}`)},
		{"ft8 claim", http.MethodPost, fixed("/v1/ft8/claim"), ctJSON, fixed(`{"mode":"ft8"}`)},
		{"ft8 path", http.MethodPost, fixed("/v1/ft8/qso/path"), ctJSON, fixed(`{"path":"S"}`)},
	}
	for _, op := range exempt {
		t.Run(op.name, func(t *testing.T) {
			probe := newDefaultFixture(t)
			absent, _, _, _, _ := probe.headers()
			ref := serveDefault(probe, op, headerServed, absent)
			if ref.Code == http.StatusNotFound {
				t.Fatalf("route not served (404): the fixture must register FT8 routes")
			}
			for i := range 4 {
				f := newDefaultFixture(t)
				_, malformed, repeated, stale, current := f.headers()
				h := []defaultHeader{malformed, repeated, stale, current}[i]
				got := serveDefault(f, op, headerServed, h)
				if got.Code != ref.Code || got.Body.String() != ref.Body.String() {
					t.Errorf("default header %s: %d %s; want the headerless %d %s",
						h.name, got.Code, got.Body.String(), ref.Code, ref.Body.String())
				}
			}
		})
	}
}

func TestDefaultGuard_ArchiveGuardDecidesFirst(t *testing.T) {
	f := newDefaultFixture(t)
	absent, _, _, _, _ := f.headers()
	for _, op := range append(liveFt8Ops(), liveSubmit("submit to A", f.lbID)) {
		t.Run(op.name, func(t *testing.T) {
			wantGuardRefusal(t, serveDefault(f, op, headerStale, absent), http.StatusConflict, "archive_mismatch")
		})
	}
}

// A refused default spends no submit-rate token. The burst is two — one per
// refusal — so a guard placed inside the limiter still gives both refusals its
// own answers, and only the final, correct submit shows the spent tokens.
func TestDefaultGuard_RefusalSpendsNoSubmitToken(t *testing.T) {
	f := newDefaultFixture(t)
	f.srv.limits = newLoadLimiter(128, 16, 0, 2)
	absent, _, _, stale, current := f.headers()
	wantDefaultRefusal(t, serveDefault(f, liveSubmit("submit to A", f.lbID), headerServed, absent),
		http.StatusBadRequest, "missing_required_field")
	wantDefaultRefusal(t, serveDefault(f, liveSubmit("submit to B", f.lbB), headerServed, stale),
		http.StatusConflict, "default_logbook_mismatch")
	if w := serveDefault(f, liveSubmit("submit to A", f.lbID), headerServed, current); w.Code != http.StatusCreated {
		t.Fatalf("correct submit after two guard refusals: %d %s; want 201 (a refusal spent the token)", w.Code, w.Body.String())
	}
}

// An effective default that names no logbook can be matched by no header.
func TestDefaultGuard_UnresolvableDefaultRefusesEveryHeader(t *testing.T) {
	for _, op := range liveFt8Ops() {
		t.Run(op.name, func(t *testing.T) {
			f := newDefaultFixture(t)
			if _, err := f.srv.cfg.Update(func(c *config.Config) error {
				c.DefaultLogbookID = 999
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			_, _, _, _, current := f.headers()
			wantDefaultRefusal(t, serveDefault(f, op, headerServed, current), http.StatusConflict, "default_logbook_mismatch")
		})
	}
}

func TestVersion_ReportsTheEffectiveDefaultLogbook(t *testing.T) {
	const id = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.QsoArchives = []types.QsoArchiveConfig{{ID: id, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/data/db/station-manager.db"}}
		cfg.ActiveQsoArchiveID = id
	})
	lbID := createTestLogbook(t, srv, "Main", "G4ABC")
	if lbID != srv.cfg.Snapshot().DefaultLogbookID {
		t.Fatalf("fixture: logbook %d is not the default", lbID)
	}
	want := logbookUUID(t, srv, lbID)
	w := httptest.NewRecorder()
	srv.handleVersion(w, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
	var resp struct {
		Archive *struct {
			ID                 string `json:"id"`
			DefaultLogbookUUID string `json:"default_logbook_uuid"`
		} `json:"archive"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Archive == nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	if resp.Archive.DefaultLogbookUUID != want {
		t.Fatalf("archive.default_logbook_uuid = %q, want %q (body %s)", resp.Archive.DefaultLogbookUUID, want, w.Body.String())
	}
}

// DG10. A pending change seals live entry. The seal admits by the tab's own
// classification: live is "the target is the logbook this tab asserts".
// Classifying by the effective default instead would refuse the stale tab's
// deliberate submit to A.
func TestDefaultGuard_SealedEntryKeepsDeliberateSubmitsDeliberate(t *testing.T) {
	sealed := func(t *testing.T) (defaultFixture, int64) {
		f := newDefaultFixture(t)
		lbC := createTestLogbook(t, f.srv, "Third", "G4ABC")
		gate := NewLiveEntryGate()
		f.srv.SetLiveEntryGate(gate)
		if err := gate.Seal(); err != nil {
			t.Fatal(err)
		}
		return f, lbC
	}
	t.Run("stale tab names the effective default A: deliberate", func(t *testing.T) {
		f, _ := sealed(t)
		_, _, _, stale, _ := f.headers()
		w := serveDefault(f, liveSubmit("submit to A", f.lbID), headerServed, stale)
		if w.Code != http.StatusCreated {
			t.Fatalf("got %d %s; want 201: naming A while asserting B is deliberate", w.Code, w.Body.String())
		}
		if n := storedQsos(t, f.srv, f.lbID); n != 2 {
			t.Fatalf("A holds %d; want 2", n)
		}
	})
	t.Run("stale tab names a third logbook: deliberate", func(t *testing.T) {
		f, lbC := sealed(t)
		_, _, _, stale, _ := f.headers()
		w := serveDefault(f, liveSubmit("submit to C", lbC), headerServed, stale)
		if w.Code != http.StatusCreated || storedQsos(t, f.srv, lbC) != 1 {
			t.Fatalf("got %d %s; want 201 stored in C", w.Code, w.Body.String())
		}
	})
	t.Run("current tab names a third logbook: deliberate", func(t *testing.T) {
		f, lbC := sealed(t)
		_, _, _, _, current := f.headers()
		w := serveDefault(f, liveSubmit("submit to C", lbC), headerServed, current)
		if w.Code != http.StatusCreated || storedQsos(t, f.srv, lbC) != 1 {
			t.Fatalf("got %d %s; want 201 stored in C", w.Code, w.Body.String())
		}
	})
	t.Run("matching live submit is refused", func(t *testing.T) {
		f, lbC := sealed(t)
		_, _, _, _, current := f.headers()
		w := serveDefault(f, liveSubmit("submit to A", f.lbID), headerServed, current)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"default_change_pending"`) {
			t.Fatalf("got %d %s; want 409 default_change_pending", w.Code, w.Body.String())
		}
		assertNothingLogged(t, f)
		if n := storedQsos(t, f.srv, lbC); n != 0 {
			t.Fatalf("C holds %d; want 0", n)
		}
	})
}

// DG12. The effective default cannot be read: that is the datastore's failure,
// a 503 db_unavailable, not a proof that the tab is stale.
func TestDefaultGuard_DatastoreFailureIsUnavailableNotAMismatch(t *testing.T) {
	ops := append(liveFt8Ops(), guardedOp{"submit to B", http.MethodPost, nil, "application/x-adif", fixed(guardSubmitADIF)})
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			f := newDefaultFixture(t)
			if op.path == nil {
				op.path = fixed(fmt.Sprintf("/v1/qso?logbook=%d", f.lbB))
			}
			_, _, _, _, current := f.headers()
			if err := f.srv.db.Close(); err != nil {
				t.Fatal(err)
			}
			w := serveDefault(f, op, headerServed, current)
			if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"db_unavailable"`) {
				t.Fatalf("got %d %s; want 503 db_unavailable", w.Code, w.Body.String())
			}
		})
	}
}

// recordingKeyer is the FT8 keyer seam (ft8.Service.SetTxKeyer) as an observer.
// Arming and every session start consult TxReady before anything can key, so
// its count shows whether the request reached FT8 admission. It is never
// ready, and any keying attempt fails the test.
type recordingKeyer struct {
	t     *testing.T
	ready atomic.Int32
}

func (k *recordingKeyer) TxReady() bool { k.ready.Add(1); return false }

func (k *recordingKeyer) KeyTx(context.Context, string) error {
	k.t.Errorf("KeyTx called: nothing may key in this test")
	return fmt.Errorf("keying refused by the test")
}

func (k *recordingKeyer) UnkeyTx(context.Context) error { return nil }

// DG13. A refused default never reaches FT8 admission: not answered first and
// replaced, not asked at all.
func TestDefaultGuard_RefusedFt8RequestsNeverReachAdmission(t *testing.T) {
	admitting := map[string]bool{"ft8 arm on": true, "ft8 qso start": true, "ft8 qso work": true, "ft8 cq start": true}
	for _, op := range liveFt8Ops() {
		if !admitting[op.name] {
			continue
		}
		for _, pick := range []string{"current", "absent", "stale"} {
			t.Run(op.name+"/"+pick, func(t *testing.T) {
				f := newDefaultFixture(t)
				k := &recordingKeyer{t: t}
				f.srv.ft8.SetTxKeyer(k)
				absent, _, _, stale, current := f.headers()
				h := map[string]defaultHeader{"current": current, "absent": absent, "stale": stale}[pick]
				serveDefault(f, op, headerServed, h)
				got := k.ready.Load()
				if pick == "current" && got == 0 {
					t.Fatalf("the matching default never reached FT8 admission (keyer consulted 0 times): the fixture proves nothing")
				}
				if pick != "current" && got != 0 {
					t.Fatalf("the %s default reached FT8 admission (keyer consulted %d times); the guard must refuse before the service is asked", pick, got)
				}
			})
		}
	}
}

// A submit naming no existing logbook is deliberate (it names nothing the tab
// asserts): with the live-entry gate wired, as in production, the handler's own
// 404 logbook_not_found answers — never an unclassified-request failure.
func TestDefaultGuard_SubmitToAMissingLogbookIsNotFound(t *testing.T) {
	f := newDefaultFixture(t)
	f.srv.SetLiveEntryGate(NewLiveEntryGate())
	_, _, _, _, current := f.headers()
	w := serveDefault(f, liveSubmit("submit to 999", 999), headerServed, current)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":"logbook_not_found"`) {
		t.Fatalf("got %d %s; want 404 logbook_not_found", w.Code, w.Body.String())
	}
	assertNothingLogged(t, f)
}
