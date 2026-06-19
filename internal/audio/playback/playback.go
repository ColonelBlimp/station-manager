//go:build cgo

// The malgo/miniaudio playback backend requires CGO. This file is gated
// behind the built-in `cgo` constraint so the static, CGO-free default
// build excludes it entirely (doc.go carries the package clause there).
// See ADR 0024 — the static default has no live audio; live FT8 (RX or TX)
// is the CGO build.
package playback

import (
	stderr "errors"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/gen2brain/malgo"
)

const (
	// ft8SampleRateHz is the WSJT-X/jt9 canonical rate for FT8 (12 kHz mono) —
	// the rate the modulator emits and DefaultConfig requests.
	ft8SampleRateHz = 12000
	// defaultCallbackFrames is the frames-per-callback period requested from
	// the backend (~43 ms at 12 kHz). Mirrors the capture period.
	defaultCallbackFrames = 512
)

var (
	ErrNotInitialized = stderr.New("audio playback not initialized")
	ErrAlreadyPlaying = stderr.New("audio playback already playing")
	ErrNotPlaying     = stderr.New("audio playback not playing")
	ErrClosed         = stderr.New("audio playback closed")
)

// Config holds audio playback configuration.
type Config struct {
	// DeviceName selects a playback device by its enumerated name (as reported
	// by ListDevices / GET /v1/hardware). When non-empty it takes precedence
	// over DeviceIndex and is resolved to a live device at Play time, so it
	// survives device re-enumeration across replug/reboot. No match → Play
	// errors (fail-soft idle at the FT8 layer) rather than silently grabbing
	// the system default.
	DeviceName string
	// DeviceIndex selects a playback device. -1 means the system default. Used
	// only when DeviceName is empty.
	DeviceIndex int
	// SampleRate in Hz. FT8 canonical is 12000.
	SampleRate uint32
	// Channels: 1 for mono (FT8).
	Channels uint32
	// BufferSize is the frames-per-callback period requested from the audio
	// backend. Smaller = lower latency, higher syscall load.
	BufferSize uint32
	// Logger receives warn-level diagnostics from the playback lifecycle.
	// nil → logging.Noop().
	Logger logging.Logger
}

// DefaultConfig returns the FT8 canonical playback configuration:
// 12 kHz mono S16, default system device, 512-frame callback period.
func DefaultConfig() Config {
	return Config{
		DeviceIndex: -1,
		SampleRate:  ft8SampleRateHz,
		Channels:    1,
		BufferSize:  defaultCallbackFrames,
	}
}

// Player streams an int16 PCM waveform to an audio output device. One Player
// plays one waveform at a time; Play is rejected while a previous waveform is
// still running (Stop or wait for the done channel first).
type Player struct {
	config  Config
	ctx     *malgo.AllocatedContext
	device  *malgo.Device
	playing atomic.Bool
	closed  atomic.Bool // Close makes the player terminal — mirrors capture
	mu      sync.Mutex  // protects ctx, device, buf, done

	// buf is the waveform currently being played; pos is the next sample the
	// callback will emit (atomic for lock-free hot-path access). done is closed
	// once the whole of buf has been handed to the device.
	buf  []int16
	pos  atomic.Int64
	done chan struct{}
}

// New creates a new audio player.
func New(cfg Config) *Player {
	if cfg.Logger == nil {
		cfg.Logger = logging.Noop()
	}
	return &Player{config: cfg}
}

// Init initialises the audio backend. Idempotent: a second call after a
// successful initialisation returns nil.
//
// Returns ErrClosed once Close has been called — a Player is terminal after
// close and cannot be reused (mirrors capture; FT8 arms a fresh Player per
// session rather than reopening a closed one).
func (p *Player) Init() error {
	const op errors.Op = "playback.Player.Init"

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return ErrClosed
	}

	if p.ctx != nil {
		return nil
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	p.ctx = ctx
	return nil
}

// ListDevices returns the available playback devices reported by the audio
// backend. Init must have been called first.
func (p *Player) ListDevices() ([]malgo.DeviceInfo, error) {
	const op errors.Op = "playback.Player.ListDevices"

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return nil, ErrClosed
	}
	if p.ctx == nil {
		return nil, ErrNotInitialized
	}

	infos, err := p.ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	return infos, nil
}

