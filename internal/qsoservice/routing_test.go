package qsoservice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0082 part 6 (W-0021 5B): every enqueue site routes a QSO by ITS logbook's
// enabled bindings from the start-time snapshot. No bindings → nothing queued,
// no error; a binding serves one logbook only; a manual backfill names a
// binding and refuses QSOs of another logbook.

func routingRec(call, timeOn string) adif.Record {
	return adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
	}
}

func TestRouting_NoBindingsQueuesNowhereAndBackfillIsRefusedByName(t *testing.T) {
	s := newTestService(t,
		types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}},
	)
	lbID := seedLogbook(t, s, "Contest", "G0XYZ")
	s.SetDestinationRoutes(nil)

	res, err := s.Submit(context.Background(), lbID, routingRec("M0CMC", "1200"), false)
	require.NoError(t, err)
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), res.ID)
	require.NoError(t, err)
	require.Empty(t, rows, "a logbook with no enabled binding queues nowhere")

	_, err = s.EnqueueUploads(context.Background(), "qrz", []string{res.UUID}, false, origin.Manual)
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "forwarder_unavailable", se.Code, "a name no enabled binding carries is refused, not silently skipped")

	// A disabled binding is not a route either.
	s.SetDestinationRoutes([]forwarding.BoundForwarder{{LogbookID: lbID, Config: types.ForwarderConfig{
		Name: "qrz", Type: "qrz", Enabled: false, ActionFilter: []string{"insert"}}}})
	res2, err := s.Submit(context.Background(), lbID, routingRec("EA1B", "1201"), false)
	require.NoError(t, err)
	rows, _ = s.DB.FetchUploadsByQsoIDWithContext(context.Background(), res2.ID)
	require.Empty(t, rows)
}

func TestRouting_EachLogbookFansOutToItsOwnBindingsOnly(t *testing.T) {
	s := newTestService(t,
		types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}},
	)
	lb1 := seedLogbook(t, s, "Main", "G0XYZ")
	lb2 := seedLogbook(t, s, "Second", "G0XYZ") // bindFromConfig names it qrz.<uuid>
	routes := s.DestinationRoutes()
	require.Len(t, routes, 2)
	second := routes[1].Config.Name
	require.NotEqual(t, "qrz", second)

	r1, err := s.Submit(context.Background(), lb1, routingRec("M0CMC", "1200"), false)
	require.NoError(t, err)
	r2, err := s.Submit(context.Background(), lb2, routingRec("M0CMC", "1201"), false)
	require.NoError(t, err)
	require.True(t, hasInsertRow(t, s, r1.ID, "qrz"))
	require.False(t, hasInsertRow(t, s, r1.ID, second))
	require.True(t, hasInsertRow(t, s, r2.ID, second))
	require.False(t, hasInsertRow(t, s, r2.ID, "qrz"), "a QSO never routes to another logbook's binding")

	// Manual backfill: the binding named `qrz` serves logbook 1 only.
	out, err := s.EnqueueUploads(context.Background(), "qrz", []string{r1.UUID, r2.UUID}, true, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, []string{r2.UUID}, out.SkippedOtherLogbook)
	// Delete backfill likewise.
	snap, err := s.DB.FetchQsoByUUIDWithContext(context.Background(), r2.UUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(context.Background(), snap, source.API))
	dout, err := s.EnqueueDeleteUploads(context.Background(), "qrz", []string{r2.UUID}, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, []string{r2.UUID}, dout.SkippedOtherLogbook)
}

func TestRouting_StampSyncMirrorsEachRowToItsLogbooksBinding(t *testing.T) {
	s := newTestService(t,
		types.ForwarderConfig{Name: "smcloud", Type: "smcloud", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}},
	)
	lb1 := seedLogbook(t, s, "Main", "G0XYZ")
	lb2 := seedLogbook(t, s, "Second", "G0XYZ")
	second := s.DestinationRoutes()[1].Config.Name
	_, id1 := seedStoredQso(t, s, lb1, "K1AAA", "1200")
	_, id2 := seedStoredQso(t, s, lb2, "K1AAA", "1201")

	n, err := s.EnqueueStampSync(context.Background(), []int64{id1, id2, 999})
	require.NoError(t, err)
	require.Equal(t, 2, n, "one mirror row per existing QSO; the unknown id is skipped")
	require.True(t, hasUpdateRow(t, s, id1, "smcloud"))
	require.True(t, hasUpdateRow(t, s, id2, second))
	require.False(t, hasUpdateRow(t, s, id2, "smcloud"))
}

// An import naming a forwarder into a logbook with no such binding stores the
// QSOs and queues nothing for that name (the CLI validates names against the
// archive's bindings up front; the service never refuses a store).
func TestRouting_ImportForwardToAnUnboundNameQueuesNothing(t *testing.T) {
	s := newTestService(t, types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"}})
	lbID := seedLogbook(t, s, "Contest", "G0XYZ")
	s.SetDestinationRoutes(nil)
	res, err := s.SubmitImport(context.Background(), lbID, routingRec("M0CMC", "1200"), false, []string{"qrz"})
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
	rows, _ := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), res.ID)
	require.Empty(t, rows)
}
