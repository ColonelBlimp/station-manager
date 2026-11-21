package sqlite

import (
	"context"
	"reflect"
	"testing"
)

// The arrival at an archive (ADR 0100 §4; W-0029 part 3, criteria A). An
// activation has an identity. The process says which one it serves the file
// under; the file records the one it was last arrived at under. When they
// differ the file has been arrived at: a session still open in it is closed
// with archive_switched, its interval's end unconfirmed, and the file's value
// is set, in one commit. Every assertion is on rows read back from the file.

const (
	actW = "activation-W" // the activation a file was served under before
	actX = "activation-X" // the one the process now works under
)

func (b *sessBed) arrivedUnder() string {
	b.t.Helper()
	return b.text(`SELECT arrived_activation FROM archive_metadata WHERE singleton = 1`)
}

func (b *sessBed) startCheck(minutes int, want SessionOutcome) OperatingSessionState {
	b.t.Helper()
	st, got, err := b.svc.CheckOperatingSessionAtStartWithContext(context.Background(), at(minutes))
	return b.done("the start step", st, got, err, want)
}

// remnant leaves a session of lifetime A open in a file arrived at under W,
// with one contact unless empty, as an earlier visit to the archive left it.
func remnant(t *testing.T, empty bool) (b *sessBed, id string, qso int64) {
	t.Helper()
	b = newSessBed(t, true)
	execT(t, b.svc, `UPDATE archive_metadata SET arrived_activation = ?`, actW)
	id = b.open(lifeA, 0, SessionApplied).Open.ID
	if !empty {
		qso, _ = b.log(b.x, lifeA, 1)
	}
	return b, id, qso
}

func TestOperatingSession_Arrival_AtTheStartStep(t *testing.T) {
	t.Run("a remnant with contacts is closed: archive_switched, its end unconfirmed", func(t *testing.T) {
		b, id, qso := remnant(t, false)
		gen := b.generation()
		b.svc.SetServedActivation(actX)

		st := b.startCheck(10, SessionApplied)

		if st.Open != nil || st.Generation != int64(gen)+1 {
			t.Fatalf("state = %+v; want no session at generation %d", st, gen+1)
		}
		b.wantClosed(id, SessionClosedBySwitch, 10)
		b.wantIntervals(id, "1 unconfirmed "+lifeA+" 0 -")
		if m := b.members(id); !reflect.DeepEqual(m, []int64{qso}) {
			t.Fatalf("the closed session's members = %v, want [%d]", m, qso)
		}
		if got := b.arrivedUnder(); got != actX {
			t.Fatalf("the file records %q after the arrival, want %q", got, actX)
		}
		b.invariants()
	})
	t.Run("an empty remnant is removed", func(t *testing.T) {
		b, id, _ := remnant(t, true)
		b.svc.SetServedActivation(actX)
		b.startCheck(10, SessionApplied)
		if b.session(id).exists {
			t.Fatal("the empty remnant was kept")
		}
		if got := b.arrivedUnder(); got != actX {
			t.Fatalf("the file records %q, want %q", got, actX)
		}
	})
	t.Run("with no session open the file still records the arrival, and no generation moves", func(t *testing.T) {
		b := newSessBed(t, true)
		gen := b.generation()
		b.svc.SetServedActivation(actX)
		st := b.startCheck(10, SessionUnchanged)
		if got := b.arrivedUnder(); got != actX {
			t.Fatalf("the file records %q, want %q", got, actX)
		}
		if st.Generation != int64(gen) {
			t.Fatalf("generation %d, want %d: no session changed", st.Generation, gen)
		}
	})
	t.Run("an ordinary start, the same activation, closes nothing", func(t *testing.T) {
		b, id, _ := remnant(t, false)
		b.svc.SetServedActivation(actW)
		b.unchanged("the start step", func() { b.startCheck(10, SessionUnchanged) })
		if b.session(id).closedAt.Valid {
			t.Fatal("an ordinary start closed the session")
		}
	})
}

// Once for each activation: a session opened after an arrival is not closed by
// the same activation processed again (a promotion lost and repeated, a
// fallback repeated).
func TestOperatingSession_Arrival_OnceForEachActivation(t *testing.T) {
	b, _, _ := remnant(t, false)
	b.svc.SetServedActivation(actX)
	b.startCheck(10, SessionApplied)
	later := b.open(lifeB, 11, SessionApplied).Open.ID
	b.log(b.x, lifeB, 12)

	// The daemon starts again and works under X again.
	b.svc.SetServedActivation(actX)
	b.unchanged("the same activation processed again", func() { b.startCheck(20, SessionUnchanged) })
	st := b.open(lifeB, 21, SessionUnchanged)
	if st.Open == nil || st.Open.ID != later {
		t.Fatalf("after the replay the open session is %+v, want %s still", st.Open, later)
	}
	b.wantIntervals(later, "1 open "+lifeB+" 11 -")
}

