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
	// 14.074 + 1500 Hz = 14.0755 MHz → 20m.
	require.Equal(t, "14.075500", q.Freq)
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

	l := NewLoggedQso(q, "uuid-123")

	require.Equal(t, "uuid-123", l.UUID)
	require.Equal(t, "K1ABC", l.Callsign)
	require.Equal(t, int64(14_075_500), l.FreqHz) // 14.0755 MHz → Hz
	require.Equal(t, "20m", l.Band)
	require.Equal(t, "FT8", l.Mode)
	require.Equal(t, "09:05", l.TimeOn)       // HHMM → HH:MM
	require.Equal(t, "2026-06-10", l.QsoDate) // YYYYMMDD → YYYY-MM-DD
	require.Equal(t, "-12", l.RstSent)
	require.Equal(t, "-10", l.RstRcvd)
	require.Equal(t, "FN42", l.Gridsquare)
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
