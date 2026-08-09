package bridge

import (
	"context"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
)

// ADR 0064 — continuous ALC/PO meter polling while an FT8 capture session is
// live. The rigdef's METERPOLL command (FTdx10: `RM4;RM5;`) is the per-rig
// capability flag; a rig that doesn't declare it never meter-polls, which is
// the ADR's "a rig whose CAT reference restricts during-TX reads" trigger
// built in as data.
//
// This is the bridge's one deliberate source of keyed-interval CAT traffic.
// It is safe where the ADR 0035 Icom snapshot poll is not because the shape
// is different: ONE two-frame ASCII burst (12 bytes, ~3 ms at 38400 baud),
// not a five-frame CI-V burst holding cmdMu for seconds — which is why
// runMeterPollLoop polls THROUGH keyed intervals while runPollLoop hard-skips
// them (the divergence is the point: mid-TX ALC is the feature, measured
// answering live on 2026-08-06). The unkey's worst-case wait behind a poll
// write is ~3 ms healthy, and on a WEDGED port the serial write watchdog
// (500 ms default since the 0064 amendment, operator-ratified 2026-08-07) —
// NOT ft8MeterPollTimeout, which cannot interrupt a blocked write(2) (codex
// P1 on d7c4dcdc). A lost answer is a skipped cycle, never a retry
// (invariant 2).

// meterPollCommandName is the optional rigdef command holding the meter-query
// burst. Optional exactly like POLL: absent → no meter poll loop.
const meterPollCommandName = "METERPOLL"

// Defaults, operator-ratified 2026-08-06 (ADR 0064). Config overrides live in
// bridge.timeouts.{ft8_meter_poll_interval_ms,ft8_meter_poll_timeout_ms}.
var (
	ft8MeterPollInterval = 250 * time.Millisecond
	ft8MeterPollTimeout  = 100 * time.Millisecond
)

// meterAnswerStaleAfter is how many consecutive WRITTEN-but-unanswered polls
// raise the one sustained-loss notice (ADR 0064 invariant 2: "sustained loss
// at most surfaces a monitoring notice"). 8 polls ≈ 2 s at the default
// cadence — long enough to ride out a TX→RX tail eating a couple of answers.
// Written polls, not wall time: a skipped or never-scheduled poll cannot have
// lost an answer (codex 2026-08-09 P3).
// PROVISIONAL: not operator-ratified; adjust freely.
const meterAnswerStaleAfter = 8

// SetFt8CaptureLive gates the ADR 0064 meter poll on the FT8 capture-session
// lifecycle (invariant 5: the session lifecycle IS the state machine — no
// windowing of its own). Wired in cmd/smd from the FT8 service's capture
// listener; idempotent, safe with no pipeline running.
func (s *Service) SetFt8CaptureLive(live bool) {
	s.mu.Lock()
	s.ft8CaptureLive = live
	if !live {
		// The loss window is session-scoped: a notice must not carry from
		// one capture session into the next.
		s.meterUnansweredPolls = 0
		s.meterAnswerStale = false
	}
	s.mu.Unlock()
}

