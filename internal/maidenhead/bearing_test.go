package maidenhead

import (
	"math"
	"strings"
	"testing"
)

// helper for approx equality
func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestNormalizeAndValidate_OK_MixedCase(t *testing.T) {
	// Mixed case should be accepted and normalized internally
	lat1, err := LatitudeFromGridSquare("JN58TD")
	if err != nil {
		t.Fatalf("LatitudeFromGridSquare error: %v", err)
	}
	lon1, err := LongitudeFromGridSquare("JN58TD")
	if err != nil {
		t.Fatalf("LongitudeFromGridSquare error: %v", err)
	}

	lat2, err := LatitudeFromGridSquare("jn58td")
	if err != nil {
		t.Fatalf("LatitudeFromGridSquare error: %v", err)
	}
	lon2, err := LongitudeFromGridSquare("jn58td")
	if err != nil {
		t.Fatalf("LongitudeFromGridSquare error: %v", err)
	}

	if !almostEqual(lat1, lat2, 1e-9) || !almostEqual(lon1, lon2, 1e-9) {
		t.Fatalf("expected same coords for different cases: (%.6f,%.6f) vs (%.6f,%.6f)", lat1, lon1, lat2, lon2)
	}
}

func TestNormalizeGridSquare_Lengths(t *testing.T) {
	// length != 6 should be returned unchanged
	cases := []string{"J", "JN58T", "JN58TDX", ""}
	for _, in := range cases {
		if out := normalizeGridSquare(in); out != in {
			t.Errorf("normalizeGridSquare(%q) = %q, want unchanged", in, out)
		}
	}

	// proper 6-char should be normalized
	if got := normalizeGridSquare("jN58Td"); got != "JN58td" {
		t.Errorf("normalizeGridSquare mixed case got %q want %q", got, "JN58td")
	}
}

func TestLatitudeLongitude_KnownValues(t *testing.T) {
	// Some representative locators and expected center coordinates computed per algorithm
	// Expectations checked against independent calculators and this package's formulas.
	tests := []struct {
		grid   string
		expLat float64
		expLon float64
	}{
		// Center of subsquare as computed by current implementation
		{"AA00aa", -89.9791667, -179.9583333},
		{"JJ00aa", 0.0208333, 0.0416667},
		{"JN58td", 48.1458333, 11.6250000}, // Munich area typical JN58td center
	}
	for _, tc := range tests {
		lat, err := LatitudeFromGridSquare(tc.grid)
		if err != nil {
			t.Fatalf("%s lat err: %v", tc.grid, err)
		}
		lon, err := LongitudeFromGridSquare(tc.grid)
		if err != nil {
			t.Fatalf("%s lon err: %v", tc.grid, err)
		}

		if !almostEqual(lat, tc.expLat, 1e-4) { // 1e-4 ~ 0.0001 deg tolerance
			t.Errorf("%s latitude got %.7f want %.7f", tc.grid, lat, tc.expLat)
		}
		if !almostEqual(lon, tc.expLon, 1e-4) {
			t.Errorf("%s longitude got %.7f want %.7f", tc.grid, lon, tc.expLon)
		}
	}
}

func TestValidationErrors(t *testing.T) {
	bad := []string{
		"J",       // too short
		"JN58T",   // too short
		"JN58TDX", // too long
		"SN58td",  // 'S' out of A-R for first char
		"JS58yd",  // 'y' out of a-x for 5th char
		"JA5x7d",  // 'x' at third position should be a digit
	}
	for _, s := range bad {
		if _, err := LatitudeFromGridSquare(s); err == nil {
			t.Errorf("expected error for %q latitude, got nil", s)
		}
		if _, err := LongitudeFromGridSquare(s); err == nil {
			t.Errorf("expected error for %q longitude, got nil", s)
		}
	}
}

func TestValidationSpecificPositionErrors(t *testing.T) {
	cases := []struct {
		in       string
		contains string
	}{
		{"ZN58td", "first character must be A-R"},
		{"JZ58td", "second character must be A-R"},
		{"JNa8td", "third character must be a digit"},
		{"JN5xtd", "fourth character must be a digit"},
		{"JN58Yd", "fifth character must be a-x"},
		{"JN58tA", "sixth character must be a-x"},
	}
	for _, tc := range cases {
		if err := validateInput(tc.in); err == nil || !strings.Contains(err.Error(), tc.contains) {
			t.Errorf("validateInput(%q) error %v, want to contain %q", tc.in, err, tc.contains)
		}
	}
}

func TestCalculateBearing_Known(t *testing.T) {
	// From London (51.5074, -0.1278) to New York (40.7128, -74.0060)
	b := CalculateBearing(51.5074, -0.1278, 40.7128, -74.0060)
	// Known initial bearing approx 288.3° with this spherical model.
	if !almostEqual(b, 288.3, 1.0) {
		// allow 1.0 deg tolerance to account for model differences
		t.Errorf("bearing got %.3f want approx 288.3", b)
	}
}

