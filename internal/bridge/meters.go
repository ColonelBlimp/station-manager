package bridge

import (
	"sort"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// Per-transmission rig-meter observation (follow-up (d)).
//
// MEASURED ON HARDWARE, 2026-07-29 (dogfood FTdx10, via cmd/catcli). The rig
// pushes because WE enable it: the rigdef's INIT is `AI1;`, and per the CAT
// reference AI is "set to 0 (OFF) automatically when the transceiver is turned
// OFF", so auto-information is off until the bridge arms it. What then arrives
// unsolicited is:
//
//	RM0nnn000   the value of whatever meter is CURRENTLY SELECTED
//
// and nothing under RM4/RM5/RM6 — those answer explicit queries only. The CAT
// reference (2308-F) documents this: the RM page carries a P1=0 form with
// "P2: Meter 0 - 255 / P3: 000 (Fixed)", distinct from the Read form whose
// selector list runs 1:S 3:COMP 4:ALC 5:PO 6:SWR 7:IDD 8:VDD and contains no 0.
// The first version of this file modelled RM4/5/6 alone — a misreading of the
// page, not a gap in it — and two real on-air transmissions consequently
// reported "no meter data" while the rig pushed RM0 at ~26 Hz throughout both.
//
// RM0 is therefore tagged METER, not PO: the frame does not say which meter it
// is. MS METER SW does (0:PO 1:COMP 2:ALC 3:VDD 4:ID 5:SWR), so MS is in the
// rigdef's READ burst and its answer travels with the summary. Observed
// behaviour is that the meter reads S during receive and the MS-selected meter
// while transmitting — receive values sat at 103-132 matching RM1 (S-meter),
// then dropped to a 0-33 band tracking speech envelope under mic modulation.
//
// Nothing here writes to the rig. Adding CAT traffic to the key-down path would
// put frames on a half-duplex bus that ADR 0057 already documents as dropping
// commands in the TX→RX tail, and the guaranteed-stop unkey is the one write
// that must never queue behind anything.
//
// Readings are accumulated per keyed transmission and summarised in ONE log
// line when it ends, rather than logged per frame. That needs no invented
// sampling interval — the boundaries are the key/unkey events the bridge
// already knows exactly — and cannot flood however fast the rig pushes, which
// matters because the observed rate is NOT constant (~26 Hz in receive, ~4 Hz
// under voice).

// meterTags are the rigdef state tags accumulated per transmission. An
// allowlist rather than "every numeric tag": a future rigdef state (S-meter,
// IDD, VDD) must not silently join the summary and change what the operator
// is reading. The allowlist is applied twice — here and again in
// flushFt8TxMetersLocked — and only the flush-side check is load-bearing for
// correctness. This one exists so the constantly-arriving non-meter tags
// (VFOAFREQ on every dial push, MAINMODE, TXPWR) don't allocate a junk
// accumulator entry per decoded frame for the length of a transmission.
//
// METER is the pushed one and the only one populated in normal operation;
// ALC/PO/SWR are the explicit query answers, kept because they are unambiguous
// and cost nothing when absent.
var meterTags = []string{"METER", "ALC", "PO", "SWR"}

// meterSelTag is the MS METER SW selection — not a reading, so it is recorded
// rather than accumulated. Without it a pushed value is uninterpretable: "the
// meter read 210" says nothing unless you know whether that is power, ALC or
// drain voltage.
const meterSelTag = "METERSEL"

// meterPushedTag is the tag carrying RM0 — the meter the rig actually pushes.
// It is the ONLY tag whose meaning depends on the MS selection; ALC/PO/SWR name
// their meter in the query itself.
const meterPushedTag = "METER"

// meterKey identifies one accumulator. Sel is populated only for the pushed
// tag, so readings taken under different meter selections accumulate SEPARATELY
// (c49e12f2 review P2). Merging them would produce a min/max spanning two
// different physical quantities that share nothing but a 0-255 scale.
type meterKey struct {
	Tag string
	Sel string
}

// meterSample is one meter's readings across a single keyed transmission.
// Values are the rig's raw 0-255 meter scale, NOT engineering units: the CAT
// reference documents no conversion to watts or SWR ratio, and inventing one
// would put a fabricated number in the log where a real reading belongs.
//
// Min is ONSET-RELATIVE: it ignores readings before the first non-zero one.
// Measured on air 2026-07-29, every transmission reported a raw minimum of zero
// — healthy ones included — because PTT comes up a few samples ahead of the
// waveform. A raw minimum is therefore identical on a clean transmission and on
// one where drive collapsed, which is exactly the distinction this exists to
// draw. Onset-relative needs no invented settling time: the rig says when drive
// arrived by reporting a non-zero value. Drive that never arrives at all stays
// distinguishable because Max is then zero too.
type meterSample struct {
	Tag            string
	Sel            string
	Count          int
	Min, Max, Last int
	// started records that a non-zero reading has been seen, so Min tracks only
	// from drive onset. Unexported: an implementation detail of accumulation,
	// not part of what a transmission reports.
	started bool
}

// ft8MeterSummary is what one transmission's meter pushes amounted to.
// Present distinguishes "a transmission ended and the rig pushed nothing" from
// "no transmission has ended yet" — an absence of readings is itself the
// answer to whether this rig pushes RM at all, so it must be reportable rather
// than silent.
// The meter selection is NOT a summary-level field: it belongs to each reading
// (meterSample.Sel), because it can change mid-transmission.
type ft8MeterSummary struct {
	Present bool
	Samples []meterSample
}

// observeMeter consumes a decoded meter push. Two SEPARATE layers, and the
// distinction is load-bearing:
//
//   - OBSERVATION is unconditional. A pushed meter frame is rig state arriving
//     exactly like FA or MD0, so the current reading is recorded whether or not
//     anything is transmitting. An earlier version returned early when not
//     keyed and destroyed the reading, which would have left any consumer (a
//     browser meter display) blank until the operator transmitted — a
//     transmission must not be needed to bring a meter to life.
//   - The per-transmission SUMMARY is gated on ft8TxActive. PO reads ~0 in
//     receive, so folding receive-time readings into a transmission's range
//     would peg every minimum at zero and hide the very fault this exists to
//     catch.
func (s *Service) observeMeter(status cat.Status) {
	s.mu.Lock()
	announce := false
	// The selection is a mapped literal ("PO"), not a number, so it is recorded
	// rather than run through the numeric accumulation below.
	if sel, ok := status[meterSelTag]; ok && sel != "" {
		s.meterSel = sel
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
		// Layer 1 — always current.
		if s.meterLatest == nil {
			s.meterLatest = make(map[string]int, len(meterTags))
		}
		s.meterLatest[tag] = n
		if s.markMeterSeenLocked() {
			announce = true
		}
		// Layer 2 — only while this rig is actually transmitting.
		if !s.ft8TxActive {
			continue
		}
		// Bind the reading to the selection in force RIGHT NOW, not to whatever
		// is selected when the transmission ends: an MS frame arriving after this
		// reading (including in the TX→RX tail, which needs no operator action)
		// must not retro-label it.
		key := meterKey{Tag: tag}
		if tag == meterPushedTag {
			key.Sel = s.meterSel
		}
		acc, seen := s.ft8Meters[key]
		if !seen {
			if s.ft8Meters == nil {
				s.ft8Meters = make(map[meterKey]*meterSample, len(meterTags))
			}
			acc = &meterSample{Tag: tag, Sel: key.Sel, Max: n}
			s.ft8Meters[key] = acc
		}
		acc.Count++
		// Minimum tracks from drive ONSET. Leading zeros are the key-up ramp, not
		// a fault; a zero AFTER onset is the collapse being hunted.
		if n != 0 && !acc.started {
			acc.started = true
			acc.Min = n
		} else if acc.started && n < acc.Min {
			acc.Min = n
		}
		if n > acc.Max {
			acc.Max = n
		}
		acc.Last = n
	}
	s.mu.Unlock()

	// Outside the lock — a stalled log write must not block the read loop.
	if announce {
		s.logger.InfoWith().
			Str("meters", strings.Join(meterTags, ",")).
			Msg("bridge: rig pushes meter frames (RM); meter observation is live")
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
		// A tag may have several samples when the selection changed during the
		// transmission. Sorted by Sel so the summary is deterministic rather than
		// following Go's randomised map order.
		keys := make([]meterKey, 0, 2)
		for k := range s.ft8Meters {
			if k.Tag == tag {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].Sel < keys[j].Sel })
		for _, k := range keys {
			if acc := s.ft8Meters[k]; acc.Count > 0 {
				sum.Samples = append(sum.Samples, *acc)
			}
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
		k := meterFieldPrefix(m)
		e = e.Int(k+"_min", m.Min).Int(k+"_max", m.Max).Int(k+"_last", m.Last).Int(k+"_n", m.Count)
		names = append(names, k)
	}
	// Meters are the rig's raw 0-255 scale — stated in the message so a reading
	// is never mistaken for watts.
	e.Str("meters", strings.Join(names, ",")).
		Msg("bridge: ft8 tx meters (raw 0-255 scale)")
}

// meterReading is one meter's most recent value, on the rig's raw 0-255 scale.
type meterReading struct {
	Tag   string
	Value int
}

// latestMeters reports the rig's current meter readings, in meterTags order so
// a consumer renders them consistently. Populated whether or not anything is
// transmitting — this is the seam a browser meter display reads.
//
// NOT YET WIRED TO A PRODUCTION CONSUMER, and that is deliberate rather than an
// oversight (42fc869f review P1, which is correct on the facts: mapStatusToPayload
// does not map ALC/PO/SWR, so a meter-only frame publishes nothing).
//
// Publishing meters to /v1/rig/events fans them out to every connected browser,
// and the ONLY thing that decides whether that is free or a firehose is the
// rig's push RATE — which is undocumented and, as of this commit, has never
// been observed even once. Sizing a coalescing interval now would be inventing
// the number the design depends on. The measurement is already built and one
// transmission away: the per-transmission summary's Count field IS the rate.
//
// So the ordering is: observe a real transmission, read the count, then wire the
// publish path with a policy justified by it. The wider question this belongs to
// — that a tag declared in the rigdef should reach every CAT consumer without a
// second Go-side whitelist (operator, 2026-07-29) — is an ADR, because it changes
// a shipped wire contract with a live SPA on the other end.
func (s *Service) latestMeters() []meterReading {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]meterReading, 0, len(meterTags))
	for _, tag := range meterTags {
		if v, ok := s.meterLatest[tag]; ok {
			out = append(out, meterReading{Tag: tag, Value: v})
		}
	}
	return out
}

