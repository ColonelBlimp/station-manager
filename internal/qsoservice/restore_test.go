package qsoservice

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/stretchr/testify/require"
)

// restorableQso builds a rich types.Qso the way the restore command does —
// unmarshalled from the export payload, envelope metadata applied.
func restorableQso(uuid string, modified time.Time) types.Qso {
	q := types.Qso{
		UUID:            uuid,
		DedupeKey:       "", // exercise the recompute fallback by default
		AppSmRequestQsl: true,
		QrzlogLogid:     "12345",
		ModifiedAt:      modified,
		Revision:        7, // mid-sequence — restore must resume, not reset (ADR 0050)
	}
	q.Call = "DL9UW"
	q.Band = "20m"
	q.Mode = "SSB"
	q.Submode = "USB"
	q.Freq = "14.255"
	q.QsoDate = "20260601"
	q.TimeOn = "142559" // seconds preserved
	q.RstSent = "59"
	q.RstRcvd = "57"
	q.Gridsquare = "JO41"
	q.Name = "Uwe"
	q.CountryDetails = types.Country{Name: "Germany"}
	return q
}

func TestRestore_RoundTripPreservesEverything(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "7Q5MLV")
	ctx := context.Background()

	uuid := utils.NewUUIDv7()
	modified := time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC)
	orig := restorableQso(uuid, modified)

	status, err := s.Restore(ctx, lbID, orig)
	require.NoError(t, err)
	require.Equal(t, RestoreStored, status)

	got, err := s.DB.FetchQsoByUUIDWithContext(ctx, uuid)
	require.NoError(t, err)
	require.Equal(t, uuid, got.UUID, "identity preserved")
	require.True(t, got.ModifiedAt.Equal(modified), "modified_at preserved: %v", got.ModifiedAt)
	require.Equal(t, int64(7), got.Revision, "revision preserved — the sync sequence resumes post-restore")
	// The additional_data-carried extras — what an ADIF re-import flattens.
	require.Equal(t, "142559", got.TimeOn, "seconds preserved")
	require.True(t, got.AppSmRequestQsl)
	require.Equal(t, "12345", got.QrzlogLogid)
	require.Equal(t, "Germany", got.CountryDetails.Name)
	require.Equal(t, "USB", got.Submode)
	require.NotEmpty(t, got.DedupeKey, "dedupe key recomputed for old payloads")

	// Idempotent re-run: the existing row wins.
	status, err = s.Restore(ctx, lbID, orig)
	require.NoError(t, err)
	require.Equal(t, RestoreSkippedExisting, status)
}

func TestRestore_TombstoneStaysDeleted(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "7Q5MLV")
	ctx := context.Background()

	uuid := utils.NewUUIDv7()
	q := restorableQso(uuid, time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	q.DeletedAt = time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)

	status, err := s.Restore(ctx, lbID, q)
	require.NoError(t, err)
	require.Equal(t, RestoreStored, status)

	// Live fetch misses (soft-deleted)…
	_, err = s.DB.FetchQsoByUUIDWithContext(ctx, uuid)
	require.Error(t, err)
	// …the including-deleted fetch sees the preserved tombstone.
	got, err := s.DB.FetchQsoByUUIDIncludingDeletedWithContext(ctx, uuid)
	require.NoError(t, err)
	require.True(t, got.DeletedAt.Equal(q.DeletedAt), "tombstone instant preserved: %v", got.DeletedAt)

	// And the manifest reads it as deleted with the ORIGINAL recency.
	entries, err := s.DB.FetchQsoManifestWithContext(ctx, lbID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.True(t, entries[0].Deleted)
	require.True(t, entries[0].ModifiedAt.Equal(q.ModifiedAt))
}

func TestRestore_GuardRails(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "7Q5MLV")
	ctx := context.Background()

	bad := restorableQso("not-a-uuid", time.Now())
	_, err := s.Restore(ctx, lbID, bad)
	require.Error(t, err, "invalid uuid refused")

	noMod := restorableQso(utils.NewUUIDv7(), time.Time{})
	_, err = s.Restore(ctx, lbID, noMod)
	require.Error(t, err, "zero modified_at refused — never silently defaulted")

	ok := restorableQso(utils.NewUUIDv7(), time.Now().UTC())
	_, err = s.Restore(ctx, 0, ok)
	require.Error(t, err, "missing target logbook refused")
}

// Restore never queues upload rows — the cloud already holds these QSOs;
// re-pushing a restore would be circular.
func TestRestore_QueuesNoUploads(t *testing.T) {
	s := newTestService(t, enabledCloud())
	lbID := seedLogbook(t, s, "Main", "7Q5MLV")
	ctx := context.Background()

	uuid := utils.NewUUIDv7()
	_, err := s.Restore(ctx, lbID, restorableQso(uuid, time.Now().UTC()))
	require.NoError(t, err)

	got, err := s.DB.FetchQsoByUUIDWithContext(ctx, uuid)
	require.NoError(t, err)
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, got.ID)
	require.NoError(t, err)
	require.Empty(t, rows, "restore must not enqueue uploads")
}
