package utils

import (
	"math"
	"testing"
)

// These vectors mirror the SPA's frontend/logging/src/lib/utils/bearing.test.ts
// so the daemon-side QSO geometry (FT8 ANT_AZ / DISTANCE / ANT_PATH) matches what
// the SPA computes for the same grids.

func TestInitialBearing_Cardinals(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		want                   float64
	}{
		{"north", 0, 0, 10, 0, 0},
		{"east", 0, 0, 0, 10, 90},
		{"south", 10, 0, 0, 0, 180},
		{"west", 0, 10, 0, 0, 270},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := initialBearing(c.lat1, c.lon1, c.lat2, c.lon2)
			if math.Abs(got-c.want) > 1.0 {
				t.Fatalf("bearing = %v, want ~%v", got, c.want)
			}
		})
	}
}

func TestInitialBearing_LondonToNY(t *testing.T) {
	// London → New York, ~288.3° per v1's spherical model.
	got := initialBearing(51.5074, -0.1278, 40.7128, -74.006)
	if math.Abs(got-288.3) > 1.0 {
		t.Fatalf("London→NY bearing = %v, want ~288.3", got)
	}
}

func TestHaversineKm(t *testing.T) {
	if d := haversineKm(51.5, -1, 51.5, -1); d != 0 {
		t.Fatalf("identical points = %v, want 0", d)
	}
	// London → Paris ~344 km (ceil rounding; ±2 km for endpoint precision).
	if d := haversineKm(51.5074, -0.1278, 48.8566, 2.3522); d < 342 || d > 346 {
		t.Fatalf("London→Paris = %v, want 342..346", d)
	}
}

func TestGridPath_InvalidGrids(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"ZZ99", "JN58td"}, {"JN58td", "ZZ99"}, {"", "JN58td"}, {"JN58td", ""},
	} {
		if _, ok := GridPath(c.a, c.b); ok {
			t.Fatalf("GridPath(%q,%q) ok=true, want false", c.a, c.b)
		}
	}
}

func TestGridPath_ShortLongRelationship(t *testing.T) {
	geo, ok := GridPath("JN58td", "FN31pr")
	if !ok {
		t.Fatal("GridPath ok=false, want true")
	}
	// Short + long bearings are 180° apart.
	if d := math.Abs(math.Mod(geo.ShortBearingDeg+180, 360) - geo.LongBearingDeg); d > 0.2 {
		t.Fatalf("long bearing %v not opposite short %v", geo.LongBearingDeg, geo.ShortBearingDeg)
	}
	// Short + long distance ≈ Earth circumference.
	circ := math.Ceil(2 * math.Pi * earthRadiusKm)
	if sum := geo.ShortDistanceKm + geo.LongDistanceKm; math.Abs(sum-circ) > 2 {
		t.Fatalf("short+long = %v, want ~%v", sum, circ)
	}
	// Bearings rounded to 0.1°.
	if geo.ShortBearingDeg*10 != math.Round(geo.ShortBearingDeg*10) {
		t.Fatalf("short bearing %v not rounded to 0.1°", geo.ShortBearingDeg)
	}
}
