package ft8

/*
   Ship-gate findings 7 + 8 (docs/reviews/ft8-logging-gaps.md) — silent
   degradation of stored and forwarded data.

   FINDING 7 — BuildQso/NewLoggedQso degrade fail-soft in four places, and
   every one lands on data that is stored and FORWARDED to QRZ/ClubLog/
   SM Cloud: durable, outbound, not correctable after the fact. Degrading is
   CORRECT (enrichment never blocks logging); degrading invisibly is the
   defect. The sharpest is the zero StartedAt fallback, whose own comment
   calls it "a path that failed to stamp a start" — a known defect indicator,
   swallowed. Record: Warn on each, with the input that failed to resolve;
   behaviour unchanged. The functions stay assembly-pure — the logger is an
   injected parameter (nil-safe), not a storage dependency.

   FINDING 8 — a completed exchange with no sink wired was discarded with no
   record, while finalrung.go logs "QSO complete" on the same path: the log
   AFFIRMED a QSO that was never handed anywhere. cmd/smd always wires the
   sink, so this is a wiring bug, not a runtime condition — Error.

   Confusable-state form throughout: the CLEAN build warns nothing (a
   degradation line on a healthy QSO would train the operator to ignore
   them), and each degraded input draws its own specific line.
*/

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

func warnStation() types.LoggingStation {
	return types.LoggingStation{Operator: "7Q5MLV", MyGridsquare: "KH78ka"}
}

func cleanCompleted() CompletedQso {
	return CompletedQso{
		TheirCall:   "DL9UW",
		TheirGrid:   "JO41",
		DialFreqMHz: 14.074,
		StartedAt:   time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		AntPath:     antPathShort,
	}
}

// Q1 — THE CLEAN BUILD WARNS NOTHING: every field resolves, so any
// degradation line here would be noise that buries the real ones.
func TestBuildQso_CleanBuildWarnsNothing(t *testing.T) {
	buf := &bytes.Buffer{}
	q := BuildQso(cleanCompleted(), warnStation(), 1,
		time.Date(2026, 8, 7, 12, 1, 0, 0, time.UTC), logging.NewForWriter(buf))

	require.Equal(t, "20m", q.Band, "fixture: the dial must resolve")
	require.NotEmpty(t, q.AntennaAzimuth, "fixture: both grids must resolve")
	require.NotContains(t, buf.String(), `"level":"warn"`)
}

// Q2 — AN UNRESOLVABLE DIAL WARNS WITH THE INPUT, and the fail-soft outcome
// is unchanged: the QSO still builds, BAND empty. Without the line, an empty
// BAND on a forwarded row is indistinguishable from an odd dial, a parser
// bug, or a config problem.
func TestBuildQso_UnresolvedBandWarns(t *testing.T) {
	buf := &bytes.Buffer{}
	c := cleanCompleted()
	c.DialFreqMHz = 0.001 // no amateur band resolves this dial
	q := BuildQso(c, warnStation(), 1, time.Date(2026, 8, 7, 12, 1, 0, 0, time.UTC),
		logging.NewForWriter(buf))

	require.Empty(t, q.Band, "fail-soft behaviour must not change")
	lines := strings.Split(buf.String(), "\n")
	var hit string
	for _, l := range lines {
		if strings.Contains(l, "ft8: QSO band unresolved from dial") {
			hit = l
		}
	}
	require.NotEmpty(t, hit, "the degradation must be on record")
	require.Contains(t, hit, `"level":"warn"`)
	require.Contains(t, hit, `"freq":"0.001000"`)
	require.Contains(t, hit, `"their_call":"DL9UW"`)
}

