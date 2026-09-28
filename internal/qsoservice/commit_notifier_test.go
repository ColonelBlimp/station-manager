package qsoservice

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
)

// ADR 0084 slice 2b: after a commit that changes an archive's QSO counts, the
// service calls the injected commit notifier — the archive summary's dirty
// signal — directly, not through the event hub (whose subscribers can be
// evicted). Once per committed store or delete; never for a refused write.
func TestCommitNotifier_CalledAfterStoreAndDeleteCommits(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	var calls atomic.Int32
	s.SetCommitNotifier(func() { calls.Add(1) })

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "a stored QSO notifies once")

	// A duplicate is refused before any commit: no notification.
	dup, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	require.Equal(t, "duplicate", dup.Status)
	require.EqualValues(t, 1, calls.Load(), "a refused duplicate must not notify")

	// An update does not change any count: no notification.
	existing, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	_, err = s.Update(ctx, existing, []byte(`{"rst_sent":"599"}`), source.API)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "an update changes no count")

	existing, err = s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, existing, source.API))
	require.EqualValues(t, 2, calls.Load(), "a deleted QSO notifies once")
}

// Unwired (the offline commands, tests) the service works as before.
func TestCommitNotifier_UnwiredIsANoOp(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	_, err := s.Submit(context.Background(), lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
}
