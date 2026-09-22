package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// The interim forwarding gate (W-0021 slice 2B): in any archive other than the
// adopted one, no submit, edit, delete, stamp sync or manual backfill enqueues a
// row to ANY destination, however many forwarders are enabled — a contest file
// must never upload into the home archive's accounts. The gate keys on the
// archive the service is writing (set by the daemon from the resolved path);
// nil means the adopted file.
func TestForwardingGate_ManagedArchiveEnqueuesNothing(t *testing.T) {
	s := newTestService(t,
		types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}},
		types.ForwarderConfig{Name: "smcloud", Type: "smcloud", Enabled: true, ActionFilter: []string{"insert", "update", "delete"}},
	)
	s.SetArchive(&types.QsoArchiveConfig{ID: "019fd5c5-efcc-7193-be4f-1fee532ee316", Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged})
	require.False(t, s.ForwardingAdmitted())
	lbID := seedLogbook(t, s, "Contest", "G0XYZ")

	// The LIVE submit path (an import with no --forward never enqueues, gate or
	// not, so it would prove nothing here).
	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "M0CMC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
	}
	res, err := s.Submit(context.Background(), lbID, rec, false)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), res.ID)
	require.NoError(t, err)
	require.Empty(t, rows, "a submit in a gated archive must enqueue to no destination")

	// Manual backfill is refused by name, not silently skipped.
	_, err = s.EnqueueUploads(context.Background(), "qrz", []string{res.UUID}, false, origin.Manual)
	se := IsSubmitError(err)
	require.NotNil(t, se, "want a SubmitError, got %v", err)
	require.Equal(t, "forwarding_gated", se.Code)

	// The adopted file (nil entry, or a legacy entry) is admitted as before.
	s.SetArchive(nil)
	require.True(t, s.ForwardingAdmitted())
	s.SetArchive(&types.QsoArchiveConfig{ID: "019fd5c5-efcc-7193-be4f-1fee532ee315", Ownership: types.QsoArchiveOwnershipLegacy})
	require.True(t, s.ForwardingAdmitted())
	res2, err := s.Submit(context.Background(), lbID, adif.Record{
		ContactedStation: types.ContactedStation{Call: "EA1B"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1201"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
	}, false)
	require.NoError(t, err)
	require.True(t, hasInsertRow(t, s, res2.ID, "qrz"), "the adopted archive still enqueues")
}

// An import that names forwarders (`smd import --forward`) into a gated archive
// is refused up front with the gate's reason, before any record is stored.
func TestForwardingGate_ImportWithForwardIsRefused(t *testing.T) {
	s := newTestService(t, types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"}})
	s.SetArchive(&types.QsoArchiveConfig{ID: "019fd5c5-efcc-7193-be4f-1fee532ee316", Ownership: types.QsoArchiveOwnershipManaged})
	lbID := seedLogbook(t, s, "Contest", "G0XYZ")
	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: "M0CMC"},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
	}
	_, err := s.SubmitImport(context.Background(), lbID, rec, false, []string{"qrz"})
	se := IsSubmitError(err)
	require.NotNil(t, se, "want a SubmitError, got %v", err)
	require.Equal(t, "forwarding_gated", se.Code)
	// Nothing stored: the refusal precedes the write.
	n, err := s.DB.FetchQsoCountByLogbookIdWithContext(context.Background(), lbID, "", false)
	require.NoError(t, err)
	require.Zero(t, n)
}
