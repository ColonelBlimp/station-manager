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

// LoggedQso is the `ft8-logged` SSE payload (EventLogged): the just-stored FT8
// QSO's session-list fields, shaped for the SPA's SessionQso row. Carries the
// canonical UUID so the SPA's email-out / edit paths (which key off it) work for
// FT8 rows exactly as they do for Phone/CW ones. Country + distance are left to
// the SPA (it has the operator's grid + an enrichment cache); this payload is
// only what the daemon already holds at log time.
type LoggedQso struct {
	UUID       string `json:"uuid"`
	Callsign   string `json:"callsign"`
	FreqHz     int64  `json:"freq_hz"`
	Band       string `json:"band"`
	RstSent    string `json:"rst_sent"`
	RstRcvd    string `json:"rst_rcvd"`
	Mode       string `json:"mode"`
	TimeOn     string `json:"time_on"`  // UTC "HH:MM"
	QsoDate    string `json:"qso_date"` // "YYYY-MM-DD"
	Gridsquare string `json:"gridsquare"`
}

// NewLoggedQso maps a stored types.Qso (as built by BuildQso) plus its canonical
// UUID into the SSE payload, converting the daemon's storage formats to the
// SPA-friendly shapes the session list expects: FREQ MHz → Hz, TIME_ON "HHMM" →
// "HH:MM", QSO_DATE "YYYYMMDD" → "YYYY-MM-DD". A malformed freq/time/date degrades
// to a zero/blank field rather than failing — the QSO is already logged.
func NewLoggedQso(q types.Qso, uuid string) LoggedQso {
	var freqHz int64
	if mhz, err := strconv.ParseFloat(q.Freq, 64); err == nil {
		freqHz = int64(mhz*1_000_000 + 0.5)
	}
	timeOn := q.TimeOn
	if len(timeOn) == 4 {
		timeOn = timeOn[:2] + ":" + timeOn[2:]
	}
	qsoDate := q.QsoDate
	if len(qsoDate) == 8 {
		qsoDate = qsoDate[:4] + "-" + qsoDate[4:6] + "-" + qsoDate[6:]
	}
	return LoggedQso{
		UUID:       uuid,
		Callsign:   q.Call,
		FreqHz:     freqHz,
		Band:       q.Band,
		RstSent:    q.RstSent,
		RstRcvd:    q.RstRcvd,
		Mode:       q.Mode,
		TimeOn:     timeOn,
		QsoDate:    qsoDate,
		Gridsquare: q.Gridsquare,
	}
}
