package sqlite

/*
   Coordinates may not contradict the grid they are stored beside.

   THE DEFECT (dogfood 2026-08-04, whole-logbook scan). Five QSO rows across
   four stations carried a correct gridsquare next to coordinates for somewhere
   else entirely — R9LAU logged as MO27 (Tyumen) with lat/lon -89.979167 /
   -179.958333, the centre of AA00AA, i.e. the South Pole. All five had already
   been uploaded to QRZ and ClubLog.

   WHAT REACHED QRZ, verified 2026-08-04 from the operator's own logbook page
   (QRZ entry serial 1483186312, the 2026-07-27 18.100 MHz FT8 QSO): QRZ shows
   "Coordinates 57.173356 N, 65.559720 E / Grid MO27" — the CORRECT Tyumen
   position, not the South Pole we sent. So the bad LAT/LON did not land there;
   QRZ either overrode it or never accepted it, and the page shows the outcome,
   not which. Recorded because the harm is otherwise assumed: the rows are NOT
   being repaired or re-uploaded (operator, 2026-08-04), and a future reader who
   inherits "we shipped wrong coordinates to QRZ" as established fact would
   propose exactly the re-upload that was declined. ClubLog is UNCHECKED.
   The same page shows OUR side as -11.437500 S / 34.041667 E, which is our
   MY_LAT of "S011 26.250" parsed correctly — the ADIF-format field survives the
   trip where the decimal one did not (see the separate LAT/LON format defect).

   HOW THE PAIR COMES APART, which is the part worth keeping: QRZ returns grid
   and lat/lon as INDEPENDENT fields and mergeContactedStation merges every
   field independently ("non-empty incoming wins"). So the sequence is not "QRZ
   sent nonsense" — it is:

       1. QRZ's record carries a placeholder grid AND its matching coordinates.
          They AGREE, so nothing looks wrong, and both are cached.
       2. A later lookup supplies the station's REAL grid — from the on-air FT8
          exchange, which is first-hand and correct.
       3. The merge replaces Gridsquare alone. The stale coordinates stay.

   Nothing in that sequence is individually wrong, which is why it survived: the
   defect only exists in the RELATIONSHIP between two fields that no code owned
   together. That is what these rules pin.

   OPERATOR'S RULING (2026-08-04): keep the coordinates when they AGREE with the
   grid, derive from the grid when they do not. Not "always derive" — a locator
   is a cell (~5×10 km at 6 chars) and QRZ's position is usually the station's
   actual one, so flattening every station to a cell centre would discard real
   precision to fix a rare contradiction.

   THE AGREEMENT TEST IS THE CELL ITSELF, not a chosen distance. A locator
   declares an extent; coordinates inside it are consistent with it by
   definition. This deliberately mirrors the SPA's rowPoint/agreesWithCell
   (frontend/app/src/lib/map/mapData.svelte.ts), which already applies exactly
   this rule for DISPLAY — the map has been hiding this defect since 2026-07-30
   while the stored data kept rotting. Same predicate on both sides, so the two
   layers cannot disagree about what "contradicts" means.

   THE NEAREST CONFUSABLE STATES:
     · R1 vs R2 — coordinates that are MORE PRECISE than the grid (the normal
       case, must survive untouched) versus coordinates that CONTRADICT it (must
       not). A rule that cannot tell these apart either loses precision on every
       station or fixes nothing.
     · R3/R4 — "no grid to check against" versus "grid says these are wrong".
       With no grid there is nothing to contradict the coordinates, and inventing
       a plausibility rule would relocate stations on a guess — a worse fault,
       because nothing would show it had happened.
     · R5 — a grid with NO coordinates. Deriving here would be inventing data
       that was never supplied; the row simply has no position. Distinct from
       "coordinates were rejected".
     · R6 — "these coordinates CONTRADICT the grid" versus "these coordinates
       are UNREADABLE here". The first is a comparison that happened and failed;
       the second is a comparison that never took place. Treating them alike
       silently relocated a station 3.57 km and destroyed the original value,
       while recording it as a contradiction — the sharpest confusable pair in
       this file, and the one that shipped wrong for an hour.
*/

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The R9LAU fixture: AA00AA's centre, which is what QRZ actually supplied.
const (
	polarLat = "-89.979167"
	polarLon = "-179.958333"
)

func upsertAndFetch(t *testing.T, svc *Service, st types.ContactedStation) types.ContactedStation {
	t.Helper()
	if err := svc.UpsertContactedStationWithContext(context.Background(), st); err != nil {
		t.Fatalf("upsert %s: %v", st.Call, err)
	}
	got, err := svc.FetchContactedStationByCallsign(st.Call)
	if err != nil {
		t.Fatalf("fetch %s: %v", st.Call, err)
	}
	return got
}

