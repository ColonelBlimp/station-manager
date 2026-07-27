package ft8

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/stretchr/testify/require"
)

// fakeKeyer records KeyTx/UnkeyTx calls and can be told to fail the key.
type fakeKeyer struct {
	mu       sync.Mutex
	keyErr   error
	keyMode  string
	keyCount int
	unkeyN   int
	notReady bool // when true, TxReady reports false
}

func (k *fakeKeyer) KeyTx(_ context.Context, mode string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.keyErr != nil {
		return k.keyErr
	}
	k.keyCount++
	k.keyMode = mode
	return nil
}

func (k *fakeKeyer) UnkeyTx(_ context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.unkeyN++
	return nil
}

func (k *fakeKeyer) keys() int   { k.mu.Lock(); defer k.mu.Unlock(); return k.keyCount }
func (k *fakeKeyer) unkeys() int { k.mu.Lock(); defer k.mu.Unlock(); return k.unkeyN }

// ready reports the fake's TX readiness; defaults to true (a connected rig).
func (k *fakeKeyer) TxReady() bool { k.mu.Lock(); defer k.mu.Unlock(); return !k.notReady }

// setNotReady toggles readiness mid-test (e.g. the rig disconnecting after arming).
func (k *fakeKeyer) setNotReady(v bool) { k.mu.Lock(); k.notReady = v; k.mu.Unlock() }

// fakePlayer records Play/Stop and hands back a done channel the test controls.
type fakePlayer struct {
	mu      sync.Mutex
	playErr error
	done    chan struct{}
	played  []int16
	playN   int
	stopN   int
}

func newFakePlayer() *fakePlayer { return &fakePlayer{done: make(chan struct{})} }

