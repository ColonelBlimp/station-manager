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

	// OmittedBefore counts the physical slots the scheduler failed to
	// deliver immediately before this one — channel-full drops plus
	// late-boundary skips since the last successful emit. Mid-session the
	// consumer already infers omissions from StartUTC gaps (AC8), so this
	// matters for the FIRST delivered slot of a session, which has no
	// predecessor to infer from (review 68514620 P1). Cold-start boundaries
	// (ring not yet full) are not omissions — no complete slot of session
	// audio existed to lose.
	OmittedBefore int

	// DialMHz is the rig dial frequency this slot's audio was captured on, or
	// 0 when the slot could not be placed on one (no CAT, the frequency
	// unknown, or the dial moved during the window). Occupancy is band-specific
	// decision data, so a consumer must not attribute a report to a band
	// without this: the alternative — labelling a report with whatever band the
	// rig is on when the report lands — is wrong for every slot in flight
	// across a band change, and no downstream clock comparison can repair it (a
	// report's publication lags its capture by the decode, so
	// distance-from-the-last-report cannot establish that the capture happened
	// after the QSY).
	DialMHz float64

	// DialChanged reports that the dial moved DURING this slot: its audio spans
	// two frequencies and belongs to neither.
	//
	// LOAD-BEARING, not merely diagnostic (it was diagnostic when introduced —
	// DialMHz is 0 either way — and became load-bearing a round later). It is what
	// suppresses the slot's DECODES: every consumer resolves a decode against the
	// CURRENT dial, so an A→B→A window would render stations heard on B as
	// workable here and spot them at the wrong frequency. It also separates "the
	// operator was tuning" from "CAT was down" in the slot log — the first question
	// on air when the panel goes quiet.
	DialChanged bool

	// DialTracked reports that a dial source was installed for this capture
	// session, i.e. the daemon is CAT-attached and every slot is EXPECTED to
	// carry a frequency. It separates "no CAT, nothing to attribute with" from
	// "CAT present but this slot could not be placed", which consumers must
	// treat differently: FT8 transmit is refused without a writable rig (the
	// keyer's TxReady shares that precondition with the dial read), so an
	// unattributed slot on an untracked session can only mislead a display,
	// while on a tracked one it could steer a transmission.
	DialTracked bool

	// Starved reports that FEWER than minLiveWindowSamples fresh samples
	// arrived during this window, so the ring's snapshot is mostly the PRIOR
	// slot's audio. LOAD-BEARING like DialChanged: decoding a starved
	// snapshot surfaces prior-slot messages as current, drives the sequencer
	// off them, and records a false `decoded` coverage — so every consumer
	// suppresses it as capture loss (package review, 2026-08-10). The
	// dead-source watchdog (deadsource.go) still tracks starvation
	// SEPARATELY for its 2-strike release alarm; this per-slot flag is the
	// immediate suppression the alarm's strike delay cannot provide.
	Starved bool
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

	// dead is the dead-capture-stream watchdog (see deadsource.go): driven at
	// batch arrival + every boundary, inert unless SetOnDeadSource installed a
	// callback. Lives here because the boundary timer fires even when the
	// source delivers nothing — the failure mode being watched for.
	dead deadSourceMonitor

	// dialSource reads the rig's current dial frequency in MHz (ok=false when
	// unknown). Sampled on every audio batch AND at every boundary — see
	// observeDial for why endpoints alone are not enough. Installed before Run;
	// all of this is owned by Run's goroutine, so no lock is needed.
	dialSource  func() (float64, bool)
	onDialMoved func(fromMHz, toMHz float64)
	lastDial    float64
	lastDialOK  bool
	// lastKnownDial is the last reading that actually carried a frequency, held
	// separately so a QSY made while CAT was quiet is still seen: comparing only
	// ADJACENT readings lets the unknown sample overwrite the frequency, and
	// A -> unknown -> B then looks identical to a blink (codex P1 on 7c2e66ad).
	lastKnownDial   float64
	lastKnownDialOK bool
	dialSampled     bool // lastDial/lastDialOK hold a real reading
	slotDialMoved   bool // the dial moved somewhere inside the slot being captured

	dropped atomic.Int64

	// undeliveredStart/undeliveredN track the CONSECUTIVE run of physical
	// slots not delivered (dropped on a full channel, or skipped past the
	// lateness budget) since the last successful emit. Written only from the
	// Run goroutine; the Service reads the trailing run via UndeliveredTail
	// AFTER Run returns (its wg.Wait supplies the happens-before), so plain
	// fields suffice. A successful emit stamps the run onto the delivered
	// slot (Slot.OmittedBefore) and resets it — what remains at Run exit is
	// exactly the tail no later slot could ever report (review 68514620 P1).
	undeliveredStart time.Time
	undeliveredN     int

	// prevBoundaryFilled is the ring's total fill count at the PREVIOUS
	// boundary, so a boundary's delta (this window's fresh arrivals) is
	// computable for the starvation flag. Updated every boundary; a normal
	// window delivers ~SlotSamples, a starved one < minLiveWindowSamples.
	// Independent of the dead-source watchdog's own lastFilled (that drives
	// the multi-strike release alarm; this drives per-slot suppression).
	prevBoundaryFilled int64
	// starveResync suppresses the starvation flag for exactly the next
	// boundary after a lateness SKIP (review P2 on bf07a552): a resync jumps
	// to the next UTC boundary, so the delta into it is a short remainder,
	// but its ring snapshot is a full continuously-captured window — the
	// partial delta must not flag it. boundaryStarved re-primes the baseline
	// and returns false when set.
	starveResync bool
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