// markMeterSeenLocked records that this rig has pushed at least one meter frame
// and reports whether it was the FIRST of the pipeline lifecycle, so the caller
// announces once and stays silent thereafter. Silence matters: the push rate is
// undocumented and could be tens per second.
//
// Its value is diagnostic — with no announcement at all, a rig that does not
// push RM is distinguishable from a fault on our side WITHOUT needing a
// transmission to find out. Caller holds s.mu.
func (s *Service) markMeterSeenLocked() bool {
	if s.meterSeen {
		return false
	}
	s.meterSeen = true
	return true
}

// resetMeterObservation drops observation state at pipeline teardown. Scoped
// per-pipeline like identityVerified: a reconnect may be a different rig, or
// the same rig with AI mode no longer armed (the FTdx10 clears AI on
// power-off), so the previous instance's answer must not carry over — and its
// readings must not linger on a display as if current.
func (s *Service) resetMeterObservation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meterSeen = false
	s.meterLatest = nil
	s.meterSel = ""
	s.ft8Meters = nil
}

// meterSelection reports which meter the rig's pushed RM0 value represents,
// per the last MS frame. Empty means unknown — never guessed, because a wrong
// label on a reading is worse than an absent one.
func (s *Service) meterSelection() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meterSel
}

// meterFieldPrefix names a sample's log fields. Pushed readings are namespaced
// under "meter_" and carry their selection; explicit query answers use the meter
// name alone.
//
// The namespace is load-bearing, not cosmetic (7b35b9c1 review P2). A pushed
// reading taken while PO was selected and an RM5 query answer are INDEPENDENT
// samples of the same transmission; naming both "po_" would put duplicate keys
// in one structured log event, and a JSON consumer keeps only one of them —
// silently discarding a whole sample. It also keeps two pushed samples taken
// under different selections apart.
func meterFieldPrefix(m meterSample) string {
	if m.Tag != meterPushedTag {
		return strings.ToLower(m.Tag)
	}
	if m.Sel == "" {
		return "meter"
	}
	return "meter_" + strings.ToLower(m.Sel)
}
