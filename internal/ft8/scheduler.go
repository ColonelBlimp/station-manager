package ft8

import (
	"context"
	"sync/atomic"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const (
	// slotSeconds is the FT8 slot length in whole seconds. FT4 uses 7.5 s
	// slots; SM is FT8-only, so this is a const.
	slotSeconds = 15

	// SlotDuration is the FT8 transmit / decode window length. A new slot
	// boundary lands every SlotDuration seconds on UTC: …14:30:00,
	// …14:30:15, …14:30:30, …14:30:45, …14:31:00, etc.
	SlotDuration = slotSeconds * time.Second

	// SlotSamples is the number of 12 kHz mono samples in one FT8 slot
	// (slotSeconds × go-ft8's SampleRate = 180000). This is exactly the
	// frame length go-ft8's checked decode API requires, and the ring
	// capacity the scheduler snapshots on each boundary.
	SlotSamples = goft8.SampleRate * slotSeconds

	// SchedulerSlotChannelBufferSize is the capacity of the Slots channel.
	// One pending slot is enough headroom for the decode worker to be busy
	// when the next boundary fires; extra capacity just delays back-
	// pressure visibility.
	SchedulerSlotChannelBufferSize = 1
)

// Slot is one completed 15-second window of audio ready for decode.
type Slot struct {
	// StartUTC is the boundary at which this slot started — i.e. the UTC
	// second that was a multiple of 15 immediately before the scheduler
	// fired. The slot's samples cover [StartUTC, StartUTC+SlotDuration).
	StartUTC time.Time

	// OffsetMs is how late the slot boundary timer actually fired relative
	// to its target UTC boundary, in milliseconds. Healthy values are
	// under ~100 ms on a non-overloaded host; large values suggest
	// scheduling pressure or a stalled consumer.
	OffsetMs int64

	// Samples is a fresh copy of the SlotSamples int16 samples that
	// preceded StartUTC + SlotDuration. The slot owns the slice — the
	// scheduler does not retain or mutate it after delivery — so the
	// decode worker can hand it straight to DecodeSlot.
	Samples []int16
}

// Scheduler drains a continuous audio source (typically the capture
// layer's samples channel) into a ring buffer and emits one Slot per UTC
// 15-second boundary.
//
// Usage:
//
//	sch := ft8.NewScheduler(capture.Samples(), logging.Noop())
//	go func() {
//	    if err := sch.Run(ctx); err != nil { ... }
//	}()
//	for slot := range sch.Slots() {
//	    ft8.DecodeSlot(slot.Samples, true, log)
//	}
//
// Concurrency: Run owns its goroutine. Slots is safe to read from any
// goroutine. Dropped is a lock-free counter.
type Scheduler struct {
	source <-chan []int16
	log    logging.Logger
	out    chan Slot

	dropped atomic.Int64
}

// NewScheduler constructs a Scheduler reading from source. nil logger →
// logging.Noop().
func NewScheduler(source <-chan []int16, log logging.Logger) *Scheduler {
	if log == nil {
		log = logging.Noop()
	}
	return &Scheduler{
		source: source,
		log:    log,
		out:    make(chan Slot, SchedulerSlotChannelBufferSize),
	}
}

// Slots returns the receive-only channel of completed slots. The channel
// is closed when Run returns.
func (s *Scheduler) Slots() <-chan Slot { return s.out }

// Dropped returns the number of slots that were discarded because the
// Slots channel was full when the boundary fired. A non-zero value means
// the decode consumer is too slow to keep up with the slot cadence; the
// live-path acceptance bar expects this to stay at zero.
func (s *Scheduler) Dropped() int64 { return s.dropped.Load() }

// Run blocks until ctx is cancelled or the source channel closes. Drains
// source into the ring buffer continuously; on every UTC 15-second
// boundary, snapshots the ring and emits a Slot.
func (s *Scheduler) Run(ctx context.Context) error {
	defer close(s.out)

	ring := newSampleRing(SlotSamples)

	target := nextSlotBoundary(time.Now().UTC())
	timer := time.NewTimer(time.Until(target))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case batch, ok := <-s.source:
			if !ok {
				return nil
			}
			ring.Append(batch)

		case <-timer.C:
			now := time.Now().UTC()
			// Normal: the timer fired ~at target; emit the slot that just ended
			// there. Delayed past the NEXT boundary (a slow goroutine): the ring now
			// holds a later slot's audio, so a target-stamped emit would carry the
			// wrong StartUTC/parity — and the sequencer keys rung timing off parity.
			// Skip the stale slot and resync at the next future boundary (review M2).
			if staleTarget(now, target) {
				s.log.WarnWith().
					Str("missed_target", target.Format(time.RFC3339)).
					Str("now", now.Format(time.RFC3339)).
					Msg("ft8.scheduler: timer delayed past slot boundary; skipping stale slot")
			} else {
				s.emitSlot(ring, target, now)
			}
			target = nextSlotBoundary(now)
			timer.Reset(time.Until(target))
		}
	}
}

// staleTarget reports whether the scheduler goroutine was delayed past the slot
// that ended at target — i.e. now has already reached the next boundary. When
// true the ring no longer holds target's slot, so emitting it would mis-stamp
// StartUTC/parity; the caller skips it and resyncs (review M2).
func staleTarget(now, target time.Time) bool {
	return !now.Before(target.Add(SlotDuration))
}

// emitSlot snapshots the ring and tries to publish the slot. If the ring
// has not yet seen a full SlotDuration of audio (cold start), the slot is
// silently skipped — emitting a half-filled buffer would produce
// false-decode noise. Once enough samples have accumulated, every
// subsequent boundary fires a slot. now is the actual service time (not the
// timer payload), so OffsetMs reflects real servicing delay (review M2).
func (s *Scheduler) emitSlot(ring *sampleRing, target, now time.Time) {
	if ring.Filled() < int64(ring.Cap()) {
		return
	}
	slot := Slot{
		StartUTC: target.Add(-SlotDuration),
		OffsetMs: now.Sub(target).Milliseconds(),
		Samples:  ring.Snapshot(),
	}
	select {
	case s.out <- slot:
	default:
		s.dropped.Add(1)
		s.log.WarnWith().
			Int64("offset_ms", slot.OffsetMs).
			Int64("total_dropped", s.dropped.Load()).
			Msg("ft8.scheduler slot dropped (consumer backpressure)")
	}
}

// nextSlotBoundary returns the next time strictly after now whose
// .Second() is a multiple of SlotDuration/time.Second and whose
// sub-second component is zero. The returned time is in UTC.
//
// Examples (now → next):
//
//	14:30:07.4 → 14:30:15.000
//	14:30:14.999 → 14:30:15.000
//	14:30:15.000 → 14:30:30.000  (strictly after)
//	14:30:59.000 → 14:31:00.000
func nextSlotBoundary(now time.Time) time.Time {
	now = now.UTC()
	slotSecs := int(SlotDuration / time.Second)
	// Round down to the current slot start, then add one slot.
	currentSlotStart := time.Date(
		now.Year(), now.Month(), now.Day(),
		now.Hour(), now.Minute(),
		(now.Second()/slotSecs)*slotSecs,
		0, time.UTC,
	)
	return currentSlotStart.Add(SlotDuration)
}
