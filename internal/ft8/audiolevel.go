package ft8

import (
	"context"
	"math"

	"github.com/ColonelBlimp/station-manager/internal/safego"
)

// RX audio-level meter (dogfood 2026-08-06): peak + RMS dBFS over fixed
// windows of the capture stream, fed from the source→scheduler tee. Pure
// arithmetic, CGO-free, single-goroutine (the tee's) — no locking needed.
// The daemon publishes MEASUREMENTS; classification against the
// operator-calibratable thresholds is the SPA's (config-served bounds).

// audioLevelFloorDbfs is the reported level for silence — a finite number,
// because JSON cannot carry -Inf and the SPA must render "silent but alive"
// as a state distinct from "no capture" (which publishes nothing at all).
const audioLevelFloorDbfs = -120.0

// audioLevelWindowSamples sizes the measurement window: 250 ms at the
// pipeline's 12 kHz → ~4 Hz on the wire. Snappy enough to watch while
// turning a gain knob, light enough to be noise on the SSE stream.
const audioLevelWindowSamples = 3000

// AudioLevel is the ft8-audio-level SSE payload: one measurement window of
// the incoming capture audio. dBFS (0 = full-scale int16), rounded to
// 0.1 dB — display precision, not a lab instrument.
type AudioLevel struct {
	PeakDbfs float64 `json:"peak_dbfs"`
	RmsDbfs  float64 `json:"rms_dbfs"`
}

type audioLevelMeter struct {
	window int
	emit   func(peakDbfs, rmsDbfs float64)

	// Window in progress — survives across feed() batches, because capture
	// hands samples in device-period chunks that never align with windows.
	n     int
	peak  int32
	sumSq float64
}

func newAudioLevelMeter(window int, emit func(peakDbfs, rmsDbfs float64)) *audioLevelMeter {
	return &audioLevelMeter{window: window, emit: emit}
}

func (m *audioLevelMeter) feed(batch []int16) {
	for _, s := range batch {
		v := int32(s)
		if v < 0 {
			v = -v // int16 min negates safely in int32
		}
		if v > m.peak {
			m.peak = v
		}
		m.sumSq += float64(v) * float64(v)
		m.n++
		if m.n == m.window {
			m.emit(dbfs(float64(m.peak)), dbfs(math.Sqrt(m.sumSq/float64(m.n))))
			m.n, m.peak, m.sumSq = 0, 0, 0
		}
	}
}

// teeAudioLevel interposes the level meter between the capture source and the
// scheduler: every batch is measured and forwarded UNTOUCHED. A session
// goroutine (joins s.wg, so the release drain waits for it); exits on source
// close OR ctx cancel — the ctx arm on the forward matters, because the
// scheduler exits on cancel without draining, and a tee blocked on a send
// nobody will receive would wedge releaseCaptureLocked's s.wg.Wait forever.
func (s *Service) teeAudioLevel(ctx context.Context, in <-chan []int16) <-chan []int16 {
	out := make(chan []int16)
	meter := newAudioLevelMeter(audioLevelWindowSamples, func(peak, rms float64) {
		// hub.publish is mutex-guarded with non-blocking per-subscriber sends,
		// so publishing from the tee goroutine cannot stall the sample flow.
		s.hub.publish(hubEvent{name: EventAudioLevel, payload: AudioLevel{
			PeakDbfs: math.Round(peak*10) / 10,
			RmsDbfs:  math.Round(rms*10) / 10,
		}})
	})
	safego.GoTracked(ctx, "ft8.audiolevel", s.onPanic, func() {
		defer close(out)
		for {
			select {
			case batch, ok := <-in:
				if !ok {
					return
				}
				meter.feed(batch)
				select {
				case out <- batch:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}, false, &s.wg)
	return out
}

// dbfs converts a linear amplitude (int16 units) to dB re full scale (32768),
// clamped to the finite silence floor.
func dbfs(x float64) float64 {
	if x <= 0 {
		return audioLevelFloorDbfs
	}
	d := 20 * math.Log10(x/32768)
	if d < audioLevelFloorDbfs {
		return audioLevelFloorDbfs
	}
	return d
}
