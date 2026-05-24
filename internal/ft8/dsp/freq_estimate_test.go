package dsp

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// freqEstimateTolerance is the per-test absolute tolerance on the
// recovered frequency offset, in Hz. Clean synthetic signal + 18
// coherent pair products → the estimator typically lands within
// 0.005 Hz; 0.02 Hz tolerance leaves comfortable headroom for
// float-precision noise without admitting genuine algorithm bugs.
const freqEstimateTolerance = 0.02

// TestEstimateFreqOffsetCostas_ZeroOffset pins that a signal exactly
// on the candidate's nominal frequency produces ~0 Hz offset.
func TestEstimateFreqOffsetCostas_ZeroOffset(t *testing.T) {
	msg := make([]byte, codec.MessageBits)
	audio := Synthesize(msg, 1500.0, 0)
	got := EstimateFreqOffsetCostas(audio, 1500.0, 0)
	if math.Abs(got) > freqEstimateTolerance {
		t.Errorf("zero-offset signal: got %.5f Hz, want ~0", got)
	}
}

// TestEstimateFreqOffsetCostas_PositiveOffset pins that a signal
// 0.5 Hz above the candidate's nominal frequency is recovered as
// approximately +0.5 Hz.
func TestEstimateFreqOffsetCostas_PositiveOffset(t *testing.T) {
	msg := make([]byte, codec.MessageBits)
	audio := Synthesize(msg, 1500.5, 0)
	got := EstimateFreqOffsetCostas(audio, 1500.0, 0)
	if math.Abs(got-0.5) > freqEstimateTolerance {
		t.Errorf("+0.5 Hz signal: got %.5f Hz, want ~0.5", got)
	}
}

// TestEstimateFreqOffsetCostas_NegativeOffset pins recovery of a
// negative residual offset (signal below the candidate's nominal
// frequency).
func TestEstimateFreqOffsetCostas_NegativeOffset(t *testing.T) {
	msg := make([]byte, codec.MessageBits)
	audio := Synthesize(msg, 1499.7, 0)
	got := EstimateFreqOffsetCostas(audio, 1500.0, 0)
	if math.Abs(got-(-0.3)) > freqEstimateTolerance {
		t.Errorf("-0.3 Hz signal: got %.5f Hz, want ~-0.3", got)
	}
}

// TestEstimateFreqOffsetCostas_NearUnambiguousLimit pins that
// offsets approaching the ±3.125 Hz ambiguity boundary still resolve
// correctly. 1.5 Hz is the worst-case sync-grid residual (half a
// 3.125 Hz bin) — the primary case this estimator targets.
func TestEstimateFreqOffsetCostas_NearUnambiguousLimit(t *testing.T) {
	for _, off := range []float64{1.0, 1.5, -1.0, -1.5} {
		msg := make([]byte, codec.MessageBits)
		audio := Synthesize(msg, 1500.0+off, 0)
		got := EstimateFreqOffsetCostas(audio, 1500.0, 0)
		if math.Abs(got-off) > freqEstimateTolerance {
			t.Errorf("offset %.2f Hz: got %.5f Hz, want ~%.2f", off, got, off)
		}
	}
}

// TestEstimateFreqOffsetCostas_NonZeroDT pins that recovery is
// independent of the slot-relative timing offset dt. The estimator
// uses the supplied dt to locate anchors in the audio; matching dt
// in synth + estimate must yield the same offset estimate as dt=0.
func TestEstimateFreqOffsetCostas_NonZeroDT(t *testing.T) {
	msg := make([]byte, codec.MessageBits)
	audio := Synthesize(msg, 1500.5, 0.2)
	got := EstimateFreqOffsetCostas(audio, 1500.0, 0.2)
	if math.Abs(got-0.5) > freqEstimateTolerance {
		t.Errorf("dt=0.2 s, +0.5 Hz signal: got %.5f Hz, want ~0.5", got)
	}
}

// TestEstimateFreqOffsetCostas_DifferentMessage pins that the
// estimate is independent of the data payload — only the 21 Costas
// anchors contribute. Two different non-zero messages at the same
// freq offset must yield matching estimates.
func TestEstimateFreqOffsetCostas_DifferentMessage(t *testing.T) {
	makeMsg := func(seed byte) []byte {
		msg := make([]byte, codec.MessageBits)
		for i := range msg {
			msg[i] = (seed + byte(i)) & 1
		}
		return msg
	}
	audioA := Synthesize(makeMsg(0), 1500.5, 0)
	audioB := Synthesize(makeMsg(1), 1500.5, 0)
	gotA := EstimateFreqOffsetCostas(audioA, 1500.0, 0)
	gotB := EstimateFreqOffsetCostas(audioB, 1500.0, 0)
	if math.Abs(gotA-gotB) > freqEstimateTolerance {
		t.Errorf("payload-dependent estimate: msgA=%.5f Hz, msgB=%.5f Hz", gotA, gotB)
	}
	if math.Abs(gotA-0.5) > freqEstimateTolerance {
		t.Errorf("msgA: got %.5f Hz, want ~0.5", gotA)
	}
}

// TestEstimateFreqOffsetCostas_ZeroAudio pins the degenerate case:
// silent audio produces 0 (no coherent energy to lock onto).
func TestEstimateFreqOffsetCostas_ZeroAudio(t *testing.T) {
	audio := make([]float32, NMAX)
	got := EstimateFreqOffsetCostas(audio, 1500.0, 0)
	if got != 0 {
		t.Errorf("zero audio: got %.5f Hz, want exactly 0", got)
	}
}

// TestEstimateFreqOffsetCostas_ShortAudio pins that audio too short
// to cover the TX window returns 0 without panicking.
func TestEstimateFreqOffsetCostas_ShortAudio(t *testing.T) {
	audio := make([]float32, 1000)
	got := EstimateFreqOffsetCostas(audio, 1500.0, 0)
	if got != 0 {
		t.Errorf("short audio: got %.5f Hz, want exactly 0", got)
	}
}

// TestEstimateFreqOffsetCostas_NegativeTxStart pins that a dt that
// would push TX start before sample 0 returns 0 cleanly.
func TestEstimateFreqOffsetCostas_NegativeTxStart(t *testing.T) {
	audio := make([]float32, NMAX)
	got := EstimateFreqOffsetCostas(audio, 1500.0, -1.0)
	if got != 0 {
		t.Errorf("negative tx start: got %.5f Hz, want exactly 0", got)
	}
}
