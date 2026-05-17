package decoder

import (
	"strconv"
	"strings"
	"testing"
)

// All LDPC matrix tests below operate on the package-level
// ldpcGenerator and ldpcParity vars populated at init() from the
// embedded qexref14/*.dat files. If the embed/parse failed at init,
// the package would have panicked before any test ran — so reaching
// these tests at all is itself the first invariant.

func TestLDPCGenerator_Dimensions(t *testing.T) {
	// Compile-time-equivalent: the matrix type pins the shape, so
	// if init() succeeded the dimensions are guaranteed. This test
	// re-asserts at runtime for documentation value and to catch
	// any future change that loosens the type.
	if got, want := len(ldpcGenerator), ParityBits; got != want {
		t.Errorf("generator rows: got %d, want %d", got, want)
	}
	if got, want := len(ldpcGenerator[0]), InfoBits; got != want {
		t.Errorf("generator cols: got %d, want %d", got, want)
	}
}

func TestLDPCGenerator_OnlyBinaryEntries(t *testing.T) {
	// Every cell of the dense generator must be 0 or 1.
	for r := range ldpcGenerator {
		for c := range ldpcGenerator[r] {
			if v := ldpcGenerator[r][c]; v > 1 {
				t.Errorf("generator[%d][%d] = %d, want 0 or 1", r, c, v)
			}
		}
	}
}

func TestLDPCParity_Dimensions(t *testing.T) {
	if got, want := len(ldpcParity), CodewordBits; got != want {
		t.Errorf("parity columns: got %d, want %d", got, want)
	}
	if got, want := len(ldpcParity[0]), LDPCParityColumnDensity; got != want {
		t.Errorf("parity sparse density: got %d, want %d", got, want)
	}
}

func TestLDPCParity_AllRowIndicesInRange(t *testing.T) {
	// Every row index must be in [0, ParityBits). Catches any
	// off-by-one in the 1-based-to-0-based conversion at parse time.
	for col := range ldpcParity {
		for k, row := range ldpcParity[col] {
			if int(row) >= ParityBits {
				t.Errorf("parity[col=%d][k=%d] = %d, out of range [0, %d)", col, k, row, ParityBits)
			}
		}
	}
}

func TestLDPCParity_ColumnsHaveExactly3Ones(t *testing.T) {
	// QEX §3: "Each of the 174 columns contains exactly 3 ones."
	// The sparse representation enforces this by construction
	// (3-element fixed array), but the entries must also be DISTINCT
	// — three duplicate row indices would mean a column with fewer
	// than 3 distinct ones.
	for col := range ldpcParity {
		seen := map[uint8]bool{}
		for _, row := range ldpcParity[col] {
			if seen[row] {
				t.Errorf("parity[col=%d] has duplicate row %d", col, row)
			}
			seen[row] = true
		}
	}
}

func TestLDPCParity_RowWeightsAre6or7(t *testing.T) {
	// QEX §3: "The rows contain either 6 or 7 ones." Derived from
	// the column-major sparse representation by counting how many
	// columns reference each row.
	rowWeight := [ParityBits]int{}
	for col := range ldpcParity {
		for _, row := range ldpcParity[col] {
			rowWeight[row]++
		}
	}
	for row, w := range rowWeight {
		if w != 6 && w != 7 {
			t.Errorf("parity row %d has weight %d, want 6 or 7", row, w)
		}
	}
}

func TestLDPCParity_TotalOnesIs522(t *testing.T) {
	// Cross-check: 174 columns × 3 ones each = 522 total. Equally,
	// sum of row weights must equal 522 (83 rows with weights 6 or 7
	// averaging ~6.29). Catches any silent corruption of the parser.
	const want = CodewordBits * LDPCParityColumnDensity // 522

	rowSum := 0
	rowWeight := [ParityBits]int{}
	for col := range ldpcParity {
		for _, row := range ldpcParity[col] {
			rowWeight[row]++
		}
	}
	for _, w := range rowWeight {
		rowSum += w
	}
	if rowSum != want {
		t.Errorf("total ones in parity matrix: got %d (via row sum), want %d", rowSum, want)
	}
}

