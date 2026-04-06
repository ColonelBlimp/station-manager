package message

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- EncodeGrid --------------------------------------------------

// TestEncodeGrid_KnownValues verifies grid encoding against values cross-checked
// with ft8_lib packgrid() output (commit 9fec6ca).
//
// The mixed-radix formula is: f1*18*10*10 + f2*10*10 + d1*10 + d2
// where f1,f2 ∈ [0,17] (A–R) and d1,d2 ∈ [0,9].
//
// Derivations:
//
//	AA00 → (0,0,0,0)   → 0
//	FN31 → (5,13,3,1)  → 5*1800 + 13*100 + 30 + 1 = 10,331
//	JO22 → (9,14,2,2)  → 9*1800 + 14*100 + 20 + 2 = 17,622
//	EN37 → (4,13,3,7)  → 4*1800 + 13*100 + 30 + 7 = 8,537
//	RR99 → (17,17,9,9) → 17*1800 + 17*100 + 90 + 9 = 32,399
//	IO91 → (8,14,9,1)  → 8*1800 + 14*100 + 90 + 1 = 15,891
func TestEncodeGrid_KnownValues(t *testing.T) {
	tests := []struct {
		grid string
		want uint16
	}{
		{"AA00", 0},
		{"FN31", 10331},
		{"JO22", 17622},
		{"EN37", 8537},
		{"RR99", 32399},
		{"IO91", 15891},
	}
	for _, tt := range tests {
		t.Run(tt.grid, func(t *testing.T) {
			got, err := EncodeGrid(tt.grid)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEncodeGrid_CaseInsensitive(t *testing.T) {
	n1, err := EncodeGrid("fn31")
	require.NoError(t, err)
	n2, err := EncodeGrid("FN31")
	require.NoError(t, err)
	require.Equal(t, n1, n2)
}

func TestEncodeGrid_Boundaries(t *testing.T) {
	// Minimum grid value.
	ig, err := EncodeGrid("AA00")
	require.NoError(t, err)
	require.Equal(t, uint16(0), ig)

	// Maximum grid value.
	ig, err = EncodeGrid("RR99")
	require.NoError(t, err)
	require.Equal(t, MaxGrid4-1, ig)
}

func TestEncodeGrid_Invalid(t *testing.T) {
	tests := []struct {
		name string
		grid string
	}{
		{"too short", "FN3"},
		{"too long", "FN310"},
		{"field1 out of range", "SN31"}, // S > R
		{"field2 out of range", "FS31"}, // S > R
		{"digit1 not digit", "FNA1"},
		{"digit2 not digit", "FN3A"},
		{"empty", ""},
		{"lowercase out of range", "sn31"}, // s > R after uppercasing
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncodeGrid(tt.grid)
			require.Error(t, err, "expected error for %q", tt.grid)
		})
	}
}

// --------------- DecodeGrid --------------------------------------------------

func TestDecodeGrid_KnownValues(t *testing.T) {
	tests := []struct {
		igrid4 uint16
		want   string
	}{
		{0, "AA00"},
		{10331, "FN31"},
		{17622, "JO22"},
		{8537, "EN37"},
		{32399, "RR99"},
		{15891, "IO91"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := DecodeGridField(tt.igrid4, false)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// --------------- Grid round-trip ---------------------------------------------

func TestGrid_RoundTrip(t *testing.T) {
	grids := []string{"AA00", "FN31", "JO22", "EN37", "RR99", "IO91", "AB00", "RR00"}
	for _, grid := range grids {
		t.Run(grid, func(t *testing.T) {
			ig, err := EncodeGrid(grid)
			require.NoError(t, err)
			dec, err := DecodeGridField(ig, false)
			require.NoError(t, err)
			require.Equal(t, grid, dec)
		})
	}
}

// --------------- Grid field layout -------------------------------------------

func TestGridFieldLayout(t *testing.T) {
	// MaxGrid4 must equal 18*18*10*10.
	require.Equal(t, uint16(18*18*10*10), MaxGrid4,
		"MaxGrid4 must equal 18×18×10×10")

	// All field values must fit in 15 bits.
	require.True(t, MaxGrid4-1 < 1<<15, "max grid must fit in 15 bits")
	require.True(t, gridEmpty < 1<<15, "gridEmpty must fit in 15 bits")
	require.True(t, gridRRR < 1<<15, "gridRRR must fit in 15 bits")
	require.True(t, gridRR73 < 1<<15, "gridRR73 must fit in 15 bits")
	require.True(t, grid73 < 1<<15, "grid73 must fit in 15 bits")

	// Max report igrid4 must fit in 15 bits.
	maxReport, err := EncodeReport(ReportMax)
	require.NoError(t, err)
	require.True(t, maxReport < 1<<15, "max report igrid4 must fit in 15 bits")

	// Report region must not overlap token region.
	minReport, err := EncodeReport(ReportMin)
	require.NoError(t, err)
	require.True(t, minReport > grid73,
		"min report igrid4 must be above last token")
}

// --------------- Tokens ------------------------------------------------------

func TestEncodeTokens(t *testing.T) {
	require.Equal(t, uint16(32401), EncodeEmpty())
	require.Equal(t, uint16(32402), EncodeRRR())
	require.Equal(t, uint16(32403), EncodeRR73())
	require.Equal(t, uint16(32404), Encode73())
}

func TestDecodeTokens(t *testing.T) {
	tests := []struct {
		igrid4 uint16
		want   string
	}{
		{gridEmpty, ""},
		{gridRRR, "RRR"},
		{gridRR73, "RR73"},
		{grid73, "73"},
	}
	for _, tt := range tests {
		name := tt.want
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			got, err := DecodeGridField(tt.igrid4, false)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// --------------- Report encoding ---------------------------------------------

// TestEncodeReport_KnownValues verifies report encoding against ft8_lib
// packgrid() output. igrid4 = MaxGrid4 + 35 + dB.
//
// Derivations (cross-checked with compiled ft8_lib):
//
//	-30 dB → MaxGrid4 + 35 + (-30) = 32,405
//	-12 dB → MaxGrid4 + 35 + (-12) = 32,423
//	  0 dB → MaxGrid4 + 35 + 0     = 32,435
//	 +5 dB → MaxGrid4 + 35 + 5     = 32,440
//	+30 dB → MaxGrid4 + 35 + 30    = 32,465
func TestEncodeReport_KnownValues(t *testing.T) {
	tests := []struct {
		db   int
		want uint16
	}{
		{-30, 32405},
		{-12, 32423},
		{0, 32435},
		{5, 32440},
		{30, 32465},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%+d_dB", tt.db), func(t *testing.T) {
			got, err := EncodeReport(tt.db)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEncodeReport_OutOfRange(t *testing.T) {
	_, err := EncodeReport(-31)
	require.Error(t, err)
	_, err = EncodeReport(31)
	require.Error(t, err)
}

// --------------- Report decode -----------------------------------------------

func TestDecodeReport(t *testing.T) {
	tests := []struct {
		igrid4 uint16
		ir     bool
		want   string
	}{
		{32405, false, "-30"},
		{32423, false, "-12"},
		{32435, false, "+00"},
		{32440, false, "+05"},
		{32465, false, "+30"},
		// Roger-prefixed reports.
		{32423, true, "R-12"},
		{32440, true, "R+05"},
		{32405, true, "R-30"},
		{32465, true, "R+30"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := DecodeGridField(tt.igrid4, tt.ir)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// --------------- Roger + grid ------------------------------------------------

func TestDecodeGrid_WithRoger(t *testing.T) {
	// ir=true with a grid produces "R " + grid.
	got, err := DecodeGridField(10331, true) // FN31
	require.NoError(t, err)
	require.Equal(t, "R FN31", got)
}

// --------------- EncodeGridField (all-in-one parser) -------------------------

func TestEncodeGridField_Grids(t *testing.T) {
	ig, ir, err := EncodeGridField("FN31")
	require.NoError(t, err)
	require.Equal(t, uint16(10331), ig)
	require.False(t, ir)
}

func TestEncodeGridField_RogerGrid(t *testing.T) {
	ig, ir, err := EncodeGridField("R FN31")
	require.NoError(t, err)
	require.Equal(t, uint16(10331), ig)
	require.True(t, ir)
}

func TestEncodeGridField_Tokens(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{"", gridEmpty},
		{"RRR", gridRRR},
		{"RR73", gridRR73},
		{"73", grid73},
	}
	for _, tt := range tests {
		name := tt.input
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			ig, ir, err := EncodeGridField(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, ig)
			require.False(t, ir)
		})
	}
}

func TestEncodeGridField_Reports(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
		ir    bool
	}{
		{"+05", 32440, false},
		{"-12", 32423, false},
		{"-30", 32405, false},
		{"+30", 32465, false},
		{"+00", 32435, false},
		{"R+05", 32440, true},
		{"R-12", 32423, true},
		{"R-30", 32405, true},
		{"R+30", 32465, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ig, ir, err := EncodeGridField(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, ig)
			require.Equal(t, tt.ir, ir)
		})
	}
}

func TestEncodeGridField_CaseInsensitive(t *testing.T) {
	ig1, ir1, err := EncodeGridField("fn31")
	require.NoError(t, err)
	ig2, ir2, err := EncodeGridField("FN31")
	require.NoError(t, err)
	require.Equal(t, ig1, ig2)
	require.Equal(t, ir1, ir2)

	ig3, ir3, err := EncodeGridField("r fn31")
	require.NoError(t, err)
	require.Equal(t, ig1, ig3)
	require.True(t, ir3)

	ig4, _, err := EncodeGridField("rrr")
	require.NoError(t, err)
	require.Equal(t, gridRRR, ig4)
}

func TestEncodeGridField_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"random text", "HELLO"},
		{"just R", "R"},
		{"R without sign", "R12"},
		{"out of range report", "+31"},
		{"out of range roger report", "R-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := EncodeGridField(tt.input)
			require.Error(t, err, "expected error for %q", tt.input)
		})
	}
}

// --------------- Full round-trip via EncodeGridField / DecodeGridField --------

func TestGridField_RoundTrip(t *testing.T) {
	tests := []string{
		// Grids.
		"AA00", "FN31", "JO22", "RR99",
		// Roger + grid.
		"R FN31", "R AA00", "R RR99",
		// Tokens.
		"", "RRR", "RR73", "73",
		// Reports.
		"+05", "-12", "+00", "+30", "-30",
		// Roger + reports.
		"R+05", "R-12", "R+00", "R+30", "R-30",
	}
	for _, input := range tests {
		name := input
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			ig, ir, err := EncodeGridField(input)
			require.NoError(t, err)
			dec, err := DecodeGridField(ig, ir)
			require.NoError(t, err)
			require.Equal(t, input, dec)
		})
	}
}

// --------------- Out of range decode -----------------------------------------

func TestDecodeGridField_OutOfRange(t *testing.T) {
	// igrid4 = MaxGrid4 (32400) is unused — falls into report region with
	// irpt=0, dB = 0 - 35 = -35, which is below ReportMin.
	_, err := DecodeGridField(MaxGrid4, false)
	require.Error(t, err)

	// Above max report (32466+).
	_, err = DecodeGridField(32466, false)
	require.Error(t, err)

	// Maximum 15-bit value.
	_, err = DecodeGridField(1<<15-1, false)
	require.Error(t, err)
}
