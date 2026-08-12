package ft8

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// #10 — A PRE-KEY DIAL SAFETY REFUSAL LOGS DISTINCTLY FROM AN AUDIO/KEYER FAILURE.
//
// preKeyDialCheck refuses INSIDE the launched TX goroutine, long after its caller
// returned, so its ErrTxDialUnknown / ErrTxSuperseded surfaced through the generic
// "ft8 tx: transmission failed" line — the SAME message an audio play error
// produces. "SM declined to key for safety" and "the audio device failed" demand
// different operator responses, and only the first is a working guard doing its job.
// The classification reuses the existing isDialRefusal predicate; the fn here just
// returns the outcome error, so no rig is keyed.
func TestStartTransmission_DialRefusalLogsDistinctFromFailure(t *testing.T) {
	newArmed := func(t *testing.T) (*Service, *logSink) {
		t.Helper()
		sink, logger := newLogSink()
		s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
		s.log = logger
		require.NoError(t, s.ArmTx(true))
		return s, sink
	}

	t.Run("a dial refusal logs its own message, not the generic one", func(t *testing.T) {
		s, sink := newArmed(t)
		require.NoError(t, s.startTransmission("CQ G0XYZ IO91", 1500, 14.074,
			func() bool { return true },
			func(context.Context, *TxController) error { return ErrTxDialUnknown },
			nil, nil))
		s.txWg.Wait()

		if _, ok := sink.record(t, "declined to key"); !ok {
			t.Fatal("a dial safety refusal must log a distinct message")
		}
		if _, ok := sink.record(t, "transmission failed"); ok {
			t.Fatal("a dial refusal must NOT use the generic transmission-failed message")
		}
	})

	t.Run("a genuine failure still logs the generic message", func(t *testing.T) {
		s, sink := newArmed(t)
		require.NoError(t, s.startTransmission("CQ G0XYZ IO91", 1500, 14.074,
			func() bool { return true },
			func(context.Context, *TxController) error { return errors.New("audio device failed") },
			nil, nil))
		s.txWg.Wait()

		if _, ok := sink.record(t, "transmission failed"); !ok {
			t.Fatal("a genuine audio/keyer failure must still log the generic message")
		}
		if _, ok := sink.record(t, "declined to key"); ok {
			t.Fatal("a non-refusal must NOT be mislabelled a safety refusal")
		}
	})
}
