package ft8

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

/*
	STALE-DECODE REFUSAL — found in dogfooding, 2026-07-31.

	WHAT HAPPENED. UA4FKT's last decode of the night was 01:27:45 UTC. At 01:33:16
	— five and a half minutes later — the operator clicked their row in Band
	Activity and SM transmitted a full six-rung ladder at a station that had long
	since left the air. The row was still on screen because Band Activity retains
	by COUNT (historyMax 100), never by AGE, and exactly ONE decode had arrived on
	the whole band in that window: the cap evicted nothing.

	ACCEPTANCE CRITERION (operator-approved 2026-07-31, threshold theirs):

	  When I click a row whose decode is older than THREE MINUTES, no RF is keyed
	  and I am told the decode is stale — and I can tell that apart from "the click
	  didn't register" and from "the rig refused".

	WHY THE DAEMON AND NOT ONLY THE SPA. Greying the row (Ft8BandActivity) is the
	half the operator sees, and it stops the click being made at all. It is not the
	guarantee: any other client, and the render-versus-click race in this one, can
	still ask. The daemon already HAS the fact — every Start* entry point parses
	the decode's slot_utc off the wire, and until now used it only to derive the
	caller's parity and threw the age away. Enforcing where the fact already lives
	costs one shared helper.

	THE TABLE IS THE POINT. Six entry points parse a slot, and a guard applied to
	the one that happened to bite is a guard the next path evades silently. They
	are enumerated here rather than sampled:

	    StartQso  StartQsoFd  StartQsoT4  StartWorkCaller  StartWorkCallerFd
	    StartWorkCallerT4

	StartCallCq is deliberately absent and that is not an oversight: a CQ answers
	no decode, takes no slot_utc, and has nothing to be stale about.

	EACH CASE CARRIES ITS OWN FRESH ROW, and that is what makes the stale row mean
	anything. These calls have long argument lists; if one were malformed it would
	fail for its own reason and a test asserting merely "an error" would pass while
	proving nothing. The fresh row must return nil — that is the proof the
	arguments are good — and only then does the stale row's ErrStaleDecode say the
	guard fired.
*/

// startFn calls one Start* entry point with a given slot time, on a sequencer
// whose `now` is fixed. Every one takes theirSlotUTC in the same RFC3339 shape.
type startFn func(s *Sequencer, slotUTC string, now time.Time) error

func staleDecodeEntryPoints() map[string]startFn {
	return map[string]startFn{
		"StartQso": func(s *Sequencer, slot string, now time.Time) error {
			return s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now)
		},
		"StartQsoFd": func(s *Sequencer, slot string, now time.Time) error {
			return s.StartQsoFd("G0XYZ", "1D", "MDC", "K1ABC", "FN42", -12, slot, 1500, 14.074, now)
		},
		"StartQsoT4": func(s *Sequencer, slot string, now time.Time) error {
			return s.StartQsoT4("G0XYZ", "PJ4/NA2AA", "", -12, slot, 1500, 14.074, now)
		},
		"StartWorkCaller": func(s *Sequencer, slot string, now time.Time) error {
			return s.StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, slot, 1500, 14.074, now)
		},
		"StartWorkCallerFd": func(s *Sequencer, slot string, now time.Time) error {
			return s.StartWorkCallerFd("G0XYZ", "1D", "MDC", "K1ABC", "FN42", "2A", "EMA", -12,
				slot, 1500, 14.074, now)
		},
		"StartWorkCallerT4": func(s *Sequencer, slot string, now time.Time) error {
			return s.StartWorkCallerT4("G0XYZ", "PJ4/NA2AA", "", -12, slot, 1500, 14.074, now)
		},
	}
}

// E1 — THE CRITERION, on every entry point. A decode older than the limit is
// refused with a distinct sentinel, and nothing is transmitted.
//
// A DISTINCT sentinel, not a generic failure, because the criterion's third
// clause is that the operator can tell this apart from the rig refusing: the
// sentinel is what lets the API layer say "that decode is stale" instead of
// "could not start the QSO".
func TestStaleDecode_EveryStartPathRefusesAnOldSlot(t *testing.T) {
	for name, start := range staleDecodeEntryPoints() {
		t.Run(name, func(t *testing.T) {
			now := time.Unix(600, 0).UTC()

			// The fresh row proves the arguments are valid — without it a malformed
			// call would fail for its own reason and the stale row would prove nothing.
			fresh := time.Unix(570, 0).UTC().Format(time.RFC3339) // 30 s old
			r := &seqRecorder{}
			require.NoError(t, start(newTestSeq(r), fresh, now),
				"fixture: a fresh slot must start normally, else the stale case is meaningless")

			// Same call, same arguments, one field older than the limit.
			stale := time.Unix(600-int64(staleDecodeLimit.Seconds())-1, 0).UTC().Format(time.RFC3339)
			r2 := &seqRecorder{}
			s2 := newTestSeq(r2)
			err := start(s2, stale, now)

			require.ErrorIs(t, err, ErrStaleDecode)
			require.False(t, s2.Active(), "a refused start must leave no session behind")
			require.Empty(t, r2.sentMsgs(), "no RF may be keyed at a station that has left the air")
		})
	}
}

// E2 — THE BOUNDARY, stated so the threshold cannot drift unnoticed. A decode
// exactly at the limit is still workable; one second past it is not. The
// operator's number is three minutes, and this is where that lives.
func TestStaleDecode_BoundaryIsThreeMinutes(t *testing.T) {
	require.Equal(t, 3*time.Minute, staleDecodeLimit,
		"the operator set three minutes (2026-07-31); changing it is their call, not a tuning detail")

	now := time.Unix(1000, 0).UTC()
	at := time.Unix(1000-int64(staleDecodeLimit.Seconds()), 0).UTC().Format(time.RFC3339)
	past := time.Unix(1000-int64(staleDecodeLimit.Seconds())-1, 0).UTC().Format(time.RFC3339)

	require.NoError(t, newTestSeq(&seqRecorder{}).
		StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, at, 1500, 14.074, now),
		"a decode exactly at the limit is still workable")
	require.ErrorIs(t, newTestSeq(&seqRecorder{}).
		StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, past, 1500, 14.074, now), ErrStaleDecode)
}

// E3 — a malformed slot is still a PARSE failure, not a staleness one. The two
// reach the operator as different messages (400 bad input versus "that decode has
// aged out"), and folding them together would send someone hunting a clock
// problem for a typo, or the reverse.
func TestStaleDecode_MalformedSlotIsNotReportedAsStale(t *testing.T) {
	err := newTestSeq(&seqRecorder{}).
		StartWorkCaller("G0XYZ", "K1ABC", "FN42", -12, "not-a-timestamp", 1500, 14.074, time.Unix(600, 0).UTC())

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrStaleDecode)
}