// SetOnDeadSource installs the dead-source callback (fired at most once per
// Run, from the scheduler goroutine — the callback must not block; the
// Service's handler hands off to a goroutine). Must be called before Run.
func (s *Scheduler) SetOnDeadSource(cb func(reason string)) { s.dead.onDead = cb }

// SetDialSource installs the rig dial-frequency reader used to attribute each
// slot to the frequency it was captured on (see Slot.DialMHz). It is called on
// every audio batch from the scheduler goroutine, so it must be a cheap cached
// read — never a CAT round-trip. Must be called before Run; leaving it unset
// leaves every slot unattributed AND untracked, which is the correct no-CAT
// behaviour.
func (s *Scheduler) SetDialSource(fn func() (float64, bool)) { s.dialSource = fn }

// SetOnDialMoved installs the callback fired the moment a dial change is SEEN —
// per audio batch (~43 ms), not at a slot boundary. The dial guard hangs off this:
// an FT8 session is bound to one frequency, and waiting for the next rung to notice
// leaves up to 30 s in which the operator cannot tell a deliberate stop from a hang.
// Must be called before Run; the callback runs on the scheduler goroutine and must
// not block.
func (s *Scheduler) SetOnDialMoved(fn func(fromMHz, toMHz float64)) { s.onDialMoved = fn }

func (s *Scheduler) readDial() (float64, bool) {
	if s.dialSource == nil {
		return 0, false
	}
	return s.dialSource()
}

// observeDial samples the dial and folds the reading into the current slot's
// stability tracker.
//
// Called on every audio batch (~43 ms at the default capture period) as well as
// at each boundary, NOT just at the two ends of a slot. Comparing endpoints alone
// calls an A→B→A excursion stable — and band-stack recall returns to exactly the
// frequency you left, so hitting the wrong band button and correcting it inside
// one 15 s window is a real sequence, not a contrived one. That slot's audio is
// mostly B while both endpoints read A, and attributing it to A would hand the
// picker B's occupancy to choose a TX offset from.
//
// A change in the KNOWN-ness of the reading counts as a move for the same reason:
// a slot whose dial went unknown partway through cannot be placed either.
func (s *Scheduler) observeDial() {
	d, ok := s.readDial()
	// Two DIFFERENT questions, deliberately answered separately:
	//   - "is this slot placeable?" — any change, including one of KNOWN-ness,
	//     makes the window unattributable for occupancy.
	//   - "did the operator move the rig?" — only a change between two KNOWN
	//     frequencies. CAT going quiet and coming back on the same frequency is not
	//     a QSY, and treating it as one disarms a perfectly good arm (codex P2 on
	//     6e974717).
	unplaceable := s.dialSampled && (d != s.lastDial || ok != s.lastDialOK)
	// A move is judged against the last KNOWN frequency, however long ago it was
	// read — so an unreadable stretch defers the comparison rather than losing it.
	movedFrom, moved := s.lastKnownDial, ok && s.lastKnownDialOK && d != s.lastKnownDial
	if unplaceable {
		s.slotDialMoved = true
	}
	s.lastDial, s.lastDialOK, s.dialSampled = d, ok, true
	if ok {
		s.lastKnownDial, s.lastKnownDialOK = d, true
	}
	// Report the move NOW, not at the slot boundary. The dial guard hangs off this:
	// an FT8 session is bound to one frequency, and letting the next rung notice
	// leaves up to 30 s in which nothing on screen changes — long enough that a
	// working guard read on air as "moving the dial does not stop TX".
	// After the state update, so a callback that reaches back in sees a settled
	// tracker rather than the reading it was told about.
	// Carry WHAT WAS SEEN. A handler that re-reads live state loses the event: two
	// observations A->B and B->A both find the dial back at A, so neither acts even
	// though a waveform in flight jumped frequency (codex P1 on 6e974717).
	if moved && s.onDialMoved != nil {
		s.onDialMoved(movedFrom, d)
	}
}

