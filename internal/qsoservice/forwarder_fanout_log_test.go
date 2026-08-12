package qsoservice

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Q5 — A STORED / DELETED QSO'S FORWARDER FAN-OUT IS RECORDED ON ITS LOG LINE.
//
// CRITERION (qsoservice-logging-gaps Q5): submit()'s "QSO stored" and Delete's "QSO
// soft-deleted" recorded the QSO comprehensively but said NOTHING about which
// forwarders it was queued to. So "why did this QSO never reach ClubLog?" had no
// answer — queued-and-failed, queued-and-pending, and never-queued are three
// different problems that all looked identical. The destination names now ride the
// existing structured line, computed in the fan-out loop that already runs.

// logLineWith returns the one log line containing sub (shared across the qsoservice
// logging-gaps tests).
func logLineWith(t *testing.T, out, sub string) string {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, sub) {
			return ln
		}
	}
	t.Fatalf("no log line containing %q in:\n%s", sub, out)
	return ""
}

func fanoutRec(call string) adif.Record {
	return adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"}, // import relaxes the callsign match
	}
}

// Q5a — the destinations a QSO WAS queued to are named on "QSO stored". A live submit
// fans out to every enabled insert-filtered forwarder (NoBulkBackfill gates imports,
// not live QSOs), so both appear.
func TestSubmit_QsoStoredNamesForwarderFanOut(t *testing.T) {
	s := newTestService(t, enabledClublog(), enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	buf := logbuf(s)

	res, err := s.Submit(context.Background(), lbID, fanoutRec("K1AAA"), false)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)

	line := logLineWith(t, buf.String(), "QSO stored")
	require.Contains(t, line, `"forwarded_to"`,
		"the QSO stored line must name where it was queued — queued-and-failed, pending, "+
			"and never-queued were three problems with one identical log")
	require.Contains(t, line, "clublog")
	require.Contains(t, line, "qrz")
}

// Q5b — queued-NOWHERE is the confusable state, and it must show as an explicit empty
// destination list, not a missing field (which reads the same as "line predates it").
func TestSubmit_QsoStoredForwardedToEmptyWhenQueuedNowhere(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	buf := logbuf(s)

	// A default import (nil forwardTo) queues to nothing.
	res, err := s.SubmitImport(context.Background(), lbID, fanoutRec("K1AAA"), false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)

	line := logLineWith(t, buf.String(), "QSO stored")
	require.Contains(t, line, `"forwarded_to":[]`,
		"queued-nowhere must show as an explicit empty destination list")
}

// Q5c — Delete has the same fan-out shape; "QSO soft-deleted" names it too.
func TestDelete_QsoSoftDeletedNamesForwarderFanOut(t *testing.T) {
	s := newTestService(t, enabledClublog(), enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	uuid, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	buf := logbuf(s)

	ctx := context.Background()
	existing, err := s.DB.FetchQsoByUUIDWithContext(ctx, uuid)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, existing, source.Source("test")))

	line := logLineWith(t, buf.String(), "QSO soft-deleted")
	require.Contains(t, line, `"forwarded_to"`, "the delete line must name the delete fan-out too")
	require.Contains(t, line, "clublog")
	require.Contains(t, line, "qrz")
}