func TestParseGenerator_RejectsShortRow(t *testing.T) {
	// Doctor a known-good file with a too-short data row; parser
	// must reject it.
	bad := strings.Replace(generatorRaw, "1000001100101001110011100001000110111111001100011110101011110101000010011111001001111111110", "100000110", 1)
	var m GeneratorMatrix
	if err := parseGenerator(bad, &m); err == nil {
		t.Error("parseGenerator should reject row with wrong column count; did not")
	}
}

func TestParseGenerator_RejectsTooFewRows(t *testing.T) {
	// Truncate the file to two data rows only.
	lines := strings.SplitN(generatorRaw, "\n", 6) // 3 header + 2 data + tail
	truncated := strings.Join(lines[:5], "\n") + "\n"
	var m GeneratorMatrix
	if err := parseGenerator(truncated, &m); err == nil {
		t.Error("parseGenerator should reject file with fewer than ParityBits rows; did not")
	}
}

func TestParseParity_RejectsRowIndexOutOfRange(t *testing.T) {
	// Insert a row index that exceeds ParityBits (83). Parser
	// currently SKIPS lines that don't parse as 3 valid 1..83 ints
	// (treating them as prose), so an out-of-range index causes the
	// data line to be skipped and the total count to come up short.
	bad := strings.Replace(parityRaw, "  16   45   73", "  16   45  999", 1)
	var m ParityMatrix
	if err := parseParity(bad, &m); err == nil {
		t.Error("parseParity should reject file with row index > 83 (via short total count); did not")
	}
}

func TestParseParity_RejectsRowIndexZero(t *testing.T) {
	// File is 1-based; index 0 is out of range. Same skip-as-prose
	// path as the >83 case: silently dropped, total count comes up
	// short, parse fails at the final tally.
	bad := strings.Replace(parityRaw, "  16   45   73", "  16   45    0", 1)
	var m ParityMatrix
	if err := parseParity(bad, &m); err == nil {
		t.Error("parseParity should reject row index 0 (1-based file); did not")
	}
}

func TestParseGenerator_RejectsLongRow(t *testing.T) {
	// First data row plus extra '0' bits → length > InfoBits, fails
	// the column-count check (not the isBitLine check).
	bad := strings.Replace(
		generatorRaw,
		"1000001100101001110011100001000110111111001100011110101011110101000010011111001001111111110",
		"1000001100101001110011100001000110111111001100011110101011110101000010011111001001111111110000000",
		1,
	)
	var m GeneratorMatrix
	if err := parseGenerator(bad, &m); err == nil {
		t.Error("parseGenerator should reject row with too many cols; did not")
	}
}

func TestParseGenerator_RejectsTooManyRows(t *testing.T) {
	// Append one extra valid-shaped data row; parser must reject
	// before assignment to keep the matrix bounded.
	extra := strings.Repeat("0", InfoBits)
	bad := strings.TrimRight(generatorRaw, "\n") + "\n" + extra + "\n"
	var m GeneratorMatrix
	if err := parseGenerator(bad, &m); err == nil {
		t.Error("parseGenerator should reject file with more than ParityBits rows; did not")
	}
}

func TestParseGenerator_HappyPath_Synthetic(t *testing.T) {
	// Header lines + ParityBits all-zero rows: parser should accept
	// and leave every cell at 0. Exercises the success branch on
	// input independent of the embedded file.
	var sb strings.Builder
	sb.WriteString("header that should be skipped\n")
	sb.WriteString("another header line\n")
	sb.WriteString("\n")
	row := strings.Repeat("0", InfoBits)
	for range ParityBits {
		sb.WriteString(row + "\n")
	}

	var m GeneratorMatrix
	if err := parseGenerator(sb.String(), &m); err != nil {
		t.Fatalf("parseGenerator unexpected error: %v", err)
	}
	for r := range m {
		for c := range m[r] {
			if m[r][c] != 0 {
				t.Fatalf("cell [%d][%d] = %d, want 0", r, c, m[r][c])
			}
		}
	}
}

func TestParseParity_RejectsTooManyColumns(t *testing.T) {
	// Append one extra valid-shaped data line.
	bad := strings.TrimRight(parityRaw, "\n") + "\n   1   2   3\n"
	var m ParityMatrix
	if err := parseParity(bad, &m); err == nil {
		t.Error("parseParity should reject file with more than CodewordBits columns; did not")
	}
}

