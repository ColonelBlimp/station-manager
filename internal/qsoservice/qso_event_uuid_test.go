package qsoservice

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/events"
)

// AW-1 alpha.2: every qso.* SSE event must carry the QSO's canonical uuid (qso_uuid), not
// only the deprecated daemon-local numeric qso_id — so a consumer can key on the stable
// external identifier. The uuid is in scope at each publisher; this pins that it is
// actually populated (equal to the row's uuid) at store, update, and delete.
func TestQsoEvents_CarryQsoUUID(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	lbID := seedLogbook(t, s, "Main", "M0ABC")

	ch, unsub := s.Hub.Subscribe()
	defer unsub()

	nextEvent := func(t *testing.T, wantName string) events.Event {
		t.Helper()
		select {
		case ev := <-ch:
			require.Equal(t, wantName, ev.Name)
			return ev
		default:
			t.Fatalf("expected a %s event, got none", wantName)
			return events.Event{}
		}
	}

	// Store
	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	require.NotEmpty(t, res.UUID)

	stored := nextEvent(t, events.NameQsoStored).Payload.(events.QsoStoredPayload)
	require.Equal(t, res.UUID, stored.QsoUUID, "qso.stored must carry the QSO uuid")

	// Update (a real edit so it publishes)
	existing, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	_, err = s.Update(ctx, existing, []byte(fmt.Sprintf(`{"rst_sent":%q}`, "599")), source.API)
	require.NoError(t, err)

	updated := nextEvent(t, events.NameQsoUpdated).Payload.(events.QsoUpdatedPayload)
	require.Equal(t, res.UUID, updated.QsoUUID, "qso.updated must carry the QSO uuid")

	// Delete
	existing, err = s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, existing, source.API))

	deleted := nextEvent(t, events.NameQsoDeleted).Payload.(events.QsoDeletedPayload)
	require.Equal(t, res.UUID, deleted.QsoUUID, "qso.deleted must carry the QSO uuid")
}
