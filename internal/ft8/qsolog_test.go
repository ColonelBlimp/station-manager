package ft8

import (
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

func TestBuildQso(t *testing.T) {
	station := types.LoggingStation{
		StationCallsign: "G0XYZ",
		Operator:        "G0XYZ",
		MyGridsquare:    "IO91",
	}
	c := CompletedQso{
		TheirCall:      "K1ABC",
		TheirGrid:      "FN42",
		OurReport:      -12,
		HasOurReport:   true,
		TheirReport:    -10,
		HasTheirReport: true,
		StartedAt:      time.Date(2026, 6, 10, 14, 28, 30, 0, time.UTC),
		OffsetHz:       1500,
		DialFreqMHz:    14.074,
	}
	now := time.Date(2026, 6, 10, 14, 30, 45, 0, time.UTC)

	q := BuildQso(c, station, 7, now)

	require.Equal(t, int64(7), q.LogbookID)
	require.Equal(t, "K1ABC", q.Call)
	require.Equal(t, "FN42", q.Gridsquare)
	require.Equal(t, "FT8", q.Mode)
	require.Equal(t, "G0XYZ", q.StationCallsign)
	require.Equal(t, "IO91", q.MyGridsquare)
	// FT8 logs the DIAL frequency, not dial+offset (WSJT-X convention) — the
	// 1500 Hz TX offset is deliberately NOT added. 14.074 MHz → 20m.
	require.Equal(t, "14.074000", q.Freq)
	require.Equal(t, "20m", q.Band)
	require.Equal(t, "20260610", q.QsoDate)
	require.Equal(t, "20260610", q.QsoDateOff) // always populated; same day here
	// HHMMSS — FT8 keeps its real slot seconds now (the CHECK accepts HHMM or
	// HHMMSS; dedupe stays minute-precision in qsoservice). TIME_ON is the contact
	// START (14:28:30), TIME_OFF the completion instant (14:30:45).
	require.Equal(t, "142830", q.TimeOn)
	require.Equal(t, "143045", q.TimeOff)
	require.Equal(t, "-12", q.RstSent) // we sent
	require.Equal(t, "-10", q.RstRcvd) // they sent us
}

// TestBuildQso_Type4 locks the degraded reduced-type-4 row (ADR 0048): a nonstandard
// SPELLED call, our SNR as RST_SENT, a BLANK RST_RCVD (no report is exchanged), no grid,
// and no contest fields — the shape a completed type-4 exchange produces.
func TestBuildQso_Type4(t *testing.T) {
	station := types.LoggingStation{StationCallsign: "7Q5MLV", Operator: "7Q5MLV", MyGridsquare: "KH53"}
	c := CompletedQso{
		TheirCall:    "PJ4/NA2AA",
		TheirGrid:    "", // type-4 CQ carries no grid
		OurReport:    -12,
		HasOurReport: true,
		// HasTheirReport deliberately false — no report on the wire.
		StartedAt:   time.Date(2026, 7, 16, 14, 28, 30, 0, time.UTC),
		OffsetHz:    1500,
		DialFreqMHz: 14.074,
	}
	now := time.Date(2026, 7, 16, 14, 30, 0, 0, time.UTC)

	q := BuildQso(c, station, 3, now)

	require.Equal(t, "PJ4/NA2AA", q.Call, "the spelled nonstandard call is logged, not a hash")
	require.Equal(t, "FT8", q.Mode)
	require.Equal(t, "-12", q.RstSent, "our SNR of them → RST_SENT")
	require.Empty(t, q.RstRcvd, "no report is exchanged in type-4 — RST_RCVD stays blank")
	require.Empty(t, q.Gridsquare, "type-4 carries no grid")
	require.Empty(t, q.ContestId, "type-4 is not a contest exchange")
	require.Empty(t, q.Class)
	require.Empty(t, q.ArrlSect)
}

func TestBuildQso_TimeOn(t *testing.T) {
	station := types.LoggingStation{Operator: "G0XYZ"}
	base := CompletedQso{TheirCall: "K1ABC", DialFreqMHz: 14.074}

	t.Run("TIME_ON is the start instant, TIME_OFF the completion", func(t *testing.T) {
		c := base
		c.StartedAt = time.Date(2026, 6, 10, 14, 28, 30, 0, time.UTC)
		q := BuildQso(c, station, 1, time.Date(2026, 6, 10, 14, 30, 15, 0, time.UTC))
		require.Equal(t, "142830", q.TimeOn)
		require.Equal(t, "143015", q.TimeOff)
		require.Equal(t, "20260610", q.QsoDate)
	})

	t.Run("a non-UTC start is normalised to UTC", func(t *testing.T) {
		// +02:00 local 16:28 == 14:28 UTC — TIME_ON must be the UTC minute.
		loc := time.FixedZone("CEST", 2*60*60)
		c := base
		c.StartedAt = time.Date(2026, 6, 10, 16, 28, 30, 0, loc)
		q := BuildQso(c, station, 1, time.Date(2026, 6, 10, 16, 30, 0, 0, loc))
		require.Equal(t, "142830", q.TimeOn)
		require.Equal(t, "143000", q.TimeOff)
	})

	t.Run("a zero start falls back to the completion instant", func(t *testing.T) {
		q := BuildQso(base, station, 1, time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC))
		require.Equal(t, "143000", q.TimeOn)
		require.Equal(t, "143000", q.TimeOff)
		require.Equal(t, "20260610", q.QsoDate)
	})

	t.Run("QSO_DATE follows the start when the exchange crosses midnight", func(t *testing.T) {
		c := base
		c.StartedAt = time.Date(2026, 6, 10, 23, 59, 30, 0, time.UTC)
		q := BuildQso(c, station, 1, time.Date(2026, 6, 11, 0, 0, 13, 0, time.UTC))
		require.Equal(t, "20260610", q.QsoDate) // start date, not completion date
		require.Equal(t, "235930", q.TimeOn)
		require.Equal(t, "000013", q.TimeOff)
		// QSO_DATE_OFF must be the following day — without it submit's coherence
		// check reads TIME_ON (2359) > TIME_OFF (0000) as an invalid range and the
		// completed QSO is silently dropped.
		require.Equal(t, "20260611", q.QsoDateOff)
	})

	t.Run("a same-day QSO sets QSO_DATE_OFF equal to QSO_DATE (always populated)", func(t *testing.T) {
		c := base
		c.StartedAt = time.Date(2026, 6, 10, 14, 28, 30, 0, time.UTC)
		q := BuildQso(c, station, 1, time.Date(2026, 6, 10, 14, 30, 15, 0, time.UTC))
		require.Equal(t, "20260610", q.QsoDateOff)
	})
}

