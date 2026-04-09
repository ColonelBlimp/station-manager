package audio

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// newPlayback creates a Playback for testing and registers cleanup.
func newPlayback(t *testing.T) *Playback {
	t.Helper()
	p := NewPlayback(DefaultConfig())
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// --------------- Errors -------------------------------------------------------

func TestPlaybackErrors(t *testing.T) {
	require.Equal(t, "audio playback not initialized", ErrPlaybackNotInitialized.Error())
	require.Equal(t, "audio playback already playing", ErrPlaybackAlreadyPlaying.Error())
	require.Equal(t, "audio playback not playing", ErrPlaybackNotPlaying.Error())
	require.Equal(t, "audio playback closed", ErrPlaybackClosed.Error())
	require.Equal(t, "audio playback samples are nil or empty", ErrPlaybackEmptySamples.Error())
}

// --------------- NewPlayback --------------------------------------------------

func TestNewPlayback(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NotNil(t, p)
}

func TestNewPlayback_NilLogger_DefaultsToNoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Logger = nil
	p := NewPlayback(cfg)
	require.NotNil(t, p.config.Logger)
}

// --------------- IsPlaying ---------------------------------------------------

func TestPlayback_IsPlaying_InitialState(t *testing.T) {
	p := newPlayback(t)
	require.False(t, p.IsPlaying())
}

// --------------- Stop --------------------------------------------------------

func TestPlayback_Stop_NotPlaying(t *testing.T) {
	p := newPlayback(t)
	err := p.Stop()
	require.ErrorIs(t, err, ErrPlaybackNotPlaying)
}

// --------------- Init --------------------------------------------------------

func TestPlayback_Init_AfterClose_ReturnsErrClosed(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())
	require.NoError(t, p.Close())
	require.ErrorIs(t, p.Init(), ErrPlaybackClosed)
}

// --------------- Close -------------------------------------------------------

func TestPlayback_Close_SetsClosedFlag(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())
	require.NoError(t, p.Close())
	require.True(t, p.closed.Load())
}

func TestPlayback_Close_Idempotent(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())
	require.NoError(t, p.Close())
	// Second close must not panic or return error from double-uninit
	_ = p.Close()
}

func TestPlayback_Close_WithoutInit(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	// Close without Init must not panic
	require.NoError(t, p.Close())
}

func TestPlayback_ConcurrentClose(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Close()
		}()
	}
	wg.Wait() // must not panic
}

// --------------- PlayFile — state-machine errors (no hardware) ---------------

func TestPlayback_PlayFile_NotInitialized(t *testing.T) {
	p := newPlayback(t)
	err := p.PlayFile(context.Background(), "any.wav")
	require.ErrorIs(t, err, ErrPlaybackNotInitialized)
}

func TestPlayback_PlayFile_AfterClose(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())
	require.NoError(t, p.Close())
	err := p.PlayFile(context.Background(), "any.wav")
	require.ErrorIs(t, err, ErrPlaybackClosed)
}

func TestPlayback_PlayFile_AlreadyPlaying(t *testing.T) {
	p := newPlayback(t)
	// Manually set the playing flag to simulate an in-progress play.
	p.playing.Store(true)
	err := p.PlayFile(context.Background(), "any.wav")
	require.ErrorIs(t, err, ErrPlaybackAlreadyPlaying)
}

func TestPlayback_PlayFile_FileNotFound(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())
	defer p.Close()

	err := p.PlayFile(context.Background(), "/tmp/no-such-file-for-station-manager-test.wav")
	require.Error(t, err)
	require.False(t, p.IsPlaying(), "IsPlaying must be false after failed PlayFile")
}

func TestPlayback_PlayFile_InvalidWAV(t *testing.T) {
	// Write a non-WAV temp file.
	f, err := createTempFile(t, []byte("this is not a wav file at all"))
	require.NoError(t, err)

	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())
	defer p.Close()

	playErr := p.PlayFile(context.Background(), f)
	require.Error(t, playErr)
	require.False(t, p.IsPlaying())
}

// createTempFile writes content to a temp file and returns its path.
func createTempFile(t *testing.T, content []byte) (string, error) {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "*.bin")
	if err != nil {
		return "", err
	}
	_, err = tmp.Write(content)
	return tmp.Name(), err
}

// --------------- PlaySamples — state-machine errors (no hardware) ------------

func TestPlayback_PlaySamples_NilSamples(t *testing.T) {
	p := newPlayback(t)
	err := p.PlaySamples(context.Background(), nil, 12000, 1)
	require.ErrorIs(t, err, ErrPlaybackEmptySamples)
}

func TestPlayback_PlaySamples_EmptySamples(t *testing.T) {
	p := newPlayback(t)
	err := p.PlaySamples(context.Background(), []float32{}, 12000, 1)
	require.ErrorIs(t, err, ErrPlaybackEmptySamples)
}

func TestPlayback_PlaySamples_NotInitialized(t *testing.T) {
	p := newPlayback(t)
	samples := make([]float32, 100)
	err := p.PlaySamples(context.Background(), samples, 12000, 1)
	require.ErrorIs(t, err, ErrPlaybackNotInitialized)
}

func TestPlayback_PlaySamples_AfterClose(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())
	require.NoError(t, p.Close())
	samples := make([]float32, 100)
	err := p.PlaySamples(context.Background(), samples, 12000, 1)
	require.ErrorIs(t, err, ErrPlaybackClosed)
}

func TestPlayback_PlaySamples_AlreadyPlaying(t *testing.T) {
	p := newPlayback(t)
	// Manually set the playing flag to simulate an in-progress play.
	p.playing.Store(true)
	samples := make([]float32, 100)
	err := p.PlaySamples(context.Background(), samples, 12000, 1)
	require.ErrorIs(t, err, ErrPlaybackAlreadyPlaying)
}

func TestPlayback_PlaySamples_MutualExclusion_WithPlayFile(t *testing.T) {
	p := newPlayback(t)
	// Simulate PlayFile in progress — PlaySamples should be blocked.
	p.playing.Store(true)
	samples := make([]float32, 100)
	err := p.PlaySamples(context.Background(), samples, 48000, 1)
	require.ErrorIs(t, err, ErrPlaybackAlreadyPlaying)

	// And vice-versa: simulate PlaySamples in progress — PlayFile should be blocked.
	err = p.PlayFile(context.Background(), "any.wav")
	require.ErrorIs(t, err, ErrPlaybackAlreadyPlaying)
}

func TestPlayback_PlaySamples_EmptyBeforeAcquire(t *testing.T) {
	// Verify that the empty-samples check happens before acquirePlay,
	// so it doesn't touch wg/playing/ctx at all.
	p := NewPlayback(DefaultConfig())
	// Don't init — if PlaySamples tried to acquire, it would get ErrPlaybackNotInitialized.
	// But it should return ErrPlaybackEmptySamples first.
	err := p.PlaySamples(context.Background(), nil, 12000, 1)
	require.ErrorIs(t, err, ErrPlaybackEmptySamples)
	require.False(t, p.IsPlaying())
}
