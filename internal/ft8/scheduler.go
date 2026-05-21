package ft8

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// SlotDuration is the FT8 transmit / decode window length. A new slot
// boundary lands every SlotDuration seconds on UTC: …14:30:00,
// …14:30:15, …14:30:30, …14:30:45, …14:31:00, etc.
//
// FT4 uses 7.5 s slots; SM is FT8-only at M4 so this is a const.
const SlotDuration = 15 * time.Second

// SchedulerSlotChannelBufferSize is the capacity of the Slots channel.
// One pending slot is enough headroom for the decode worker to be busy
// when the next boundary fires; extra capacity just delays back-
// pressure visibility.
const SchedulerSlotChannelBufferSize = 1

// Slot is one completed 15-second window of audio ready for decode.
type Slot struct {
	// StartUTC is the boundary at which this slot started — i.e. the
	// UTC second that was a multiple of 15 immediately before the
	// scheduler fired. The slot's samples cover [StartUTC,
	// StartUTC+SlotDuration).
	StartUTC time.Time

	// OffsetMs is how late the slot boundary timer actually fired
	// relative to its target UTC boundary, in milliseconds. Healthy
	// values are under ~100 ms on a non-overloaded host; large values
	// (> SlotOffsetWarnMs) suggest scheduling pressure or a stalled
	// consumer.
	OffsetMs int64

	// Samples is a fresh copy of the dsp.NMAX float32 samples that
	// preceded StartUTC + SlotDuration. The slot owns the slice — the
	// scheduler does not retain or mutate it after delivery.
	Samples []float32
}

// Scheduler drains a continuous audio source (typically a
// capture.Capture's Samples channel) into a ring buffer and emits one
// Slot per UTC 15-second boundary.
//
// Usage:
//
//	sch := ft8.NewScheduler(capture.Samples(), logging.Noop())
//	go func() {
//	    if err := sch.Run(ctx); err != nil { ... }
//	}()
//	for slot := range sch.Slots() {
//	    decoded := ft8.Decode(slot.Samples, ft8.DecodeOptions{})
//	    ...
//	}
//
// Concurrency: Run owns its goroutine. Slots is safe to read from
// any goroutine. Dropped is a lock-free counter.
type Scheduler struct {
	source <-chan []float32
	log    logging.Logger
	out    chan Slot

	dropped atomic.Int64
}

// NewScheduler constructs a Scheduler reading from source. nil logger
// → logging.Noop().
func NewScheduler(source <-chan []float32, log logging.Logger) *Scheduler {
	if log == nil {
		log = logging.Noop()
	}
	return &Scheduler{
		source: source,
		log:    log,
		out:    make(chan Slot, SchedulerSlotChannelBufferSize),
	}
}

// Slots returns the receive-only channel of completed slots. The
// channel is closed when Run returns.
func (s *Scheduler) Slots() <-chan Slot { return s.out }

// Dropped returns the number of slots that were discarded because the
// Slots channel was full when the boundary fired. A non-zero value
// means the decode consumer is too slow to keep up with the slot
// cadence; M4.2's acceptance bar expects this to stay at zero.
func (s *Scheduler) Dropped() int64 { return s.dropped.Load() }

// Run blocks until ctx is cancelled or the source channel closes.
// Drains source into the ring buffer continuously; on every UTC 15-
// second boundary, snapshots the ring and emits a Slot.
func (s *Scheduler) Run(ctx context.Context) error {
	defer close(s.out)

	ring := newSampleRing(dsp.NMAX)

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

		case fired := <-timer.C:
			s.emitSlot(ring, target, fired)
			target = target.Add(SlotDuration)
			timer.Reset(time.Until(target))
		}
	}
}

// emitSlot snapshots the ring and tries to publish the slot. If the
// ring has not yet seen a full SlotDuration of audio (cold start), the
// slot is silently skipped — emitting a half-filled buffer would
// produce false-decode noise. Once enough samples have accumulated,
// every subsequent boundary fires a slot.
func (s *Scheduler) emitSlot(ring *sampleRing, target, fired time.Time) {
	if ring.Filled() < int64(ring.Cap()) {
		return
	}
	slot := Slot{
		StartUTC: target.Add(-SlotDuration),
		OffsetMs: fired.Sub(target).Milliseconds(),
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
// .Second() is a multiple of SlotDuration/time.Second and whose sub-
// second component is zero. The returned time is in UTC.
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
