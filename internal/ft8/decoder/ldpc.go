package decoder

import (
	_ "embed"
	"strconv"
	"strings"
)

// LDPC(174,91) code dimensions per QEX paper §3:
//
//	"A 14-bit cyclic redundancy check (CRC) is appended to each 77-bit
//	information packet to create a 91-bit message-plus-CRC word. ...
//	Forward error correction is accomplished using a (174, 91) LDPC
//	code designed specifically for FT8 and FT4."
//
// Bits:
//
//	InfoBits     = 91   (message + CRC)
//	ParityBits   = 83   (FEC parity)
//	CodewordBits = 174  (Info + Parity, transmitted across 58 channel
//	                    symbols × 3 bits each)
//
// Matrix dimensions:
//
//	Generator G: 83 rows × 91 cols. Row i lists which info bits XOR
//	             together to form parity bit i.
//	Parity   H: 83 rows × 174 cols. Sparse: each column has exactly
//	             3 ones (per QEX §3). Used for verification and
//	             belief-propagation decoding.
const (
	InfoBits     = 91
	ParityBits   = 83
	CodewordBits = 174

	// LDPCParityColumnDensity is the exact column weight of the
	// parity-check matrix H, pinned by the QEX paper §3 description.
	// Used as a structural invariant in the parser.
	LDPCParityColumnDensity = 3
)

// Embedded matrices from the public-domain QEX reference [14]
// tarball. See `qexref14/README.md` for provenance and licensing.

//go:embed qexref14/generator.dat
var generatorRaw string

//go:embed qexref14/parity.dat
var parityRaw string

// GeneratorMatrix is the dense LDPC generator matrix in row-major
// form: g[row][col] == 1 iff info bit col contributes to parity bit
// row. Used for encoding (parity bit i = XOR of info bits selected
// by row i of the generator).
type GeneratorMatrix [ParityBits][InfoBits]byte

// ParityMatrix is the LDPC parity-check matrix in sparse column-
// major form: parity[col] lists the 3 row indices (0-based) where
// column col has a 1. Stored this way because the matrix is sparse
// (≤522 ones in 14,442 cells) and column-major is the file's native
// layout.
//
// To check whether a 174-bit candidate codeword c is valid, compute
// (H × c) mod 2 — equivalently, for each column j where c[j]=1, XOR
// 1 into the 83 syndrome bits at the row indices parity[j]; the
// syndrome must be all-zero. See ldpc_test.go for the invariant.
type ParityMatrix [CodewordBits][LDPCParityColumnDensity]uint8

// ldpcGenerator and ldpcParity are populated at package init from
// the embedded files. Panic on parse error — these are load-bearing
// production data; a broken embed means the package can't function.
var (
	ldpcGenerator GeneratorMatrix
	ldpcParity    ParityMatrix
)

func init() {
	if err := parseGenerator(generatorRaw, &ldpcGenerator); err != nil {
		panic("decoder: parsing embedded generator.dat: " + err.Error())
	}
	if err := parseParity(parityRaw, &ldpcParity); err != nil {
		panic("decoder: parsing embedded parity.dat: " + err.Error())
	}
}

// parseGenerator parses generator.dat's ASCII '0'/'1' rows into the
// dense matrix. Format: any number of header lines (skipped), then
// exactly ParityBits data lines each exactly InfoBits chars long.
//
// A "data line" is any non-empty line whose every character is '0'
// or '1'. Header lines mix prose and are skipped.
func parseGenerator(raw string, m *GeneratorMatrix) error {
	rowCount := 0
	for lineNo, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !isBitLine(line) {
			continue
		}
		if rowCount >= ParityBits {
			return errAt(lineNo+1, "more than "+strconv.Itoa(ParityBits)+" data rows in generator.dat")
		}
		if len(line) != InfoBits {
			return errAt(lineNo+1, "data row has "+strconv.Itoa(len(line))+" cols, want "+strconv.Itoa(InfoBits))
		}
		for c := range InfoBits {
			m[rowCount][c] = line[c] - '0'
		}
		rowCount++
	}
	if rowCount != ParityBits {
		return errAt(0, "expected "+strconv.Itoa(ParityBits)+" data rows in generator.dat, got "+strconv.Itoa(rowCount))
	}
	return nil
}

