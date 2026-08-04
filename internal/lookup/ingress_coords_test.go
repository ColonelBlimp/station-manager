package lookup_test

/*
   Coordinates are normalised to the canonical form at the PROVIDER boundary.

   THE DECISION (operator, 2026-08-04): decimal degrees is the canonical
   internal representation; every perimeter converts to it on the way in and
   away from it on the way out. This is the INGRESS half for callsign
   providers. The ADIF perimeter already does its half (adif.QsoToRecord /
   RecordToQso); SM Cloud needs none, since it mirrors the canonical form.

   WHAT THIS REPLACES. lookup.go's FilterToCallsignFields carried the opposite
   rule in a comment — "Lat / Lon / Altitude pass through" — so whatever a
   provider sent was stored verbatim. That worked only because QRZ sends
   decimals, which is luck rather than design: we could not establish HamCall's
   format at all (site unreachable, 2026-08-04), and a provider can change
   format without telling us. The perimeter makes the question stop mattering.

   WHY HERE AND NOT IN EACH PROVIDER. The orchestrator already narrows every
   CallsignProvider result at this seam "so provider authors don't have to
   remember to clear the fields manually" (FilterToCallsignFields' own words).
   Format normalisation is the same kind of obligation, so it belongs in the
   same place: a provider added tomorrow inherits it without writing any code,
   which is the property I5 pins with a stub provider that is not QRZ.

   WHY ONLY FORMAT, AND NOT THE GRID ARBITRATION. "Gridsquare is king" cannot
   be enforced here. The contradiction that motivated it arrives ACROSS two
   writes from two different sources — QRZ's placeholder grid in one, the real
   grid from the on-air FT8 exchange in another, which never passes through a
   provider at all. No ingress adapter can see both, so arbitration stays at the
   storage merge (reconcileStationCoords) where the merged record exists. Two
   jobs, two layers, deliberately.

   THE NEAREST CONFUSABLE STATES:
     · I3 vs I4 — coordinates we could not READ versus coordinates never
       SUPPLIED. Both end with no position stored, but only the first is a
       provider sending something we did not understand, and it is logged. A
       silent drop here would look exactly like a provider that sends no
       coordinates, which is the normal case for most of them.
     · I3 vs the storage layer's R6 — ingress DROPS what it cannot read;
       reconcileStationCoords LEAVES such a value alone. Not a contradiction:
       ingress decides what is allowed to become ours, and the canonical form is
       the whole point of the boundary. R6 governs a value that is ALREADY ours,
       where overwriting destroys the only copy.
*/

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// enrichWith runs a cold lookup through an arbitrary provider and returns the
// station the orchestrator produced.
func enrichWith(t *testing.T, st types.ContactedStation) types.ContactedStation {
	t.Helper()
	db := newTestSqlite(t)
	// Deliberately NOT named "qrz": the rule is about any provider.
	p := &stubCallsignProvider{name: "somefutureprovider", result: st}
	o := &lookup.Orchestrator{
		DB:         db,
		Chain:      []lookup.CallsignProvider{p},
		CountryTTL: time.Hour,
		StationTTL: time.Hour,
		Refresher:  &syncRefresher{},
	}
	got := o.Enrich(context.Background(), st.Call)
	if p.calls == 0 {
		t.Fatal("fixture never reached the provider, so it proves nothing")
	}
	return got.Station
}

func TestIngressCoords_I1_AdifLocationBecomesDecimal(t *testing.T) {
	got := enrichWith(t, types.ContactedStation{
		Call: "7Q5MLV", Gridsquare: "KH78AN", Lat: "S011 26.635", Lon: "E034 00.576",
	})
	if got.Lat != "-11.443917" {
		t.Fatalf("LAT was not normalised to decimal: %q", got.Lat)
	}
	if got.Lon != "34.009600" {
		t.Fatalf("LON was not normalised to decimal: %q", got.Lon)
	}
}