// UndeliveredTail returns the consecutive run of physical slots not
// delivered since the last successful emit — the session's trailing loss.
// Call only after Run has returned (the Service's drain provides the
// happens-before); the release path turns it into capture_dropped coverage.
func (s *Scheduler) UndeliveredTail() (time.Time, int) {
	return s.undeliveredStart, s.undeliveredN
}

// noteLateBoundaries joins every physical slot a lateness stall consumed to
// the undelivered run — a single stall can carry the goroutine past SEVERAL
// boundaries (the timer is reset only after servicing), and Run then resyncs
// to nextSlotBoundary(now), so each boundary in [target, now] is a slot that
// will never emit: counting one per FIRING under-reported the rest (review
// c76818a8 P1). Boundary b's lost slot starts at b−SlotDuration. Only once
// the ring is full: a cold-start stall lost no session audio (emitSlot's own
// early return mirrors this — and a ring never shrinks once filled, so one
// check covers the whole stall).
func (s *Scheduler) noteLateBoundaries(ring *sampleRing, target, now time.Time) {
	if ring.Filled() < int64(ring.Cap()) {
		return
	}
	for b := target; !b.After(now); b = b.Add(SlotDuration) {
		s.noteUndelivered(b.Add(-SlotDuration))
	}
}

// noteUndelivered extends the consecutive undelivered run with the physical
// slot starting at start.
func (s *Scheduler) noteUndelivered(start time.Time) {
	if s.undeliveredN == 0 {
		s.undeliveredStart = start
	}
	s.undeliveredN++
}

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
			s.dead.observeBatch(batch)
			// Sample the dial alongside the audio it accompanies, so a mid-slot
			// excursion is seen even if it ends where it began (observeDial).
			s.observeDial()
			ring.Append(batch)

		case <-timer.C:
			now := time.Now().UTC()
			// This window's fresh-sample delta, before the watchdog consumes
			// its own counter: a starved window (too few new samples, so the
			// snapshot is mostly the prior slot's audio) is flagged on the
			// emitted slot and suppressed downstream. Updated every boundary
			// so the delta always spans exactly one window.
			starved := s.boundaryStarved(ring.Filled())
			// Watchdog first, independent of the lateness/emit logic: the dead
			// cases (starved / all-zero source) mostly never fill the ring, so
			// emitSlot's cold-start skip would hide them from any slot consumer.
			s.dead.onBoundary(ring.Filled())
			// Close the slot's dial window. Sampled here rather than inside
			// emitSlot so the tracker still advances on a skipped slot (cold
			// start / lateness resync).
			s.observeDial()
			// Normal: the timer fired ~at target (sub-50 ms); emit the slot that
			// ended there. If the goroutine was delayed more than maxSlotLateness,
			// the ring no longer represents [target-SlotDuration, target) — it has
			// shed the front of the slot and gained samples from after target — so a
			// target-stamped emit would carry a shifted window and a stale
			// StartUTC/parity (which the sequencer keys rung timing off). Skip and
			// resync at the next future boundary (review follow-up M2).
			if slotTooLate(now, target) {
				s.noteLateBoundaries(ring, target, now)
				// The resync jumps to the next UTC boundary; its delta will be
				// a short remainder against a full window, so don't let it
				// flag the next slot as starved (review P2 on bf07a552).
				s.starveResync = true
				s.log.WarnWith().
					Str("missed_target", target.Format(time.RFC3339)).
					Str("now", now.Format(time.RFC3339)).
					Int64("late_ms", now.Sub(target).Milliseconds()).
					Msg("ft8.scheduler: timer delay exceeded the lateness budget; skipping slot")
			} else {
				s.emitSlot(ring, target, now, starved)
			}
			// A new slot begins at this same boundary: its dial window starts
			// clean, anchored on the reading just taken.
			s.slotDialMoved = false
			target = nextSlotBoundary(now)
			timer.Reset(time.Until(target))
		}
	}
}

