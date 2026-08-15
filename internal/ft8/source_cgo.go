//go:build cgo

package ft8

import (
	"context"
	stderrors "errors"

	"github.com/ColonelBlimp/station-manager/internal/audio/capture"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// malgoSource adapts the CGO miniaudio capture layer (internal/audio/capture)
// to the Service's captureSource seam. capture.Capture delivers float32 @
// 12 kHz mono — miniaudio resamples the device's native rate internally, so
// no manual decimation is needed. This adapter converts each batch to int16
// (the FT8 pipeline's sample type) and forwards it on a channel the scheduler
// drains.
type malgoSource struct {
	cfg capture.Config
	log logging.Logger

	cap    *capture.Capture
	out    chan []int16
	done   chan struct{}
	cancel context.CancelFunc
}

// newCaptureSource builds the real (CGO) capture source. cfg.Device selects the
// input device (projected from the active rig's audio.rx by ActiveFt8): empty →
// system default; a device NAME → resolved to a live index at acquire time by
// the capture layer (the per-rig RigConfig.Audio.RX model); an integer string →
// honoured as a raw index for any un-migrated config.
func newCaptureSource(cfg types.Ft8Config, log logging.Logger) captureSource {
	if log == nil {
		log = logging.Noop()
	}
	cc := capture.DefaultConfig() // 12 kHz mono float32, default device, 512-frame period
	cc.Logger = log
	cc.DeviceName, cc.DeviceIndex = resolveAudioDevice(cfg.Device)
	return &malgoSource{cfg: cc, log: log}
}

// Start initialises and starts the audio device, then spawns the pump that
// converts and forwards samples. The returned channel closes when capture
// stops (Stop, ctx cancel, or a fatal device error).
func (m *malgoSource) Start(ctx context.Context) (<-chan []int16, error) {
	const op errors.Op = "ft8.malgoSource.Start"

	m.cap = capture.New(m.cfg)
	if err := m.cap.Init(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("init capture")
	}
	if err := m.cap.Start(ctx); err != nil {
		if closeErr := m.cap.Close(); closeErr != nil {
			return nil, errors.New(op).WithErr(stderrors.Join(err, closeErr)).
				WithMsg("start capture; releasing initialized capture also failed")
		}
		return nil, errors.New(op).WithErr(err).WithMsg("start capture")
	}

	// Own cancel so Stop can unblock pump even if the caller never cancels
	// ctx and the scheduler has stopped draining m.out.
	pumpCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.out = make(chan []int16, capture.SampleChannelBufferSize)
	m.done = make(chan struct{})
	// The pump receives the samples channel as a STABLE argument rather than
	// dereferencing m.cap per iteration — Stop nils m.cap, and a pump still
	// draining a buffered batch must not race that write (review 2026-07-20
	// round 12 #2).
	go m.pump(pumpCtx, m.cap.Samples())
	return m.out, nil
}

// pump converts float32 capture batches to int16 and forwards them until ctx
// is cancelled or the capture channel closes. It owns the close of m.out so
// the scheduler sees a clean stream end. The send is itself ctx-guarded so a
// stalled consumer can't wedge shutdown.
func (m *malgoSource) pump(ctx context.Context, samples <-chan []float32) {
	defer close(m.out)
	defer close(m.done)
	for {
		select {
		case <-ctx.Done():
			return
		case batch, ok := <-samples:
			if !ok {
				return
			}
			out := make([]int16, len(batch))
			for i, f := range batch {
				out[i] = floatToInt16(f)
			}
			select {
			case m.out <- out:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Stop cancels the pump, releases the device, and waits for the pump to drain.
// Idempotent and safe even if Start failed or was never called. The capture
// pointer is cleared so a second Stop — possible when both capture goroutines
// die at once and each runs the unexpected-exit cleanup (review 2026-07-20
// #4) — cannot double-Close the CGO device; the nil-out happens only AFTER
// <-m.done proves the pump has exited (review 2026-07-20 round 12 #2 — nilling
// before the drain raced a pump still looping on a buffered batch; the pump
// also no longer dereferences m.cap at all, taking its channel as an
// argument). Callers serialise Stop under the Service mutex.
func (m *malgoSource) Stop() error {
	if m.cap == nil {
		return nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	err := m.cap.Close() // closes capture.Samples()
	if m.done != nil {
		<-m.done
	}
	m.cap = nil
	return err
}
