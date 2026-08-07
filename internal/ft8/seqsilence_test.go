package ft8

/*
   Ship-gate findings 5 + 11 (docs/reviews/ft8-logging-gaps.md) — the rest of
   the "a slot passed and the log cannot say why" cluster.

   FINDING 5 — the stalled-caller exclusions are invisible. The STALL is
   logged (Warn, callsign + attempts); the resulting cool-off and round
   exclusion are not, so "SM ignored a caller it can hear" and "the band went
   quiet" look identical from the operator's chair — and stallCooloffSlots=5,
   flagged in-code as the operator's number, cannot be judged without a line.
   Record (per the doc): callsign + expiry when the cool-off is SET; callsign
   + reason when a pick is SKIPPED for either exclusion. The file already
   agrees "why we did not answer this station" deserves a line — the
   unencodable-reply skip has one at Info eleven lines below; these join it.

   FINDING 11 — three distinct reasons a qualifying slot passes without RF,
   all previously silent and indistinguishable:
     - the decode landed TOO LATE (dt > txLateWindowSec): expected, the thing
       ADR 0032's truncation budget exists for → Info;
     - dt < 0, "our slot has not started yet": a CLOCK or SLOT-REF fault, a
       completely different problem sharing the same silent branch → Warn;
     - the same-slot DEDUP (a rung already went out in this physical slot):
       expected mechanics, RF DID leave this slot → Debug.
   Confusable with: a quiet band, a session waiting on the partner, or a
   wedged sequencer — "no RF and no explanation" all four ways. The record is
   factored into two Sequencer helpers with a per-site path tag, because 15
   hand-copied lines across four files is how the levels drift apart.

   All rules below are in confusable-state form: the same run feeds the
   normal case and the suppressed case, and asserts the DISTINCTION — a line
   for one, silence (or a different message) for the other.
*/

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/stretchr/testify/require"
)

// buf is any String()-bearing sink (*bytes.Buffer, or decodelogloss_test's
// concurrency-safe syncBuf for records emitted off the test goroutine).
func logLines(buf fmt.Stringer, message string) []string {
	var out []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, `"message":"`+message+`"`) {
			out = append(out, line)
		}
	}
	return out
}

// C1 — SETTING THE COOL-OFF SAYS SO, with the callsign and the expiry: the
// operator's stallCooloffSlots number is unjudgeable without both ends of
// the interval on record.
func TestSeqSilence_CooloffSetIsLogged(t *testing.T) {
	buf := &bytes.Buffer{}
	s := newTestSeqLogged(&seqRecorder{}, logging.NewForWriter(buf))

	s.mu.Lock()
	s.coolOffStalledCallerLocked("DL9UW", time.Unix(100, 0).UTC())
	s.mu.Unlock()

	lines := logLines(buf, "ft8 seq: stalled caller cooled off — excluded from selection")
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], `"their_call":"DL9UW"`)
	// 100 s + 5 slots × 15 s = 175 s — the expiry is on the record.
	require.Contains(t, lines[0], `"until":"1970-01-01T00:02:55Z"`)
	require.Contains(t, lines[0], `"level":"info"`)
}

// C2 — A COOLED CANDIDATE'S SKIP IS LOGGED AND THE FRESH ONE'S PICK IS NOT:
// the distinction between "SM ignored a caller" and "nobody else was
// calling". Two answerers in one slot; the cooled one draws exactly one
// reasoned skip line, the fresh one is worked with no skip line at all.
func TestSeqSilence_CooloffSkipNamesCallAndReason(t *testing.T) {
	buf := &bytes.Buffer{}
	r := &seqRecorder{}
	s := newTestSeqLogged(r, logging.NewForWriter(buf))
	require.NoError(t, s.StartCallCq("7q5mlv", "kh78", 2700, 28.074, "auto_first", "",
		time.Unix(0, 0).UTC()))

	s.mu.Lock()
	s.coolOffStalledCallerLocked("DL9UW", time.Unix(40, 0).UTC()) // until 115 s
	s.mu.Unlock()

	driveTheir(s, 30, []goft8.DecodedMessage{
		dm("7Q5MLV DL9UW JO41", -8),
		dm("7Q5MLV G0XYZ IO91", -10),
	})

	require.NotEmpty(t, r.sentMsgs(), "the fresh answerer must be worked")
	require.Contains(t, r.sentMsgs()[len(r.sentMsgs())-1], "G0XYZ",
		"the pick must fall to the un-excluded answerer")

	lines := logLines(buf, "ft8 seq: skipping answerer — excluded")
	require.Len(t, lines, 1, "exactly one skip line: the excluded call, not the picked one")
	require.Contains(t, lines[0], `"answerer":"DL9UW"`)
	require.Contains(t, lines[0], `"reason":"stall_cooloff"`)
}

