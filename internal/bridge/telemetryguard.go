package bridge

// L5 (internal-codebase logging audit): malformed CAT telemetry — a VFO
// frequency or TX-power value the rig pushes that will not parse — was dropped
// silently by mapStatusToPayload. Because a dropped field reads as "unchanged"
// (omitempty + the SPA's partial-merge), the prior value stayed apparently
// current on the SSE wire AND in the rolling dial/power snapshots that stamp
// every logged FT8 QSO (CurrentDialMHz / CurrentPowerW). A single garbled frame
// could therefore log a contact on a stale frequency or power, indistinguishable
// from a fresh reading.
//
// The guard here does three things, per the operator's rulings (2026-08-15):
//   - warns once per malformed episode per tag, with the raw value bounded to
//     maxTelemetryRawLen bytes (G1/G2/G5);
//   - invalidates the affected rolling snapshot rather than retaining a stale
//     value, so attribution reports "unknown" instead of a wrong value — the
//     same "unknown beats wrong" doctrine CurrentDialMHz already documents
//     (G3, OPEN-1(b));
//   - logs one recovery line when a valid value for that tag resumes (G4).
//
// It does NOT change control flow or timing on any TX path: power invalidation
// respects the tune-restore freeze (captureTuneSnapshot's !tuneActive guard), so
// a malformed TXPWR arriving during a tune cannot corrupt the pre-tune snapshot
// StartTune will restore to.

// maxTelemetryRawLen bounds how much of an unparseable raw value reaches the log
// (OPEN-2, operator 2026-08-15). CAT telemetry is short ASCII; 32 bytes identify
// the garble without letting a pathological push write unbounded input.
const maxTelemetryRawLen = 32

// The three rig-state tags whose values are parsed (and can therefore be
// malformed). String/enum tags — mode, split, selected VFO, identity — cannot
// fail to parse (an unrecognised value passes through as-is), so they are not
// tracked here.
const (
	tagVfoAFreq = "VFOAFREQ"
	tagVfoBFreq = "VFOBFREQ"
	tagTxPwr    = "TXPWR"
)

// telemetryParseTags is the fixed evaluation order for per-frame reconciliation.
var telemetryParseTags = []string{tagVfoAFreq, tagVfoBFreq, tagTxPwr}

// telemetryParseError reports one rig-state tag whose pushed value could not be
// parsed. raw is already bounded to maxTelemetryRawLen.
type telemetryParseError struct {
	tag string
	raw string
}

// boundTelemetryRaw returns s truncated to at most maxTelemetryRawLen bytes, so
// a pathological rig push cannot write an unbounded value into smd.log.
func boundTelemetryRaw(s string) string {
	if len(s) <= maxTelemetryRawLen {
		return s
	}
	return s[:maxTelemetryRawLen]
}

// malformedTelemetryTracker latches, per telemetry tag, whether a malformed-value
// episode is currently open. Owned by a single readLoop goroutine (no lock): it
// makes a burst of garbled frames warn once, a return to valid data log one
// recovery, and a later fresh episode warn again.
type malformedTelemetryTracker struct {
	active map[string]bool
}

func newMalformedTelemetryTracker() malformedTelemetryTracker {
	return malformedTelemetryTracker{active: make(map[string]bool, len(telemetryParseTags))}
}

// markMalformed records a malformed value for tag this frame, returning true only
// on the transition INTO an episode — so the caller warns once, not per frame.
func (t *malformedTelemetryTracker) markMalformed(tag string) (firstOfEpisode bool) {
	if t.active[tag] {
		return false
	}
	t.active[tag] = true
	return true
}

// markValid records a cleanly-parsed value for tag this frame, returning true
// only on the transition OUT of an open episode — so the caller logs one
// recovery.
func (t *malformedTelemetryTracker) markValid(tag string) (recovered bool) {
	if !t.active[tag] {
		return false
	}
	delete(t.active, tag)
	return true
}

// telemetryTagValid reports whether the payload carries a real, cleanly-parsed
// value for tag this frame — the recovery signal that closes a malformed episode.
//
// The recovery signal is deliberately keyed on a NON-ZERO parsed value, not on
// "parsed without error". 0 is this subsystem's sentinel for "not a real value":
// events.go states it outright ("0 is never a legitimate rig value for them"),
// and captureDialFreq / CurrentDialMHz both treat 0 as absent/unknown. So a frame
// that parses to 0 (e.g. VFOAFREQ "000000000") is transport-valid but
// SEMANTICALLY ABSENT — it is neither malformed nor a recovery, and it must not
// close an open episode: the episode ends only when a genuinely real value
// resumes. This intentionally leaves an episode open across all-zero frames
// (correct: nothing real recovered), where keying on parse-success instead would
// log a false "telemetry recovered" on a meaningless 0 Hz / 0 W reading.
//
// Leaving the episode open is safe for the reason the recovery log is not
// safety-critical in the first place: observeMalformedTelemetry invalidates the
// rolling snapshot on EVERY malformed frame, independent of the tracker/episode
// state, so snapshot freshness (the thing that stamps a logged QSO) is protected
// regardless of whether a recovery line was emitted. Reviewed 2026-08-15.
func telemetryTagValid(tag string, p RigStatePayload) bool {
	switch tag {
	case tagVfoAFreq:
		return p.VfoA != 0
	case tagVfoBFreq:
		return p.VfoB != 0
	case tagTxPwr:
		return p.Power != 0
	default:
		return false
	}
}

// observeMalformedTelemetry runs the per-frame malformed-telemetry reconciliation
// for a decoded rig-state (payload) and its parse errors: warn on each new
// malformed episode, recovery on each return to valid, and invalidate the
// affected rolling snapshots so a stale value is never presented as fresh.
// Called from readLoop BEFORE the empty-payload short-circuit, so a frame whose
// ONLY content is a malformed value is still warned and invalidated.
func (s *Service) observeMalformedTelemetry(t *malformedTelemetryTracker, p RigStatePayload, errs []telemetryParseError, driverID string) {
	bad := make(map[string]string, len(errs))
	for _, e := range errs {
		bad[e.tag] = e.raw
	}
	for _, tag := range telemetryParseTags {
		if raw, isBad := bad[tag]; isBad {
			if t.markMalformed(tag) {
				s.logger.WarnWith().
					Str("driver", driverID).
					Str("tag", tag).
					Str("raw", raw).
					Msg("bridge: malformed rig telemetry; reading dropped and state invalidated")
			}
			continue
		}
		if telemetryTagValid(tag, p) && t.markValid(tag) {
			s.logger.InfoWith().
				Str("driver", driverID).
				Str("tag", tag).
				Msg("bridge: rig telemetry recovered; valid value resumed")
		}
	}
	s.invalidateMalformedSnapshots(errs)
}

// invalidateMalformedSnapshots clears the rolling snapshot fields whose latest
// pushed value was malformed, so CurrentDialMHz / CurrentPowerW report unknown
// rather than a retained stale value (OPEN-1(b)). Power invalidation respects the
// tune-restore freeze: while a tune is active, lastPower is the frozen pre-tune
// value StartTune will restore to, and a malformed TXPWR (the rig is pushing
// RTTY/tune-power state anyway) must not disturb it — exactly the !tuneActive
// guard captureTuneSnapshot uses.
func (s *Service) invalidateMalformedSnapshots(errs []telemetryParseError) {
	if len(errs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range errs {
		switch e.tag {
		case tagVfoAFreq:
			s.lastVfoA = 0
		case tagVfoBFreq:
			s.lastVfoB = 0
		case tagTxPwr:
			if !s.tuneActive {
				s.lastPower = 0
			}
		}
	}
}
