package ft8

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

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

	require.NoError(t, c.transmit(context.Background(), wave, nil))
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

	require.Error(t, c.transmit(context.Background(), []int16{1}, nil))
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

	require.Error(t, c.transmit(context.Background(), []int16{1}, nil))
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

	require.Error(t, c.transmit(ctx, []int16{1}, nil))
	require.Equal(t, 1, k.unkeys(), "must unkey on cancel")
	require.GreaterOrEqual(t, p.stops(), 1, "must stop the player on cancel")
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