// parseParity parses parity.dat's sparse column representation into
// the sparse matrix. Format: any number of header lines (skipped),
// then exactly CodewordBits data lines each containing 3 whitespace-
// separated 1-based row indices. The parser converts to 0-based
// internally so the rest of the code uses 0-based indexing
// consistently.
func parseParity(raw string, m *ParityMatrix) error {
	colCount := 0
	for lineNo, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != LDPCParityColumnDensity {
			continue // header or blank line
		}
		// Must look like a triple of integers; otherwise skip
		// (defensive against any prose row with exactly 3 fields).
		idx := [LDPCParityColumnDensity]uint8{}
		isData := true
		for k, f := range fields {
			n, err := strconv.Atoi(f)
			if err != nil || n < 1 || n > ParityBits {
				isData = false
				break
			}
			idx[k] = uint8(n - 1) // file is 1-based; convert to 0-based
		}
		if !isData {
			continue
		}
		if colCount >= CodewordBits {
			return errAt(lineNo+1, "more than "+strconv.Itoa(CodewordBits)+" data columns in parity.dat")
		}
		m[colCount] = idx
		colCount++
	}
	if colCount != CodewordBits {
		return errAt(0, "expected "+strconv.Itoa(CodewordBits)+" data columns in parity.dat, got "+strconv.Itoa(colCount))
	}
	return nil
}

// LDPCEncode produces the 174-bit FT8 LDPC codeword for a 91-bit
// info word. The codeword has systematic form:
//
//	codeword[0..91)   = info[0..91)            (the message+CRC bits)
//	codeword[91..174) = parity[0..83)          (computed FEC bits)
//
// where parity[i] = XOR over j of (G[i][j] AND info[j]) — the standard
// matrix-vector multiplication over GF(2). The ordering matches QEX
// paper §3 ("83 bits are appended for forward error correction,
// creating a 174-bit codeword") and the convention asserted by
// TestLDPCMatricesAreConsistent.
//
// Inputs and outputs use one-bit-per-byte form (each byte 0 or 1)
// to match the project's bit-vector convention; callers convert
// to/from packed form via Pack and Unpack as needed.
//
// Panics if info is not exactly InfoBits long or contains a byte
// outside {0, 1}. The 91-bit invariant is upstream-guaranteed by
// the message-packer (77-bit message + 14-bit CRC); a panic here
// signals a bug at the call site, not user data.
//
// The implementation iterates row-by-row through the dense generator
// matrix; for each parity-bit row i, the inner loop sums G[i][j]·info[j]
// over all 91 info bits. Inner access pattern is contiguous in the
// row-major matrix storage — cache-friendly without any explicit
// optimisation. A correctness-first implementation; faster variants
// (bit-packed AND/XOR, SIMD) become worthwhile only if the inner
// decoder's iteration count makes encode time visible in profiling.
func LDPCEncode(info []byte) []byte {
	if len(info) != InfoBits {
		panic("decoder.LDPCEncode: info must be exactly " + strconv.Itoa(InfoBits) + " bits, got " + strconv.Itoa(len(info)))
	}
	for i, b := range info {
		if b > 1 {
			panic("decoder.LDPCEncode: info bit at index " + strconv.Itoa(i) + " is not 0 or 1")
		}
	}

	codeword := make([]byte, CodewordBits)
	copy(codeword[:InfoBits], info)

	for i := range ParityBits {
		var bit byte
		row := &ldpcGenerator[i]
		for j := range InfoBits {
			bit ^= row[j] & info[j]
		}
		codeword[InfoBits+i] = bit
	}

	return codeword
}

// isBitLine reports whether every char in s is '0' or '1' (and s is
// non-empty).
func isBitLine(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] != '0' && s[i] != '1' {
			return false
		}
	}
	return true
}

// errAt constructs a parse-error string with optional line context.
type ldpcErr struct {
	line int
	msg  string
}

func (e *ldpcErr) Error() string {
	if e.line == 0 {
		return e.msg
	}
	return "line " + strconv.Itoa(e.line) + ": " + e.msg
}

func errAt(line int, msg string) error {
	return &ldpcErr{line: line, msg: msg}
}