func (p *fakePlayer) Play(samples []int16) (<-chan struct{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.playErr != nil {
		return nil, p.playErr
	}
	p.playN++
	p.played = samples
	return p.done, nil
}

func (p *fakePlayer) Stop() error { p.mu.Lock(); defer p.mu.Unlock(); p.stopN++; return nil }
func (p *fakePlayer) plays() int  { p.mu.Lock(); defer p.mu.Unlock(); return p.playN }
func (p *fakePlayer) stops() int  { p.mu.Lock(); defer p.mu.Unlock(); return p.stopN }
func (p *fakePlayer) finishPlayback() {
	p.mu.Lock()
	defer p.mu.Unlock()
	close(p.done)
}

// deliverDone hands the controller ONE playback-complete signal via a blocking
// send: it returns only after transmit's mid-play select has received it, so it
// doubles as a synchronization point proving transmit has left that select and
// entered the drain. (finishPlayback closes done instead — fine when the drain
// is skipped, but a preclosed channel can't prove *when* transmit consumed it.)
func (p *fakePlayer) deliverDone() { p.done <- struct{}{} }

// zeroTiming sets the pre-key lead and play tail to zero for the duration of a
// test so transmit() doesn't sleep, restoring them on cleanup.
func zeroTiming(t *testing.T) {
	t.Helper()
	lead, tail := txPreKeyLead, txPlayTail
	txPreKeyLead, txPlayTail = 0, 0
	t.Cleanup(func() { txPreKeyLead, txPlayTail = lead, tail })
}

// TestTransmit_HappyPath: key → play(waveform) → on done, unkey + stop.
func TestTransmit_HappyPath(t *testing.T) {
	zeroTiming(t)
	k := &fakeKeyer{}
	p := newFakePlayer()
	c := NewTxController(k, p, "DATA-U", logging.Noop())

	wave := []int16{1, 2, 3}
	go p.finishPlayback() // playback completes promptly

	require.NoError(t, c.transmit(context.Background(), wave, time.Time{}, nil))
	require.Equal(t, 1, k.keys())
	require.Equal(t, "DATA-U", k.keyMode)
	require.Equal(t, 1, p.plays())
	require.Equal(t, wave, p.played)
	require.Equal(t, 1, k.unkeys(), "unkey once on success")
	require.Equal(t, 1, p.stops())
}

// TestTransmit_KeyError: a failed key returns the error, never plays, and does
// NOT unkey (PTT was never raised).
func TestTransmit_KeyError(t *testing.T) {
	zeroTiming(t)
	k := &fakeKeyer{keyErr: stderrors.New("no rig")}
	p := newFakePlayer()
	c := NewTxController(k, p, "", logging.Noop())

	require.Error(t, c.transmit(context.Background(), []int16{1}, time.Time{}, nil))
	require.Equal(t, 0, p.plays(), "must not play after a failed key")
	require.Equal(t, 0, k.unkeys(), "must not unkey when key never succeeded")
}

// TestTransmit_PlayError: a play failure still unkeys (the deferred guard) —
// PTT was raised, so it must come down.
func TestTransmit_PlayError(t *testing.T) {
	zeroTiming(t)
	k := &fakeKeyer{}
	p := &fakePlayer{playErr: stderrors.New("device busy")}
	c := NewTxController(k, p, "", logging.Noop())

	require.Error(t, c.transmit(context.Background(), []int16{1}, time.Time{}, nil))
	require.Equal(t, 1, k.keys())
	require.Equal(t, 1, k.unkeys(), "must unkey after a play error")
}

// TestTransmit_ContextCancel: cancelling mid-playback stops the player and
// unkeys — the guaranteed stop holds on the cancel path.
func TestTransmit_ContextCancel(t *testing.T) {
	zeroTiming(t)
	k := &fakeKeyer{}
	p := newFakePlayer() // done never closes
	c := NewTxController(k, p, "", logging.Noop())

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	require.Error(t, c.transmit(ctx, []int16{1}, time.Time{}, nil))
	require.Equal(t, 1, k.unkeys(), "must unkey on cancel")
	require.GreaterOrEqual(t, p.stops(), 1, "must stop the player on cancel")
}

// TestTransmit_CancelDuringDrainReportsFailure: a cancel that lands in the
// txPlayTail drain window (the waveform has reached the device, but its buffered
// tail is still playing out) must be reported as a cancel — not swallowed as
// success. Otherwise Stop() clips the tail AND onDone(true) logs an incomplete
// final-rung QSO despite Abandon/shutdown. Guards the servicetx contract that a
// cancel means "did NOT complete on air".
func TestTransmit_CancelDuringDrainReportsFailure(t *testing.T) {
	// Real (non-zero) drain window; a long timer so the cancel — not the timer —
	// resolves the drain select. Lead stays zero so transmit() doesn't sleep.
	lead, tail := txPreKeyLead, txPlayTail
	txPreKeyLead, txPlayTail = 0, 10*time.Second
	t.Cleanup(func() { txPreKeyLead, txPlayTail = lead, tail })

	k := &fakeKeyer{}
	p := newFakePlayer()
	c := NewTxController(k, p, "", logging.Noop())

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.transmit(ctx, []int16{1, 2, 3}, time.Time{}, nil) }()

	// Rendezvous: deliverDone blocks until transmit's mid-play select has received
	// it, so transmit is provably PAST that select (ctx still uncancelled, so it
	// could only have taken <-done) and now in the drain. Cancelling here therefore
	// lands deterministically in the drain window — never racing the first select.
	p.deliverDone()
	cancel()

	err := <-errCh
	require.Error(t, err, "a cancel during the drain must be reported, not returned as success")
	require.ErrorIs(t, err, context.Canceled)
	// Assert the DRAIN path specifically (not the mid-play "transmit cancelled"),
	// so the test can't quietly pass by exercising the wrong cancellation branch.
	require.ErrorContains(t, err, "during drain")
	require.Equal(t, 1, k.unkeys(), "must still unkey on the drain-cancel path")
	require.GreaterOrEqual(t, p.stops(), 1, "must still stop the player on the drain-cancel path")
}

// TestTransmitSlot_EncodeError: a non-encodable message fails fast (before any
// slot wait), keying nothing.
func TestTransmitSlot_EncodeError(t *testing.T) {
	k := &fakeKeyer{}
	p := newFakePlayer()
	c := NewTxController(k, p, "", logging.Noop())

	// Free text isn't an encodable standard message.
	err := c.TransmitSlot(context.Background(), "this is not a standard ft8 message", 1500)
	require.Error(t, err)
	require.Equal(t, 0, k.keys(), "encode failure must not key")
}

