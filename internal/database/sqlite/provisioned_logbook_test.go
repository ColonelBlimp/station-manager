package sqlite

import (
	"context"
	stderr "errors"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// W-0021 Q7 commit 5 (ADR 0071 refinement): provisioning writes the cloud
// archive's logbook UUIDs into the file it is building, through a call used on
// no other path. Everywhere else the UUID is minted.
//
//	PL1 InsertProvisionedLogbookWithContext keeps the given UUID, name and
//	    callsign.
//	PL2 it refuses a UUID that is not a UUIDv7, an empty one included.
//	PL3 it refuses once the file has an archive identity: only a file under
//	    construction takes a chosen UUID.
//	PL4 a UUID or a name already in the file is refused, the name as
//	    ErrDuplicateName.
//	PL5 InsertLogbookWithContext still mints its own UUID, whatever the caller
//	    passes.

const (
	plLB1 = "0197f9a0-0000-7000-8000-0000000f0001"
	plLB2 = "0197f9a0-0000-7000-8000-0000000f0002"
)

func TestInsertProvisionedLogbook_PL1_KeepsTheGivenUUID(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	id, err := svc.InsertProvisionedLogbookWithContext(ctx, types.Logbook{UUID: plLB1, Name: "Contest", Callsign: "7Q5MLV"})
	if err != nil || id < 1 {
		t.Fatalf("insert = %d, %v; want a new id", id, err)
	}
	lbs, err := svc.FetchAllLogbooksWithContext(ctx)
	if err != nil || len(lbs) != 1 || lbs[0].ID != id || lbs[0].UUID != plLB1 || lbs[0].Name != "Contest" || lbs[0].Callsign != "7Q5MLV" {
		t.Fatalf("logbooks = %+v (%v); want Contest / 7Q5MLV with UUID %s", lbs, err, plLB1)
	}
}

func TestInsertProvisionedLogbook_PL2_RefusesAnInvalidUUID(t *testing.T) {
	svc := testService(t)
	for _, uuid := range []string{"", "nope", "0197f9a0-0000-4000-8000-0000000f0001"} {
		if _, err := svc.InsertProvisionedLogbookWithContext(context.Background(), types.Logbook{UUID: uuid, Name: "L" + uuid, Callsign: "7Q5MLV"}); err == nil {
			t.Fatalf("UUID %q accepted; want a UUIDv7 required", uuid)
		}
	}
	if lbs, _ := svc.FetchAllLogbooksWithContext(context.Background()); len(lbs) != 0 {
		t.Fatalf("refused inserts left %+v", lbs)
	}
	// The control: a UUIDv7 is accepted on the same service.
	if _, err := svc.InsertProvisionedLogbookWithContext(context.Background(), types.Logbook{UUID: plLB1, Name: "L", Callsign: "7Q5MLV"}); err != nil {
		t.Fatalf("control: a UUIDv7 was refused: %v", err)
	}
}

func TestInsertProvisionedLogbook_PL3_OnlyBeforeTheArchiveIdentity(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	id, err := svc.InsertProvisionedLogbookWithContext(ctx, types.Logbook{UUID: plLB1, Name: "A", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteArchiveIdentityWithContext(ctx, utils.NewUUIDv7(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InsertProvisionedLogbookWithContext(ctx, types.Logbook{UUID: plLB2, Name: "B", Callsign: "7Q5MLV"}); err == nil {
		t.Fatal("a chosen UUID was accepted by a file that already has its identity")
	}
	if lbs, _ := svc.FetchAllLogbooksWithContext(ctx); len(lbs) != 1 {
		t.Fatalf("logbooks = %+v; want only the one inserted before the identity", lbs)
	}
}

func TestInsertProvisionedLogbook_PL4_RefusesADuplicate(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if _, err := svc.InsertProvisionedLogbookWithContext(ctx, types.Logbook{UUID: plLB1, Name: "A", Callsign: "7Q5MLV"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InsertProvisionedLogbookWithContext(ctx, types.Logbook{UUID: plLB1, Name: "B", Callsign: "7Q5MLV"}); err == nil {
		t.Fatal("a second logbook with the same UUID was accepted")
	}
	_, err := svc.InsertProvisionedLogbookWithContext(ctx, types.Logbook{UUID: plLB2, Name: "A", Callsign: "7Q5MLV"})
	if !stderr.Is(err, errors.ErrDuplicateName) {
		t.Fatalf("a second logbook named A = %v; want ErrDuplicateName", err)
	}
}

func TestInsertLogbook_PL5_StillMintsItsOwnUUID(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	id, err := svc.InsertLogbookWithContext(ctx, types.Logbook{UUID: plLB1, Name: "A", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	lb, err := svc.FetchLogbookByIDWithContext(ctx, id)
	if err != nil || lb.UUID == plLB1 || !utils.IsValidUUIDv7(lb.UUID) {
		t.Fatalf("logbook = %+v (%v); want a minted UUIDv7, never the caller's %s", lb, err, plLB1)
	}
}
