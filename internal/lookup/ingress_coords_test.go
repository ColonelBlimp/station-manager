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