// TestTruncateHead covers the ADR 0032 head-drop: skip≤0 is a no-op, a mid-stream
// skip returns the tail (and ramps its new edge toward zero), and skip past the
// end returns nil (nothing left to send).
func TestTruncateHead(t *testing.T) {
	full := make([]int16, 1000)
	for i := range full {
		full[i] = 5000 // a flat non-zero signal so the ramp is observable
	}

	require.Equal(t, full, truncateHead(full, 0), "skip 0 is a no-op")
	require.Equal(t, full, truncateHead(full, -3), "negative skip is a no-op")
	require.Nil(t, truncateHead(make([]int16, 100), 100), "skip == len → nil")
	require.Nil(t, truncateHead(make([]int16, 100), 250), "skip past end → nil")

	// Fresh copy: truncateHead mutates the backing array (re-ramps the edge).
	src := make([]int16, 1000)
	for i := range src {
		src[i] = 5000
	}
	out := truncateHead(src, 400)
	require.Len(t, out, 600)
	require.InDelta(t, 0, out[0], 1, "new leading edge is ramped to ~0")
	require.EqualValues(t, 5000, out[len(out)-1], "the tail past the ramp is untouched")
}

// TestApplyLeadingRamp: the first symbol-eighth fades in monotonically from ~0 to
// full scale; samples past the ramp are untouched. Guards the short-slice case.
func TestApplyLeadingRamp(t *testing.T) {
	nramp := txSamplesPerSymbol / 8
	s := make([]int16, nramp*3)
	for i := range s {
		s[i] = 8000
	}
	applyLeadingRamp(s)

	require.InDelta(t, 0, s[0], 1, "ramp starts at ~0")
	require.Less(t, s[1], s[nramp/2], "ramp rises monotonically")
	require.Less(t, s[nramp/2], s[nramp-1])
	require.EqualValues(t, 8000, s[nramp], "first sample past the ramp is untouched")

	// Shorter than a full ramp: clamps, no panic.
	short := []int16{9000, 9000}
	applyLeadingRamp(short)
	require.InDelta(t, 0, short[0], 1)
}

// TestTransmitAligned_LateTruncates: when the slot's nominal start is already in
// the past (the answer-a-CQ case), transmitAligned drops the elapsed head and
// plays only the synchronised remainder — and still keys/unkeys/stops cleanly.
func TestTransmitAligned_LateTruncates(t *testing.T) {
	zeroTiming(t) // preKeyLead = 0, so audioStart = now for a past boundary
	k := &fakeKeyer{}
	p := newFakePlayer()
	c := NewTxController(k, p, "", logging.Noop())

	wave := make([]int16, 100_000)
	for i := range wave {
		wave[i] = 4000
	}
	// Boundary 3 s ago → nominal 2.5 s ago → ~2.5 s (30000 samples) of head dropped.
	boundary := time.Now().UTC().Add(-3 * time.Second)
	const wantSkip = int(2.5 * float64(12000))

	go p.finishPlayback()
	require.NoError(t, c.transmitAligned(context.Background(), wave, boundary))

	require.Equal(t, 1, k.keys())
	require.Equal(t, 1, k.unkeys())
	require.Equal(t, 1, p.plays())
	// Played length ≈ full − skip; allow a small window for clock drift between
	// the test's boundary and time.Now() inside transmitAligned.
	played := len(p.played)
	require.InDelta(t, len(wave)-wantSkip, played, 600,
		"played the truncated remainder, not the full waveform")
	require.Less(t, played, len(wave), "head was actually dropped")
}

// delayKeyer wraps fakeKeyer with a fixed KeyTx latency — the CAT reality
// (mode switch + serial round-trip) the review 2026-07-20 #3 fix compensates.
type delayKeyer struct {
	fakeKeyer
	delay time.Duration
}

func (k *delayKeyer) KeyTx(ctx context.Context, mode string) error {
	time.Sleep(k.delay)
	return k.fakeKeyer.KeyTx(ctx, mode)
}

// TestTransmit_TruncatesForKeyLatency pins review 2026-07-20 #3: head
// truncation happens against the post-key clock, so KeyTx latency shortens the
// transmitted head instead of silently shifting every symbol off the computed
// timebase. With symbol 0 nominally due at KeyTx time and a 50 ms keying
// delay, at least ~50 ms of head must be dropped before playback.
func TestTransmit_TruncatesForKeyLatency(t *testing.T) {
	zeroTiming(t)
	k := &delayKeyer{delay: 50 * time.Millisecond}
	p := newFakePlayer()
	c := NewTxController(k, p, "DATA-U", logging.Noop())

	const rate = 12000 // goft8.SampleRate
	wave := make([]int16, rate)
	for i := range wave {
		wave[i] = 1 // non-zero so ramping never zero-extends confusingly
	}
	go p.finishPlayback()

	require.NoError(t, c.transmit(context.Background(), wave, time.Now().UTC(), nil))
	played := len(p.played)
	require.Greater(t, played, 0, "something must still transmit")
	require.LessOrEqual(t, played, rate-rate*50/1000,
		"the 50 ms keying delay must be dropped from the head (post-key truncation)")
	require.Greater(t, played, rate/2,
		"only latency-sized head loss expected, not wholesale truncation")
}

