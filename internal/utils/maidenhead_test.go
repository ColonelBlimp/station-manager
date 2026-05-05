package utils

import (
	"math"
	"testing"
)

func TestIsValidMaidenhead(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"IO91", true},
		{"IO91vl", true},
		{"IO91vl42", true},
		{"io91vl", true},
		{"IO91VL", true},
		{"  IO91vl  ", true},
		{"", true},
		{"   ", true},
		{"IO91v", false},
		{"IO91vl4", false},
		{"SS91", false},
		{"IO91yy", false},
		{"1O91vl", false},
		{"IOAAvl", false},
		{"IO-91", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := IsValidMaidenhead(c.in); got != c.want {
				t.Fatalf("IsValidMaidenhead(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestNormalizeMaidenhead(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"io91", "IO91"},
		{"io91vl", "IO91vl"},
		{"IO91VL", "IO91vl"},
		{"io91vl42", "IO91vl42"},
		{"IO91VL42", "IO91vl42"},
		{"  IO91vl  ", "IO91vl"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := NormalizeMaidenhead(c.in); got != c.want {
				t.Fatalf("NormalizeMaidenhead(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestMaidenheadToDecimal(t *testing.T) {
	const eps = 1e-6
	cases := []struct {
		name      string
		in        string
		wantLat   float64
		wantLon   float64
		wantOk    bool
		tolerance float64
	}{
		// IO91: field I=8 → lon -180+160=-20, square 9 → lon -20+18=-2, half cell +1 → -1.
		// field O=14 → lat -90+140=50, square 1 → lat 51, half cell +0.5 → 51.5.
		{"4-char IO91 centre", "IO91", 51.5, -1.0, true, eps},
		// AA00 corner of world; centre at lat -89.5, lon -179.
		{"4-char AA00 centre", "AA00", -89.5, -179.0, true, eps},
		// RR99 max field+square; field R=17 → lon -180+340=160, square 9 → 178, +1 centre = 179.
		// lat: R=17 → -90+170=80, sq 9 → 89, +0.5 → 89.5.
		{"4-char RR99 centre", "RR99", 89.5, 179.0, true, eps},
		// 6-char IO91vl: 4-char corner (-2, 51) then subsquare v=21 → lon += 5/60*21 = 1.75, l=11 → lat += 2.5/60*11 ≈ 0.4583, then half-cell (lon 5/120, lat 2.5/120).
		{"6-char IO91vl centre", "IO91vl", 51 + (2.5/60.0)*11 + (2.5 / 120.0), -2 + (5.0/60.0)*21 + (5.0 / 120.0), true, eps},
		// 8-char IO91vl42: 6-char corner + extended (4 lon, 2 lat) at 0.5'/0.25', then half-extended cell.
		{
			"8-char IO91vl42 centre",
			"IO91vl42",
			51 + (2.5/60.0)*11 + (2.5/600.0)*2 + (2.5 / 1200.0),
			-2 + (5.0/60.0)*21 + (5.0/600.0)*4 + (5.0 / 1200.0),
			true,
			eps,
		},
		{"empty", "", 0, 0, false, 0},
		{"invalid", "ZZ99", 0, 0, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lat, lon, ok := MaidenheadToDecimal(c.in)
			if ok != c.wantOk {
				t.Fatalf("ok = %v, want %v", ok, c.wantOk)
			}
			if !ok {
				return
			}
			if math.Abs(lat-c.wantLat) > c.tolerance {
				t.Errorf("lat = %v, want %v (±%v)", lat, c.wantLat, c.tolerance)
			}
			if math.Abs(lon-c.wantLon) > c.tolerance {
				t.Errorf("lon = %v, want %v (±%v)", lon, c.wantLon, c.tolerance)
			}
		})
	}
}

func TestMaidenheadToADIFLatLon(t *testing.T) {
	// IO91 centre is lat 51.5, lon -1.0 → "N051 30.000" / "W001 00.000".
	lat, lon, ok := MaidenheadToADIFLatLon("IO91")
	if !ok {
		t.Fatal("expected ok=true for IO91")
	}
	if lat != "N051 30.000" {
		t.Errorf("lat = %q, want %q", lat, "N051 30.000")
	}
	if lon != "W001 00.000" {
		t.Errorf("lon = %q, want %q", lon, "W001 00.000")
	}

	if _, _, ok := MaidenheadToADIFLatLon(""); ok {
		t.Error("expected ok=false for empty input")
	}
	if _, _, ok := MaidenheadToADIFLatLon("ZZ99"); ok {
		t.Error("expected ok=false for invalid input")
	}
}
