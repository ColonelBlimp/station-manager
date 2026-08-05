package sqlite

/*
   A QSO ROW's coordinates may not contradict the gridsquare stored beside them.

   THE GAP (dogfood 2026-08-05). station_coords_test.go pins this rule for the
   contacted-station CACHE, and it holds there. It did not hold for the QSO
   ROW, because reconcileStationCoords had exactly one caller —
   writeContactedStation. A QSO is assembled from the enrichment RESULT, not
   read back from the reconciled cache row, so the two disagreed:

       US2YW, logged 2026-08-05 04:54 on the then-current build
         QSO row       grid KN28, lat 49.062500, lon 31.458333  ← outside KN28
         station cache grid KN28, lat 48.500000, lon 25.000000  ← reconciled

   THE QSO ROW IS THE ONE THAT LEAVES THE STATION. ADIF export reads it, and
   the forwarding worker re-reads it from the database by id
   (forwarding/worker/worker.go fetchQsoForAction) before submitting to QRZ,
   ClubLog and SM Cloud. So the half of the 2026-08-04 fix that guarded
   durable outbound data was the half that was missing; the map only looked
   correct because the SPA re-does the same arbitration client-side for
   DISPLAY (mapData.svelte.ts rowPoint), exactly as it did while the cache was
   rotting a day earlier. Same shape of defect, one layer along.

   WHY THE ADAPTER IS THE CHOKE POINT. Five paths write a QSO
   (InsertQsoWithContext, UpdateQsoWithContext, InsertQsoTx, UpdateQsoTx, and
   the manifest restore) and every one of them converts through
   adapters.QsoTypeToModel, which already normalises frequency and date/time
   at that same boundary. Reconciling at the five call sites instead would be
   five things to remember, and this defect exists precisely because one call
   site was the whole mechanism.

   THE RULE IS NOT A NEW ONE and must not become a second copy of it: the same
   ReconcileStationCoords runs here and for the cache, over the same four
   predicates in internal/utils. A separate QSO-side implementation is how the
   two layers would start disagreeing about what "contradicts" means — which
   is the fault the shared predicates were extracted to prevent.

   THE NEAREST CONFUSABLE STATES, inherited unchanged from the cache rules and
   re-pinned here because a choke point in a different package is a different
   piece of code:
     · Q2 vs Q1 — coordinates MORE PRECISE than the grid (must survive) versus
       coordinates that CONTRADICT it (must not). A fix that cannot tell these
       apart either flattens every station to a cell centre or fixes nothing.
     · Q1 vs Q3 — "replaced by the grid's position" versus "cleared". A blank
       row is not the fix; it is a second, quieter defect.
     · Q4 — a grid with NO coordinates. Deriving would invent a position
       nobody supplied.
     · Q6 — reconciling on UPDATE, not only on INSERT. The operator can edit a
       QSO's grid after the fact, which creates the contradiction all over
       again from a path that never touches enrichment.

   Q4 IS GUARDED TWICE, which matters if you ever try to prove it. Both the
   empty-pair early return and the readable-pair check independently stop a
   bare grid from gaining coordinates, so removing EITHER one leaves this file
   green and the rule looks like decoration. It is not — remove both and Q4
   fails. Noted because the reversion proof of one guard is otherwise quietly
   inconclusive, and reading it as "Q4 proves nothing" would invite deleting a
   rule that is doing real work.
*/

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The US2YW fixture, exactly as it was logged: a real on-air grid beside
// QRZ-profile coordinates ~500 km away, both plausible in isolation.
const (
	us2ywGrid = "KN28"
	us2ywLat  = "49.062500"
	us2ywLon  = "31.458333"
)

// storeAndFetch writes a QSO through the normal insert path and reads back what
// was actually persisted — the row a forwarder or an ADIF export would see.
func storeAndFetch(t *testing.T, svc *Service, q types.Qso) types.Qso {
	t.Helper()
	if _, err := svc.InsertQso(q); err != nil {
		t.Fatalf("insert qso %s: %v", q.ContactedStation.Call, err)
	}
	got, err := svc.FetchQsoByUUIDWithContext(context.Background(), q.UUID)
	if err != nil {
		t.Fatalf("fetch qso %s: %v", q.UUID, err)
	}
	return got
}

func qsoWithCoords(lbID int64, call, grid, lat, lon string) types.Qso {
	q := validTestQso(lbID, call, "40m", "FT8", "20260805", "0253")
	q.ContactedStation.Gridsquare = grid
	q.ContactedStation.Lat = lat
	q.ContactedStation.Lon = lon
	return q
}

