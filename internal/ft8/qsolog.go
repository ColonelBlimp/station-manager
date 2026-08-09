package ft8

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
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
func BuildQso(c CompletedQso, station types.LoggingStation, logbookID int64, now time.Time, log logging.Logger) types.Qso {
	if log == nil {
		log = logging.Noop()
	}
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
	// The run identity persists with the QSO (app_sm_run_id via additional_data,
	// and thence the cloud payload) — what "logged this run" is answered from.
	// Empty for non-run contacts; omitempty keeps it off those records entirely.
	q.AppSmRunID = c.RunID
	// Every degradation below is Warn'd rather than silent: the row is stored AND
	// forwarded (QRZ/ClubLog/SM Cloud) — durable, outbound data that cannot be
	// corrected after the fact, so an invisible fallback is unauditable. Degrading
	// itself is correct (enrichment never blocks logging); each line carries the
	// input that failed to resolve, and a clean build emits nothing.
	if q.Band == "" {
		log.WarnWith().Str("freq", freq).Str("their_call", c.TheirCall).
			Msg("ft8: QSO band unresolved from dial")
	}
	// time_on/time_off are HHMMSS — FT8 has the exact slot instant, so we keep the
	// real seconds (the storage CHECK now accepts HHMM or HHMMSS, and the QSL
	// manager's OQRS matches on the full timestamp; dedupe stays minute-precision
	// in qsoservice). TIME_ON is the contact's start instant (c.StartedAt, stamped
	// when the session began); now is the completion instant (final rung sent), the
	// TIME_OFF. A path that failed to stamp a start (zero) falls back to the
	// completion instant.
	start := c.StartedAt
	if start.IsZero() {
		start = now
		// A zero StartedAt means a path failed to stamp the start — a defect
		// indicator, not an expected input; the fallback keeps the QSO but the
		// record must exist.
		log.WarnWith().Str("their_call", c.TheirCall).
			Msg("ft8: QSO start instant was never stamped")
	}
	start = start.UTC()
	q.QsoDate = start.Format("20060102")
	q.TimeOn = start.Format("150405")
	q.TimeOff = now.Format("150405")
	// QSO_DATE_OFF is always populated — the date at TIME_OFF: the same day as
	// QSO_DATE for a normal contact, the following day when the exchange crossed
	// 00:00 UTC. `now` is the completion instant, so its date IS the TIME_OFF date.
	// Always setting it also satisfies submit's time-coherence check, which would
	// otherwise read a midnight-crossing TIME_ON > TIME_OFF as an invalid range and
	// drop the completed QSO.
	q.QsoDateOff = now.Format("20060102")
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
		} else {
			log.WarnWith().Str("my_grid", station.MyGridsquare).Str("their_grid", c.TheirGrid).
				Msg("ft8: antenna path unresolved")
		}
	}
	// ARRL Field Day exchange (answer-a-CQ-FD): the worked station's class + section,
	// plus the contest id, so the QSO logs as a Field Day contact. Set only when the
	// exchange was an FD one (Class non-empty); a standard QSO leaves these empty.
	if c.Class != "" {
		q.Class = c.Class
		q.ArrlSect = c.Section
		q.ContestId = "ARRL-FD"
	}
	return q
}

// LoggedQso is the `ft8-logged` SSE payload (EventLogged): the just-stored FT8
// QSO's session-list fields, shaped for the SPA's SessionQso row. Carries the
// canonical UUID so the SPA's email-out / edit paths (which key off it) work for
// FT8 rows exactly as they do for Phone/CW ones. Country and Name are included
// because the daemon enriches the contacted station before submit (the cmd/smd
// sink), so both hold their values at log time; distance is still left to the SPA
// (it has the operator's grid + the on-air locator).
type LoggedQso struct {
	UUID       string `json:"uuid"`
	Callsign   string `json:"callsign"`
	FreqHz     int64  `json:"freq_hz"`
	Band       string `json:"band"`
	RstSent    string `json:"rst_sent"`
	RstRcvd    string `json:"rst_rcvd"`
	Mode       string `json:"mode"`
	TimeOn     string `json:"time_on"`  // UTC "HH:MM:SS" ("HH:MM" if the record has no seconds)
	QsoDate    string `json:"qso_date"` // "YYYY-MM-DD"
	Gridsquare string `json:"gridsquare"`
	Country    string `json:"country"` // contacted station's country (enriched at log time)
	Name       string `json:"name"`    // contacted operator's name (enriched at log time)
}

// NewLoggedQso maps a stored types.Qso (as built by BuildQso) plus its canonical
// UUID into the SSE payload, converting the daemon's storage formats to the
// SPA-friendly shapes the session list expects: FREQ MHz → Hz, TIME_ON "HHMMSS"
// → "HH:MM:SS", QSO_DATE "YYYYMMDD" → "YYYY-MM-DD". A malformed freq/time/date degrades
// to a zero/blank field rather than failing — the QSO is already logged.
func NewLoggedQso(q types.Qso, uuid string, log logging.Logger) LoggedQso {
	if log == nil {
		log = logging.Noop()
	}
	// Malformed-field degradations are Warn'd per field (same rationale as
	// BuildQso's lines: the blank/zero lands in the operator-visible session row
	// with no explanation otherwise). An EMPTY field is absence, not malformation
	// — it passes through blank without a line.
	var freqHz int64
	if mhz, err := strconv.ParseFloat(q.Freq, 64); err == nil {
		freqHz = int64(mhz*1_000_000 + 0.5)
	} else if q.Freq != "" {
		log.WarnWith().Str("field", "freq").Str("value", q.Freq).Str("call", q.Call).
			Msg("ft8: logged-QSO field malformed")
	}
	// Colon-separate at the precision the record actually carries: HHMMSS →
	// "HH:MM:SS", HHMM → "HH:MM". This used to truncate to HH:MM unconditionally
	// for a "compact" session list, which made FT8 rows the odd ones out — the
	// Phone/CW path fills the same session column from the submit response at
	// full precision, and the SPA's row type documents it as HH:MM:SS (dogfood
	// 2026-07-23). BuildQso always stamps seconds, so FT8 now matches; the
	// shorter form stays for an imported/legacy HHMM value.
	timeOn := q.TimeOn
	switch len(timeOn) {
	case 6:
		timeOn = timeOn[:2] + ":" + timeOn[2:4] + ":" + timeOn[4:6]
	case 4:
		timeOn = timeOn[:2] + ":" + timeOn[2:4]
	default:
		if timeOn != "" {
			log.WarnWith().Str("field", "time_on").Str("value", timeOn).Str("call", q.Call).
				Msg("ft8: logged-QSO field malformed")
		}
	}
	qsoDate := q.QsoDate
	if len(qsoDate) == 8 {
		qsoDate = qsoDate[:4] + "-" + qsoDate[4:6] + "-" + qsoDate[6:]
	} else if qsoDate != "" {
		log.WarnWith().Str("field", "qso_date").Str("value", qsoDate).Str("call", q.Call).
			Msg("ft8: logged-QSO field malformed")
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
		Name:       q.Name,
	}
}
