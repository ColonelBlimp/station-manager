package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// Register the "smcloud" type as a row mirror for these tests (the real
// smcloud package isn't imported here — same pattern as the qrz AdifPrefix
// registration in enqueue_test.go).
func init() {
	forwarding.RegisterRowMirror("smcloud")
}

// enabledMirror is a minimal enabled row-mirror forwarder config.
// EnqueueStampSync only reads name/type/enabled/action_filter.
func enabledMirror() types.ForwarderConfig {
	return types.ForwarderConfig{
		Name:         "smcloud",
		Type:         "smcloud",
		Enabled:      true,
		ActionFilter: []string{"insert", "update", "delete"},
	}
}

// hasUpdateRow reports whether the QSO has an update-action upload row to
// forwarderName (any status).
func hasUpdateRow(t *testing.T, s *Service, qsoID int64, forwarderName string) bool {
	t.Helper()
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.ForwarderName == forwarderName && r.Action == "update" {
			return true
		}
	}
	return false
}

func TestEnqueueStampSync_EnqueuesToEnabledMirror(t *testing.T) {
	s := newTestService(t, enabledMirror(), enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	_, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	_, id2 := seedStoredQso(t, s, lbID, "K2BBB", "1201")

	n, err := s.EnqueueStampSync(ctx, []int64{id1, id2})
	require.NoError(t, err)
	require.Equal(t, 2, n)

	require.True(t, hasUpdateRow(t, s, id1, "smcloud"), "K1AAA queued to mirror")
	require.True(t, hasUpdateRow(t, s, id2, "smcloud"), "K2BBB queued to mirror")
	// The non-mirror forwarder (qrz) must NOT receive stamp-sync rows —
	// re-uploading to QRZ because QRZ's own stamp landed would loop.
	require.False(t, hasUpdateRow(t, s, id1, "qrz"), "no stamp-sync row to non-mirror types")
}

func TestEnqueueStampSync_NoMirrorConfigured_NoOp(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	_, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")

	n, err := s.EnqueueStampSync(context.Background(), []int64{id1})
	require.NoError(t, err)
	require.Zero(t, n)
	require.False(t, hasUpdateRow(t, s, id1, "qrz"))
}

func TestEnqueueStampSync_DisabledMirror_NoOp(t *testing.T) {
	mirror := enabledMirror()
	mirror.Enabled = false
	s := newTestService(t, mirror)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	_, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")

	n, err := s.EnqueueStampSync(context.Background(), []int64{id1})
	require.NoError(t, err)
	require.Zero(t, n)
	require.False(t, hasUpdateRow(t, s, id1, "smcloud"))
}

func TestEnqueueStampSync_EmptyIDs_NoOp(t *testing.T) {
	s := newTestService(t, enabledMirror())

	n, err := s.EnqueueStampSync(context.Background(), nil)
	require.NoError(t, err)
	require.Zero(t, n)
}

// Q4 — THE STAMP-SYNC RE-ENQUEUE LOGS AT INFO, so the fix for the smcloud
// bandwidth-churn item is observable at the default production level. Volume is
// bounded by stamp events (a handful a day), not traffic. Confusable state: the
// mechanism not firing at all — both were silent below Debug.
func TestEnqueueStampSync_LogsAtInfo(t *testing.T) {
	s := newTestService(t, enabledMirror())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	_, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	buf := logbuf(s)

	n, err := s.EnqueueStampSync(context.Background(), []int64{id1})
	require.NoError(t, err)
	require.Equal(t, 1, n, "fixture: one mirror row queued")

	line := logLineWith(t, buf.String(), "stamp sync")
	require.Contains(t, line, `"level":"info"`,
		"the stamp-sync re-enqueue must be visible at the default level — its silent "+
			"non-firing returns the daemon to the expensive full-manifest reconcile path")
}

func TestEnqueueStampSync_RepeatIsIdempotentReArm(t *testing.T) {
	// Two stamps in quick succession (QRZ stamp, then the session-email
	// stamp) must not error and must leave a single re-armed queue row —
	// the InsertQsoUploadTx UPSERT absorbs the repeat.
	s := newTestService(t, enabledMirror())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	_, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	ctx := context.Background()

	_, err := s.EnqueueStampSync(ctx, []int64{id1})
	require.NoError(t, err)
	_, err = s.EnqueueStampSync(ctx, []int64{id1})
	require.NoError(t, err)

	rows, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, id1)
	require.NoError(t, err)
	updates := 0
	for _, r := range rows {
		if r.ForwarderName == "smcloud" && r.Action == "update" {
			updates++
		}
	}
	require.Equal(t, 1, updates, "repeat enqueue re-arms the same row, not a duplicate")
}