// runMeterPollLoop fires the rigdef's METERPOLL burst on its own ticker while
// an FT8 capture session is live — receive and transmit alike (see the file
// header for why keyed intervals are polled through, not skipped). Answers
// ride the normal readLoop → decode path and publish via publishMeterAnswers;
// this loop only writes. Single-flight is structural: one write per tick,
// sequential by construction.
func (s *Service) runMeterPollLoop(ctx context.Context, client serial.Client, pollBytes []byte, civ bool) {
	ticker := time.NewTicker(s.ft8MeterPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			live := s.ft8CaptureLive
			busy := time.Since(s.lastBroadcastAt) < s.civPollQuiet
			s.mu.Unlock()
			if !live || busy {
				continue
			}
			// The ctx deadline bounds the HEALTHY path (and the CI-V
			// between-frames gap); it CANNOT interrupt a single blocked
			// write(2) — serial honours ctx only between writes (codex P1 on
			// d7c4dcdc). The fault-path bound on how long an unkey can queue
			// behind a wedged poll write is the serial write watchdog
			// (write_watchdog_ms, 500 ms default since the 0064 amendment),
			// which frees writeMu by closing the port. Healthy hold: one
			// 12-byte burst, ~3 ms at 38400 baud. No retry on any failure —
			// a missed cycle recovers at the next tick (invariant 2).
			//
			// Count BEFORE the write: the answer is decoded on the readLoop
			// goroutine and can be processed before this write call returns,
			// and a count taken afterwards loses the race with its own
			// answer — the reset lands first and the increment leaves an
			// answered poll counted as unanswered (codex 2026-08-09 P2 on
			// 2653e859). Counting first makes the reset always win.
			s.countMeterPollWritten()
			wctx, cancel := context.WithTimeout(ctx, s.ft8MeterPollTimeout)
			err := s.underCmdMuCIV(civ, func() error {
				return s.writeSnapshotReads(wctx, client, civ, pollBytes)
			})
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					s.logger.WarnWith().Str("error", errMessage(err)).
						Msg("bridge: ft8 meter poll write failed")
				}
				s.retractMeterPollWritten()
				continue
			}
			s.checkMeterPollLoss()
		}
	}
}

// countMeterPollWritten counts a poll toward the sustained-loss window,
// BEFORE its bytes are written (see the call site for why the order is
// load-bearing). Skipped ticks (capture gate off, broadcast-storm quiet
// window) never reach here, and pipeline teardown resets the count — a poll
// that was never written cannot have lost an answer, so neither may age the
// window (codex 2026-08-09 P3: the wall-clock predecessor warned on the
// first poll written after a reconnect or storm, before its answer could
// arrive).
func (s *Service) countMeterPollWritten() {
	s.mu.Lock()
	s.meterUnansweredPolls++
	s.mu.Unlock()
}

// retractMeterPollWritten undoes countMeterPollWritten when the write FAILED —
// a poll that never left the host cannot lose an answer. Floored at zero
// rather than trusted: a previous poll's answer arriving between the count
// and the retraction has already reset the window, and the retraction must
// not drive it negative.
func (s *Service) retractMeterPollWritten() {
	s.mu.Lock()
	if s.meterUnansweredPolls > 0 {
		s.meterUnansweredPolls--
	}
	s.mu.Unlock()
}

// checkMeterPollLoss raises the one sustained-loss notice when
// meterAnswerStaleAfter consecutive written polls have gone unanswered.
// Recovery (any answer) re-arms it, so the log carries one line per loss
// episode, not one per silent cycle.
func (s *Service) checkMeterPollLoss() {
	s.mu.Lock()
	fire := s.meterUnansweredPolls >= meterAnswerStaleAfter && !s.meterAnswerStale
	if fire {
		s.meterAnswerStale = true
	}
	s.mu.Unlock()
	if fire {
		s.logger.WarnWith().Int("polls", meterAnswerStaleAfter).
			Msg("bridge: ft8 meter poll answers missing (rig silent on RM4/RM5)")
	}
}

// publishMeterAnswers fans decoded RM4/RM5 poll answers out on the rig-meters
// SSE event. Called from readLoop beside observeMeter. ONLY the query answers
// publish here — they name their meter in the frame; the pushed RM0 stream
// does not say which meter it is (that is METERSEL's job) and stays off this
// event. Matched by decoded tag, which the rigdef derives from the frame
// prefix — never by arrival order (ADR 0064 invariant 3; the push stream
// interleaves freely, observed live).
func (s *Service) publishMeterAnswers(status cat.Status) {
	for _, tag := range []string{"ALC", "PO"} {
		v, ok := status[tag]
		if !ok || v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			continue // a garbled frame is not a reading
		}
		s.mu.Lock()
		s.meterUnansweredPolls = 0
		s.meterAnswerStale = false
		s.mu.Unlock()
		s.hub.publish(Event{Name: EventRigMeters, Payload: RigMetersPayload{Meter: tag, Value: n}})
	}
}
