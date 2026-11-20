package main

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/testbarrier"
)

/*
   W-0029 part 2, group G: an exit report and an app start are ordered with a
   change of the default logbook and an archive activation, through a real
   daemon, its real archive manager and its real seals.

     G5  no operation releases another's seal. A default change keeps live
         entry closed and TX admission sealed until the restart; an exit that
         succeeds, an exit that fails, and an app start in that time leave both
         as they were.
     G6  ordered, not interleaved, and not refused. A default change or an
         activation asked for while an exit is under way takes its single
         flight and then waits: it seals and records nothing until the exit
         has finished. A second archive operation asked for in that time is
         refused as it is today. An exit asked for while a default change is
         in flight is not refused.

   Each case holds one side at a point the test sees (the archive's write lock,
   or a submit admitted to live entry) and observes the other shut out: the
   other is seen parked where it takes the admission's order, having answered
   nothing (testbarrier; G7: none sleeps and then looks). The watch is begun
   before the other is sent, and requires that nothing was waiting there
   already and that exactly one goroutine waits there afterwards.
*/

// Where an archive operation, and an exit, take the admission's order.
const (
	archiveOpTakesOrder = "(*Server).HoldSessionOrder"
	exitTakesOrder      = "(*Server).exitOperatingSession"
)

// orderWatch begins the watch for a request that will take the admission's
// order in takesOrder. It is taken BEFORE that request is sent, and fails if
// anything is already waiting there.
func orderWatch(t *testing.T, takesOrder string) testbarrier.Watch {
	t.Helper()
	return testbarrier.Expect(t, testbarrier.AtMutex, takesOrder)
}

// parkedAtOrder: the request behind ch, sent after w was taken, is the one
// goroutine parked at the admission's lock there, and has answered nothing.
func parkedAtOrder(w testbarrier.Watch, what string, ch chan httpResult) {
	testbarrier.Waiting(w, what, ch)
}

// unanswered: a request the test itself is holding has answered nothing. It is
// held by construction; this is a look, not the proof.
func unanswered(t *testing.T, what string, ch chan httpResult) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("%s answered %d %s while the test was holding it", what, r.code, r.body)
	default:
	}
}

const (
	orderLifeA = "AAAAAAAAAAAAAAAAAAAAAA"
	orderLifeB = "BBBBBBBBBBBBBBBBBBBBBA"
)

type orderBed struct {
	f       *changeFixture // nil for a bed without the default-change fixture
	d       *daemon
	archive string
}

func newOrderBed(t *testing.T) *orderBed {
	t.Helper()
	releaseRoutes(t)
	withRestartPort(t)
	f := newChangeFixture(t)
	return &orderBed{f: f, d: f.mustStart(t), archive: f.archive}
}

func (b *orderBed) as(t *testing.T, lifetime, method, path, body string) (int, []byte) {
	t.Helper()
	return apiCallHeaders(t, b.d, method, path, body,
		map[string]string{"X-SM-Archive-Id": b.archive, "X-SM-App-Lifetime": lifetime})
}

// admit is an app start: the read, then the admission against it.
func (b *orderBed) admit(t *testing.T, lifetime string) (int, []byte) {
	t.Helper()
	code, raw := b.as(t, lifetime, http.MethodGet, "/v1/app/lifetime", "")
	var st struct {
		Instance   string `json:"daemon_instance"`
		Generation uint64 `json:"generation"`
	}
	if code != http.StatusOK || json.Unmarshal(raw, &st) != nil {
		t.Fatalf("read the admission as %s: %d %s", lifetime, code, raw)
	}
	return b.as(t, lifetime, http.MethodPost, "/v1/app/lifetime",
		fmt.Sprintf(`{"daemon_instance":%q,"generation":%d}`, st.Instance, st.Generation))
}

func (b *orderBed) mustAdmit(t *testing.T, lifetime string) {
	t.Helper()
	if code, raw := b.admit(t, lifetime); code != http.StatusOK {
		t.Fatalf("admit %s: %d %s", lifetime, code, raw)
	}
}

