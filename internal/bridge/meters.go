package bridge

import (
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// Per-transmission rig-meter observation (follow-up (d)).
//
// The FTdx10 marks RM READ METER as AI=O in its command index (CAT ref 2308-F
// p.5), so the rig pushes meter frames unprompted once AUTO mode is armed by
// the rigdef's INIT. Nothing here writes to the rig: adding CAT traffic to the
// key-down path would put frames on a half-duplex bus that ADR 0057 already
// documents as dropping commands in the TX→RX tail, and the guaranteed-stop
// unkey is the one write that must never queue behind anything.
//
// Readings are accumulated per keyed transmission and summarised in ONE log
// line when it ends, rather than logged per frame. That choice needs no
// invented sampling interval — the boundaries are the key/unkey events the
// bridge already knows exactly — and it cannot flood however fast the rig
// pushes. The per-transmission Count is also what tells us the push rate,
// which is not documented anywhere.

// meterTags are the rigdef state tags accumulated per transmission. An
// allowlist rather than "every numeric tag": a future rigdef state (S-meter,
// IDD, VDD) must not silently join the summary and change what the operator
// is reading. The allowlist is applied twice — here and again in
// flushFt8TxMetersLocked — and only the flush-side check is load-bearing for
// correctness. This one exists so the constantly-arriving non-meter tags
// (VFOAFREQ on every dial push, MAINMODE, TXPWR) don't allocate a junk
// accumulator entry per decoded frame for the length of a transmission.
var meterTags = []string{"ALC", "PO", "SWR"}

// meterSample is one meter's readings across a single keyed transmission.
// Values are the rig's raw 0-255 meter scale, NOT engineering units: the CAT
// reference documents no conversion to watts or SWR ratio, and inventing one
// would put a fabricated number in the log where a real reading belongs.
type meterSample struct {
	Tag            string
	Count          int
	Min, Max, Last int
}

// ft8MeterSummary is what one transmission's meter pushes amounted to.
// Present distinguishes "a transmission ended and the rig pushed nothing" from
// "no transmission has ended yet" — an absence of readings is itself the
// answer to whether this rig pushes RM at all, so it must be reportable rather
// than silent.
type ft8MeterSummary struct {
	Present bool
	Samples []meterSample
}

// observeMeter accumulates a decoded meter push against the transmission in
// flight. Readings taken outside a keyed transmission are discarded: PO reads
// ~0 in receive, so accumulating them would peg every transmission's minimum
// at zero and hide the very fault this exists to catch.
func (s *Service) observeMeter(status cat.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ft8TxActive {
		return
	}
	for _, tag := range meterTags {
		v, ok := status[tag]
		if !ok || v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			continue // a garbled frame is not a reading
		}
		acc, seen := s.ft8Meters[tag]
		if !seen {
			if s.ft8Meters == nil {
				s.ft8Meters = make(map[string]*meterSample, len(meterTags))
			}
			acc = &meterSample{Tag: tag, Min: n, Max: n}
			s.ft8Meters[tag] = acc
		}
		acc.Count++
		if n < acc.Min {
			acc.Min = n
		}
		if n > acc.Max {
			acc.Max = n
		}
		acc.Last = n
	}
}

// flushFt8TxMeters returns the readings accumulated during the transmission
// that just ended and clears the accumulator for the next one.
func (s *Service) flushFt8TxMeters() ft8MeterSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushFt8TxMetersLocked()
}

// flushFt8TxMetersLocked is the caller-holds-s.mu form, so the end-of-TX path
// can clear meters in the same critical section that clears the TX flags —
// a reading decoded between the two would otherwise be filed against a
// transmission that had already ended.
func (s *Service) flushFt8TxMetersLocked() ft8MeterSummary {
	sum := ft8MeterSummary{Present: true}
	for _, tag := range meterTags {
		if acc, ok := s.ft8Meters[tag]; ok && acc.Count > 0 {
			sum.Samples = append(sum.Samples, *acc)
		}
	}
	s.ft8Meters = nil
	return sum
}

// lastFt8TxMeters reports the summary from the most recently ended
// transmission.
func (s *Service) lastFt8TxMeters() ft8MeterSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ft8MeterLast
}

// logFt8TxMeters emits the one-line-per-transmission summary. Called
// UNCONDITIONALLY at the end of every FT8 transmission, including those where
// the rig pushed nothing: "no meter data" is a finding, not a non-event.
// Called without s.mu held — a stalled log write must not block the read loop.
func (s *Service) logFt8TxMeters(sum ft8MeterSummary) {
	if !sum.Present {
		return
	}
	if len(sum.Samples) == 0 {
		s.logger.InfoWith().Msg("bridge: ft8 tx meters — rig pushed no meter data for this transmission")
		return
	}
	e := s.logger.InfoWith()
	names := make([]string, 0, len(sum.Samples))
	for _, m := range sum.Samples {
		k := strings.ToLower(m.Tag)
		e = e.Int(k+"_min", m.Min).Int(k+"_max", m.Max).Int(k+"_last", m.Last).Int(k+"_n", m.Count)
		names = append(names, k)
	}
	// Meters are the rig's raw 0-255 scale — stated in the message so a reading
	// is never mistaken for watts.
	e.Str("meters", strings.Join(names, ",")).
		Msg("bridge: ft8 tx meters (raw 0-255 scale)")
}
