package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

func TestParseMessage_FieldDay(t *testing.T) {
	t.Run("CQ FD sets the fd flag + keeps call/grid", func(t *testing.T) {
		m := parseMessage("CQ FD K1ABC FN42")
		if m.kind != msgCQ || m.from != "K1ABC" || m.grid != "FN42" || !m.fd {
			t.Fatalf("got %+v, want msgCQ from K1ABC grid FN42 fd=true", m)
		}
	})
	t.Run("plain CQ is not FD", func(t *testing.T) {
		if m := parseMessage("CQ K1ABC FN42"); m.fd {
			t.Fatalf("plain CQ marked fd: %+v", m)
		}
	})
	t.Run("R-exchange (rung 2 we receive)", func(t *testing.T) {
		m := parseMessage("7Q5MLV K1ABC R 2A EMA")
		if m.kind != msgFdExchange || m.to != "7Q5MLV" || m.from != "K1ABC" ||
			m.class != "2A" || m.section != "EMA" {
			t.Fatalf("got %+v, want msgFdExchange 2A/EMA", m)
		}
	})
	t.Run("bare exchange (opening reply, no R)", func(t *testing.T) {
		m := parseMessage("K1ABC 7Q5MLV 1D DX")
		if m.kind != msgFdExchange || m.class != "1D" || m.section != "DX" {
			t.Fatalf("got %+v, want msgFdExchange 1D/DX", m)
		}
	})
	t.Run("R-report is NOT mistaken for an FD exchange", func(t *testing.T) {
		// "R-15" is one token, not the standalone "R" of an FD exchange.
		if m := parseMessage("K1ABC W9XYZ R-15"); m.kind != msgRReport {
			t.Fatalf("got %+v, want msgRReport", m)
		}
	})
	t.Run("a grid reply is NOT mistaken for an FD exchange", func(t *testing.T) {
		if m := parseMessage("K1ABC W9XYZ EN37"); m.kind != msgGrid {
			t.Fatalf("got %+v, want msgGrid", m)
		}
	})
	t.Run("FD-shaped class with a bogus section degrades to msgOther", func(t *testing.T) {
		if m := parseMessage("K1ABC W9XYZ 2A ZZ"); m.kind == msgFdExchange {
			t.Fatalf("bogus section accepted as FD exchange: %+v", m)
		}
	})
}