// session is what the session route says to lifetime.
func (b *orderBed) session(t *testing.T, lifetime string) (generation int64, state string) {
	t.Helper()
	code, raw := b.as(t, lifetime, http.MethodGet, "/v1/operating-session", "")
	var v struct {
		Generation int64  `json:"generation"`
		State      string `json:"state"`
	}
	if code != http.StatusOK || json.Unmarshal(raw, &v) != nil {
		t.Fatalf("read the session as %s: %d %s", lifetime, code, raw)
	}
	return v.Generation, v.State
}

func (b *orderBed) exit(t *testing.T, lifetime string, generation int64) (int, []byte) {
	t.Helper()
	return b.as(t, lifetime, http.MethodPost, "/v1/operating-session/exit", fmt.Sprintf(`{"generation":%d}`, generation))
}

func (b *orderBed) exitRecorded(t *testing.T, lifetime string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+b.d.paths.QSO+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM operating_app_exit WHERE app_lifetime = ?`, lifetime).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// started admits lifetime A, with its FT8 stream open, and returns the
// generation its exit must name. The session is open: the fixture's archive
// file names its default.
func (b *orderBed) started(t *testing.T) int64 {
	t.Helper()
	b.mustAdmit(t, orderLifeA)
	b.stream(t, orderLifeA)
	gen, state := b.session(t, orderLifeA)
	if state != "open" {
		t.Fatalf("the fixture: after the app start the session is %q, want open", state)
	}
	return gen
}

// stream opens lifetime's FT8 stream, without which an arm is refused for its
// caller before the seal is looked at. FT8 is off in this daemon's config, so
// the stream starts no capture.
func (b *orderBed) stream(t *testing.T, lifetime string) {
	t.Helper()
	_, leave, err := b.d.ft8.SubscribeModeAs("", lifetime)
	if err != nil {
		t.Fatalf("%s's FT8 stream: %v", lifetime, err)
	}
	t.Cleanup(leave)
}

// requireChangeSealsHeld: live entry is closed and TX admission sealed, as the
// accepted default change left them. who must be the current lifetime with its
// stream open.
func (b *orderBed) requireChangeSealsHeld(t *testing.T, who, when string) {
	t.Helper()
	if code, body := b.f.submit(t, b.d, b.f.a, "1210"); code != http.StatusConflict || !strings.Contains(string(body), "default_change_pending") {
		t.Fatalf("%s a live submit = %d %s; want 409 default_change_pending: live entry was reopened under the default change", when, code, body)
	}
	if n := b.f.qsosIn(t, b.f.a); n != 0 {
		t.Fatalf("%s %d QSO(s) were stored in the default logbook", when, n)
	}
	if err := b.d.ft8.OperatingAs(who).ArmTx(true); !stderr.Is(err, ft8.ErrArchiveSwitchPending) {
		t.Fatalf("%s an arm by %s = %v; want the pending-change refusal: TX admission was unsealed", when, who, err)
	}
}

// accepted asks for the change of the default to B and requires it accepted:
// its seals are then held until the restart.
func (b *orderBed) accepted(t *testing.T) {
	t.Helper()
	if code, body := b.f.ask(t, b.d, b.f.b); code != http.StatusAccepted {
		t.Fatalf("the default change = %d %s; want 202", code, body)
	}
}

// ---- G5 ----

func TestSessionOrder_G5_AnExitLeavesAnAcceptedDefaultChangesSeals(t *testing.T) {
	t.Run("an exit that succeeds, and the app start after it", func(t *testing.T) {
		b := newOrderBed(t)
		gen := b.started(t)
		b.accepted(t)
		b.requireChangeSealsHeld(t, orderLifeA, "before the exit")

		// Not refused, and not made to wait for the restart (G6, second limit).
		r := within(t, "the exit under an accepted default change", async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) }))
		if r.code != http.StatusOK || !b.exitRecorded(t, orderLifeA) {
			t.Fatalf("the exit = %d %s, recorded %v; want 200 and recorded", r.code, r.body, b.exitRecorded(t, orderLifeA))
		}
		// A new start is admitted under the same seals, and they hold for it.
		b.mustAdmit(t, orderLifeB)
		b.stream(t, orderLifeB)
		b.requireChangeSealsHeld(t, orderLifeB, "after the exit and a new app start")
	})
	t.Run("an exit that fails and gives operation back", func(t *testing.T) {
		b := newOrderBed(t)
		gen := b.started(t)
		b.accepted(t)
		b.f.exec(t, `CREATE TRIGGER fail_exit BEFORE INSERT ON operating_app_exit BEGIN SELECT RAISE(ABORT, 'forced: the exit cannot be stored'); END`)
		code, body := b.exit(t, orderLifeA, gen)
		if code != http.StatusServiceUnavailable || !strings.Contains(string(body), "operating_session_unavailable") {
			t.Fatalf("the exit = %d %s; want 503 operating_session_unavailable", code, body)
		}
		b.f.exec(t, `DROP TRIGGER fail_exit`)
		if b.exitRecorded(t, orderLifeA) {
			t.Fatal("a failed exit was recorded")
		}
		// The lifetime is published to FT8 again: the arm reaches the seal, and
		// is refused by it, not for its caller.
		b.requireChangeSealsHeld(t, orderLifeA, "after the failed exit")
	})
}

// activated provisions another archive and asks for its activation, which must
// be accepted: its FT8 and rig seals are then held until the restart. An
// activation takes no live-entry seal.
func (b *orderBed) activated(t *testing.T) (other string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	made, err := b.d.archives.Create(ctx, archive.CreateRequest{RequestKey: "k-order-g5", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision the other archive: %v", err)
	}
	code, body := apiCallHeaders(t, b.d, http.MethodPost, "/v1/qso-archives/"+made.Entry.ID+"/activate", "", nil)
	if code != http.StatusAccepted && code != http.StatusOK {
		t.Fatalf("the activation = %d %s; want it accepted", code, body)
	}
	return made.Entry.ID
}

// requireActivationSealsHeld: the activation is still pending and TX admission
// still sealed. who must be the current lifetime with its stream open.
func (b *orderBed) requireActivationSealsHeld(t *testing.T, other, who, when string) {
	t.Helper()
	if p := b.d.cfgSvc.Snapshot().PendingQsoArchiveID; p != other {
		t.Fatalf("%s the pending archive is %q, want %q", when, p, other)
	}
	if err := b.d.ft8.OperatingAs(who).ArmTx(true); !stderr.Is(err, ft8.ErrArchiveSwitchPending) {
		t.Fatalf("%s an arm by %s = %v; want the pending-switch refusal: TX admission was unsealed", when, who, err)
	}
}

// The same for the seals an activation takes (G5: "The same for the seals an
// activation takes, and for the exit's failure path").
func TestSessionOrder_G5_AnExitLeavesAnAcceptedActivationsSeals(t *testing.T) {
	t.Run("an exit that succeeds, and the app start after it", func(t *testing.T) {
		b := newOrderBed(t)
		gen := b.started(t)
		other := b.activated(t)
		b.requireActivationSealsHeld(t, other, orderLifeA, "before the exit")

		r := within(t, "the exit under an accepted activation", async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) }))
		if r.code != http.StatusOK || !b.exitRecorded(t, orderLifeA) {
			t.Fatalf("the exit = %d %s, recorded %v; want 200 and recorded", r.code, r.body, b.exitRecorded(t, orderLifeA))
		}
		b.mustAdmit(t, orderLifeB)
		b.stream(t, orderLifeB)
		b.requireActivationSealsHeld(t, other, orderLifeB, "after the exit and a new app start")
	})
	t.Run("an exit that fails and gives operation back", func(t *testing.T) {
		b := newOrderBed(t)
		gen := b.started(t)
		other := b.activated(t)
		b.f.exec(t, `CREATE TRIGGER fail_exit BEFORE INSERT ON operating_app_exit BEGIN SELECT RAISE(ABORT, 'forced: the exit cannot be stored'); END`)
		code, body := b.exit(t, orderLifeA, gen)
		if code != http.StatusServiceUnavailable || !strings.Contains(string(body), "operating_session_unavailable") {
			t.Fatalf("the exit = %d %s; want 503 operating_session_unavailable", code, body)
		}
		b.f.exec(t, `DROP TRIGGER fail_exit`)
		if b.exitRecorded(t, orderLifeA) {
			t.Fatal("a failed exit was recorded")
		}
		// The lifetime is published to FT8 again: the arm reaches the seal, and
		// is refused by it, not for its caller.
		b.requireActivationSealsHeld(t, other, orderLifeA, "after the failed exit")
	})
}

// ---- G6 ----

// heldExit holds lifetime A's exit between its seals and its commit, at the
// archive's write lock, and returns once the exit has sealed live entry.
func (b *orderBed) heldExit(t *testing.T, gen int64) (exit chan httpResult, release func()) {
	t.Helper()
	release = b.f.holdWrites(t)
	exit = async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) })
	waitFor(t, "the exit to seal live entry", func() bool { return b.d.liveEntry.Closed() })
	return exit, release
}

func TestSessionOrder_G6_ADefaultChangeWaitsForAnExitUnderWay(t *testing.T) {
	b := newOrderBed(t)
	gen := b.started(t)
	exit, release := b.heldExit(t, gen)

	w := orderWatch(t, archiveOpTakesOrder)
	change := async(func() (int, []byte) { return b.f.ask(t, b.d, b.f.b) })
	parkedAtOrder(w, "the default change, with an exit under way", change)
	if op := b.d.archives.OperationInFlight(); op != "default_change" {
		t.Fatalf("while it waits the change holds the single flight as %q, want default_change", op)
	}
	if r := b.d.cfgSvc.Snapshot().DefaultLogbookChange; r != nil {
		t.Fatalf("the change recorded %+v while the exit was still under way", r)
	}
	// A second archive operation is refused as today, also while the first is
	// itself waiting behind the exit.
	r := within(t, "an activation while the change waits", async(func() (int, []byte) {
		return apiCallHeaders(t, b.d, http.MethodPost, "/v1/qso-archives/"+b.f.archive+"/activate", "", nil)
	}))
	if r.code != http.StatusConflict || !strings.Contains(string(r.body), "default_change_in_progress") {
		t.Fatalf("an activation while the change waits = %d %s; want 409 default_change_in_progress", r.code, r.body)
	}
	r = within(t, "a deletion while the change waits", async(func() (int, []byte) {
		return apiCall(t, b.d, http.MethodDelete, "/v1/logbook/"+itoa(b.f.c))
	}))
	if r.code != http.StatusConflict || !strings.Contains(string(r.body), "default_change_in_progress") {
		t.Fatalf("a deletion while the change waits = %d %s; want 409 default_change_in_progress", r.code, r.body)
	}
	unanswered(t, "the exit, held at its commit", exit)

	release()
	if r := answered(t, "the exit", exit); r.code != http.StatusOK {
		t.Fatalf("the exit = %d %s; want 200", r.code, r.body)
	}
	if r := answered(t, "the default change", change); r.code != http.StatusAccepted {
		t.Fatalf("the default change after the exit = %d %s; want 202", r.code, r.body)
	}
	if !b.exitRecorded(t, orderLifeA) {
		t.Fatal("the exit is not recorded")
	}
	// The exit released its own seal before the change sealed; the change's
	// seals are in force for the next start of the app.
	b.mustAdmit(t, orderLifeB)
	b.stream(t, orderLifeB)
	b.requireChangeSealsHeld(t, orderLifeB, "after the exit and then the change")
}

func TestSessionOrder_G6_AnActivationWaitsForAnExitUnderWay(t *testing.T) {
	b := newOrderBed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	other, err := b.d.archives.Create(ctx, archive.CreateRequest{RequestKey: "k-order", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision the other archive: %v", err)
	}
	gen := b.started(t)
	exit, release := b.heldExit(t, gen)

	w := orderWatch(t, archiveOpTakesOrder)
	activation := async(func() (int, []byte) {
		return apiCallHeaders(t, b.d, http.MethodPost, "/v1/qso-archives/"+other.Entry.ID+"/activate", "", nil)
	})
	parkedAtOrder(w, "the activation, with an exit under way", activation)
	if op := b.d.archives.OperationInFlight(); op != "activation" {
		t.Fatalf("while it waits the activation holds the single flight as %q, want activation", op)
	}
	if p := b.d.cfgSvc.Snapshot().PendingQsoArchiveID; p != "" {
		t.Fatalf("the activation recorded %q as pending while the exit was still under way", p)
	}
	r := within(t, "a default change while the activation waits", async(func() (int, []byte) { return b.f.ask(t, b.d, b.f.b) }))
	if r.code != http.StatusConflict || !strings.Contains(string(r.body), "activation_in_progress") {
		t.Fatalf("a default change while the activation waits = %d %s; want 409 activation_in_progress", r.code, r.body)
	}

	release()
	if r := answered(t, "the exit", exit); r.code != http.StatusOK {
		t.Fatalf("the exit = %d %s; want 200", r.code, r.body)
	}
	if r := answered(t, "the activation", activation); r.code != http.StatusAccepted && r.code != http.StatusOK {
		t.Fatalf("the activation after the exit = %d %s; want it accepted", r.code, r.body)
	}
	if p := b.d.cfgSvc.Snapshot().PendingQsoArchiveID; p != other.Entry.ID || !b.exitRecorded(t, orderLifeA) {
		t.Fatalf("pending %q, exit recorded %v; want the activation pending and the exit recorded", p, b.exitRecorded(t, orderLifeA))
	}
}

// An exit asked for while a default change is in flight is not refused; it
// finishes after the change and leaves the change's seals.
func TestSessionOrder_G6_AnExitDuringADefaultChangeIsNotRefused(t *testing.T) {
	b := newOrderBed(t)
	gen := b.started(t)
	held, change, release := b.f.draining(t, b.d) // the change holds the flight, waiting for an admitted submit

	w := orderWatch(t, exitTakesOrder)
	exit := async(func() (int, []byte) { return b.exit(t, orderLifeA, gen) })
	parkedAtOrder(w, "the exit, with a default change in flight", exit)
	if b.exitRecorded(t, orderLifeA) {
		t.Fatal("the exit was stored while the default change was in flight")
	}
	release()
	if r := answered(t, "the held submit", held); r.code != http.StatusCreated && r.code != http.StatusOK {
		t.Fatalf("the held submit = %d %s; want it logged", r.code, r.body)
	}
	if r := answered(t, "the default change", change); r.code != http.StatusAccepted {
		t.Fatalf("the default change = %d %s; want 202", r.code, r.body)
	}
	if r := answered(t, "the exit", exit); r.code != http.StatusOK || !b.exitRecorded(t, orderLifeA) {
		t.Fatalf("the exit = %d %s, recorded %v; want 200 and recorded", r.code, r.body, b.exitRecorded(t, orderLifeA))
	}
	b.mustAdmit(t, orderLifeB)
	b.stream(t, orderLifeB)
	if code, body := b.f.submit(t, b.d, b.f.a, "1215"); code != http.StatusConflict || !strings.Contains(string(body), "default_change_pending") {
		t.Fatalf("after the change and then the exit a live submit = %d %s; want 409 default_change_pending", code, body)
	}
	if err := b.d.ft8.OperatingAs(orderLifeB).ArmTx(true); !stderr.Is(err, ft8.ErrArchiveSwitchPending) {
		t.Fatalf("after the change and then the exit an arm = %v; want the pending-change refusal", err)
	}
}
