package qsoservice

import (
	"context"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// enabledCloud is a minimal enabled smcloud-shaped forwarder config. Like the
// backfill tests' enabledQRZ, the type never gets built here — only
// name/enabled/action_filter are read.
func enabledCloud() types.ForwarderConfig {
	return types.ForwarderConfig{
		Name:         "cloud",
		Type:         "smcloud",
		Enabled:      true,
		ActionFilter: []string{"insert", "update", "delete"},
	}
}

// hasDeleteRow reports whether the QSO has a delete-upload row to forwarderName.
func hasDeleteRow(t *testing.T, s *Service, qsoID int64, forwarderName string) bool {
	t.Helper()
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.ForwarderName == forwarderName && r.Action == "delete" {
			return true
		}
	}
	return false
}

// The reconcile repair path (ADR 0040 S4): a missed tombstone is re-queued as
// a delete row; live and unknown UUIDs are refused per-row, never fatal.
func TestEnqueueDeleteUploads(t *testing.T) {
	s := newTestService(t, enabledCloud())
	lbID := seedLogbook(t, s, "Main", "G0XYZ")
	ctx := context.Background()

	deletedUUID, deletedID := seedStoredQso(t, s, lbID, "DL9UW", "1200")
	liveUUID, liveID := seedStoredQso(t, s, lbID, "9A4ZM", "1201")

	// Soft-delete the first QSO (the normal path — it also queues its own
	// delete row; the repair enqueue below must be idempotent over that).
	deleted, err := s.DB.FetchQsoByUUIDWithContext(ctx, deletedUUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, deleted, source.Source("test")))

	res, err := s.EnqueueDeleteUploads(ctx, "cloud", []string{
		deletedUUID,
		liveUUID,
		"0197f9a0-9999-7999-8999-999999999999", // unknown
		"not-a-uuid",
	}, origin.Reconcile)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued, "only the tombstone enqueues")
	require.Equal(t, []string{liveUUID}, res.SkippedLive, "a live QSO must never get a delete row")
	require.Len(t, res.NotFound, 2)

	require.True(t, hasDeleteRow(t, s, deletedID, "cloud"))
	require.False(t, hasDeleteRow(t, s, liveID, "cloud"))

	// Unknown / disabled forwarder → forwarder_unavailable, nothing queued.
	_, err = s.EnqueueDeleteUploads(ctx, "nope", []string{deletedUUID}, origin.Reconcile)
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "forwarder_unavailable", se.Code)
}

// FetchQsoManifestWithContext is the local half of the reconcile diff: every
// row (tombstones included), uuid-ordered, with UTC second-precision
// modified_at from the trigger/default.
func TestFetchQsoManifest(t *testing.T) {
	s := newTestService(t, enabledCloud())
	lbID := seedLogbook(t, s, "Main", "G0XYZ")
	ctx := context.Background()

	u1, _ := seedStoredQso(t, s, lbID, "DL9UW", "1200")
	u2, _ := seedStoredQso(t, s, lbID, "9A4ZM", "1201")
	q2, err := s.DB.FetchQsoByUUIDWithContext(ctx, u2)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, q2, source.Source("test")))

	entries, err := s.DB.FetchQsoManifestWithContext(ctx, lbID)
	require.NoError(t, err)
	require.Len(t, entries, 2, "tombstones included")

	byUUID := map[string]types.QsoManifestEntry{}
	for i, e := range entries {
		byUUID[e.UUID] = e
		require.False(t, e.ModifiedAt.IsZero(), "modified_at populated")
		require.Equal(t, "UTC", e.ModifiedAt.Location().String(), "normalised to UTC")
		if i > 0 {
			require.Less(t, entries[i-1].UUID, e.UUID, "uuid-ordered")
		}
	}
	require.False(t, byUUID[u1].Deleted)
	require.True(t, byUUID[u2].Deleted)

	// Unknown logbook → empty, not an error (nothing to reconcile).
	empty, err := s.DB.FetchQsoManifestWithContext(ctx, lbID+99)
	require.NoError(t, err)
	require.Empty(t, empty)
}
