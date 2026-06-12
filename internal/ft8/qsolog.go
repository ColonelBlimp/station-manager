package ft8

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// BuildQso assembles a types.Qso from a completed FT8 exchange and the operator's
// station identity (ADR 0029 step e4 — "a completed exchange is a QSO"). Pure and
// dependency-light: the daemon (cmd/smd) converts the result to an adif.Record
// and submits it via qsoservice, so internal/ft8 stays free of the storage path.
//
//   - Frequency is the dial frequency at QSO start plus the audio offset; the
//     band is derived from the sum.
//   - Reports map straight across: RST_SENT is the report WE sent (our SNR of
//     their signal, rung 3); RST_RCVD is the report THEY sent us (rung 2).
//   - The whole LoggingStation identity block is copied in; STATION_CALLSIGN
//     falls back to OPERATOR (ADIF rule) so the submit's required-field check
//     passes when the operator set only OPERATOR.
func BuildQso(c CompletedQso, station types.LoggingStation, logbookID int64, now time.Time) types.Qso {
	now = now.UTC()
	freqMHz := c.DialFreqMHz + c.OffsetHz/1_000_000.0
	freq := fmt.Sprintf("%.6f", freqMHz)

	q := types.Qso{LogbookID: logbookID}
	q.LoggingStation = station
	if q.StationCallsign == "" {
		q.StationCallsign = station.Operator
	}
	q.Call = c.TheirCall
	q.Gridsquare = c.TheirGrid
	q.Mode = "FT8"
	q.Freq = freq
	q.Band = utils.FrequencyToBand(freq)
	q.QsoDate = now.Format("20060102")
	// time_on/time_off are HHMM (the storage schema's CHECK constraint requires
	// exactly 4 chars, matching the rest of the logging path — not ADIF's optional
	// HHMMSS). now is the completion instant (73 sent), so it is the true TIME_OFF;
	// TIME_ON reuses it as a close approximation — an FT8 exchange spans ~2 min, well
	// inside QSL time-matching tolerance. Threading the real StartQso instant through
	// for an exact TIME_ON is a noted follow-up.
	hhmm := now.Format("1504")
	q.TimeOn = hhmm
	q.TimeOff = hhmm
	if c.HasOurReport {
		q.RstSent = strconv.Itoa(c.OurReport)
	}
	if c.HasTheirReport {
		q.RstRcvd = strconv.Itoa(c.TheirReport)
	}
	return q
}