func TestIngressCoords_I2_DecimalIsAlreadyCanonicalAndUnchanged(t *testing.T) {
	got := enrichWith(t, types.ContactedStation{
		Call: "SV2CWV", Gridsquare: "KN10", Lat: "40.609066", Lon: "22.968657",
	})
	if got.Lat != "40.609066" || got.Lon != "22.968657" {
		t.Fatalf("canonical coordinates were altered: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

func TestIngressCoords_I3_UnreadableCoordinatesDoNotEnter(t *testing.T) {
	// The boundary's job is to guarantee the canonical form. A value we cannot
	// interpret is not admitted as though it were decimal degrees — everything
	// downstream (map, bearing, distance, the cell-agreement test) would then be
	// parsing something arbitrary.
	got := enrichWith(t, types.ContactedStation{
		Call: "G0ABC", Gridsquare: "IO91", Lat: "somewhere near the pub", Lon: "??",
	})
	if got.Lat != "" || got.Lon != "" {
		t.Fatalf("an uninterpretable coordinate was admitted: lat=%q lon=%q", got.Lat, got.Lon)
	}
	// The grid is untouched — dropping a coordinate must not cost the position
	// we can still express.
	if got.Gridsquare != "IO91" {
		t.Fatalf("the grid was collateral damage: %q", got.Gridsquare)
	}
}

func TestIngressCoords_I4_NoCoordinatesStaysNoCoordinates(t *testing.T) {
	got := enrichWith(t, types.ContactedStation{Call: "G0ABC", Gridsquare: "IO91", Name: "Bob"})
	if got.Lat != "" || got.Lon != "" {
		t.Fatalf("coordinates were invented: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

func TestIngressCoords_I5_HalfAPairIsNotAPosition(t *testing.T) {
	// A readable latitude beside an unreadable longitude is not half a position;
	// it is no position. Guards the check against being applied per-field, which
	// would admit a lone coordinate the map would plot on the prime meridian.
	got := enrichWith(t, types.ContactedStation{
		Call: "G0ABC", Gridsquare: "IO91", Lat: "51.500000", Lon: "not a number",
	})
	if got.Lat != "" || got.Lon != "" {
		t.Fatalf("half a pair was admitted: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

/*
   I6/I7 — "parses" is not "is a coordinate", and a hemisphere belongs to an axis.

   codex fd3062b7, both P1. canonicalCoord treated any successful ParseFloat as
   canonical, so NaN, ±Inf, latitude 91 and longitude 181 entered as though they
   were positions — and then ADIF export, which DOES validate, emitted nothing,
   while the map and distance maths consumed the garbage. And because the ADIF
   branch called an axis-agnostic parser, "E022 58.119" was accepted as a
   LATITUDE.

   The boundary that promises a canonical value is the one that has to enforce
   what makes it canonical. Anything less makes the promise the interior relies
   on untrue in exactly the cases that matter.
*/

func TestIngressCoords_I6_NonFiniteAndOutOfRangeDecimalsAreRefused(t *testing.T) {
	// A DISTINCT callsign per case: enrichWith seeds the cache, so reusing one
	// makes every iteration after the first a cache hit that never reaches the
	// provider — the helper's own guard caught exactly that.
	for _, tc := range []struct{ call, lat, lon string }{
		{"G0AA1", "NaN", "10.0"},
		{"G0AA2", "10.0", "Inf"},
		{"G0AA3", "91.0", "10.0"},  // past the pole
		{"G0AA4", "10.0", "181.0"}, // past the antimeridian
		{"G0AA5", "-90.5", "10.0"},
	} {
		got := enrichWith(t, types.ContactedStation{
			Call: tc.call, Gridsquare: "IO91", Lat: tc.lat, Lon: tc.lon,
		})
		if got.Lat != "" || got.Lon != "" {
			t.Fatalf("admitted a non-position (%s,%s): lat=%q lon=%q",
				tc.lat, tc.lon, got.Lat, got.Lon)
		}
	}
}

func TestIngressCoords_I7_AHemisphereFromTheWrongAxisIsRefused(t *testing.T) {
	got := enrichWith(t, types.ContactedStation{
		Call: "G0ABC", Gridsquare: "IO91", Lat: "E022 58.119", Lon: "N040 36.544",
	})
	if got.Lat != "" || got.Lon != "" {
		t.Fatalf("axis-swapped hemispheres were admitted: lat=%q lon=%q", got.Lat, got.Lon)
	}
}

/*
   I8 — the placeholder grid is rejected HERE, not at the merge.

   codex c3d99362 P1, verified before fixing: a station cached with a real grid
   and precise coordinates, refreshed by a provider whose record carries AA00aa
   and no coordinates, came back as grid="" lat="" lon="". The merge kept the
   old coordinates (incoming were empty) and replaced the grid, and the sentinel
   then cleared all three — a placeholder meaning "I have no location for this
   station" DESTROYED a location we already had. R9LAU's QRZ profile carries
   exactly that grid, so this was reachable on any refresh.

   The placement was the error, and it is the same distinction this file already
   draws: a placeholder is a property of ONE INPUT — "this provider supplied no
   location" — not of the merged record. Rejected at ingress it never reaches
   the merge, and the merge keeps what it already knew.
*/

func TestIngressCoords_I8_APlaceholderGridSuppliesNoLocation(t *testing.T) {
	got := enrichWith(t, types.ContactedStation{
		Call: "R9LAU", Gridsquare: "AA00aa", Lat: "-89.979167", Lon: "-179.958333",
	})
	if got.Gridsquare != "" {
		t.Fatalf("the sentinel grid entered: %q", got.Gridsquare)
	}
	if got.Lat != "" || got.Lon != "" {
		t.Fatalf("coordinates derived from the sentinel entered: lat=%q lon=%q", got.Lat, got.Lon)
	}
}