// slowStartPlayer models the production player's Play, which returns only once the
// device is RUNNING: it does device enumeration, malgo.InitDevice and device.Start
// inline, none context-bounded. The plain fakePlayer returns instantly and so can
// never exercise that delay — the gap a review caught in the first version of the
// decodability floor.
type slowStartPlayer struct {
	*fakePlayer
	startDelay time.Duration
}

func (p *slowStartPlayer) Play(samples []int16) (<-chan struct{}, error) {
	time.Sleep(p.startDelay)
	return p.fakePlayer.Play(samples)
}

// fullTxWaveform is a synthetic waveform of the real standard-message length, so
// the decodability floor under test is the one live transmissions face.
func fullTxWaveform() []int16 {
	wave := make([]int16, txWaveformSamples)
	for i := range wave {
		wave[i] = 1000
	}
	return wave
}

// TestTransmit_RejectsUndecodablyLateFragment pins the review finding that a
// severely late start was reported as a SUCCESSFUL transmission: the old check
// refused only a completely empty remainder, so any surviving tail — even a few
// samples — returned nil. Success is what logs the QSO, so a delayed final
// 73/RR73 could book (and forward) a contact that never went out decodably.
func TestTransmit_RejectsUndecodablyLateFragment(t *testing.T) {
	zeroTiming(t)
	k := &fakeKeyer{}
	p := newFakePlayer()
	c := NewTxController(k, p, "", logging.Noop())

	// Symbol 0 was due 7 s ago — the head loss reaches past FT8's middle Costas
	// array (5.92 s), so what is left cannot carry a decode even though it is a
	// long way from empty. finishPlayback is armed so that a controller which
	// wrongly plays the fragment completes and returns nil, failing the assertion
	// below rather than blocking the test.
	go p.finishPlayback()
	err := c.transmit(context.Background(), fullTxWaveform(), time.Now().UTC().Add(-7*time.Second), nil)
	require.Error(t, err, "an undecodable fragment must be reported as a failed transmission, not sent")
	require.Equal(t, 0, p.plays(), "nothing may go on air")
	// The guaranteed stop is unchanged on the new reject path.
	require.Equal(t, 1, k.keys())
	require.Equal(t, 1, k.unkeys(), "PTT must still drop")
}

// TestTransmit_KeyLatencyPushesRungPastTheLimit is the finding's exact mechanism.
// The rung is inside the limit when the sequencer decides it AND when the
// controller makes its pre-key estimate; only the CAT keying latency (mode switch
// + serial round-trip, which lands after both) carries it past the point of no
// decode. That is the gap a pre-key check alone cannot close.
func TestTransmit_KeyLatencyPushesRungPastTheLimit(t *testing.T) {
	zeroTiming(t)
	k := &delayKeyer{delay: 300 * time.Millisecond}
	p := newFakePlayer()
	c := NewTxController(k, p, "", logging.Noop())

	wave := fullTxWaveform()
	limitSec := float64(maxDecodableSkip(len(wave))) / float64(goft8.SampleRate)
	// 100 ms inside the limit at decision time; the 300 ms key delay crosses it.
	nominal := time.Now().UTC().Add(-time.Duration((limitSec - 0.1) * float64(time.Second)))

	go p.finishPlayback() // see TestTransmit_RejectsUndecodablyLateFragment
	err := c.transmit(context.Background(), wave, nominal, nil)
	require.Error(t, err, "keying latency must be able to fail a rung that was inside the window when decided")
	require.Equal(t, 0, p.plays())
	require.Equal(t, 1, k.unkeys(), "PTT drops on the reject path")
}