func TestBuildQso_AntPath(t *testing.T) {
	station := types.LoggingStation{StationCallsign: "G0XYZ", MyGridsquare: "IO91"}
	base := CompletedQso{TheirCall: "K1ABC", TheirGrid: "FN42", DialFreqMHz: 14.074}

	t.Run("short stamps ANT_PATH + short bearing/distance", func(t *testing.T) {
		c := base
		c.AntPath = "S"
		q := BuildQso(c, station, 1, time.Now())
		require.Equal(t, "S", q.AntPath)
		require.NotEmpty(t, q.AntennaAzimuth, "ANT_AZ should be set when both grids resolve")
		require.NotEmpty(t, q.Distance)
	})

	t.Run("long stamps ANT_PATH=L + the long-path bearing/distance", func(t *testing.T) {
		short := base
		short.AntPath = "S"
		qs := BuildQso(short, station, 1, time.Now())

		long := base
		long.AntPath = "L"
		ql := BuildQso(long, station, 1, time.Now())

		require.Equal(t, "L", ql.AntPath)
		// Long-path bearing/distance differ from short for a non-degenerate pair.
		require.NotEqual(t, qs.AntennaAzimuth, ql.AntennaAzimuth)
		require.NotEqual(t, qs.Distance, ql.Distance)
	})

	t.Run("unset path leaves ANT_PATH empty (default short is stamped by the Service)", func(t *testing.T) {
		q := BuildQso(base, station, 1, time.Now()) // c.AntPath == ""
		require.Empty(t, q.AntPath)
		require.Empty(t, q.AntennaAzimuth)
	})

	t.Run("missing grid still stamps ANT_PATH, omits bearing/distance", func(t *testing.T) {
		c := base
		c.TheirGrid = ""
		c.AntPath = "L"
		q := BuildQso(c, station, 1, time.Now())
		require.Equal(t, "L", q.AntPath)
		require.Empty(t, q.AntennaAzimuth)
		require.Empty(t, q.Distance)
	})
}

