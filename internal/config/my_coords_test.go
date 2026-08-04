package config

/*
   The operator's own position: the grid SUGGESTS it, the operator may refine
   it, and the grid arbitrates whether the refinement is credible.

   WHAT THIS REPLACES (operator's ruling, 2026-08-04). Normalize used to derive
   MyLat/MyLon from MyGridsquare unconditionally, in ADIF Location form. Two
   consequences, both wrong:

     1. It was the only place in the system storing a WIRE format. Everything
        else — QRZ's values, the map, bearing, distance, the contacted-station
        reconcile — speaks decimal degrees. MY_LAT was the exception that made
        the canonical-form rule untrue.
     2. Deriving unconditionally means a value the operator sets is destroyed at
        the next startup. Their own station is the ONE position in the whole
        system that could be known exactly, and a 6-character locator is a
        ~5x10 km cell (a 4-character one, ~110 km). Overwriting with the cell
        centre throws away the only precise position available to us.

   SO THE RULE IS THE ONE THE CONTACTED STATION ALREADY USES: the grid decides
   whether coordinates are credible, it does not supply them. Absent, they are
   derived as a starting point; present, they are kept while the grid vouches
   for them.

   WITH ONE DELIBERATE DIFFERENCE, and it is the interesting half. For a
   CONTACTED station, coordinates that contradict the grid are silently replaced
   — there is nobody to ask, and the grid is the better evidence. For the
   OPERATOR'S OWN station a human typed the value and is standing right there,
   so a contradiction is REFUSED with an explanation (M3) rather than corrected
   behind their back. Silently relocating an operator to their own cell centre
   would be the worst of both: input ignored, no indication why.

   THE NEAREST CONFUSABLE STATES:
     · M1 vs M2 — "no position yet, here is one from your grid" versus "you gave
       a better one, keep it". A rule that cannot tell these apart either never
       suggests anything or flattens every refinement.
     · M3 vs M2 — a refinement WITHIN the cell versus a position that contradicts
       it. Only the second is evidence of a mistake.
     · M4 — coordinates with no grid to check them against. Nothing contradicts
       them, so nothing may reject them; the alternative is inventing a
       plausibility rule and relocating a station on a guess.

   A RULE THAT WAS DROPPED, and why. An earlier draft asserted that clearing the
   grid also cleared a position DERIVED from it, while M4 keeps a position the
   operator SET. Both are "no grid, coordinates present" — telling them apart
   needs provenance the system does not carry, and inventing a marker for it is
   exactly the trap CLAUDE.md names ("if a behaviour test cannot be written
   without inventing a fact the system does not carry, the SYSTEM is missing that
   fact"). So the rule is uniform: with no grid, coordinates stand. A derived
   value outliving its grid is the accepted cost — it was a real position, and
   the case that matters (moving to a NEW grid) is caught by M3.
*/

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func normalizedStation(ls types.LoggingStation) types.LoggingStation {
	cfg := &Config{}
	cfg.LoggingStation = ls
	Normalize(cfg)
	return cfg.LoggingStation
}

func TestMyCoords_M1_AGridSuggestsAPositionInDecimal(t *testing.T) {
	got := normalizedStation(types.LoggingStation{MyGridsquare: "KH78an"})
	if got.MyLat == "" || got.MyLon == "" {
		t.Fatalf("the grid suggested no position: lat=%q lon=%q", got.MyLat, got.MyLon)
	}
	if strings.ContainsAny(got.MyLat, "NSEW") {
		t.Fatalf("MY_LAT was stored in the ADIF wire format, not decimal: %q", got.MyLat)
	}
	if got.MyLat != "-11.437500" || got.MyLon != "34.041667" {
		t.Fatalf("KH78an's centre is wrong: lat=%q lon=%q", got.MyLat, got.MyLon)
	}
}

func TestMyCoords_M2_ARefinementInsideTheCellSurvivesNormalize(t *testing.T) {
	// The operator's actual position, more precise than the locator. Running
	// Normalize twice models a restart — the value must survive both.
	in := types.LoggingStation{MyGridsquare: "KH78an", MyLat: "-11.443917", MyLon: "34.009600"}
	got := normalizedStation(normalizedStation(in))
	if got.MyLat != "-11.443917" || got.MyLon != "34.009600" {
		t.Fatalf("a refinement was overwritten by the cell centre: lat=%q lon=%q", got.MyLat, got.MyLon)
	}
}

