// internal/audio/playback.go
package audio

import (
	"context"
	stderr "errors"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/gen2brain/malgo"
)

var (
	ErrPlaybackNotInitialized = stderr.New("audio playback not initialized")
	ErrPlaybackAlreadyPlaying = stderr.New("audio playback already playing")
	ErrPlaybackNotPlaying     = stderr.New("audio playback not playing")
	ErrPlaybackClosed         = stderr.New("audio playback closed")
	ErrPlaybackEmptySamples   = stderr.New("audio playback samples are nil or empty")
)

// Playback handles audio playback of WAV files and in-memory sample buffers.
// A single Playback can play one source at a time; concurrent PlayFile or
// PlaySamples calls return ErrPlaybackAlreadyPlaying. A Playback cannot be
// reused after Close().
type Playback struct {
	config Config

	ctx *malgo.AllocatedContext

	// playing is true while a PlayFile or PlaySamples call is in progress.
	// CompareAndSwap prevents concurrent plays.
	playing atomic.Bool

	// closed is set permanently by Close() to prevent reuse after shutdown.
	closed atomic.Bool

	// mu protects ctx and cancelPlay.
	mu sync.Mutex

	// cancelPlay is set by PlayFile/PlaySamples and called by Stop() or Close()
	// to interrupt in-progress playback. Nil when no playback is in progress.
	cancelPlay context.CancelFunc

	// wg tracks in-progress PlayFile/PlaySamples calls so that Close() can wait
	// for device teardown to complete before uninitialising the malgo context.
	wg sync.WaitGroup
}

// NewPlayback creates a new Playback instance.
// If cfg.Logger is nil it defaults to a no-op logger.
// cfg.SampleRate and cfg.Channels are ignored — these are taken from each WAV file.
func NewPlayback(cfg Config) *Playback {
	if cfg.Logger == nil {
		cfg.Logger = logging.Noop()
	}
	return &Playback{config: cfg}
}

// Init initialises the audio backend.
// Returns ErrPlaybackClosed if Close has already been called.
func (p *Playback) Init() error {
	const op errors.Op = "audio.Playback.Init"

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return ErrPlaybackClosed
	}
	if p.ctx != nil {
		return errors.New(op).Msg("already initialized")
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return errors.New(op).Err(err)
	}
	p.ctx = ctx
	return nil
}

// ListDevices returns available playback devices.
// Init must be called first.
func (p *Playback) ListDevices() ([]malgo.DeviceInfo, error) {
	const op errors.Op = "audio.Playback.ListDevices"

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx == nil {
		return nil, ErrPlaybackNotInitialized
	}

	infos, err := p.ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}
	return infos, nil
}

// IsPlaying returns true if a PlayFile call is currently in progress.
func (p *Playback) IsPlaying() bool {
	return p.playing.Load()
}

// Stop interrupts any in-progress PlayFile, causing it to return immediately.
// Returns ErrPlaybackNotPlaying if nothing is playing.
// If Stop is called in the sub-microsecond window between playing being set true
// and cancelPlay being registered (essentially impossible in practice), it returns
// nil without cancelling — IsPlaying() remains the authoritative "is it running" check.
func (p *Playback) Stop() error {
	if !p.playing.Load() {
		return ErrPlaybackNotPlaying
	}

	p.mu.Lock()
	cancel := p.cancelPlay
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return nil
}

// Close releases all audio resources.
// It cancels any in-progress PlayFile and waits for device teardown before
// releasing the malgo context. Safe to call concurrently with Stop or PlayFile.
func (p *Playback) Close() error {
	const op errors.Op = "audio.Playback.Close"

	p.closed.Store(true)

	// Signal any running PlayFile to stop.
	p.mu.Lock()
	cancel := p.cancelPlay
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	// Wait for PlayFile to finish device teardown before freeing the context.
	p.wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		if err := p.ctx.Uninit(); err != nil {
			return errors.New(op).Err(err)
		}
		p.ctx.Free()
		p.ctx = nil
	}
	return nil
}

// PlayFile plays a WAV file to completion, blocking until the file finishes,
// the context is cancelled, or Stop/Close is called.
//
// The device is configured to match the WAV file's sample rate and channel count.
// cfg.SampleRate and cfg.Channels from the constructor are not used here.
//
// Returns ErrPlaybackAlreadyPlaying if another PlayFile or PlaySamples is already in progress.
// Returns ErrPlaybackNotInitialized if Init has not been called.
// Returns ErrPlaybackClosed if Close has been called.
func (p *Playback) PlayFile(ctx context.Context, path string) error {
	const op errors.Op = "audio.Playback.PlayFile"

	playCtx, audioCtx, err := p.acquirePlay()
	if err != nil {
		return err
	}
	defer p.releasePlay()

	// Decode the WAV file. Stop()/Close() may be called during this read.
	wav, err := readWAV(path)
	if err != nil {
		return errors.New(op).Err(err)
	}

	// If Stop() or Close() arrived during the WAV decode, bail now before
	// touching any audio hardware.
	if playCtx.Err() != nil {
		return nil
	}

	return p.playBuffer(op, ctx, playCtx, audioCtx, wav.Samples, wav.SampleRate, uint32(wav.Channels))
}

