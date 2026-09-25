package qsoservice

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 slice 5A (ADR 0082 part 10) — CHARACTERIZATION PINS, written before
// migration 0014 and the per-logbook bindings land. They record, for a
// configuration shaped like the station's, the EXACT upload-queue rows one live
// submit, one edit and one delete produce in the adopted archive, the
// `forwarded_to` lists emitted by submit and delete, and that a managed archive
// queues nowhere. The bindings seed must leave every assertion here
// byte-identical: the adopted Home archive keeps its legacy names, its action
// filters and its fan-out across the slice boundary.
//
// The shape: `qrz`, `clublog` and `smcloud` enabled, `qrzcq` present and
// disabled — the station as deployed (dossier, 2026-09-24 drills).

// stationForwarders is the station-shaped configuration, in config order.
func stationForwarders() []types.ForwarderConfig {
	return []types.ForwarderConfig{
		{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}},
		{Name: "clublog", Type: "clublog", Enabled: true, ActionFilter: []string{"insert", "delete"}},
		{Name: "smcloud", Type: "smcloud", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}},
		{Name: "qrzcq", Type: "qrzcq", Enabled: false, ActionFilter: []string{"insert"}},
	}
}

// uploadRowsOf returns every qso_upload row of the QSO as
// "forwarder_name/forwarder_type/action/origin/status", sorted — the whole
// row identity the seed must preserve, not a per-name existence probe.
func uploadRowsOf(t *testing.T, s *Service, qsoID int64) []string {
	t.Helper()
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	require.NoError(t, err)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s/%s/%s/%s/%s", r.ForwarderName, r.ForwarderType, r.Action, r.Origin, r.Status))
	}
	sort.Strings(out)
	return out
}

func TestCharacterization_HomeFanOut_LiveSubmitEditDelete(t *testing.T) {
	s := newTestService(t, stationForwarders()...)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	buf := logbuf(s)
	ctx := context.Background()

	// Live submit: one insert row per ENABLED forwarder whose filter carries
	// insert; the disabled qrzcq gets none; forwarded_to lists config order.
	res, err := s.Submit(ctx, lbID, fanoutRec("K1AAA"), false)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
	require.Equal(t, []string{
		"clublog/clublog/insert/live/pending",
		"qrz/qrz/insert/live/pending",
		"smcloud/smcloud/insert/live/pending",
	}, uploadRowsOf(t, s, res.ID), "live submit fan-out in the adopted archive")
	require.Contains(t, logLineWith(t, buf.String(), "QSO stored"),
		`"forwarded_to":["qrz","clublog","smcloud"]`, "config order, disabled qrzcq absent")

	// Edit: update rows only where the filter carries update (clublog's does not).
	snap, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	_, err = s.Update(ctx, snap, []byte(`{"rst_sent":"57"}`), source.API)
	require.NoError(t, err)
	require.Equal(t, []string{
		"clublog/clublog/insert/live/pending",
		"qrz/qrz/insert/live/pending",
		"qrz/qrz/update/edit/pending",
		"smcloud/smcloud/insert/live/pending",
		"smcloud/smcloud/update/edit/pending",
	}, uploadRowsOf(t, s, res.ID), "edit fan-out in the adopted archive")

	// Delete: a delete row for every enabled forwarder whose filter carries delete.
	snap, err = s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, snap, source.API))
	require.Equal(t, []string{
		"clublog/clublog/delete/edit/pending",
		"clublog/clublog/insert/live/pending",
		"qrz/qrz/delete/edit/pending",
		"qrz/qrz/insert/live/pending",
		"qrz/qrz/update/edit/pending",
		"smcloud/smcloud/delete/edit/pending",
		"smcloud/smcloud/insert/live/pending",
		"smcloud/smcloud/update/edit/pending",
	}, uploadRowsOf(t, s, res.ID), "delete fan-out in the adopted archive")
	require.Contains(t, logLineWith(t, buf.String(), "QSO soft-deleted"),
		`"forwarded_to":["qrz","clublog","smcloud"]`)
}

// The adopted archive's routes are its seeded bindings, named after the
// station's entries: the fan-out is the same set as before the seed.
func TestCharacterization_LegacyEntryFansOutLikeHome(t *testing.T) {
	s := newTestService(t, stationForwarders()...)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	routes := s.DestinationRoutes()
	require.Len(t, routes, 4, "one route per station entry on the one logbook")
	require.Equal(t, "qrz", routes[0].Config.Name, "the adopted logbook keeps the legacy names")

	res, err := s.Submit(context.Background(), lbID, fanoutRec("K1AAA"), false)
	require.NoError(t, err)
	require.Equal(t, []string{
		"clublog/clublog/insert/live/pending",
		"qrz/qrz/insert/live/pending",
		"smcloud/smcloud/insert/live/pending",
	}, uploadRowsOf(t, s, res.ID))
}

// A NEW managed archive queues nowhere — submit, edit and delete alike — and
// says so with an explicit empty list: it has no bindings, whatever the
// station's config entries say. The observable did not move across 5B.
func TestCharacterization_NewManagedArchive_QueuesNowhere(t *testing.T) {
	s := newTestService(t, stationForwarders()...)
	lbID := seedLogbook(t, s, "Contest", "M0ABC")
	s.SetDestinationRoutes(nil) // a new archive: no bindings
	buf := logbuf(s)
	ctx := context.Background()

	res, err := s.Submit(ctx, lbID, fanoutRec("K1AAA"), false)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
	require.Empty(t, uploadRowsOf(t, s, res.ID))
	require.Contains(t, logLineWith(t, buf.String(), "QSO stored"), `"forwarded_to":[]`)

	snap, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	_, err = s.Update(ctx, snap, []byte(`{"rst_sent":"57"}`), source.API)
	require.NoError(t, err)
	require.Empty(t, uploadRowsOf(t, s, res.ID))

	snap, err = s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, snap, source.API))
	require.Empty(t, uploadRowsOf(t, s, res.ID))
	require.Contains(t, logLineWith(t, buf.String(), "QSO soft-deleted"), `"forwarded_to":[]`)
}