func TestMyCoords_M3_APositionOutsideTheCellIsRefusedNotCorrected(t *testing.T) {
	cfg := &Config{}
	cfg.LoggingStation = types.LoggingStation{
		MyGridsquare: "KH78an", MyLat: "51.500000", MyLon: "-0.116667", // London, not Mzuzu
	}
	Normalize(cfg)

	// Not silently corrected — the operator typed it and must be told.
	if cfg.LoggingStation.MyLat != "51.500000" {
		t.Fatalf("an operator's own value was silently rewritten: %q", cfg.LoggingStation.MyLat)
	}
	errs := Validate(*cfg)
	if !mentions(errs, "my_lat") {
		t.Fatalf("a position outside the declared grid was accepted: %v", errs)
	}
}

func TestMyCoords_M4_WithNoGridTheCoordinatesStand(t *testing.T) {
	got := normalizedStation(types.LoggingStation{MyLat: "51.500000", MyLon: "-0.116667"})
	if got.MyLat != "51.500000" || got.MyLon != "-0.116667" {
		t.Fatalf("coordinates with nothing to contradict them were altered: lat=%q lon=%q",
			got.MyLat, got.MyLon)
	}
}

func TestMyCoords_M5_NothingIsInventedFromNothing(t *testing.T) {
	got := normalizedStation(types.LoggingStation{})
	if got.MyLat != "" || got.MyLon != "" {
		t.Fatalf("a position was invented with no grid and no input: lat=%q lon=%q",
			got.MyLat, got.MyLon)
	}
}

func mentions(errs []Finding, field string) bool {
	for _, e := range errs {
		if strings.Contains(e.Field, field) {
			return true
		}
	}
	return false
}

func TestMyCoords_M7_AnExistingAdifFormatConfigStillStarts(t *testing.T) {
	// THE MIGRATION. Every config.json written before 2026-08-04 holds MY_LAT in
	// ADIF Location form, because that is what Normalize used to derive. With the
	// new validation and no conversion, such a file is REFUSED — turning a format
	// change into a daemon that will not boot on an existing install. Caught by
	// an existing api test going 400, not by this file, which is why it is here
	// now: the rule is "a format change must never brick a running station".
	cfg := &Config{}
	cfg.LoggingStation = types.LoggingStation{
		MyGridsquare: "KH78an", MyLat: "S011 26.250", MyLon: "E034 02.500",
	}
	Normalize(cfg)
	if cfg.LoggingStation.MyLat != "-11.437500" {
		t.Fatalf("a legacy ADIF value was not migrated: %q", cfg.LoggingStation.MyLat)
	}
	if errs := Validate(*cfg); mentions(errs, "my_lat") {
		t.Fatalf("an existing install would fail to start: %v", errs)
	}
}

func TestMyCoords_M8_ImpossibleCoordinatesAreRefusedEvenWithNoGrid(t *testing.T) {
	// codex fbaafe73 P1. Validation used CoordsReadable, which asks only whether
	// ParseFloat succeeded — and ParseFloat happily returns NaN, ±Inf and any
	// magnitude. With no grid there is no cell check either, so latitude 91 and
	// NaN were accepted and persisted.
	//
	// The inconsistency is the tell: the SAME commit bound-checked coordinates at
	// the provider ingress and not at this one. A boundary that promises a
	// canonical value has to enforce the same thing wherever values enter, or the
	// promise is only true for whichever door the author was looking at.
	for _, tc := range []struct{ lat, lon string }{
		{"91.0", "10.0"},
		{"10.0", "181.0"},
		{"NaN", "10.0"},
		{"10.0", "Inf"},
		{"-90.5", "10.0"},
	} {
		cfg := &Config{}
		cfg.LoggingStation = types.LoggingStation{MyLat: tc.lat, MyLon: tc.lon}
		Normalize(cfg)
		if !mentions(Validate(*cfg), "my_lat") {
			t.Fatalf("accepted an impossible position (%s,%s)", tc.lat, tc.lon)
		}
	}
}
