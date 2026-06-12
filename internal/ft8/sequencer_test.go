package ft8

import (
	stderrors "errors"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// seqRecorder captures the sequencer's side effects (transmits, published
// statuses, completions) for assertions.
type seqRecorder struct {
	mu          sync.Mutex
	sent        []string
	offsets     []float64
	statuses    []QsoStatus
	completed   []CompletedQso
	transmitErr error
}

func (r *seqRecorder) transmit(msg string, off float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.transmitErr != nil {
		return r.transmitErr
	}
	r.sent = append(r.sent, msg)
	r.offsets = append(r.offsets, off)
	return nil
}

func (r *seqRecorder) publish(st QsoStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, st)
}

func (r *seqRecorder) sentMsgs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

func newTestSeq(r *seqRecorder) *Sequencer {
	s := newSequencer(r.transmit, r.publish, nil)
	s.onComplete = func(c CompletedQso) {
		r.mu.Lock()
		r.completed = append(r.completed, c)
		r.mu.Unlock()
	}
	return s
}

// dm builds a decoded message with a given SNR.
func dm(text string, snr int) goft8.DecodedMessage {
	return goft8.DecodedMessage{Text: text, SNR: snr}
}

// driveTheir simulates the decode of the worked station's slot starting at
// `sec` (which must be an even slot — the worked station's parity here), firing
// OnSlot ~1 s into our following slot (a valid late-start dt).
func driveTheir(s *Sequencer, sec int64, msgs []goft8.DecodedMessage) {
	ref := SlotRefFromTime(time.Unix(sec, 0).UTC())
	now := time.Unix(sec+slotSeconds+1, 0).UTC() // 1 s into the current (our) slot
	s.OnSlot(ref, msgs, now)
}

// The worked station transmits in even slots (sec 0/30/60/90 → period "even");
// we answer in the odd slots between. driveTheir uses even `sec`.
func TestSequencer_HappyPath(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)

	// Answer K1ABC's CQ heard in the even slot at epoch 0.
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42",
		time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074))
	require.True(t, s.Active())

	// Their re-CQ (or silence) → we send our call.
	driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
	// Their report to us (we measured them at -12) → we roger + report.
	driveTheir(s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
	// Their RR73 → we send 73 and the QSO completes.
	driveTheir(s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

	require.Equal(t, []string{
		"K1ABC G0XYZ IO91",
		"K1ABC G0XYZ R-12",
		"K1ABC G0XYZ 73",
	}, r.sentMsgs())

	for _, o := range r.offsets {
		require.Equal(t, 1500.0, o)
	}
	require.False(t, s.Active(), "QSO should be idle after the 73")
	require.Len(t, r.completed, 1)
	require.Equal(t, "K1ABC", r.completed[0].TheirCall)
	require.Equal(t, "FN42", r.completed[0].TheirGrid)
	require.Equal(t, -10, r.completed[0].TheirReport) // they sent us -10
	require.Equal(t, -12, r.completed[0].OurReport)   // we sent R-12
	require.Equal(t, 14.074, r.completed[0].DialFreqMHz)
	require.Equal(t, 1500.0, r.completed[0].OffsetHz)
}

func TestSequencer_OnlyTransmitsOppositeParity(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074))

	// A slot of OUR parity (odd, sec=15) just decoded → nothing to send.
	ref := SlotRefFromTime(time.Unix(15, 0).UTC())
	s.OnSlot(ref, nil, time.Unix(15+slotSeconds+1, 0).UTC())
	require.Empty(t, r.sentMsgs(), "must not transmit off a our-parity slot")

	// A their-parity slot does trigger our call.
	driveTheir(s, 30, nil)
	require.Equal(t, []string{"K1ABC G0XYZ IO91"}, r.sentMsgs())
}

func TestSequencer_LateStartGuardSkips(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074))

	// Fire OnSlot too late into our slot (6 s in > txLateWindowSec 4.5 s) → skip.
	ref := SlotRefFromTime(time.Unix(30, 0).UTC())
	s.OnSlot(ref, nil, time.Unix(30+slotSeconds+6, 0).UTC())
	require.Empty(t, r.sentMsgs(), "a too-late slot must be skipped")
	require.True(t, s.Active(), "skipping a slot does not end the QSO")
}

func TestSequencer_AbandonsAfterMaxRepeats(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.maxRepeats = 2
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074))

	// No answer ever (empty their-slots): call, call, then abandon.
	driveTheir(s, 30, nil) // repeat 1
	driveTheir(s, 60, nil) // repeat 2
	driveTheir(s, 90, nil) // would be repeat 3 > max → abandon, no transmit
	require.Equal(t, []string{"K1ABC G0XYZ IO91", "K1ABC G0XYZ IO91"}, r.sentMsgs())
	require.False(t, s.Active(), "abandons after max unanswered repeats")
}

func TestSequencer_StartErrors(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)

	require.ErrorIs(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", slot, 0, 14.074), ErrNoOffset)
	require.Error(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", "not-a-time", 1500, 14.074))

	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", slot, 1500, 14.074))
	require.ErrorIs(t, s.StartQso("G0XYZ", "IO91", "W2XYZ", "", slot, 1500, 14.074), ErrQsoInProgress)
}

func TestSequencer_Abandon(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074))
	s.Abandon()
	require.False(t, s.Active())
	// Idle: a their-slot now does nothing.
	driveTheir(s, 30, nil)
	require.Empty(t, r.sentMsgs())
}

func TestSequencer_AbandonsWhenDisarmedMidQso(t *testing.T) {
	r := &seqRecorder{transmitErr: ErrTxNotArmed} // simulate TX disarmed under us
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "", time.Unix(0, 0).UTC().Format(time.RFC3339), 1500, 14.074))
	driveTheir(s, 30, nil) // transmit returns ErrTxNotArmed → abandon
	require.False(t, s.Active(), "a not-armed transmit abandons the QSO")
	require.True(t, stderrors.Is(r.transmitErr, ErrTxNotArmed))
}
