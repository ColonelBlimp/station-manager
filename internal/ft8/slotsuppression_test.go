package ft8

/*
   Ship-gate finding 4 (docs/reviews/ft8-logging-gaps.md): dial-moved slot
   suppression is silent.

   BEHAVIOUR (the finding's confusable-state clause, operator-approved in the
   gaps doc):

       When a slot is suppressed — dial moved through its window, or a
       CAT-attached dial that could not be read (unplaceable) — smd.log
       carries ONE Info line naming the slot, WHICH rule fired, and what was
       withheld. Distinguishable from a genuinely quiet band (no suppression
       line — the empty decode publishes as usual) and from a stalled decoder
       (nothing at all: the suppression line itself proves the loop is alive
       and CHOOSING). A TX slot logs no Info line — our own transmission is
       expected, not an anomaly; it stays on the per-slot Debug record.

   Precedent for the stakes, from the doc: the dial guard's SESSION half had
   this exact hole, and the first on-air read of a WORKING guard was "moving
   the dial does not stop TX" — a log dive was needed to prove it had
   (2026-07-27). The per-slot half got nothing until this.

   The two rules withhold DIFFERENT things and the line must say so: a dial
   move suppresses decode + sequencer + occupancy; unplaceable suppresses
   occupancy only (Band Activity still shows the decodes). Severity is Info
   per the doc ("rate is bounded by slots, so Info is affordable"); the
   existing every-slot "ft8 slot processed" Debug record is unchanged and is
   NOT the fix — Debug is filtered at the daemon's default level, so the
   operator's production log stayed blank exactly where it needed to speak.
*/

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// suppressionLines returns the captured ft8-slot-suppressed records.
func suppressionLines(buf *bytes.Buffer) []string {
	var out []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, `"message":"ft8: slot suppressed"`) {
			out = append(out, line)
		}
	}
	return out
}

func newSuppressionHarness(t *testing.T) (*Service, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	s := newService(types.Ft8Config{Enabled: true}, logging.NewForWriter(buf), newFakeSource())
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return s, buf
}

// G1 — A DIAL-MOVED SLOT SAYS SO AT INFO, AND A QUIET SLOT SAYS NOTHING. The
// same run feeds both, so the assertion is the DISTINCTION, not "a line was
// emitted": exactly one suppression record, carrying the moved slot's ref and
// rule, at info level.
func TestSlotSuppression_DialMovedLogsQuietSlotDoesNot(t *testing.T) {
	s, buf := newSuppressionHarness(t)

	ch := make(chan Slot, 2)
	ch <- Slot{ // a genuinely quiet slot: decodes empty, published as usual
		StartUTC: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Samples:  make([]int16, 1000),
	}
	ch <- Slot{ // the operator tuned through this window
		StartUTC:    time.Date(2026, 8, 6, 12, 0, 15, 0, time.UTC),
		Samples:     make([]int16, 1000),
		DialChanged: true,
		DialTracked: true,
		DialMHz:     14.074,
	}
	close(ch)
	s.decodeLoop(ch, nil)

	lines := suppressionLines(buf)
	if len(lines) != 1 {
		t.Fatalf("suppression lines = %d, want exactly 1 (the quiet slot must stay silent):\n%s",
			len(lines), strings.Join(lines, "\n"))
	}
	l := lines[0]
	for _, want := range []string{
		`"level":"info"`,
		`"rule":"dial_moved"`,
		`"suppressed":"decode+sequencer+occupancy"`,
		`"slot":"2026-08-06T12:00:15Z"`,
	} {
		if !strings.Contains(l, want) {
			t.Errorf("suppression line missing %s:\n%s", want, l)
		}
	}
}

// G2 — UNPLACEABLE IS A DIFFERENT RULE WITH A DIFFERENT SCOPE, and the line
// distinguishes them: occupancy only, decodes still published.
func TestSlotSuppression_UnplaceableNamesItsNarrowerScope(t *testing.T) {
	s, buf := newSuppressionHarness(t)

	ch := make(chan Slot, 1)
	ch <- Slot{ // CAT attached, dial unreadable through the window
		StartUTC:    time.Date(2026, 8, 6, 12, 0, 30, 0, time.UTC),
		Samples:     make([]int16, 1000),
		DialTracked: true,
		DialMHz:     0,
	}
	close(ch)
	s.decodeLoop(ch, nil)

	lines := suppressionLines(buf)
	if len(lines) != 1 {
		t.Fatalf("suppression lines = %d, want 1", len(lines))
	}
	l := lines[0]
	for _, want := range []string{`"rule":"unplaceable"`, `"suppressed":"occupancy"`} {
		if !strings.Contains(l, want) {
			t.Errorf("suppression line missing %s:\n%s", want, l)
		}
	}
}

// G3 — A TX SLOT IS EXPECTED, NOT AN ANOMALY: no Info suppression line. The
// operator transmitted; telling them so at Info every other slot of a run
// would bury the two lines that matter. It stays on the Debug record.
func TestSlotSuppression_TxSlotStaysSilent(t *testing.T) {
	s, buf := newSuppressionHarness(t)

	start := time.Date(2026, 8, 6, 12, 0, 45, 0, time.UTC)
	s.markTxSlot(start)

	ch := make(chan Slot, 1)
	ch <- Slot{StartUTC: start, Samples: make([]int16, 1000), DialTracked: true, DialMHz: 14.074}
	close(ch)
	s.decodeLoop(ch, nil)

	if lines := suppressionLines(buf); len(lines) != 0 {
		t.Fatalf("a TX slot must not log a suppression line, got:\n%s", strings.Join(lines, "\n"))
	}
}