// setAudioBudget dials the slot audio budget down for a test (restoring it after)
// so a short, fast device-start delay stands in for the seconds a real USB codec or
// contended PipeWire can take. The live budget leaves ~1.54 s of slack over the
// waveform, which would otherwise mean a >1.5 s sleep per test.
func setAudioBudget(t *testing.T, d time.Duration) {
	t.Helper()
	prev := txAudioBudget
	txAudioBudget = d
	t.Cleanup(func() { txAudioBudget = prev })
}

// TestTransmit_RejectsWhenDeviceStartsTooLate pins the follow-up review finding on
// the decodability floor: the floor was evaluated immediately BEFORE Play, but the
// production Play then does unbounded device startup before the first sample. A
// rung can clear the head check and still have its audio begin seconds later.
func TestTransmit_RejectsWhenDeviceStartsTooLate(t *testing.T) {
	zeroTiming(t)
	setAudioBudget(t, 13*time.Second) // 12.96 s waveform → ~0.04 s of slack
	k := &fakeKeyer{}
	p := &slowStartPlayer{fakePlayer: newFakePlayer(), startDelay: 200 * time.Millisecond}
	c := NewTxController(k, p, "", logging.Noop())

	// 4 s late: well inside the head-truncation floor, so only the device-start
	// delay can fail this.
	go p.finishPlayback()
	err := c.transmit(context.Background(), fullTxWaveform(), time.Now().UTC().Add(-4*time.Second), nil)
	require.Error(t, err, "a transmission whose audio starts too late must not be reported as sent")
	require.Equal(t, 1, p.stops(), "output must be halted, not left running into the next slot")
	require.Equal(t, 1, k.unkeys(), "PTT still drops")
}

// TestTransmit_RejectsLateDeviceStartOnAnUntruncatedRung is the case the head-loss
// check is structurally blind to. A manual next-slot CQ starts on time and
// truncates NOTHING, so no amount of head-loss testing can notice a slow device
// start — yet the shift puts every symbol off its DT just the same.
func TestTransmit_RejectsLateDeviceStartOnAnUntruncatedRung(t *testing.T) {
	zeroTiming(t)
	setAudioBudget(t, 13*time.Second)
	k := &fakeKeyer{}
	p := &slowStartPlayer{fakePlayer: newFakePlayer(), startDelay: 200 * time.Millisecond}
	c := NewTxController(k, p, "", logging.Noop())

	wave := fullTxWaveform()
	go p.finishPlayback()
	err := c.transmit(context.Background(), wave, time.Now().UTC(), nil)
	require.Error(t, err, "an on-time rung whose device started late is still off its timebase")
	require.Equal(t, len(wave), len(p.played), "nothing was truncated — the head check could not have caught this")
	require.Equal(t, 1, p.stops())
}

// TestTransmit_ToleratesNormalDeviceStartLatency guards the other direction: the
// live budget leaves ~1.54 s of slack over the waveform, so ordinary device-start
// latency must not fail a rung.
func TestTransmit_ToleratesNormalDeviceStartLatency(t *testing.T) {
	zeroTiming(t) // real txAudioBudget (14.5 s)
	k := &fakeKeyer{}
	p := &slowStartPlayer{fakePlayer: newFakePlayer(), startDelay: 150 * time.Millisecond}
	c := NewTxController(k, p, "", logging.Noop())

	go p.finishPlayback()
	require.NoError(t, c.transmit(context.Background(), fullTxWaveform(), time.Now().UTC(), nil),
		"a normal device-start delay is inside the slot's slack and must transmit")
}

// TestTransmit_ReservesDeviceDrainInSlotBudget pins the follow-up finding on the
// slot-overrun guard: it counted only the PCM duration, but the player's done means
// the samples merely REACHED the device — the drain below waits txPlayTail because
// the buffered tail is still emitting. A waveform that fits the budget on PCM
// duration alone can therefore still carry RF into the next slot, so the drain has
// to be reserved. Here the audio fits with 40 ms to spare and the 200 ms tail is
// what must fail it.
func TestTransmit_ReservesDeviceDrainInSlotBudget(t *testing.T) {
	lead, tail := txPreKeyLead, txPlayTail
	txPreKeyLead, txPlayTail = 0, 200*time.Millisecond
	t.Cleanup(func() { txPreKeyLead, txPlayTail = lead, tail })
	setAudioBudget(t, 13*time.Second) // 12.96 s waveform → 40 ms of slack

	k := &fakeKeyer{}
	p := newFakePlayer() // instant start: only the unbudgeted drain can fail this
	c := NewTxController(k, p, "", logging.Noop())

	go p.finishPlayback()
	err := c.transmit(context.Background(), fullTxWaveform(), time.Now().UTC(), nil)
	require.Error(t, err, "the buffered tail must be counted against the slot budget")
	require.Equal(t, 1, p.stops())
	require.Equal(t, 1, k.unkeys(), "PTT still drops")
}

