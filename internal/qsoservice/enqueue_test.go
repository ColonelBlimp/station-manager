package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/stretchr/testify/require"
)

// Register the "qrz" type's ADIF stamp prefix for these tests (the real qrz
// package isn't imported here). EnqueueUploads resolves a forwarder's type → its
// stamp prefix to key the skip-check; without it, type "qrz" would report "no
// stamp" and never skip.
func init() {
	forwarding.RegisterAdifPrefix("qrz", "QRZCOM")
}

// enabledQRZ is a minimal enabled forwarder config for the backfill tests.
// EnqueueUploads only reads name/type/enabled/action_filter — it never builds
// the forwarder — so the type string need not be a registered implementation.
func enabledQRZ() types.ForwarderConfig {
	return types.ForwarderConfig{
		Name:         "qrz",
		Type:         "qrz",
		Enabled:      true,
		ActionFilter: []string{"insert", "update", "delete"},
	}
}

// seedStoredQso stores one QSO with no upload rows (SubmitImport with empty
// forwardTo queues nothing) and returns its UUID + local id.
func seedStoredQso(t *testing.T, s *Service, lbID int64, call, timeOn string) (string, int64) {
	t.Helper()
	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"}, // import relaxes the callsign match
	}
	res, err := s.SubmitImport(context.Background(), lbID, rec, false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
	return res.UUID, res.ID
}

// hasPendingInsertRow reports whether the QSO has an insert-upload row to
// forwarderName (any status).
func hasInsertRow(t *testing.T, s *Service, qsoID int64, forwarderName string) bool {
	t.Helper()
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.ForwarderName == forwarderName && r.Action == "insert" {
			return true
		}
	}
	return false
}

func TestEnqueueUploads_EnqueuesSelectedGaps(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	u2, id2 := seedStoredQso(t, s, lbID, "K2BBB", "1201")

	res, err := s.EnqueueUploads(ctx, "qrz", []string{u1, u2}, false)
	require.NoError(t, err)
	require.Equal(t, 2, res.Enqueued)
	require.Equal(t, 0, res.SkippedUploaded)
	require.Empty(t, res.NotFound)
	require.Empty(t, res.SkippedDeleted)

	require.True(t, hasInsertRow(t, s, id1, "qrz"), "K1AAA queued")
	require.True(t, hasInsertRow(t, s, id2, "qrz"), "K2BBB queued")
}

func TestEnqueueUploads_SkipsAlreadyUploadedUnlessForce(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")

	// Drive the row to uploaded via the real worker lifecycle: enqueue → claim →
	// mark success WITH the ADIF stamp (the durable per-destination "done" signal
	// the skip-check reads — the stamp, not the queue row, so it survives import).
	_, err := s.EnqueueUploads(ctx, "qrz", []string{u1}, false)
	require.NoError(t, err)
	claimed, err := s.DB.ClaimPendingUploadsWithContext(ctx, "qrz", 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, s.DB.MarkUploadSuccessWithAdifStampWithContext(ctx, claimed[0].ID, "upstream-1", id1, "QRZCOM"))

	// Default (no force): the uploaded QSO is skipped, nothing re-queued.
	res, err := s.EnqueueUploads(ctx, "qrz", []string{u1}, false)
	require.NoError(t, err)
	require.Equal(t, 0, res.Enqueued)
	require.Equal(t, 1, res.SkippedUploaded)

	// force=true re-arms the row to pending so the worker re-sends.
	res, err = s.EnqueueUploads(ctx, "qrz", []string{u1}, true)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued)
	require.Equal(t, 0, res.SkippedUploaded)

	rows, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, id1)
	require.NoError(t, err)
	require.Len(t, rows, 1, "still one insert row (re-armed, not duplicated)")
	require.Equal(t, "pending", rows[0].Status, "force re-armed to pending")
}

func TestEnqueueUploads_RejectsIneligibleForwarder(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		fwds []types.ForwarderConfig
		dest string
	}{
		{
			name: "disabled",
			fwds: []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: false, ActionFilter: []string{"insert"}}},
			dest: "qrz",
		},
		{
			name: "unknown",
			fwds: []types.ForwarderConfig{enabledQRZ()},
			dest: "clublog",
		},
		{
			name: "enabled but does not forward inserts",
			fwds: []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"delete"}}},
			dest: "qrz",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestService(t, c.fwds...)
			lbID := seedLogbook(t, s, "Main", "M0ABC")
			u1, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200")

			_, err := s.EnqueueUploads(ctx, c.dest, []string{u1}, false)
			require.Error(t, err)
			se := IsSubmitError(err)
			require.NotNil(t, se)
			require.Equal(t, "forwarder_unavailable", se.Code)
		})
	}
}

func TestEnqueueUploads_ClassifiesMissingAndDeleted(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	live, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	delUUID, _ := seedStoredQso(t, s, lbID, "K2BBB", "1201")

	// Soft-delete the second QSO.
	delQso, err := s.DB.FetchQsoByUUIDWithContext(ctx, delUUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, delQso, source.API))

	unknown := utils.NewUUIDv7() // valid shape, never stored
	const malformed = "not-a-uuid"

	res, err := s.EnqueueUploads(ctx, "qrz", []string{live, delUUID, unknown, malformed}, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued, "only the live QSO")
	require.ElementsMatch(t, []string{delUUID}, res.SkippedDeleted)
	require.ElementsMatch(t, []string{unknown, malformed}, res.NotFound)
}
