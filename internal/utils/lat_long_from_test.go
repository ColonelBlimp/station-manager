package utils

import "testing"

/*
   ConvertFromXDDDMMM — the inverse of ConvertToXDDDMMM.

   WHY IT EXISTS (2026-08-04). ADIF carries LAT/LON as the Location type
   ("XDDD MM.MMM"); SM stores them as decimal degrees, because that is what QRZ
   returns and what the SPA map parses. Export converts one way. Without this,
   IMPORT stores the ADIF string verbatim, so a row that has been through an
   ADIF round trip is no longer plottable — the map's parseFloat rejects it and
   silently falls back to the grid's cell centre.

   "Asymmetric round-trips are a clue" (CLAUDE.md): one elegant direction beside
   a missing one is the pair telling you it is half-built.
*/

func TestConvertFromXDDDMMM_RoundTripsWithItsInverse(t *testing.T) {
	// The property that matters: a value survives out-and-back. Uses both
	// hemispheres on each axis, since a sign-vs-letter slip only shows there.
	for _, tc := range []struct {
		in    string
		isLat bool
	}{
		{"40.609066", true}, {"-11.437500", true},
		{"22.968657", false}, {"-96.205750", false},
		{"0.000000", true}, {"0.000000", false},
	} {
		adif, err := ConvertToXDDDMMM(tc.in, tc.isLat)
		if err != nil {
			t.Fatalf("to ADIF %q: %v", tc.in, err)
		}
		back, err := ConvertFromXDDDMMM(adif)
		if err != nil {
			t.Fatalf("from ADIF %q: %v", adif, err)
		}
		// ADIF Location carries 3 decimal minutes ~ 1.85 m, so the round trip is
		// lossy in the last decimal place. Assert to the precision the format
		// actually holds rather than pretending it is exact.
		want, _ := ConvertToXDDDMMM(back, tc.isLat)
		if want != adif {
			t.Fatalf("round trip moved %q: %q -> %q -> %q (re-encodes as %q)",
				tc.in, tc.in, adif, back, want)
		}
	}
}

func TestConvertFromXDDDMMM_RejectsWhatIsNotALocation(t *testing.T) {
	// Decimal input must be REFUSED, not silently mangled: the caller uses the
	// error to mean "leave this value alone", which is what keeps an import of a
	// decimal-bearing ADIF file (SM's own, before 2026-08-04) unchanged.
	for _, in := range []string{"", "40.609066", "N040 36.5441", "X040 36.544", "not a location"} {
		if _, err := ConvertFromXDDDMMM(in); err == nil {
			t.Fatalf("accepted a non-Location value: %q", in)
		}
	}
}

func TestConvertFromXDDDMMM_HemisphereDecidesTheSign(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"N040 36.544", "40.609067"},
		{"S011 26.250", "-11.437500"},
		{"E022 58.119", "22.968650"},
		{"W096 12.345", "-96.205750"},
	} {
		got, err := ConvertFromXDDDMMM(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
}