func TestLooksLikeFdClass(t *testing.T) {
	for _, s := range []string{"1A", "2D", "6A", "33A", "0A"} {
		if !looksLikeFdClass(s) {
			t.Errorf("looksLikeFdClass(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "A", "1", "1G", "AA", "100A", "FN42"} {
		if looksLikeFdClass(s) {
			t.Errorf("looksLikeFdClass(%q) = true, want false", s)
		}
	}
}

// The answerer ladder, us = 7Q5MLV (1D/DX) answering K1ABC's CQ FD.
func TestFdExchange_Ladder(t *testing.T) {
	e := NewFdExchange("7q5mlv", "1d", "dx", "k1abc", "FN42AA")

	// Opening rung: our exchange. Grid is trimmed to 4 chars.
	if e.State != fdCalling || e.TheirGrid != "FN42" {
		t.Fatalf("after New: %+v", e)
	}
	if msg, ok := e.TxMessage(); !ok || msg != "K1ABC 7Q5MLV 1D DX" {
		t.Fatalf("opening TxMessage = %q (ok=%v), want \"K1ABC 7Q5MLV 1D DX\"", msg, ok)
	}

	// A line not directed to us doesn't advance.
	if _, ok := e.Advance("CQ FD W1AW FN31"); ok {
		t.Fatal("advanced on an unrelated line")
	}

	// Their R-exchange → capture class/section, move to rogering.
	next, ok := e.Advance("7Q5MLV K1ABC R 2A EMA")
	if !ok || next.State != fdRogering || next.TheirClass != "2A" || next.TheirSection != "EMA" ||
		!next.HasTheirExch {
		t.Fatalf("after their exchange: %+v (ok=%v)", next, ok)
	}
	e = next

	// Rogering rung: RR73.
	if msg, ok := e.TxMessage(); !ok || msg != "K1ABC 7Q5MLV RR73" {
		t.Fatalf("rogering TxMessage = %q (ok=%v), want \"K1ABC 7Q5MLV RR73\"", msg, ok)
	}

	// After RR73 leaves the radio the contact is done.
	e = e.Sent()
	if e.State != fdDone {
		t.Fatalf("after Sent: state %v, want fdDone", e.State)
	}
	if _, ok := e.TxMessage(); ok {
		t.Fatal("fdDone still has a TxMessage")
	}
}

// FD happy path through the daemon sequencer: us = G0XYZ (class 1D, section DX)
// answering K1ABC's CQ FD. The worked station transmits in even slots; driveTheir
// uses even sec. Proves the full answer-a-CQ-FD contact drives end-to-end and logs
// the worked station's class/section — the functional companion to the offline
// round-trip RF gate.
func TestSequencer_FieldDayHappyPath(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	require.NoError(t, s.StartQsoFd("G0XYZ", "1D", "DX", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active())

	// Their CQ FD (or silence) → we send our exchange.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ FD K1ABC FN42", -1)})
	// Their R + exchange → we send RR73 and the QSO completes + logs.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC R 2A EMA", -12)})

	require.Equal(t, []string{
		"K1ABC G0XYZ 1D DX",
		"K1ABC G0XYZ RR73",
	}, r.sentMsgs())

	require.False(t, s.Active(), "QSO should be idle after RR73")
	require.Len(t, r.completed, 1)
	require.Equal(t, "K1ABC", r.completed[0].TheirCall)
	require.Equal(t, "FN42", r.completed[0].TheirGrid)
	require.Equal(t, "2A", r.completed[0].Class)
	require.Equal(t, "EMA", r.completed[0].Section)
	require.Equal(t, 14.074, r.completed[0].DialFreqMHz)
	require.Equal(t, 1500.0, r.completed[0].OffsetHz)
}

func TestSequencer_FieldDayRequiresIdentity(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	now := time.Unix(0, 0).UTC()
	slot := now.Format(time.RFC3339)
	require.ErrorIs(t, s.StartQsoFd("G0XYZ", "", "DX", "K1ABC", "FN42", slot, 1500, 14.074, now),
		ErrFdIdentityUnset)
	require.ErrorIs(t, s.StartQsoFd("G0XYZ", "1D", "", "K1ABC", "FN42", slot, 1500, 14.074, now),
		ErrFdIdentityUnset)
}

func TestBuildQso_FieldDay(t *testing.T) {
	c := CompletedQso{
		TheirCall:   "K1ABC",
		TheirGrid:   "FN42",
		Class:       "2A",
		Section:     "EMA",
		DialFreqMHz: 14.074,
		StartedAt:   time.Unix(0, 0).UTC(),
	}
	q := BuildQso(c, types.LoggingStation{Operator: "G0XYZ"}, 1, time.Unix(0, 0).UTC())
	require.Equal(t, "K1ABC", q.Call)
	require.Equal(t, "FT8", q.Mode)
	require.Equal(t, "2A", q.Class)
	require.Equal(t, "EMA", q.ArrlSect)
	require.Equal(t, "ARRL-FD", q.ContestId)
}

// FD work-a-caller ladder: us = G0XYZ (1D/DX) working K7IOC, who called us with their
// exchange (2A WWA, captured from the call we picked).
func TestFdWorkExchange_Ladder(t *testing.T) {
	e := NewFdWorkExchange("g0xyz", "1d", "dx", "k7ioc", "", "2A", "WWA")
	if e.State != fdwReporting || e.TheirClass != "2A" || e.TheirSection != "WWA" {
		t.Fatalf("after New: %+v", e)
	}
	if msg, ok := e.TxMessage(); !ok || msg != "K7IOC G0XYZ R 1D DX" {
		t.Fatalf("reporting TxMessage = %q (ok=%v), want \"K7IOC G0XYZ R 1D DX\"", msg, ok)
	}
	if _, ok := e.Advance("K7IOC G0XYZ R 1D DX"); ok {
		t.Fatal("advanced on a non-roger")
	}
	next, ok := e.Advance("G0XYZ K7IOC RR73")
	if !ok || next.State != fdwRogering {
		t.Fatalf("after their RR73: %+v (ok=%v)", next, ok)
	}
	e = next
	if msg, ok := e.TxMessage(); !ok || msg != "K7IOC G0XYZ RR73" {
		t.Fatalf("rogering TxMessage = %q, want \"K7IOC G0XYZ RR73\"", msg)
	}
	if e = e.Sent(); e.State != fdwDone {
		t.Fatalf("after Sent: %v, want fdwDone", e.State)
	}
}

// FD work-a-caller through the daemon sequencer: a complete contact drives end-to-end
// and logs the worked caller's class/section.
func TestSequencer_FieldDayWorkCaller(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartWorkCallerFd("G0XYZ", "1D", "DX", "K7IOC", "", "2A", "WWA",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))
	require.True(t, s.Active())

	driveTheir(s, 30, nil)                                                 // send our R+exchange
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K7IOC RR73", -12)}) // their RR73 → we RR73 + log

	require.Equal(t, []string{
		"K7IOC G0XYZ R 1D DX",
		"K7IOC G0XYZ RR73",
	}, r.sentMsgs())
	require.False(t, s.Active())
	require.Len(t, r.completed, 1)
	require.Equal(t, "K7IOC", r.completed[0].TheirCall)
	require.Equal(t, "2A", r.completed[0].Class)
	require.Equal(t, "WWA", r.completed[0].Section)
}