// PlaySamples plays in-memory float32 audio samples to the playback device,
// blocking until all samples have been played, the context is cancelled, or
// Stop()/Close() is called.
//
// sampleRate is the playback sample rate (e.g., 12000 for FT8).
// channels is the number of audio channels (1 for mono FT8 audio).
//
// Returns ErrPlaybackEmptySamples if samples is nil or empty.
// Returns ErrPlaybackAlreadyPlaying if another PlayFile or PlaySamples is already in progress.
// Returns ErrPlaybackNotInitialized if Init has not been called.
// Returns ErrPlaybackClosed if Close has been called.
func (p *Playback) PlaySamples(ctx context.Context, samples []float32,
	sampleRate uint32, channels uint32) error {

	const op errors.Op = "audio.Playback.PlaySamples"

	if len(samples) == 0 {
		return ErrPlaybackEmptySamples
	}

	playCtx, audioCtx, err := p.acquirePlay()
	if err != nil {
		return err
	}
	defer p.releasePlay()

	return p.playBuffer(op, ctx, playCtx, audioCtx, samples, sampleRate, channels)
}

// acquirePlay performs the shared preamble for PlayFile and PlaySamples:
// wg.Add, closed check, playing CAS, play-context creation, double-close
// check, and audioCtx extraction.
//
// On success it returns (playCtx, audioCtx, nil) and the caller MUST call
// releasePlay when done. On failure it returns a non-nil error and no
// cleanup is needed.
func (p *Playback) acquirePlay() (context.Context, malgo.Context, error) {
	// wg.Add before closed check so Close().wg.Wait() is guaranteed to observe
	// this goroutine if it races past the closed check.
	p.wg.Add(1)

	if p.closed.Load() {
		p.wg.Done()
		return nil, malgo.Context{}, ErrPlaybackClosed
	}

	if !p.playing.CompareAndSwap(false, true) {
		p.wg.Done()
		return nil, malgo.Context{}, ErrPlaybackAlreadyPlaying
	}

	// Create the play context immediately after the CAS so that Stop() can always
	// cancel when IsPlaying() is true.
	playCtx, cancelPlay := context.WithCancel(context.Background())

	p.mu.Lock()
	// Second closed check: cover the window between wg.Add and acquiring mu.
	if p.closed.Load() {
		p.mu.Unlock()
		cancelPlay()
		p.playing.Store(false)
		p.wg.Done()
		return nil, malgo.Context{}, ErrPlaybackClosed
	}
	if p.ctx == nil {
		p.mu.Unlock()
		cancelPlay()
		p.playing.Store(false)
		p.wg.Done()
		return nil, malgo.Context{}, ErrPlaybackNotInitialized
	}
	audioCtx := p.ctx.Context
	p.cancelPlay = cancelPlay
	p.mu.Unlock()

	return playCtx, audioCtx, nil
}

// releasePlay performs the shared cleanup for PlayFile and PlaySamples:
// cancel the play context, clear cancelPlay, reset the playing flag, and
// signal the wait group.
func (p *Playback) releasePlay() {
	p.mu.Lock()
	cancel := p.cancelPlay
	p.cancelPlay = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.playing.Store(false)
	p.wg.Done()
}

// playBuffer is the shared device-level playback logic used by both PlayFile
// and PlaySamples. It configures the malgo device, streams samples through the
// audio callback, and blocks until completion/cancellation.
func (p *Playback) playBuffer(op errors.Op, ctx, playCtx context.Context,
	audioCtx malgo.Context, samples []float32, sampleRate, channels uint32) error {

	// Playback state — only accessed from the single audio callback thread.
	pos := 0
	done := make(chan struct{})
	var doneOnce sync.Once

	onSendFrames := func(outputSamples, _ []byte, _ uint32) {
		if len(outputSamples) < BytesPerFloat32 {
			return
		}
		// Zero-copy view of the output buffer as float32.
		// malgo guarantees 4-byte alignment for output buffers.
		out := unsafe.Slice((*float32)(unsafe.Pointer(&outputSamples[0])), len(outputSamples)/BytesPerFloat32)

		n := copy(out, samples[pos:])
		pos += n
		// Zero any frames beyond the end of the source.
		for i := n; i < len(out); i++ {
			out[i] = 0
		}
		if n < len(out) {
			// Consumed all samples — signal completion.
			doneOnce.Do(func() { close(done) })
		}
	}

	deviceConfig := malgo.DeviceConfig{
		DeviceType:         malgo.Playback,
		SampleRate:         sampleRate,
		PeriodSizeInFrames: p.config.BufferSize,
		Playback: malgo.SubConfig{
			Format:   malgo.FormatF32,
			Channels: channels,
		},
	}

	if p.config.DeviceIndex >= 0 {
		p.mu.Lock()
		devices, listErr := p.ctx.Devices(malgo.Playback)
		p.mu.Unlock()
		if listErr != nil {
			return errors.New(op).Err(listErr)
		}
		if p.config.DeviceIndex >= len(devices) {
			return errors.New(op).Msgf("device index %d out of range (have %d playback devices)",
				p.config.DeviceIndex, len(devices))
		}
		deviceConfig.Playback.DeviceID = devices[p.config.DeviceIndex].ID.Pointer()
	}

	device, err := malgo.InitDevice(audioCtx, deviceConfig, malgo.DeviceCallbacks{Data: onSendFrames})
	if err != nil {
		return errors.New(op).Err(err)
	}
	defer func() {
		if err := device.Stop(); err != nil {
			p.config.Logger.WarnWith().Err(err).Msg("device stop on playback exit")
		}
		device.Uninit()
	}()

	if err := device.Start(); err != nil {
		return errors.New(op).Err(err)
	}

	select {
	case <-done:
		// All samples have been submitted to the hardware, but the audio data is
		// still in transit through PipeWire/ALSA/driver buffers before reaching
		// the DAC. device.Stop() cuts the stream immediately, so we must wait for
		// the pipeline to drain. PipeWire + ALSA can have 150–250ms total latency;
		// 500ms is a conservative but safe bound.
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
		case <-playCtx.Done():
		}
	case <-ctx.Done():
		// Caller cancelled the context.
	case <-playCtx.Done():
		// Stop() or Close() was called.
	}

	return nil
}
