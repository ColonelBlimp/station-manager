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
	// HHMM, not HHMMSS — the qso table's time_on/time_off CHECK constraint requires
	// exactly 4 chars (length(time_on) = 4). Both come from `now` (the completion
	// instant). This is the regression guard for the constraint-violation that
	// silently failed every FT8 QSO insert until 2026-06-12.
	require.Equal(t, "1430", q.TimeOn)
	require.Equal(t, "1430", q.TimeOff)
	require.Equal(t, "-12", q.RstSent) // we sent
	require.Equal(t, "-10", q.RstRcvd) // they sent us
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
	// Country is filled by the cmd/smd sink's enrichment before submit (BuildQso
	// itself leaves it empty); the payload must carry it through to the SPA.
	q.Country = "United States"

	l := NewLoggedQso(q, "uuid-123")

	require.Equal(t, "uuid-123", l.UUID)
	require.Equal(t, "K1ABC", l.Callsign)
	require.Equal(t, int64(14_074_000), l.FreqHz) // dial 14.074 MHz → Hz (TX offset not added)
	require.Equal(t, "20m", l.Band)
	require.Equal(t, "FT8", l.Mode)
	require.Equal(t, "09:05", l.TimeOn)       // HHMM → HH:MM
	require.Equal(t, "2026-06-10", l.QsoDate) // YYYYMMDD → YYYY-MM-DD
	require.Equal(t, "-12", l.RstSent)
	require.Equal(t, "-10", l.RstRcvd)
	require.Equal(t, "FN42", l.Gridsquare)
	require.Equal(t, "United States", l.Country)
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
