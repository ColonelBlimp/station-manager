package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/api"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0024 slice 3b (ADR 0097 §2–§4) through the running daemon: the HTTP
   routes, the live-entry gate on POST /v1/qso, and the real archive file.
   A raw BEGIN IMMEDIATE on that file is the barrier that holds a real write
   (a Phone / CW submit, a logbook deletion) once it is admitted.

     RG   the release gate: in a shipped build (api.DefaultChangeRoutesReleased
          at its shipped value) the production router serves none of the
          /v1/default-logbook routes.
     AD1  an admitted live submit to A, held at its write, finishes on A
          before the change is persisted; the change waits for it. Later live
          submits are refused and write nothing; a submit naming another
          logbook is deliberate and is logged; a completed FT8 contact keeps
          the logbook pinned at its start.
     AD2  coordination over HTTP, both orderings: a change held at its drain
          refuses a deletion and an activation; a deletion held at its write
          refuses a change. Afterwards the pending change still protects B.
     AD3  a lost response: the status finds the change by its id; the next
          start applies it and the status says so.
*/

// releaseRoutes registers the default-change routes for this test's
// generations, as a build with W-0024 slices 4 and 5 would.
func releaseRoutes(t *testing.T) {
	t.Helper()
	prev := api.DefaultChangeRoutesReleased
	api.DefaultChangeRoutesReleased = true
	t.Cleanup(func() { api.DefaultChangeRoutesReleased = prev })
}

// withRestartPort wires the attended restart (SM_SELF_RESTART=1); the trigger
// only closes the generation's restart channel in these tests.
func withRestartPort(t *testing.T) {
	t.Helper()
	t.Setenv("SM_SELF_RESTART", "1")
	t.Cleanup(func() { restartRequested.Store(false) })
}

const submitADIF = `<CALL:5>DL9UW<BAND:3>20m<MODE:3>SSB<FREQ:6>14.210<QSO_DATE:8>20261029<TIME_ON:4>%s<TIME_OFF:4>%s<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:6>7Q5MLV<EOR>`

func (f *changeFixture) submit(t *testing.T, d *daemon, logbook int64, timeOn string) (int, []byte) {
	t.Helper()
	body := strings.ReplaceAll(submitADIF, "%s", timeOn)
	return apiCallHeaders(t, d, http.MethodPost, "/v1/qso?logbook="+itoa(logbook), body,
		map[string]string{"Content-Type": "application/x-adif", "X-SM-Archive-Id": f.archive})
}

func (f *changeFixture) ask(t *testing.T, d *daemon, target int64) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(types.DefaultChangeRequest{LogbookUUID: f.uuid[target], BelievedDefaultUUID: f.uuid[f.a]})
	return apiCallHeaders(t, d, http.MethodPost, "/v1/default-logbook", string(body), map[string]string{"X-SM-Archive-Id": f.archive})
}

func (f *changeFixture) qsosIn(t *testing.T, logbook int64) int {
	t.Helper()
	var n int
	if err := f.rawDB(t).QueryRow(`SELECT COUNT(*) FROM qso WHERE logbook_id = ? AND deleted_at IS NULL`, logbook).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// holdWrites takes the archive's write lock on a separate connection until
// the returned release is called: any admitted write then waits at the lock.
func (f *changeFixture) holdWrites(t *testing.T) (release func()) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+f.d.paths.QSO)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	done := false
	release = func() {
		if done {
			return
		}
		done = true
		_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		_ = conn.Close()
		_ = db.Close()
	}
	t.Cleanup(release)
	return release
}

type httpResult struct {
	code int
	body []byte
}

func async(fn func() (int, []byte)) chan httpResult {
	ch := make(chan httpResult, 1)
	go func() { c, b := fn(); ch <- httpResult{c, b} }()
	return ch
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stillWaiting: ch has not answered within a short settle — the negative half
// of a barrier, which a sleep can only bound.
func stillWaiting(t *testing.T, what string, ch chan httpResult) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("%s answered %d %s while it should still be waiting", what, r.code, r.body)
	case <-time.After(300 * time.Millisecond):
	}
}

func answered(t *testing.T, what string, ch chan httpResult) httpResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(15 * time.Second):
		t.Fatalf("%s never answered", what)
	}
	return httpResult{}
}

// ---- RG ----------------------------------------------------------------------

func TestDefaultChange_RG_UnreleasedInAShippedBuild(t *testing.T) {
	if api.DefaultChangeRoutesReleased {
		t.Fatal("api.DefaultChangeRoutesReleased is true: the shipped build must keep the routes unregistered until W-0024 slices 4 and 5")
	}
	withRestartPort(t)
	f := newChangeFixture(t)
	d := f.mustStart(t)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/default-logbook", ""},
		{http.MethodPost, "/v1/default-logbook", `{"logbook_uuid":"` + f.uuid[f.b] + `","believed_default_uuid":"` + f.uuid[f.a] + `"}`},
		{http.MethodPost, "/v1/default-logbook/cancel", `{"request_id":"x"}`},
		{http.MethodPost, "/v1/default-logbook/confirm", `{"request_id":"x"}`},
		{http.MethodPost, "/v1/default-logbook/retry-cleanup", `{"request_id":"x"}`},
	} {
		code, body := apiCallHeaders(t, d, route.method, route.path, route.body, map[string]string{"X-SM-Archive-Id": f.archive})
		if code != http.StatusNotFound {
			t.Fatalf("%s %s = %d %s; want 404 in a shipped build", route.method, route.path, code, body)
		}
	}
	if d.cfgSvc.Snapshot().DefaultLogbookChange != nil {
		t.Fatal("a record was written")
	}
}