// maxSlotLateness bounds how late the scheduler goroutine may service a slot
// boundary and still emit. Beyond it the ring window has shifted enough that the
// snapshot no longer matches the target slot, so we skip+resync rather than feed
// decode/occupancy a mis-stamped slot (review follow-up M2). Default 2 s: far
// above normal jitter (sub-50 ms) and a generous GC pause, far below a full
// 15 s slot. Package var so tests can dial it.
var maxSlotLateness = 2 * time.Second

// slotTooLate reports whether the goroutine serviced the boundary later than
// maxSlotLateness — i.e. the target slot's snapshot can no longer be trusted.
func slotTooLate(now, target time.Time) bool {
	return now.Sub(target) > maxSlotLateness
}

// boundaryStarved reports whether the window ending at this boundary received
// fewer than minLiveWindowSamples FRESH samples — its ring snapshot is then
// mostly the prior slot's audio, and decoding it would surface prior-slot
// messages as current (package review, 2026-08-10). Updates the per-boundary
// baseline, so call exactly once per boundary; the delta spans exactly one
// window. Owned by the Run goroutine.
func (s *Scheduler) boundaryStarved(filled int64) bool {
	// After a lateness resync the delta is a short remainder to the next UTC
	// boundary, but the ring holds a full continuously-captured window
	// (review P2 on bf07a552). Re-prime the baseline and clear the flag so
	// this one boundary is not flagged and the FOLLOWING window measures
	// normally.
	if s.starveResync {
		s.starveResync = false
		s.prevBoundaryFilled = filled
		return false
	}
	starved := filled-s.prevBoundaryFilled < minLiveWindowSamples
	s.prevBoundaryFilled = filled
	return starved
}

// emitSlot snapshots the ring and tries to publish the slot. If the ring
// has not yet seen a full SlotDuration of audio (cold start), the slot is
// silently skipped — emitting a half-filled buffer would produce
// false-decode noise. Once enough samples have accumulated, every
// subsequent boundary fires a slot. now is the actual service time (not the
// timer payload), so OffsetMs reflects real servicing delay (review M2).
func (s *Scheduler) emitSlot(ring *sampleRing, target, now time.Time, starved bool) {
	if ring.Filled() < int64(ring.Cap()) {
		return
	}
	slot := Slot{
		StartUTC: target.Add(-SlotDuration),
		OffsetMs: now.Sub(target).Milliseconds(),
		Samples:  ring.Snapshot(),
		Starved:  starved,
	}
	// Attribute only a slot the dial held one KNOWN frequency across. Anything
	// else stays unattributed: an unplaceable slot is honest, a wrongly placed
	// one is the bug. DialTracked separates "no CAT to attribute with" from
	// "CAT present but this slot could not be placed" — the consumer must treat
	// those differently (see decodeLoop).
	slot.DialTracked = s.dialSource != nil
	slot.DialChanged = s.slotDialMoved
	if !s.slotDialMoved && s.lastDialOK {
		slot.DialMHz = s.lastDial
	}
	// Stamp the undelivered run BEFORE the send: a delivered slot reports the
	// omissions immediately before it (Slot.OmittedBefore), and a successful
	// send resets the run — leaving only a trailing tail for UndeliveredTail.
	slot.OmittedBefore = s.undeliveredN
	select {
	case s.out <- slot:
		s.undeliveredN = 0
	default:
		s.dropped.Add(1)
		s.noteUndelivered(slot.StartUTC)
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
