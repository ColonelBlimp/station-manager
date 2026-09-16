package bridge

import "github.com/ColonelBlimp/station-manager/internal/logging"

// livenessLog owns the LEVEL policy of the read loop's liveness lines. The
// lines themselves (B11) are unchanged: "rig went quiet" on the no-data
// announce, "rig data resumed" on the recovery edge, each with the strike
// count. What changed is how loud a quiet-but-alive FLAP is — a no-data
// timeout the probe clears before the strike limit.
//
// Operator ruling 2026-09-16 (inbox): an idle FTdx10 with nothing polling it
// flaps every liveness window (333 warn/info pairs in 80 min on 2026-09-13,
// each recovered within the second, none operator-visible), which buries real
// warnings and inflates any warn count. So: the FIRST flap keeps its warn/info
// pair; later flaps log both lines at debug, carrying `flaps` — the flaps since
// the last WARNED event; a REAL loss (strikes reaching the disconnect limit) is
// always one warn line with that count, and the recovery after it is info with
// the strikes reached. Both reset the count, so the next flap is warned again.
// No serial traffic is added — the alternative (a keepalive poll while nothing
// captures) was declined.
//
// Per pipeline run (the read loop owns one), like the strikes it accompanies.
type livenessLog struct {
	logger *logging.Service
	driver string
	flaps  int32 // quiet-but-alive recoveries since the last warned event
}

// quiet logs the no-data announce: warn for the first flap, debug afterwards.
func (l *livenessLog) quiet(strikes int32) {
	ev := l.logger.WarnWith()
	if l.flaps > 0 {
		ev = l.logger.DebugWith()
	}
	ev.Str("driver", l.driver).Int32("strikes", strikes).Int32("flaps", l.flaps).
		Msg("bridge: rig went quiet; no data within liveness window")
}

// limitReached logs the real-loss edge once, when the strikes arrive AT the
// disconnect limit (RigConnected trips there): always a warn, whatever level
// the announce went out at, carrying the flaps that preceded it.
func (l *livenessLog) limitReached(strikes int32) {
	if strikes != noDataStrikeLimit {
		return
	}
	l.logger.WarnWith().Str("driver", l.driver).Int32("strikes", strikes).Int32("flaps", l.flaps).
		Msg("bridge: rig unreachable; liveness strikes reached the disconnect limit")
	l.flaps = 0
}

// resumed logs the recovery edge. After a real loss it is info and resets the
// flap count; a quiet-but-alive recovery is the first flap's info or a later
// flap's debug, and counts as a flap.
func (l *livenessLog) resumed(strikes int32) {
	ev := l.logger.InfoWith()
	real := strikes >= noDataStrikeLimit
	if real {
		l.flaps = 0
	} else if l.flaps > 0 {
		ev = l.logger.DebugWith()
	}
	ev.Str("driver", l.driver).Int32("strikes", strikes).Int32("flaps", l.flaps).
		Msg("bridge: rig data resumed; liveness restored")
	if !real {
		l.flaps++
	}
}