// ---- AD1 ---------------------------------------------------------------------

// within answers ch, or fails: a call that should be refused at once must not
// wait behind the operation in flight.
func within(t *testing.T, what string, ch chan httpResult) httpResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatalf("%s waited instead of being refused at once", what)
	}
	return httpResult{}
}

// draining holds a live submit to A at its write, starts a change to B, and
// returns once the change owns the single flight and has closed admission
// while that submit is still in flight.
func (f *changeFixture) draining(t *testing.T, d *daemon) (held, change chan httpResult, release func()) {
	t.Helper()
	release = f.holdWrites(t)
	held = async(func() (int, []byte) { return f.submit(t, d, f.a, "1200") })
	waitFor(t, "the submit to be admitted", func() bool { return d.liveEntry.InFlight() == 1 })
	change = async(func() (int, []byte) { return f.ask(t, d, f.b) })
	waitFor(t, "the change to own the single flight and close admission while draining", func() bool {
		return d.archives.OperationInFlight() == "default_change" && d.liveEntry.Closed() && d.liveEntry.InFlight() == 1
	})
	return held, change, release
}

func TestDefaultChange_AD1_Admission(t *testing.T) {
	t.Run("the admitted submit commits on A; a live submit while draining is refused at once", func(t *testing.T) {
		releaseRoutes(t)
		withRestartPort(t)
		f := newChangeFixture(t)
		d := f.mustStart(t)
		held, change, release := f.draining(t, d)

		late := within(t, "a live submit while draining", async(func() (int, []byte) { return f.submit(t, d, f.a, "1201") }))
		if late.code != http.StatusConflict || !strings.Contains(string(late.body), "default_change_pending") {
			t.Fatalf("live submit while draining = %d %s; want 409 default_change_pending", late.code, late.body)
		}
		stillWaiting(t, "the change", change)
		if r := d.cfgSvc.Snapshot().DefaultLogbookChange; r != nil {
			t.Fatalf("persisted %+v while an admitted live submit was still running", r)
		}
		release()
		if r := answered(t, "the held submit", held); r.code != http.StatusCreated && r.code != http.StatusOK {
			t.Fatalf("held submit = %d %s; want it logged", r.code, r.body)
		}
		if r := answered(t, "the change", change); r.code != http.StatusAccepted {
			t.Fatalf("change = %d %s; want 202", r.code, r.body)
		}
		if r := d.cfgSvc.Snapshot().DefaultLogbookChange; r == nil || r.Removed != "" {
			t.Fatalf("record = %+v; want the pending change", r)
		}

		// Sealed: a live submit is still refused and writes nothing.
		if code, body := f.submit(t, d, f.a, "1205"); code != http.StatusConflict || !strings.Contains(string(body), "default_change_pending") {
			t.Fatalf("live submit while sealed = %d %s; want 409 default_change_pending", code, body)
		}
		// A deliberate submit to another logbook is logged.
		if code, body := f.submit(t, d, f.b, "1210"); code != http.StatusCreated && code != http.StatusOK {
			t.Fatalf("submit to B = %d %s; want it logged", code, body)
		}
		// A completed FT8 contact keeps its pinned logbook, here B — not the
		// effective default A, so a fallback to the default would show.
		d.ft8QsoLogger()(context.Background(), ft8.CompletedQso{Mode: "FT8", LogbookID: f.b, TheirCall: "JA1XYZ",
			HasOurReport: true, OurReport: -10, HasTheirReport: true, TheirReport: -12,
			StartedAt: time.Now().UTC().Add(-time.Minute), DialFreqMHz: 14.074}) // TIME_OFF is the log's own now
		d.qsoLogWG.Wait()

		f.stop(t, d)
		if n := f.qsosIn(t, f.a); n != 1 {
			t.Fatalf("QSOs in A = %d; want 1 (the held submit only)", n)
		}
		if n := f.qsosIn(t, f.b); n != 2 {
			t.Fatalf("QSOs in B = %d; want 2 (the deliberate submit and the FT8 contact)", n)
		}
	})
	t.Run("the admitted submit fails; the change then proceeds", func(t *testing.T) {
		releaseRoutes(t)
		withRestartPort(t)
		f := newChangeFixture(t)
		d := f.mustStart(t)
		held, change, release := f.draining(t, d)
		// Held past the store's busy timeout, the admitted submit fails.
		r := answered(t, "the held submit", held)
		if r.code == http.StatusCreated || r.code == http.StatusOK {
			t.Fatalf("held submit = %d %s; want it to fail at the held write", r.code, r.body)
		}
		if c := answered(t, "the change", change); c.code != http.StatusAccepted {
			t.Fatalf("change = %d %s; want 202 once the failed submit left", c.code, c.body)
		}
		release()
		f.stop(t, d)
		if n := f.qsosIn(t, f.a); n != 0 {
			t.Fatalf("QSOs in A = %d; want 0", n)
		}
	})
}

