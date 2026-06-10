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

	require.NoError(t, c.transmit(context.Background(), wave))
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

	require.Error(t, c.transmit(context.Background(), []int16{1}))
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

	require.Error(t, c.transmit(context.Background(), []int16{1}))
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

	require.Error(t, c.transmit(ctx, []int16{1}))
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