func TestParseParity_HappyPath_Synthetic(t *testing.T) {
	// Header + CodewordBits valid lines, each three in-range indices.
	// Exercises the success branch on input independent of the
	// embedded file.
	var sb strings.Builder
	sb.WriteString("prose header to skip\n\n")
	for c := range CodewordBits {
		base := (c % (ParityBits - 2)) + 1
		sb.WriteString(strconv.Itoa(base) + " " + strconv.Itoa(base+1) + " " + strconv.Itoa(base+2) + "\n")
	}

	var m ParityMatrix
	if err := parseParity(sb.String(), &m); err != nil {
		t.Fatalf("parseParity unexpected error: %v", err)
	}
	// Spot-check first column: indices 1,2,3 (1-based) → 0,1,2.
	if got := m[0]; got != [LDPCParityColumnDensity]uint8{0, 1, 2} {
		t.Errorf("synthetic m[0] = %v, want [0 1 2]", got)
	}
}

func TestIsBitLine(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", true},
		{"1", true},
		{"01", true},
		{"10", true},
		{"0a1", false},
		{"0 1", false}, // space is not '0' or '1'
		{"012", false},
		{"abc", false},
		{strings.Repeat("01", 100), true},
	}
	for _, tc := range cases {
		if got := isBitLine(tc.in); got != tc.want {
			t.Errorf("isBitLine(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLdpcErr_Format(t *testing.T) {
	if got, want := errAt(0, "boom").Error(), "boom"; got != want {
		t.Errorf("line=0: got %q, want %q", got, want)
	}
	if got, want := errAt(7, "boom").Error(), "line 7: boom"; got != want {
		t.Errorf("line=7: got %q, want %q", got, want)
	}
}

// =====================================================================
// LDPC encoder tests
// =====================================================================

func TestLDPCEncode_OutputDimensions(t *testing.T) {
	info := make([]byte, InfoBits)
	c := LDPCEncode(info)
	if got, want := len(c), CodewordBits; got != want {
		t.Errorf("codeword length: got %d, want %d", got, want)
	}
}

func TestLDPCEncode_ZeroInputZeroOutput(t *testing.T) {
	// Encoding the all-zero info word produces the all-zero
	// codeword — for any linear code, c = G·0 = 0 in both halves.
	info := make([]byte, InfoBits)
	c := LDPCEncode(info)
	for i, b := range c {
		if b != 0 {
			t.Errorf("zero-info codeword: bit %d = %d, want 0", i, b)
		}
	}
}

func TestLDPCEncode_InfoBitsCopiedToPrefix(t *testing.T) {
	// Systematic form: codeword[0..91] must literally equal info.
	// Use a non-trivial info word so a bug that always writes zero
	// (or always writes the input verbatim) is distinguishable from
	// a correct copy.
	info := make([]byte, InfoBits)
	for i := range info {
		info[i] = byte(i & 1) // alternating 0101...
	}
	c := LDPCEncode(info)
	for i := range InfoBits {
		if c[i] != info[i] {
			t.Errorf("codeword[%d] = %d, want info[%d] = %d", i, c[i], i, info[i])
		}
	}
}

// TestLDPCEncode_OutputBitsAreBinary catches a future regression
// where the encoder's XOR/AND arithmetic accidentally produces a
// byte > 1 (e.g. dropping the AND and writing the product as int).
func TestLDPCEncode_OutputBitsAreBinary(t *testing.T) {
	info := make([]byte, InfoBits)
	for i := range info {
		info[i] = byte(i & 1)
	}
	c := LDPCEncode(info)
	for i, b := range c {
		if b > 1 {
			t.Errorf("codeword[%d] = %d, must be 0 or 1", i, b)
		}
	}
}

// TestLDPCEncode_CodewordPassesParityCheck is the cross-validation
// that the encoder produces codewords consistent with the parity
// matrix. By linearity it's sufficient to check unit-vector info
// words (TestLDPCMatricesAreConsistent does this for the matrices
// directly); here we exercise the encoder end-to-end for several
// arbitrary info words — including patterns the unit-vector test
// doesn't directly cover.
func TestLDPCEncode_CodewordPassesParityCheck(t *testing.T) {
	cases := []struct {
		name string
		info []byte
	}{
		{"zero", make([]byte, InfoBits)},
		{"all_ones", repeatByte(1, InfoBits)},
		{"alternating_01", alternating(InfoBits, 0, 1)},
		{"alternating_10", alternating(InfoBits, 1, 0)},
		{"only_first_bit", oneAt(InfoBits, 0)},
		{"only_last_bit", oneAt(InfoBits, InfoBits-1)},
		{"only_middle_bit", oneAt(InfoBits, InfoBits/2)},
		// Pseudo-random pattern: deterministic, not all-zero, exercises
		// many G rows simultaneously.
		{"pseudo_random_pattern", patternBits(InfoBits, 0x9E3779B9)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := LDPCEncode(tc.info)
			// Syndrome = H · c (mod 2). For each codeword bit that's
			// 1, XOR 1 into the rows where that column has a 1.
			var syndrome [ParityBits]byte
			for col := range CodewordBits {
				if c[col] == 0 {
					continue
				}
				for _, row := range ldpcParity[col] {
					syndrome[row] ^= 1
				}
			}
			for r, s := range syndrome {
				if s != 0 {
					t.Errorf("syndrome[%d] = %d, want 0 (codeword fails H·c=0)", r, s)
				}
			}
		})
	}
}

func TestLDPCEncode_RejectsWrongLength(t *testing.T) {
	cases := []struct {
		name string
		n    int
	}{
		{"too_short", InfoBits - 1},
		{"too_long", InfoBits + 1},
		{"empty", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("LDPCEncode(len=%d) should panic; did not", tc.n)
				}
			}()
			LDPCEncode(make([]byte, tc.n))
		})
	}
}