// The admission's start step runs the test, so an arrival the start step could
// not write is never skipped by an admission that succeeds: the app gets a new
// session and no question, whoever held the remnant.
func TestOperatingSession_Arrival_InTheAdmissionsStartStep(t *testing.T) {
	t.Run("another lifetime's remnant with contacts: no recovery question", func(t *testing.T) {
		b, id, _ := remnant(t, false)
		b.svc.SetServedActivation(actX)
		st := b.open(lifeB, 10, SessionApplied)
		if st.Open == nil || st.Open.ID == id || st.Open.Holder != lifeB {
			t.Fatalf("the open session is %+v; want a new one held by %s", st.Open, lifeB)
		}
		b.wantClosed(id, SessionClosedBySwitch, 10)
		b.wantIntervals(id, "1 unconfirmed "+lifeA+" 0 -")
		if got := b.arrivedUnder(); got != actX {
			t.Fatalf("the file records %q, want %q", got, actX)
		}
	})
	t.Run("the same lifetime's remnant is not carried on", func(t *testing.T) {
		b, id, _ := remnant(t, false)
		b.svc.SetServedActivation(actX)
		st := b.open(lifeA, 10, SessionApplied)
		if st.Open == nil || st.Open.ID == id {
			t.Fatalf("the open session is %+v; want a new one, not the remnant %s", st.Open, id)
		}
		b.wantClosed(id, SessionClosedBySwitch, 10)
	})
}

// Where the arrival and the default-epoch test would both close a session, the
// reason is the switch.
func TestOperatingSession_Arrival_ComesBeforeTheDefaultEpochTest(t *testing.T) {
	b, id, _ := remnant(t, false)
	b.setDefault(b.y)
	b.svc.SetServedActivation(actX)
	b.startCheck(10, SessionApplied)
	b.wantClosed(id, SessionClosedBySwitch, 10)
	b.wantIntervals(id, "1 unconfirmed "+lifeA+" 0 -")
}

// A read changes nothing, an arrival owed included (part 2, D3).
func TestOperatingSession_Arrival_AReadChangesNothing(t *testing.T) {
	b, id, _ := remnant(t, false)
	b.svc.SetServedActivation(actX)
	b.unchanged("the read", func() {
		st := b.state(lifeB)
		if st.Open == nil || st.Open.ID != id {
			t.Fatalf("the read shows %+v, want the remnant %s as it is stored", st.Open, id)
		}
	})
}

// A process that has not been told what it serves under tests for no arrival:
// a tool that opens a file leaves its sessions alone.
func TestOperatingSession_Arrival_NotTestedUntilTheProcessSaysWhatItServes(t *testing.T) {
	b := newSessBed(t, true)
	id := b.open(lifeA, 0, SessionApplied).Open.ID
	b.log(b.x, lifeA, 1)
	execT(t, b.svc, `UPDATE archive_metadata SET arrived_activation = ?`, actW)
	b.unchanged("the start step", func() { b.startCheck(10, SessionUnchanged) })
	if st := b.open(lifeA, 11, SessionUnchanged); st.Open == nil || st.Open.ID != id {
		t.Fatalf("the open session is %+v, want %s", st.Open, id)
	}
}

// The logging step runs the test too: a contact logged after an arrival joins a
// new session, never the remnant.
func TestOperatingSession_Arrival_InTheLoggingStep(t *testing.T) {
	b, id, _ := remnant(t, false)
	b.svc.SetServedActivation(actX)
	_, joined := b.log(b.x, lifeB, 10)
	if joined == "" || joined == id {
		t.Fatalf("the contact joined %q; want a new session, not the remnant %s", joined, id)
	}
	b.wantClosed(id, SessionClosedBySwitch, 10)
	if got := b.arrivedUnder(); got != actX {
		t.Fatalf("the file records %q, want %q", got, actX)
	}
	b.invariants()
}

// A control that is refused has still seen the arrival: its close stands and
// the generation it named is stale.
func TestOperatingSession_Arrival_ARefusedControlKeepsTheClose(t *testing.T) {
	b, id, _ := remnant(t, false)
	gen := int64(b.generation())
	b.svc.SetServedActivation(actX)
	st := b.newSession(lifeA, gen, 10, SessionStale)
	if st.Open != nil || st.Generation != gen+1 {
		t.Fatalf("state = %+v; want no session at generation %d", st, gen+1)
	}
	b.wantClosed(id, SessionClosedBySwitch, 10)
}

// The file's value and the close are one commit: when the close cannot be
// written the file does not record the arrival, and the next transaction
// arrives again.
func TestOperatingSession_Arrival_TheRecordAndTheCloseAreOneCommit(t *testing.T) {
	b, id, _ := remnant(t, false)
	b.svc.SetServedActivation(actX)
	execT(t, b.svc, `CREATE TRIGGER refuse_close BEFORE UPDATE ON operating_session
		BEGIN SELECT RAISE(ABORT, 'the test refuses the close'); END`)

	before := b.records()
	if _, _, err := b.svc.CheckOperatingSessionAtStartWithContext(context.Background(), at(10)); err == nil {
		t.Fatal("the fixture: the close was not refused")
	}
	if after := b.records(); !reflect.DeepEqual(before, after) {
		t.Fatalf("a failed arrival changed the record:\n before %v\n after  %v", before, after)
	}
	if got := b.arrivedUnder(); got != actW {
		t.Fatalf("the file records %q after a failed arrival, want %q still", got, actW)
	}

	// The file can be written again: the admission arrives before it gives the app a session.
	execT(t, b.svc, `DROP TRIGGER refuse_close`)
	st := b.open(lifeB, 20, SessionApplied)
	if st.Open == nil || st.Open.ID == id {
		t.Fatalf("the open session is %+v; want a new one", st.Open)
	}
	b.wantClosed(id, SessionClosedBySwitch, 20)
	if got := b.arrivedUnder(); got != actX {
		t.Fatalf("the file records %q, want %q", got, actX)
	}
}