// ---- AD2 ---------------------------------------------------------------------

func TestDefaultChange_AD2_CoordinationOverHTTP(t *testing.T) {
	t.Run("a change held at its drain refuses a deletion and an activation", func(t *testing.T) {
		releaseRoutes(t)
		withRestartPort(t)
		f := newChangeFixture(t)
		d := f.mustStart(t)
		held, change, release := f.draining(t, d)
		r := within(t, "a deletion during the change", async(func() (int, []byte) {
			return apiCall(t, d, http.MethodDelete, "/v1/logbook/"+itoa(f.b))
		}))
		if r.code != http.StatusConflict || !strings.Contains(string(r.body), "default_change_in_progress") {
			t.Fatalf("DELETE B during the change = %d %s; want 409 default_change_in_progress", r.code, r.body)
		}
		r = within(t, "an activation during the change", async(func() (int, []byte) {
			return apiCallHeaders(t, d, http.MethodPost, "/v1/qso-archives/"+f.archive+"/activate", "", nil)
		}))
		if r.code != http.StatusConflict || !strings.Contains(string(r.body), "default_change_in_progress") {
			t.Fatalf("activate during the change = %d %s; want 409 default_change_in_progress", r.code, r.body)
		}
		release()
		answered(t, "the held submit", held)
		if r := answered(t, "the change", change); r.code != http.StatusAccepted {
			t.Fatalf("change = %d %s", r.code, r.body)
		}
		code, body := apiCall(t, d, http.MethodDelete, "/v1/logbook/"+itoa(f.b))
		if code != http.StatusConflict || !strings.Contains(string(body), "default_change_target") {
			t.Fatalf("DELETE B after = %d %s; want 409 default_change_target", code, body)
		}
		f.stop(t, d)
		if !f.live(t, f.b) {
			t.Fatal("B was deleted")
		}
	})
	t.Run("a deletion held at its write refuses a change", func(t *testing.T) {
		releaseRoutes(t)
		withRestartPort(t)
		f := newChangeFixture(t)
		d := f.mustStart(t)
		release := f.holdWrites(t)
		del := async(func() (int, []byte) { return apiCall(t, d, http.MethodDelete, "/v1/logbook/"+itoa(f.c)) })
		waitFor(t, "the deletion to own the single flight", func() bool { return d.archives.OperationInFlight() == "logbook_change" })
		r := within(t, "a change during the deletion", async(func() (int, []byte) { return f.ask(t, d, f.b) }))
		if r.code != http.StatusConflict || !strings.Contains(string(r.body), "logbook_change_in_progress") {
			t.Fatalf("change during the deletion = %d %s; want 409 logbook_change_in_progress", r.code, r.body)
		}
		release()
		if r := answered(t, "the deletion", del); r.code != http.StatusNoContent {
			t.Fatalf("deletion = %d %s", r.code, r.body)
		}
		if d.cfgSvc.Snapshot().DefaultLogbookChange != nil {
			t.Fatal("the refused change wrote a record")
		}
	})
}

// ---- AD3 ---------------------------------------------------------------------

func TestDefaultChange_AD3_ALostResponseIsFoundThroughTheStatus(t *testing.T) {
	releaseRoutes(t)
	withRestartPort(t)
	f := newChangeFixture(t)
	d := f.mustStart(t)
	f.ask(t, d, f.b) // its response is lost
	code, body := apiCall(t, d, http.MethodGet, "/v1/default-logbook")
	var st types.DefaultChangeStatus
	if code != http.StatusOK || json.Unmarshal(body, &st) != nil {
		t.Fatalf("status = %d %s", code, body)
	}
	rec := d.cfgSvc.Snapshot().DefaultLogbookChange
	if rec == nil || st.Change == nil || st.Change.RequestID != rec.RequestID || st.Change.State != "pending_restart" || st.Effective.ID != f.a {
		t.Fatalf("status = %+v (change %+v), record %+v; want A in effect and the recorded request pending its restart", st, st.Change, rec)
	}
	requestID := rec.RequestID

	f.stop(t, d)
	d2 := f.mustStart(t)
	code, body = apiCall(t, d2, http.MethodGet, "/v1/default-logbook")
	st = types.DefaultChangeStatus{}
	if code != http.StatusOK || json.Unmarshal(body, &st) != nil {
		t.Fatalf("status after the restart = %d %s", code, body)
	}
	if st.Effective.ID != f.b || st.Change == nil || st.Change.RequestID != requestID || st.Change.State != defaultChangeEffective {
		t.Fatalf("status after the restart = %+v (change %+v); want B in effect and %s effective", st, st.Change, requestID)
	}
	_ = config.RemovedApplied
}