func TestLDPCEncode_RejectsInvalidBit(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("LDPCEncode(byte > 1) should panic; did not")
		}
	}()
	info := make([]byte, InfoBits)
	info[42] = 2
	LDPCEncode(info)
}

func BenchmarkLDPCEncode(b *testing.B) {
	info := make([]byte, InfoBits)
	for i := range info {
		info[i] = byte(i & 1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = LDPCEncode(info)
	}
}

// --- small bit-vector helpers, test-only ---

func repeatByte(v byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func alternating(n int, first, second byte) []byte {
	out := make([]byte, n)
	for i := range out {
		if i%2 == 0 {
			out[i] = first
		} else {
			out[i] = second
		}
	}
	return out
}

func oneAt(n, idx int) []byte {
	out := make([]byte, n)
	out[idx] = 1
	return out
}

// patternBits produces a deterministic bit sequence by hashing the
// position with a fixed multiplier. Not cryptographic; just a way
// to get a pseudo-random pattern that doesn't depend on the random
// package and is reproducible across runs.
func patternBits(n int, mul uint32) []byte {
	out := make([]byte, n)
	state := uint32(1)
	for i := range out {
		state = state*mul + 0xDEADBEEF
		out[i] = byte((state >> 31) & 1)
	}
	return out
}

// TestLDPCMatricesAreConsistent is the load-bearing cross-check
// between generator.dat and parity.dat: any 91-bit info word u,
// extended with parity = G·u into a 174-bit codeword, must satisfy
// H·codeword = 0 mod 2. If this fails, encoded transmissions will
// fail their own parity check at the decoder — a bug invisible to the
// per-matrix structural tests above.
//
// We check the 91 unit-vector info words e_j; by linearity over GF(2)
// passing on every e_j proves the property for every info word u.
//
// Codeword bit ordering is [info(91) | parity(83)] per QEX paper §3
// ("the codeword consists of the 91 information bits followed by 83
// parity-check bits") — i.e. columns 0..90 of H index info bits,
// columns 91..173 index parity bits.
func TestLDPCMatricesAreConsistent(t *testing.T) {
	for j := range InfoBits {
		// parity[i] = G[i][j] (only column j of G contributes).
		var syndrome [ParityBits]byte

		// Info side: c[j] = 1, all other c[0..91) = 0.
		for _, row := range ldpcParity[j] {
			syndrome[row] ^= 1
		}

		// Parity side: c[91+i] = G[i][j].
		for i := range ParityBits {
			if ldpcGenerator[i][j] == 0 {
				continue
			}
			for _, row := range ldpcParity[InfoBits+i] {
				syndrome[row] ^= 1
			}
		}

		for r := range syndrome {
			if syndrome[r] != 0 {
				t.Errorf("info=e_%d: syndrome[%d] != 0", j, r)
			}
		}
	}
}
