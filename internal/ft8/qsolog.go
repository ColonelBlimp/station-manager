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
//   - Frequency is the DIAL frequency at QSO start — the FT8 logging convention
//     (WSJT-X/JTDX log the dial, not dial+audio-offset). Both stations share the
//     dial but sit at different audio offsets, so baking our TX offset into FREQ
//     would disagree with the worked station's log and with QRZ/LoTW norms. The
//     band is derived from the dial. c.OffsetHz is TX placement, not the QSO freq.
//   - Reports map straight across: RST_SENT is the report WE sent (our SNR of
//     their signal, rung 3); RST_RCVD is the report THEY sent us (rung 2).
//   - The whole LoggingStation identity block is copied in; STATION_CALLSIGN
//     falls back to OPERATOR (ADIF rule) so the submit's required-field check
//     passes when the operator set only OPERATOR.
func BuildQso(c CompletedQso, station types.LoggingStation, logbookID int64, now time.Time) types.Qso {
	now = now.UTC()
	freq := fmt.Sprintf("%.6f", c.DialFreqMHz)

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
	// time_on/time_off are HHMM (the storage schema's CHECK constraint requires
	// exactly 4 chars, matching the rest of the logging path — not ADIF's optional
	// HHMMSS). TIME_ON is the contact's start instant (c.StartedAt, stamped when the
	// session began); now is the completion instant (final rung sent), the TIME_OFF.
	// A path that failed to stamp a start (zero) falls back to the completion instant
	// — an FT8 exchange spans ~2 min, well inside QSL time-matching tolerance.
	start := c.StartedAt
	if start.IsZero() {
		start = now
	}
	start = start.UTC()
	q.QsoDate = start.Format("20060102")
	q.TimeOn = start.Format("1504")
	q.TimeOff = now.Format("1504")
	if c.HasOurReport {
		q.RstSent = strconv.Itoa(c.OurReport)
	}
	if c.HasTheirReport {
		q.RstRcvd = strconv.Itoa(c.TheirReport)
	}
	// Antenna path (logging-only — the operator's short/long choice; FT8 messages
	// carry no path info, so this never affects the on-air signal). ANT_PATH always
	// records the chosen path; ANT_AZ + DISTANCE carry the matching great-circle
	// bearing/distance when both grids resolve (else left unset — ANT_PATH alone is
	// still valid ADIF). Bearing/distance math mirrors the SPA's bearing.ts so a
	// daemon-logged FT8 QSO and an SPA-logged Phone/CW QSO agree.
	if c.AntPath != "" {
		q.AntPath = c.AntPath
		if geo, ok := utils.GridPath(station.MyGridsquare, c.TheirGrid); ok {
			bearing, dist := geo.ShortBearingDeg, geo.ShortDistanceKm
			if c.AntPath == antPathLong {
				bearing, dist = geo.LongBearingDeg, geo.LongDistanceKm
			}
			// ANT_AZ to the nearest whole degree — it's a rotator heading, and
			// sub-degree precision is spurious (FormatFloat rounds at prec 0).
			q.AntennaAzimuth = strconv.FormatFloat(bearing, 'f', 0, 64)
			q.Distance = strconv.FormatFloat(dist, 'f', 0, 64)
		}
	}
	return q
}

// LoggedQso is the `ft8-logged` SSE payload (EventLogged): the just-stored FT8
// QSO's session-list fields, shaped for the SPA's SessionQso row. Carries the
// canonical UUID so the SPA's email-out / edit paths (which key off it) work for
// FT8 rows exactly as they do for Phone/CW ones. Country is included because the
// daemon enriches the contacted station before submit (the cmd/smd sink), so it
// holds the value at log time; distance is still left to the SPA (it has the
// operator's grid + the on-air locator).
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
	Country    string `json:"country"` // contacted station's country (enriched at log time)
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
		Country:    q.Country,
	}
}