// Play begins streaming samples to the output device. Non-blocking — it
// returns as soon as the device is started. The returned channel is closed
// when the entire waveform has been handed to the device (its natural end);
// Stop or Close also halt output but do not close this channel. The caller
// owns the stop: wait on done (plus a short device-buffer tail) then Stop.
//
// samples are mono int16 at the configured SampleRate — exactly what
// ft8.EncodeToSlot emits.
func (p *Player) Play(samples []int16) (<-chan struct{}, error) {
	const op errors.Op = "playback.Player.Play"

	if !p.playing.CompareAndSwap(false, true) {
		return nil, ErrAlreadyPlaying
	}

	// Hold mu across the whole start sequence — InitDevice + device.Start touch the
	// malgo context, and a concurrent Close must not free it mid-init (same reasoning
	// as capture.Start). mu is the lifecycle lock; the audio callback uses p.pos
	// (atomic) + p.buf (set here before device.Start, and Stop halts the device
	// before nil'ing buf), never mu — so holding it across the HW calls only
	// serialises Stop/Close, not the hot path.
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		p.playing.Store(false)
		return nil, ErrClosed
	}
	if p.ctx == nil {
		p.playing.Store(false)
		return nil, ErrNotInitialized
	}
	audioCtx := p.ctx.Context

	var deviceID unsafe.Pointer
	switch {
	case p.config.DeviceName != "":
		devices, err := p.ctx.Devices(malgo.Playback)
		if err != nil {
			p.playing.Store(false)
			return nil, errors.New(op).WithErr(err)
		}
		names := make([]string, len(devices))
		for i := range devices {
			names[i] = devices[i].Name()
		}
		// Fail on a duplicate (ambiguous) name rather than silently binding the
		// first match — wrong-codec is worse than idle (review 2026-06-19 M1).
		idx, merr := audio.MatchDeviceName(names, p.config.DeviceName, "playback")
		if merr != nil {
			p.playing.Store(false)
			return nil, errors.New(op).WithErr(merr)
		}
		deviceID = devices[idx].ID.Pointer()
	case p.config.DeviceIndex >= 0:
		devices, err := p.ctx.Devices(malgo.Playback)
		if err != nil {
			p.playing.Store(false)
			return nil, errors.New(op).WithErr(err)
		}
		if p.config.DeviceIndex >= len(devices) {
			p.playing.Store(false)
			return nil, errors.New(op).WithMsgf("device index %d out of range (have %d devices)",
				p.config.DeviceIndex, len(devices))
		}
		deviceID = devices[p.config.DeviceIndex].ID.Pointer()
	}

	p.buf = samples
	p.pos.Store(0)
	done := make(chan struct{})
	p.done = done
	var doneOnce sync.Once

	deviceConfig := malgo.DeviceConfig{
		DeviceType:         malgo.Playback,
		SampleRate:         p.config.SampleRate,
		PeriodSizeInFrames: p.config.BufferSize,
		Playback: malgo.SubConfig{
			Format:   malgo.FormatS16,
			Channels: p.config.Channels,
		},
	}
	if deviceID != nil {
		deviceConfig.Playback.DeviceID = deviceID
	}

	// onSendFrames is the audio thread asking for the next output frame. For
	// mono S16 each frame is one int16; we copy from buf at the current
	// position and silence-pad the tail, closing done the first time the whole
	// waveform has been emitted.
	onSendFrames := func(out, _ []byte, _ uint32) {
		frame := bytesAsInt16(out)
		if len(frame) == 0 {
			return
		}
		pos := int(p.pos.Load())
		_, newPos := fillFrame(frame, p.buf, pos)
		p.pos.Store(int64(newPos))
		if newPos >= len(p.buf) {
			doneOnce.Do(func() { close(done) })
		}
	}

	device, err := malgo.InitDevice(audioCtx, deviceConfig, malgo.DeviceCallbacks{Data: onSendFrames})
	if err != nil {
		p.playing.Store(false)
		return nil, errors.New(op).WithErr(err)
	}

	p.device = device

	if err := device.Start(); err != nil {
		device.Uninit()
		p.device = nil
		p.playing.Store(false)
		return nil, errors.New(op).WithErr(err)
	}

	return done, nil
}

// Stop halts output and releases the device without releasing the audio
// context (Close releases the context). Returns ErrNotPlaying if no waveform
// is active. Idempotent against the playing flag.
func (p *Player) Stop() error {
	if !p.playing.CompareAndSwap(true, false) {
		return ErrNotPlaying
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.device != nil {
		if err := p.device.Stop(); err != nil {
			p.config.Logger.WarnWith().Err(err).Msg("device stop")
		}
		p.device.Uninit()
		p.device = nil
	}
	p.buf = nil
	return nil
}

// Close releases all audio resources, halting any active playback first. It is
// terminal and idempotent: once closed, Init / ListDevices / Play return
// ErrClosed, and a second Close is a no-op.
func (p *Player) Close() error {
	const op errors.Op = "playback.Player.Close"

	if err := p.Stop(); err != nil && !stderr.Is(err, ErrNotPlaying) {
		p.config.Logger.WarnWith().Err(err).Msg("stop on close")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed.Store(true)

	if p.ctx != nil {
		if err := p.ctx.Uninit(); err != nil {
			return errors.New(op).WithErr(err)
		}
		p.ctx.Free()
		p.ctx = nil
	}
	return nil
}

// IsPlaying reports whether a waveform is currently active.
func (p *Player) IsPlaying() bool {
	return p.playing.Load()
}