func TestCalculateBearing_EdgeCases(t *testing.T) {
	cases := []struct {
		name string
		lat1 float64
		lon1 float64
		lat2 float64
		lon2 float64
		min  float64
		max  float64
	}{
		{"same point", 0, 0, 0, 0, 0, 0},
		{"due north", 0, 0, 10, 0, -1, 1},
		{"due east", 0, 0, 0, 10, 89, 91},
		{"due south", 10, 0, 0, 0, 179, 181},
		{"due west", 0, 10, 0, 0, 269, 271},
	}
	for _, tc := range cases {
		b := CalculateBearing(tc.lat1, tc.lon1, tc.lat2, tc.lon2)
		if b < tc.min || b > tc.max {
			t.Errorf("%s: bearing=%.2f not in [%.2f, %.2f]", tc.name, b, tc.min, tc.max)
		}
	}
}

func TestShortAndLongPaths_JN58td_to_FN31pr(t *testing.T) {
	// Munich JN58td to Connecticut FN31pr (approx New Haven area)
	spb, err := GetShortPathBearing("JN58td", "FN31pr")
	if err != nil {
		t.Fatalf("GetShortPathBearing error: %v", err)
	}
	lpb, err := GetLongPathBearing("JN58td", "FN31pr")
	if err != nil {
		t.Fatalf("GetLongPathBearing error: %v", err)
	}
	// sum should be approx 180 apart
	if !almostEqual(math.Mod(spb+180, 360), lpb, 0.1) && !almostEqual(math.Mod(lpb+180, 360), spb, 0.1) {
		t.Errorf("short and long bearings not opposite: sp=%.1f lp=%.1f", spb, lpb)
	}

	spKm, spMi, err := GetShortPathDistance("JN58td", "FN31pr")
	if err != nil {
		t.Fatalf("GetShortPathDistance error: %v", err)
	}
	lpKm, lpMi, err := GetLongPathDistance("JN58td", "FN31pr")
	if err != nil {
		t.Fatalf("GetLongPathDistance error: %v", err)
	}

	// Check that long path + short path approx Earth's circumference (ceil introduces at most ~1-2 km error)
	earthCircumferenceKm := 2 * math.Pi * earthRad
	if math.Abs((spKm+lpKm)-math.Ceil(earthCircumferenceKm)) > 2 {
		t.Errorf("sp+lp km not approx Earth circumference: sp=%.0f lp=%.0f sum=%.0f want~%.0f", spKm, lpKm, spKm+lpKm, math.Ceil(earthCircumferenceKm))
	}
	// Miles consistency via kmToMiles with Ceil
	if spMi != math.Ceil(spKm*kmToMiles) {
		t.Errorf("short miles mismatch: got %.0f want %.0f", spMi, math.Ceil(spKm*kmToMiles))
	}
	if lpMi != math.Ceil(lpKm*kmToMiles) {
		t.Errorf("long miles mismatch: got %.0f want %.0f", lpMi, math.Ceil(lpKm*kmToMiles))
	}
}

func TestGetLocation(t *testing.T) {
	loc, err := GetLocation("JN58TD", "FN31pr")
	if err != nil {
		t.Fatalf("GetLocation error: %v", err)
	}
	if loc.LocalGridSquare != "JN58TD" || loc.RemoteGridSquare != "FN31pr" {
		t.Errorf("grid squares echoed incorrectly: %+v", loc)
	}
	// Spot-check types and plausible ranges
	if loc.ShortPathBearing < 0 || loc.ShortPathBearing >= 360 {
		t.Errorf("invalid SP bearing: %.1f", loc.ShortPathBearing)
	}
	if loc.LongPathBearing < 0 || loc.LongPathBearing >= 360 {
		t.Errorf("invalid LP bearing: %.1f", loc.LongPathBearing)
	}
	if loc.ShortPathDistanceKm <= 0 || loc.LongPathDistanceKm <= 0 {
		t.Errorf("distances should be positive: %+v", loc)
	}
}

func TestGetLocation_ErrorPropagation(t *testing.T) {
	if _, err := GetLocation("BADGRID", "FN31pr"); err == nil {
		t.Fatalf("expected error for bad local grid")
	}
	if _, err := GetLocation("JN58td", "BADGRID"); err == nil {
		t.Fatalf("expected error for bad remote grid")
	}
}

func TestShortPathDistance_ErrorCases(t *testing.T) {
	if _, _, err := GetShortPathDistance("BAD", "JN58td"); err == nil || !strings.Contains(err.Error(), "invalid local grid square") {
		t.Errorf("expected invalid local grid square error, got %v", err)
	}
	if _, _, err := GetShortPathDistance("JN58td", "BAD"); err == nil || !strings.Contains(err.Error(), "invalid remote grid square") {
		t.Errorf("expected invalid remote grid square error, got %v", err)
	}
}

func TestLongPathDistance_ErrorCases(t *testing.T) {
	if _, _, err := GetLongPathDistance("BAD", "JN58td"); err == nil {
		t.Errorf("expected error from GetLongPathDistance when short path fails")
	}
}