// Q3 — THE ZERO-START FALLBACK IS A KNOWN DEFECT INDICATOR AND SAYS SO. Its
// own comment names it "a path that failed to stamp a start"; if it ever
// fires in production the record must exist.
func TestBuildQso_ZeroStartWarns(t *testing.T) {
	buf := &bytes.Buffer{}
	c := cleanCompleted()
	c.StartedAt = time.Time{}
	now := time.Date(2026, 8, 7, 12, 1, 0, 0, time.UTC)
	q := BuildQso(c, warnStation(), 1, now, logging.NewForWriter(buf))

	require.Equal(t, "120100", q.TimeOn, "fail-soft fallback to the completion instant stands")
	require.Contains(t, buf.String(), "ft8: QSO start instant was never stamped")
	require.Contains(t, buf.String(), `"their_call":"DL9UW"`)
}

// Q4 — AN UNPARSEABLE GRID PAIR WARNS WITH BOTH GRIDS; ANT_AZ/DISTANCE stay
// unset (valid ADIF), but the reason is no longer invisible.
func TestBuildQso_GridPathFailureWarns(t *testing.T) {
	buf := &bytes.Buffer{}
	c := cleanCompleted()
	c.TheirGrid = "not-a-grid"
	q := BuildQso(c, warnStation(), 1, time.Date(2026, 8, 7, 12, 1, 0, 0, time.UTC),
		logging.NewForWriter(buf))

	require.Empty(t, q.AntennaAzimuth)
	require.Empty(t, q.Distance)
	require.Contains(t, buf.String(), "ft8: antenna path unresolved")
	require.Contains(t, buf.String(), `"their_grid":"not-a-grid"`)
	require.Contains(t, buf.String(), `"my_grid":"KH78ka"`)
}

// Q5 — NewLoggedQso's malformed-field degradations warn per field; a clean
// row warns nothing (same confusable rule as Q1).
func TestNewLoggedQso_MalformedFieldsWarn(t *testing.T) {
	clean := &bytes.Buffer{}
	q := BuildQso(cleanCompleted(), warnStation(), 1,
		time.Date(2026, 8, 7, 12, 1, 0, 0, time.UTC), logging.NewForWriter(clean))
	_ = NewLoggedQso(q, "uuid-1", logging.NewForWriter(clean))
	require.NotContains(t, clean.String(), `"level":"warn"`)

	buf := &bytes.Buffer{}
	bad := q
	bad.Freq = "garbage"
	bad.TimeOn = "9"
	bad.QsoDate = "202699"
	row := NewLoggedQso(bad, "uuid-2", logging.NewForWriter(buf))

	require.Zero(t, row.FreqHz, "fail-soft outcome unchanged")
	require.Contains(t, buf.String(), "ft8: logged-QSO field malformed")
	require.Contains(t, buf.String(), `"field":"freq"`)
	require.Contains(t, buf.String(), `"field":"time_on"`)
	require.Contains(t, buf.String(), `"field":"qso_date"`)
}

// Q6 — A COMPLETED EXCHANGE WITH NO SINK WIRED IS AN ERROR, NOT A SILENCE
// (finding 8): finalrung logs "QSO complete" on this same path, so without
// this line the log affirms a QSO that was never handed anywhere. The
// control half: with a sink wired, the sink receives and no error logs.
func TestOnComplete_NilSinkErrorsWiredSinkReceives(t *testing.T) {
	buf := &bytes.Buffer{}
	s := newService(types.Ft8Config{Enabled: true}, logging.NewForWriter(buf), newFakeSource())

	s.seq.onComplete(CompletedQso{TheirCall: "DL9UW"})
	require.Contains(t, buf.String(), "ft8: completed QSO discarded — no QSO sink wired")
	require.Contains(t, buf.String(), `"level":"error"`)
	require.Contains(t, buf.String(), `"their_call":"DL9UW"`)

	buf.Reset()
	got := make(chan CompletedQso, 1)
	s.SetQsoLogger(func(_ context.Context, c CompletedQso) { got <- c })
	s.seq.onComplete(CompletedQso{TheirCall: "G0XYZ"})
	require.NotContains(t, buf.String(), "no QSO sink wired")
	select {
	case c := <-got:
		require.Equal(t, "G0XYZ", c.TheirCall)
	case <-time.After(time.Second):
		t.Fatal("the wired sink never received the completion")
	}
}