// TestTransmit_SlowDeviceStartCancelStaysACancel pins the classification half: a
// disarm/shutdown that lands while Play is blocked in a slow device start used to
// be reported by the select below as a cancellation. The overrun guard runs first,
// so it must preserve that — otherwise Service.startTransmission marks a deliberate
// operator action as ft8_tx_failed and warns.
func TestTransmit_SlowDeviceStartCancelStaysACancel(t *testing.T) {
	zeroTiming(t)
	setAudioBudget(t, 13*time.Second)
	k := &fakeKeyer{}
	p := &slowStartPlayer{fakePlayer: newFakePlayer(), startDelay: 200 * time.Millisecond}
	c := NewTxController(k, p, "", logging.Noop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the (slow) device start completes

	err := c.transmit(ctx, fullTxWaveform(), time.Now().UTC(), nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled,
		"a cancel during device start is a normal stop, not a transmission failure")
	require.Equal(t, 1, k.unkeys())
}

// TestTransmit_StillSendsALateButDecodableRemainder guards the other direction:
// ADR 0032 deliberately transmits a truncated reply rather than slipping a whole
// 30 s cycle, so the floor must not tighten the late window that already works on
// air. 4.0 s is the most head-loss the sequencer's 4.5 s window can hand over
// (symbol 0 is nominally 0.5 s into the slot).
func TestTransmit_StillSendsALateButDecodableRemainder(t *testing.T) {
	zeroTiming(t)
	k := &fakeKeyer{}
	p := newFakePlayer()
	c := NewTxController(k, p, "", logging.Noop())

	wave := fullTxWaveform()
	go p.finishPlayback()
	require.NoError(t, c.transmit(context.Background(), wave, time.Now().UTC().Add(-4*time.Second), nil))
	require.Equal(t, 1, p.plays(), "a late-but-decodable remainder must still transmit")
	require.Less(t, len(p.played), len(wave), "the elapsed head was dropped")
	require.Greater(t, len(p.played), len(wave)/2, "most of the waveform still went out")
}

// TestTransmit_PreKeyCheckRefusesWithoutKeying: the pre-key gate is the last thing
// between a committed transmission and RF, and it runs AFTER the slot-boundary
// wait. A manual send is accepted up to ~15 s before it keys, and the daemon's
// view of the rig can change inside that window — so a check made when the request
// was accepted says nothing about the moment we key (codex P1 on 0d180e59).
//
// Asserted on the observable that matters: whether PTT was pressed.
func TestTransmit_PreKeyCheckRefusesWithoutKeying(t *testing.T) {
	zeroTiming(t)

	t.Run("a refusing check aborts before PTT", func(t *testing.T) {
		k := &fakeKeyer{}
		p := newFakePlayer()
		c := NewTxController(k, p, "DATA-U", logging.Noop())
		c.SetPreKeyCheck(func() error { return ErrTxDialUnknown })

		// Let playback complete if the gate is (wrongly) skipped, so this fails on
		// the assertion below rather than deadlocking — a hanging test is a poor
		// proof; it does not say WHAT went wrong.
		go p.finishPlayback()
		err := c.transmit(context.Background(), []int16{1, 2, 3}, time.Time{}, nil)
		require.ErrorIs(t, err, ErrTxDialUnknown)

		require.Zero(t, k.keys(), "the rig must not have been keyed")
		require.Zero(t, k.unkeys(), "nothing was keyed, so nothing needed unkeying")
	})

	t.Run("a passing check keys as normal", func(t *testing.T) {
		k := &fakeKeyer{}
		p := newFakePlayer()
		c := NewTxController(k, p, "DATA-U", logging.Noop())
		c.SetPreKeyCheck(func() error { return nil })

		go p.finishPlayback()
		require.NoError(t, c.transmit(context.Background(), []int16{1, 2, 3}, time.Time{}, nil))

		require.Equal(t, 1, k.keys(), "a passing gate must not change normal keying")
		require.Equal(t, 1, p.plays())
	})
}