// C3 — THE ROUND EXCLUSION IS A DIFFERENT REASON AND THE LINE SAYS WHICH:
// tried-and-stalled THIS round reads differently from cooled-off moments
// ago, and conflating them would hide which mechanism held a station out.
func TestSeqSilence_RoundExclusionSkipNamesItsOwnReason(t *testing.T) {
	buf := &bytes.Buffer{}
	r := &seqRecorder{}
	s := newTestSeqLogged(r, logging.NewForWriter(buf))
	require.NoError(t, s.StartCallCq("7q5mlv", "kh78", 2700, 28.074, "auto_first", "",
		time.Unix(0, 0).UTC()))

	s.mu.Lock()
	s.stalledCalls = []string{"DL9UW"}
	s.mu.Unlock()

	driveTheir(s, 30, []goft8.DecodedMessage{
		dm("7Q5MLV DL9UW JO41", -8),
		dm("7Q5MLV G0XYZ IO91", -10),
	})

	lines := logLines(buf, "ft8 seq: skipping answerer — excluded")
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], `"answerer":"DL9UW"`)
	require.Contains(t, lines[0], `"reason":"stalled_this_round"`)
}

// L1 — A TOO-LATE DECODE DEFERS WITH A LINE; A TIMELY SLOT TRANSMITS WITH
// NONE. Same session, both cases: the timely repeat produces RF and no
// deferral record, the late slot produces the record and no RF.
func TestSeqSilence_LateDeferralLoggedTimelySlotSilent(t *testing.T) {
	buf := &bytes.Buffer{}
	r := &seqRecorder{}
	s := newTestSeqLogged(r, logging.NewForWriter(buf))
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, nil) // timely: 1 s into our slot → transmits
	sentAfterTimely := len(r.sentMsgs())
	require.Positive(t, sentAfterTimely, "fixture: the timely slot must transmit")

	// 6 s into our slot > txLateWindowSec 4.5 → deferred, no RF.
	ref := SlotRefFromTime(time.Unix(60, 0).UTC())
	s.OnSlot(ref, nil, time.Unix(60+slotSeconds+6, 0).UTC())
	require.Len(t, r.sentMsgs(), sentAfterTimely, "the late slot must not transmit")

	lines := logLines(buf, "ft8 seq: rung deferred — decode landed too late to transmit this slot")
	require.Len(t, lines, 1, "one deferral line for the late slot, none for the timely one")
	require.Contains(t, lines[0], `"level":"info"`)
	require.Contains(t, lines[0], `"dt_sec":6`)
	require.Contains(t, lines[0], `"window_sec":4.5`)
	require.Contains(t, lines[0], `"path":`)
}

// L2 — A NEGATIVE SLOT OFFSET IS A FAULT, NOT LATENESS, AND THE RECORD IS
// DIFFERENT: different message, Warn not Info. The scheduler delivers slots
// after their window closes, so "our slot has not started" cannot happen on
// a healthy clock — sharing lateness's silent branch is what made the two
// indistinguishable.
func TestSeqSilence_NegativeOffsetWarnsDistinctly(t *testing.T) {
	buf := &bytes.Buffer{}
	r := &seqRecorder{}
	s := newTestSeqLogged(r, logging.NewForWriter(buf))
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	// now is BEFORE our tx slot's start (75 s): dt < 0.
	ref := SlotRefFromTime(time.Unix(60, 0).UTC())
	s.OnSlot(ref, nil, time.Unix(60+slotSeconds-1, 0).UTC())
	require.Empty(t, r.sentMsgs())

	warn := logLines(buf, "ft8 seq: rung skipped — slot offset negative (clock or slot-ref fault?)")
	require.Len(t, warn, 1)
	require.Contains(t, warn[0], `"level":"warn"`)
	require.Empty(t, logLines(buf, "ft8 seq: rung deferred — decode landed too late to transmit this slot"),
		"a fault must not masquerade as ordinary lateness")
}

// L3 — THE SAME-SLOT DEDUP IS EXPECTED MECHANICS AT DEBUG: RF already left
// this physical slot, so it is not a missing-RF mystery — but it must be
// DISTINGUISHABLE from both deferral records, or three reasons collapse
// into silence again the day someone greps for one of them.
func TestSeqSilence_SameSlotDedupLogsAtDebug(t *testing.T) {
	buf := &bytes.Buffer{}
	r := &seqRecorder{}
	s := newTestSeqLogged(r, logging.NewForWriter(buf))
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074, time.Unix(0, 0).UTC()))

	driveTheir(s, 30, nil) // transmits in slot 45
	sent := len(r.sentMsgs())
	require.Positive(t, sent)

	// The same their-slot delivered again inside the same physical tx slot —
	// the double-drive the dedup exists for (2026-07-20).
	s.OnSlot(SlotRefFromTime(time.Unix(30, 0).UTC()), nil, time.Unix(47, 0).UTC())
	require.Len(t, r.sentMsgs(), sent, "the dedup must hold: one rung per physical slot")

	lines := logLines(buf, "ft8 seq: rung dedup — already transmitted in this slot")
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], `"level":"debug"`)
	require.Contains(t, lines[0], `"slot":"1970-01-01T00:00:45Z"`)
}
