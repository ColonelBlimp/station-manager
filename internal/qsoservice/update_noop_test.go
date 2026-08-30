package qsoservice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/events"
)

// AW-3: a QSO PATCH with no EFFECTIVE change — an empty body, {}, an unknown-only
// body, an immutable-only body, or values that normalize back to the stored ones — is
// a no-op. It returns the existing row and performs ZERO mutations: no revision or
// modified_at bump, no qso_history row, no upload re-arm, and no qso.updated event.
// Malformed JSON and genuinely invalid edits still fail (covered elsewhere). The check
// lives at the service boundary and compares an editable-field projection, so a
// canonically equivalent edit is correctly the same as no edit.
func TestUpdate_NoEffectiveChange_IsNoOp(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	lbID := seedLogbook(t, s, "Main", "M0ABC")

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)

	seed, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)

	ch, unsub := s.Hub.Subscribe()
	defer unsub()

	noops := []struct{ name, body string }{
		{"empty body", ``},
		{"empty object", `{}`},
		{"unknown-only", `{"totally_unknown":1}`},
		{"immutable-only", `{"uuid":"11111111-1111-1111-1111-111111111111","id":999,"logbook_id":42}`},
		{"canonically equivalent call", fmt.Sprintf(`{"call":"%s"}`, strings.ToLower(seed.ContactedStation.Call))},
	}
	for _, c := range noops {
		t.Run(c.name, func(t *testing.T) {
			// Re-fetch so each case runs against the current on-disk row (a failing
			// no-op that bumps revision must not turn later cases into CAS conflicts).
			existing, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
			require.NoError(t, err)
			histBefore, err := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, res.UUID)
			require.NoError(t, err)

			out, err := s.Update(ctx, existing, []byte(c.body), source.API)
			require.NoError(t, err, "a no-effective-change edit must succeed as a no-op")
			require.Equal(t, existing.Revision, out.Revision, "returned row keeps the stored revision")

			after, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
			require.NoError(t, err)
			require.Equal(t, existing.Revision, after.Revision, "no revision bump")
			require.Equal(t, existing.ModifiedAt, after.ModifiedAt, "no modified_at bump")

			histAfter, err := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, res.UUID)
			require.NoError(t, err)
			require.Len(t, histAfter, len(histBefore), "no qso_history row appended")
		})
	}

	select {
	case ev := <-ch:
		t.Fatalf("a no-op edit published an event (%s) — no side effects allowed", ev.Name)
	default:
	}

	// Positive control: a REAL edit is NOT a no-op — it must still bump the revision
	// and append an audit row, so the short-circuit can't swallow genuine edits.
	existing, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	histBefore, err := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	newRst := "57"
	if existing.QsoDetails.RstSent == newRst {
		newRst = "55"
	}
	_, err = s.Update(ctx, existing, []byte(fmt.Sprintf(`{"rst_sent":%q}`, newRst)), source.API)
	require.NoError(t, err)
	// Re-fetch: the trigger-bumped revision lands on the row, not the returned copy.
	after, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.Greater(t, after.Revision, existing.Revision, "a real edit bumps the revision")
	require.Equal(t, newRst, after.QsoDetails.RstSent, "a real edit persists the new value")
	histAfter, err := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.Len(t, histAfter, len(histBefore)+1, "a real edit appends one audit row")

	select {
	case ev := <-ch:
		require.Equal(t, events.NameQsoUpdated, ev.Name, "a real edit publishes qso.updated")
	default:
		t.Fatal("a real edit must publish qso.updated")
	}
}

// AW-3 concurrency: the no-op short-circuit skips the revision-guarded write, so it must
// still refuse a no-op built from a STALE snapshot. Otherwise a patch requesting the old
// value against a row a concurrent edit has already moved on would report a false
// success while the newer value stays stored (codex d6c26380 P1).
func TestUpdate_NoOpAgainstStaleSnapshot_IsEditConflict(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	lbID := seedLogbook(t, s, "Main", "M0ABC")

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	stale, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)

	// A concurrent real edit moves the row past the stale snapshot's revision.
	fresh, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	newRst := "31"
	if fresh.QsoDetails.RstSent == newRst {
		newRst = "33"
	}
	_, err = s.Update(ctx, fresh, []byte(fmt.Sprintf(`{"rst_sent":%q}`, newRst)), source.API)
	require.NoError(t, err)

	// A patch that is a no-op against the STALE snapshot must NOT report success — the
	// snapshot has been superseded, so it is an edit_conflict, not a silent false OK.
	_, err = s.Update(ctx, stale, []byte(fmt.Sprintf(`{"rst_sent":%q}`, stale.QsoDetails.RstSent)), source.API)
	se := IsSubmitError(err)
	require.NotNil(t, se, "a no-op against a superseded snapshot must be a caller-facing conflict")
	require.Equal(t, "edit_conflict", se.Code)

	// The concurrent edit stands — the stale no-op did not overwrite it.
	after, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.Equal(t, newRst, after.QsoDetails.RstSent)
}