func TestBuildQso_StationCallsignFallsBackToOperator(t *testing.T) {
	// Operator set, StationCallsign empty → STATION_CALLSIGN must fall back so
	// the submit's required-field check passes.
	station := types.LoggingStation{Operator: "G0XYZ"}
	q := BuildQso(CompletedQso{TheirCall: "K1ABC", DialFreqMHz: 14.074}, station, 1, time.Now())
	require.Equal(t, "G0XYZ", q.StationCallsign)
}

func TestNewLoggedQso(t *testing.T) {
	// Build a QSO the normal way, then map it to the SSE payload — the storage
	// formats (MHz freq, HHMM time, YYYYMMDD date) must convert to the SPA shapes
	// (Hz, HH:MM, YYYY-MM-DD), and the UUID must carry through for email/edit.
	station := types.LoggingStation{StationCallsign: "G0XYZ"}
	c := CompletedQso{
		TheirCall:      "K1ABC",
		TheirGrid:      "FN42",
		OurReport:      -12,
		HasOurReport:   true,
		TheirReport:    -10,
		HasTheirReport: true,
		OffsetHz:       1500,
		DialFreqMHz:    14.074,
	}
	now := time.Date(2026, 6, 10, 9, 5, 0, 0, time.UTC)
	q := BuildQso(c, station, 7, now)
	// Country and Name are filled by the cmd/smd sink's enrichment before submit
	// (BuildQso itself leaves them empty); the payload must carry them through to
	// the SPA so the session-list row shows them for FT8 QSOs.
	q.Country = "United States"
	q.Name = "Fred"

	l := NewLoggedQso(q, "uuid-123")

	require.Equal(t, "uuid-123", l.UUID)
	require.Equal(t, "K1ABC", l.Callsign)
	require.Equal(t, int64(14_074_000), l.FreqHz) // dial 14.074 MHz → Hz (TX offset not added)
	require.Equal(t, "20m", l.Band)
	require.Equal(t, "FT8", l.Mode)
	require.Equal(t, "09:05", l.TimeOn)       // stored HHMMSS → compact HH:MM for the session list
	require.Equal(t, "2026-06-10", l.QsoDate) // YYYYMMDD → YYYY-MM-DD
	require.Equal(t, "-12", l.RstSent)
	require.Equal(t, "-10", l.RstRcvd)
	require.Equal(t, "FN42", l.Gridsquare)
	require.Equal(t, "United States", l.Country)
	require.Equal(t, "Fred", l.Name)
}

func TestNewLoggedQso_MalformedFieldsDegrade(t *testing.T) {
	// A QSO with unparseable freq / short time / date must not panic — the QSO is
	// already logged; the payload just carries zero/blank for the bad fields.
	l := NewLoggedQso(types.Qso{}, "uuid-x")
	require.Equal(t, "uuid-x", l.UUID)
	require.Equal(t, int64(0), l.FreqHz)
	require.Equal(t, "", l.TimeOn)
	require.Equal(t, "", l.QsoDate)
}
