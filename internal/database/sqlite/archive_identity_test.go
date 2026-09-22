package sqlite

import (
	"context"
	stderr "errors"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// W-0021 slice 1, ruling (b): the daemon fills the file's identity ONCE, in one
// transaction, after Migrate() — the archive UUID (minted only when the
// singleton row is absent), the default logbook pointer, and a UUID for every
// logbook row that has none. A second call mints nothing: the identities are
// byte-identical, which is what makes a crash between "identity written" and
// "catalogue written" safe (the next start reads the same UUID back).
func TestEnsureArchiveIdentity_MintsOnceThenIsIdempotent(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	// Two pre-0012 style rows: no uuid.
	for _, stmt := range []string{
		`INSERT INTO logbook (id, callsign, name) VALUES (1,'G4ABC','Home')`,
		`INSERT INTO logbook (id, callsign, name) VALUES (2,'G4ABC','Contest')`,
	} {
		if _, err := svc.handle.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if _, err := svc.ArchiveIdentityWithContext(ctx); !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("identity before Ensure = %v, want ErrNotFound", err)
	}

	first, err := svc.EnsureArchiveIdentityWithContext(ctx, 1)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !utils.IsValidUUIDv7(first.ArchiveUUID) {
		t.Fatalf("archive uuid %q is not a UUIDv7", first.ArchiveUUID)
	}
	if first.DefaultLogbookID != 1 || first.CreatedAt.IsZero() {
		t.Fatalf("identity = %+v; want default logbook 1 and a creation time", first)
	}
	lbs, err := svc.FetchAllLogbooksWithContext(ctx)
	if err != nil || len(lbs) != 2 {
		t.Fatalf("logbooks: %v (%d)", err, len(lbs))
	}
	if !utils.IsValidUUIDv7(lbs[0].UUID) || !utils.IsValidUUIDv7(lbs[1].UUID) || lbs[0].UUID == lbs[1].UUID {
		t.Fatalf("logbook uuids after backfill = %q, %q; want two distinct UUIDv7", lbs[0].UUID, lbs[1].UUID)
	}

	second, err := svc.EnsureArchiveIdentityWithContext(ctx, 1)
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if second != first {
		t.Fatalf("second Ensure changed the identity: %+v vs %+v", second, first)
	}
	again, _ := svc.FetchAllLogbooksWithContext(ctx)
	if again[0].UUID != lbs[0].UUID || again[1].UUID != lbs[1].UUID {
		t.Fatal("second Ensure re-minted logbook uuids")
	}
	read, err := svc.ArchiveIdentityWithContext(ctx)
	if err != nil || read != first {
		t.Fatalf("ArchiveIdentity read-back = %+v (%v), want %+v", read, err, first)
	}
}

// A default logbook id that names no row is stored as NULL, never as a dangling
// pointer — the daemon's default-logbook self-heal runs before adoption and
// corrects config; the file must not refuse identity over it.
func TestEnsureArchiveIdentity_UnknownDefaultLogbookStoredAsNull(t *testing.T) {
	svc := testService(t)
	id, err := svc.EnsureArchiveIdentityWithContext(context.Background(), 7)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if id.DefaultLogbookID != 0 {
		t.Fatalf("default logbook = %d, want 0 (NULL) for a row that does not exist", id.DefaultLogbookID)
	}
}

// Runtime creates mint too (review finding 4): a logbook inserted after start
// carries a UUID immediately, so no row is ever born without one.
func TestInsertLogbook_MintsUUID(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	id, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Field Day", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	lb, err := svc.FetchLogbookByIDWithContext(ctx, id)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !utils.IsValidUUIDv7(lb.UUID) {
		t.Fatalf("inserted logbook uuid = %q, want a UUIDv7 minted on insert", lb.UUID)
	}
	// A client-supplied uuid is not honoured: identity is minted here, never accepted.
	id2, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Other", Callsign: "G4ABC", UUID: "019fd5c5-efcc-7193-be4f-1fee532ee315"})
	if err != nil {
		t.Fatalf("insert 2: %v", err)
	}
	lb2, _ := svc.FetchLogbookByIDWithContext(ctx, id2)
	if lb2.UUID == "019fd5c5-efcc-7193-be4f-1fee532ee315" || !utils.IsValidUUIDv7(lb2.UUID) {
		t.Fatalf("client-supplied uuid was stored (%q); the store must mint its own", lb2.UUID)
	}
}

// Renaming or editing a logbook must keep its identity.
func TestUpdateLogbook_KeepsUUID(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	id, _ := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "A", Callsign: "G4ABC"})
	before, _ := svc.FetchLogbookByIDWithContext(ctx, id)
	if err := svc.UpdateLogbookWithContext(ctx, types.Logbook{ID: id, Name: "B", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	name := "C"
	if _, err := svc.UpdateLogbookFieldsWithContext(ctx, id, &name, nil); err != nil {
		t.Fatalf("update fields: %v", err)
	}
	after, _ := svc.FetchLogbookByIDWithContext(ctx, id)
	if after.UUID != before.UUID || after.UUID == "" {
		t.Fatalf("uuid changed across updates: %q → %q", before.UUID, after.UUID)
	}
}

func TestSetArchiveDefaultLogbook(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, 1); err == nil {
		t.Fatal("set default before any identity exists; want an error")
	}
	id, _ := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "A", Callsign: "G4ABC"})
	if _, err := svc.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, id); err != nil {
		t.Fatalf("set default: %v", err)
	}
	got, _ := svc.ArchiveIdentityWithContext(ctx)
	if got.DefaultLogbookID != id {
		t.Fatalf("default logbook = %d, want %d", got.DefaultLogbookID, id)
	}
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, 999); err == nil {
		t.Fatal("a default logbook id that names no row was accepted")
	}
}