func TestQsoCoords_Q1_CoordinatesContradictingTheGridAreReplaced(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "7Q5MLV"})

	got := storeAndFetch(t, svc, qsoWithCoords(lbID, "US2YW", us2ywGrid, us2ywLat, us2ywLon))

	if got.ContactedStation.Lat == us2ywLat || got.ContactedStation.Lon == us2ywLon {
		t.Fatalf("coordinates outside %s survived onto the QSO row: lat=%q lon=%q",
			us2ywGrid, got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
	// Derived, not blanked (Q1 vs Q3): the row must still carry a usable
	// position, because this is what an export and an upload send.
	if got.ContactedStation.Lat == "" || got.ContactedStation.Lon == "" {
		t.Fatalf("coordinates were cleared rather than derived: lat=%q lon=%q",
			got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
	if !utils.CoordsInsideGrid(us2ywGrid, got.ContactedStation.Lat, got.ContactedStation.Lon) {
		t.Fatalf("stored coordinates still fall outside %s: lat=%q lon=%q",
			us2ywGrid, got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
}

func TestQsoCoords_Q2_PreciseCoordinatesInsideTheCellSurvive(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "7Q5MLV"})

	// Davis Station (VK0DS, worked 2026-08-05): MC81 contains 68.569°S
	// 77.995°E and the coordinates are far finer than the locator. The cell
	// CENTRE is -68.5/77.0, so "kept" and "derived" produce different values
	// here — with a fixture sitting on the centre the rule would prove nothing.
	got := storeAndFetch(t, svc,
		qsoWithCoords(lbID, "VK0DS", "MC81", "-68.569050", "77.995117"))

	if got.ContactedStation.Lat != "-68.569050" || got.ContactedStation.Lon != "77.995117" {
		t.Fatalf("precise in-cell coordinates were altered: lat=%q lon=%q",
			got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
}

func TestQsoCoords_Q3_NoUsableGridLeavesCoordinatesAlone(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "7Q5MLV"})

	// Nothing can contradict them, and a plausibility rule would relocate a
	// station on a guess with nothing to show it had happened.
	got := storeAndFetch(t, svc, qsoWithCoords(lbID, "NOGRID", "", "12.345678", "-98.765432"))

	if got.ContactedStation.Lat != "12.345678" || got.ContactedStation.Lon != "-98.765432" {
		t.Fatalf("coordinates with no grid were altered: lat=%q lon=%q",
			got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
}

func TestQsoCoords_Q4_AGridWithNoCoordinatesGainsNone(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "7Q5MLV"})

	// Deriving here would manufacture a position nobody supplied. "No
	// position" and "position rejected" are different rows.
	got := storeAndFetch(t, svc, qsoWithCoords(lbID, "NOCOORD", "IO91", "", ""))

	if got.ContactedStation.Lat != "" || got.ContactedStation.Lon != "" {
		t.Fatalf("coordinates were invented from a bare grid: lat=%q lon=%q",
			got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
}

func TestQsoCoords_Q5_APlaceholderGridArbitratesNothing(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "7Q5MLV"})

	// AA00 coordinates agree with AA00 by construction, so the sentinel can
	// neither confirm nor contradict. Reconciliation must never be the thing
	// that removes a location (the cache's codex c3d99362 P1, restated).
	got := storeAndFetch(t, svc, qsoWithCoords(lbID, "SENTINEL", "AA00", "51.500000", "-0.100000"))

	if got.ContactedStation.Lat != "51.500000" || got.ContactedStation.Lon != "-0.100000" {
		t.Fatalf("a placeholder grid was treated as an arbiter: lat=%q lon=%q",
			got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
}

func TestQsoCoords_Q6_AnUpdateIsReconciledToo(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "7Q5MLV"})

	// Stored consistent, then EDITED into a contradiction — the operator
	// correcting a grid by hand, which never goes near enrichment. Guarding
	// only the insert would let the edit path reintroduce the whole defect.
	q := qsoWithCoords(lbID, "EDITED", "KN28", "48.500000", "25.000000")
	id, err := svc.InsertQso(q)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	q.ID = id // the update path addresses the row by id
	q.ContactedStation.Lat = us2ywLat
	q.ContactedStation.Lon = us2ywLon
	if err := svc.UpdateQso(q); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, ferr := svc.FetchQsoByUUIDWithContext(context.Background(), q.UUID)
	if ferr != nil {
		t.Fatalf("fetch: %v", ferr)
	}
	if !utils.CoordsInsideGrid("KN28", got.ContactedStation.Lat, got.ContactedStation.Lon) {
		t.Fatalf("an edit reintroduced contradictory coordinates: lat=%q lon=%q",
			got.ContactedStation.Lat, got.ContactedStation.Lon)
	}
}