func TestStationCoords_R1_PreciseCoordinatesInsideTheCellSurvive(t *testing.T) {
	svc := testService(t)
	// Thessaloniki: KN10 contains 40.6°N 22.9°E, and the coordinates are far
	// more precise than the locator. Flattening them would be a real loss.
	got := upsertAndFetch(t, svc, types.ContactedStation{
		Call: "SV2CWV", Gridsquare: "KN10", Lat: "40.609066", Lon: "22.968657",
	})
	if got.Lat != "40.609066" || got.Lon != "22.968657" {
		t.Fatalf("precise in-cell coordinates were altered: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

func TestStationCoords_R2_CoordinatesContradictingTheGridAreReplaced(t *testing.T) {
	svc := testService(t)
	// The defect, exactly as logged: a correct grid beside the South Pole.
	got := upsertAndFetch(t, svc, types.ContactedStation{
		Call: "R9LAU", Gridsquare: "MO27", Lat: polarLat, Lon: polarLon,
	})
	if got.Lat == polarLat || got.Lon == polarLon {
		t.Fatalf("polar coordinates survived beside grid MO27: lat=%q lon=%q", got.Lat, got.Lon)
	}
	// Replaced by the grid's own position, not merely blanked — the row still
	// has a usable location, which is what the map and any export need.
	if got.Lat == "" || got.Lon == "" {
		t.Fatalf("coordinates were cleared rather than derived: lat=%q lon=%q", got.Lat, got.Lon)
	}
	assertInsideGrid(t, got.Gridsquare, got.Lat, got.Lon)
}

func TestStationCoords_R3_TheMergeItselfCannotSeparateThePair(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// Uses a PLAUSIBLE wrong grid, not AA00. This rule originally used the
	// placeholder and asserted it was stored as given — correct until the
	// sentinel rule (R7) landed, which now rejects it outright. The reversal is
	// recorded rather than quietly deleted: the SEQUENCE this pins is not about
	// placeholders at all, and it stays reachable for any upstream grid that is
	// merely WRONG rather than a known sentinel.
	//
	// JJ00aa is a real locator near 0N 0E with coordinates to match — self
	// consistent, so it is cached as given.
	first := upsertAndFetch(t, svc, types.ContactedStation{
		Call: "R9LAU", Gridsquare: "JJ00aa", Lat: "0.020833", Lon: "0.041667",
	})
	if first.Lat != "0.020833" {
		t.Fatalf("a self-consistent non-sentinel pair was altered: lat=%q", first.Lat)
	}

	// Then the on-air grid arrives ALONE, as it does from an FT8 exchange. The
	// merge replaces Gridsquare and leaves the coordinates — this is the step
	// that produced every one of the five bad rows.
	if err := svc.UpsertContactedStationWithContext(ctx, types.ContactedStation{
		Call: "R9LAU", Gridsquare: "MO27",
	}); err != nil {
		t.Fatalf("grid-only merge: %v", err)
	}
	got, err := svc.FetchContactedStationByCallsign("R9LAU")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Gridsquare != "MO27" {
		t.Fatalf("the on-air grid must win: %q", got.Gridsquare)
	}
	if got.Lat == "0.020833" || got.Lon == "0.041667" {
		t.Fatalf("stale coordinates survived the grid update: lat=%q lon=%q", got.Lat, got.Lon)
	}
	assertInsideGrid(t, "MO27", got.Lat, got.Lon)
}

func TestStationCoords_R4_WithNoGridTheCoordinatesStand(t *testing.T) {
	svc := testService(t)
	// Nothing contradicts them. Rejecting on plausibility would relocate a
	// station on a guess, and nothing would show it had happened.
	got := upsertAndFetch(t, svc, types.ContactedStation{
		Call: "TEST1", Lat: polarLat, Lon: polarLon,
	})
	if got.Lat != polarLat || got.Lon != polarLon {
		t.Fatalf("coordinates with no grid to check against were altered: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

func TestStationCoords_R5_AGridWithNoCoordinatesInventsNone(t *testing.T) {
	svc := testService(t)
	got := upsertAndFetch(t, svc, types.ContactedStation{Call: "TEST2", Gridsquare: "IO91"})
	if got.Lat != "" || got.Lon != "" {
		t.Fatalf("coordinates were invented from a grid: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

func TestStationCoords_R6_UnreadableCoordinatesAreLeftAsSupplied(t *testing.T) {
	// REVISED 2026-08-04. This rule originally read "unparseable coordinates
	// defer to the grid" and overwrote them with the cell centre. That conflated
	// two different facts: coordinates that CONTRADICT the grid (we compared them
	// and they lost) and coordinates we simply CANNOT READ (we never compared
	// anything). Only the first is evidence of anything.
	//
	// Measured cost of the conflation: a value in ADIF Location form for a
	// station inside its own grid was replaced by the cell centre — 3.57 km off,
	// silently, and recorded internally as "contradicts the grid" when it agreed
	// perfectly. The original was destroyed in the process.
	//
	// Leaving it alone costs nothing on screen: the map's parseFloat rejects the
	// string either way and falls back to the same cell centre for DISPLAY. The
	// difference is that the supplied value survives in storage instead of being
	// overwritten by a derived one.
	svc := testService(t)
	got := upsertAndFetch(t, svc, types.ContactedStation{
		Call: "TEST3", Gridsquare: "IO91", Lat: "N051 30.000", Lon: "W000 07.000",
	})
	if got.Lat != "N051 30.000" || got.Lon != "W000 07.000" {
		t.Fatalf("unreadable coordinates were overwritten: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

func TestStationCoords_R6b_HalfAPairIsNotAPosition(t *testing.T) {
	// One readable value and one not: still nothing to compare, so still nothing
	// to judge. Guards the parse check against being written per-field.
	svc := testService(t)
	got := upsertAndFetch(t, svc, types.ContactedStation{
		Call: "TEST4", Gridsquare: "IO91", Lat: "51.500000", Lon: "not a number",
	})
	if got.Lat != "51.500000" || got.Lon != "not a number" {
		t.Fatalf("a half-readable pair was rewritten: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

func assertInsideGrid(t *testing.T, grid, lat, lon string) {
	t.Helper()
	if !coordsInsideGrid(grid, lat, lon) {
		t.Fatalf("coordinates %q,%q are not inside grid %s", lat, lon, grid)
	}
}

/*
   R7/R8 — a known-placeholder locator is NOT a location.

   ROOT CAUSE, confirmed 2026-08-04 from R9LAU's QRZ profile page
   (www.qrz.com/db/R9LAU): "Grid Square AA00aa / Geo Source: From Grid". The
   station never set a grid, QRZ derives coordinates FROM that placeholder, and
   the pair is therefore self-consistent by construction — which is precisely
   why R1's agreement rule keeps it. The mismatch rule only rescues the row once
   a real grid arrives from the air; a station worked on a mode that carries no
   grid would be stored, plotted and EXPORTED at the South Pole.

   So the sentinel has to be rejected on its own, before agreement is consulted.
   Operator's framing (2026-08-04): "protect ourselves against users who don't
   know what they are doing 'AA00aa' or deliberately add false data".

   THE LIMIT, stated rather than implied: this catches self-contradiction and
   ONE known sentinel. A plausible-but-false grid — a station who types a
   neighbouring square, or lies convincingly — is indistinguishable from truth
   with the data we hold, and no rule here should pretend otherwise. AA00 is
   the only locator treated as a sentinel because it is the only one there is
   EVIDENCE for; adding others on suspicion would start relocating real
   stations, and an Antarctic operator genuinely in AA00 is a cost knowingly
   accepted (they are vanishingly rare, and the on-air grid still corrects them).
*/

func TestStationCoords_R7_ThePlaceholderGridIsNoLocationAtAll(t *testing.T) {
	svc := testService(t)
	// Exactly what a cold QRZ lookup of R9LAU returns: the sentinel grid and the
	// coordinates QRZ derived from it. They AGREE, so nothing but the sentinel
	// itself can reject them.
	got := upsertAndFetch(t, svc, types.ContactedStation{
		Call: "R9LAU", Gridsquare: "AA00aa", Lat: polarLat, Lon: polarLon,
	})
	if got.Lat != "" || got.Lon != "" {
		t.Fatalf("placeholder-derived coordinates were stored: lat=%q lon=%q", got.Lat, got.Lon)
	}
	if got.Gridsquare != "" {
		t.Fatalf("the placeholder grid was stored as a location: %q", got.Gridsquare)
	}
}

func TestStationCoords_R8_ARealGridStillArrivesAndWins(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	// Rejecting the sentinel must not block the correction that follows it.
	if err := svc.UpsertContactedStationWithContext(ctx, types.ContactedStation{
		Call: "R9LAU", Gridsquare: "AA00AA", Lat: polarLat, Lon: polarLon,
	}); err != nil {
		t.Fatalf("cold placeholder insert: %v", err)
	}
	got := upsertAndFetch(t, svc, types.ContactedStation{Call: "R9LAU", Gridsquare: "MO27"})
	if got.Gridsquare != "MO27" {
		t.Fatalf("the on-air grid must still win: %q", got.Gridsquare)
	}
}
