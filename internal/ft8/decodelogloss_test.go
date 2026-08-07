package ft8

/*
   Ship-gate findings 12 + 13 (docs/reviews/ft8-logging-gaps.md) — decode-log
   data loss that only became visible at Close, which since the 2026-08-06
   service-lifetime change means DAEMON SHUTDOWN: hours after the ALL.TXT
   record the operator would consult had already gone incomplete.

   FINDING 12 — enqueue drops a line on a full queue (a stalled disk) and
   bumps the counter with no log; the only warning lived in Close. Record:
   warn on the FIRST drop, then on each doubling of the running total
   (1, 2, 4, 8, …) — a growth threshold, per the finding's Record clause,
   chosen over a timer because it needs no goroutine and is deterministic.
   The backoff is load-bearing in BOTH directions: one line per drop would
   hand smd.log the very firehose the queue exists to absorb (~17 lines per
   15 s slot), and Close-only is the defect under repair. Close's total
   stays.

   FINDING 13 — run's deferred teardown discarded the final Flush and Close
   errors. The teardown flush is the one carrying everything buffered at
   exit — failure there is data loss with no retry behind it — yet it was
   the only silent flush in the file (the mid-session one already warns).
   Record: both errors at Warn, distinct messages ("final flush failed" ≠
   the mid-session "flush failed", because one is retryable and one is
   terminal).

   Confusable-state form: an enqueue the queue ABSORBS logs nothing (D1 —
   backpressure working is not loss), and a CLEAN shutdown logs nothing
   (E2 — so a shutdown line always means something was lost).

   The stalled-disk fixtures build the struct by hand: a DecodeLog whose
   writer goroutine never runs IS a stalled disk, distilled — the only
   deterministic way to fill the queue. enqueue/run touch nothing that
   construction path skips.
*/

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/stretchr/testify/require"
)

// newStalledDecodeLog is a DecodeLog whose writer goroutine is never started:
// the queue fills and never drains, deterministically.
func newStalledDecodeLog(buf *bytes.Buffer, capacity int) *DecodeLog {
	return &DecodeLog{
		lines: make(chan string, capacity),
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
		log:   logging.NewForWriter(buf),
	}
}

// D1 — THE FIRST DROP WARNS IMMEDIATELY; AN ABSORBED ENQUEUE NEVER DOES. The
// same run feeds both: two lines the queue accepts (no warn — backpressure
// absorbed is not loss), then the drop, which draws exactly one warn carrying
// the running total.
func TestDecodeLogLoss_FirstDropWarnsAbsorbedEnqueueDoesNot(t *testing.T) {
	buf := &bytes.Buffer{}
	d := newStalledDecodeLog(buf, 2)

	d.enqueue("line-1\n")
	d.enqueue("line-2\n")
	require.Empty(t, logLines(buf, "ft8: decode log dropping lines (queue full / slow disk)"),
		"an enqueue the queue absorbed must not warn")

	d.enqueue("line-3\n") // queue full — this one is lost
	lines := logLines(buf, "ft8: decode log dropping lines (queue full / slow disk)")
	require.Len(t, lines, 1, "the first drop must be on record immediately, not at Close")
	require.Contains(t, lines[0], `"level":"warn"`)
	require.Contains(t, lines[0], `"dropped":1`)
}

// D2 — THE RE-WARN IS A DOUBLING BACKOFF, NOT A FIREHOSE: five consecutive
// drops draw warns at totals 1, 2 and 4 only. One line per drop would hand
// smd.log the load the queue exists to absorb; only-at-Close is the defect
// this fixes. Both wrong implementations fail this fixture (5 lines / 0
// lines vs the required 3).
func TestDecodeLogLoss_RewarnsOnDoublingOnly(t *testing.T) {
	buf := &bytes.Buffer{}
	d := newStalledDecodeLog(buf, 0) // every enqueue drops

	for i := 0; i < 5; i++ {
		d.enqueue("lost\n")
	}

	lines := logLines(buf, "ft8: decode log dropping lines (queue full / slow disk)")
	require.Len(t, lines, 3, "warns at totals 1, 2, 4 — not per-drop, not silent")
	require.Contains(t, lines[0], `"dropped":1`)
	require.Contains(t, lines[1], `"dropped":2`)
	require.Contains(t, lines[2], `"dropped":4`)
}

// failingSink errors every Write and Close — the disk that dies before
// teardown.
type failingSink struct{ writeErr, closeErr error }

func (f *failingSink) Write([]byte) (int, error) { return 0, f.writeErr }
func (f *failingSink) Close() error              { return f.closeErr }

// E1 — A TEARDOWN THAT LOSES DATA SAYS SO: the deferred final flush and the
// sink close each warn with their own message and error. A line is buffered
// when quit arrives, so the teardown flush is the one carrying data — the
// exact case the silent `_ =` discarded. (The mid-session flush warn may
// also fire depending on which ready select case run picks first; this rule
// binds only the teardown records, which fire on both paths.)
func TestDecodeLogLoss_FailedTeardownFlushAndCloseWarn(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := &failingSink{writeErr: errors.New("disk gone"), closeErr: errors.New("close refused")}
	d := &DecodeLog{
		lines: make(chan string, 8),
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
		w:     bufio.NewWriter(sink),
		wc:    sink,
		log:   logging.NewForWriter(buf),
	}
	d.enqueue("20260807_120000 -7 0.3 1234 ~ CQ DL9UW JO41\n")
	close(d.quit)
	d.run() // synchronous: drains, then the deferred teardown fires

	flush := logLines(buf, "ft8: decode log final flush failed — buffered lines lost")
	require.Len(t, flush, 1)
	require.Contains(t, flush[0], `"level":"warn"`)
	require.Contains(t, flush[0], `"error":"disk gone"`)

	cls := logLines(buf, "ft8: decode log close failed")
	require.Len(t, cls, 1)
	require.Contains(t, cls[0], `"error":"close refused"`)
}

// E2 — A CLEAN SHUTDOWN LOGS NO WARN AT ALL: the real open → write → Close
// path against a healthy temp file. This is what makes E1's lines mean
// something — a teardown record always indicates loss, never routine exit.
func TestDecodeLogLoss_CleanShutdownWarnsNothing(t *testing.T) {
	buf := &bytes.Buffer{}
	path := t.TempDir() + "/ft8-all.txt"
	d := openDecodeLog(path, "", logging.NewForWriter(buf))
	require.NotNil(t, d)

	d.WriteRx(time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), []goft8.DecodedMessage{
		{Text: "CQ DL9UW JO41", SNR: -7, DTSec: 0.3, FreqHz: 1234},
	})
	d.Close()

	require.NotContains(t, buf.String(), `"level":"warn"`)
}
