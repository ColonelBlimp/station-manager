package ldpc

import (
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/demod"
)

// TestParseParity_StructuralProperties pins the FT8 (174,91) LDPC
// code's structural invariants per QEX paper §6 + the parity.dat
// header: 174 columns, 83 rows, column weight exactly 3, row weight
// 6 or 7. A regression here means the H matrix file is wrong, the
// parser is wrong, or both — every BP decode would be wrong too.
func TestParseParity_StructuralProperties(t *testing.T) {
	if len(hColumns) != codewordBits {
		t.Fatalf("hColumns length = %d, want %d", len(hColumns), codewordBits)
	}
	if len(hRows) != parityRows {
		t.Fatalf("hRows length = %d, want %d", len(hRows), parityRows)
	}

	// Every column must have exactly 3 ones, all in [0, parityRows).
	for v := 0; v < codewordBits; v++ {
		seen := map[int]bool{}
		for k := 0; k < 3; k++ {
			r := hColumns[v][k]
			if r < 0 || r >= parityRows {
				t.Errorf("hColumns[%d][%d] = %d out of [0, %d)", v, k, r, parityRows)
			}
			if seen[r] {
				t.Errorf("hColumns[%d] has duplicate row %d", v, r)
			}
			seen[r] = true
		}
	}

	// Every row must have weight 6 or 7.
	for c := 0; c < parityRows; c++ {
		n := len(hRows[c])
		if n != 6 && n != 7 {
			t.Errorf("hRows[%d] weight = %d, want 6 or 7", c, n)
		}
		seen := map[int]bool{}
		for _, v := range hRows[c] {
			if v < 0 || v >= codewordBits {
				t.Errorf("hRows[%d] has column %d out of [0, %d)", c, v, codewordBits)
			}
			if seen[v] {
				t.Errorf("hRows[%d] has duplicate column %d", c, v)
			}
			seen[v] = true
		}
	}

	// Row-weight sum must equal column-weight sum (every edge counted twice).
	colSum := codewordBits * 3
	rowSum := 0
	for c := 0; c < parityRows; c++ {
		rowSum += len(hRows[c])
	}
	if rowSum != colSum {
		t.Errorf("edge accounting: column-side sum %d, row-side sum %d — must be equal", colSum, rowSum)
	}
}

// TestEdgeLookups_RoundTrip confirms the precomputed reverse-index
// tables (varEdgeRowPos, chkEdgeColPos) are self-consistent: walking
// from a variable's k-th edge to the check, then back via the
// precomputed slot, must land on the original variable. The BP
// inner loops trust these — a bug here would corrupt every message
// update silently.
func TestEdgeLookups_RoundTrip(t *testing.T) {
	for v := 0; v < codewordBits; v++ {
		for k := 0; k < 3; k++ {
			c := hColumns[v][k]
			i := varEdgeRowPos[v][k]
			if hRows[c][i] != v {
				t.Errorf("v=%d k=%d: hRows[%d][%d] = %d, want %d", v, k, c, i, hRows[c][i], v)
			}
			if chkEdgeColPos[c][i] != k {
				t.Errorf("v=%d k=%d: chkEdgeColPos[%d][%d] = %d, want %d", v, k, c, i, chkEdgeColPos[c][i], k)
			}
		}
	}
}

// TestCRC14_AllZeros pins the trivial property of XOR-based CRCs
// initialised from the message itself: zeros in, zeros out. The
// Fortran reference shift register is initialised with mc[0..14],
// which is all zero for an all-zero message, so the register stays
// at zero across all 82 iterations and the output CRC is zero. A
// failure here means the Fortran port is structurally wrong.
func TestCRC14_AllZeros(t *testing.T) {
	var msg [payloadBits]uint8
	got := crc14(msg)
	if got != 0 {
		t.Errorf("crc14(all-zero 77-bit message) = %#x, want 0", got)
	}
}

// TestCRC14_SensitiveToMessageBits confirms the CRC is not a constant
// — flipping any single message bit produces a different output.
// This is a basic correctness property of any non-trivial CRC and a
// cheap regression guard against an algorithm that accidentally
// ignores the message (e.g., a missed XOR or shift).
func TestCRC14_SensitiveToMessageBits(t *testing.T) {
	var base [payloadBits]uint8
	baseCrc := crc14(base)
	for bit := 0; bit < payloadBits; bit++ {
		var perturbed [payloadBits]uint8
		perturbed[bit] = 1
		if crc14(perturbed) == baseCrc {
			t.Errorf("flipping message bit %d did not change CRC (= %#x)", bit, baseCrc)
		}
	}
}

// TestCRC14Matches_ConsistentWithCRC14 ties the verifier to the
// computer: build an info word whose trailing 14 bits ARE the CRC
// over the leading 77, and confirm crc14Matches reports true.
// Trivial round-trip but it pins the two functions together so a
// change to one without the other gets caught.
func TestCRC14Matches_ConsistentWithCRC14(t *testing.T) {
	// Use a non-trivial payload so the CRC is non-zero.
	var info [infoBits]uint8
	for i := 0; i < payloadBits; i += 3 {
		info[i] = 1
	}

	var msg [payloadBits]uint8
	for i := 0; i < payloadBits; i++ {
		msg[i] = info[i]
	}
	c := crc14(msg)
	for i := 0; i < crcBits; i++ {
		info[payloadBits+i] = uint8((c >> uint(crcBits-1-i)) & 1)
	}

	if !crc14Matches(info) {
		t.Error("crc14Matches returned false for a self-consistent info word")
	}

	// Flip the last CRC bit — should now fail.
	info[infoBits-1] ^= 1
	if crc14Matches(info) {
		t.Error("crc14Matches returned true after corrupting the CRC bit")
	}
}

// TestDecode_RandomLLRsDoNotConverge confirms the decoder doesn't
// falsely accept noise. 100 random LLR vectors (each entry uniform
// in ±10) should produce ConvergedCRC=false for all of them — the
// probability of a random noise codeword being both LDPC-valid AND
// passing CRC14 is ~2^-83 * 2^-14, vanishingly small. If any
// converge, BP or CRC is buggy.
func TestDecode_RandomLLRsDoNotConverge(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 100; trial++ {
		var llrs [codewordBits]float64
		for i := range llrs {
			llrs[i] = rng.Float64()*20 - 10 // uniform in [-10, +10]
		}
		_, stats := Decode(llrs)
		if stats.ConvergedCRC {
			t.Errorf("trial %d: random LLRs produced a CRC-valid decode (iters=%d, syndrome=%d)",
				trial, stats.Iterations, stats.FinalSyndromeWeight)
		}
	}
}

// TestDecode_CleanFixture is the end-to-end integration test: feed
// the clean synthetic fixture through candidates → demod → LLRs →
// Decode and verify ConvergedCRC=true. This is the load-bearing
// "the whole pipeline works" check. At least one of the 10 CQ
// signals in the fixture must decode for the test to pass.
//
// The fixture has 10 equally-strong CQs by construction (gen10cq);
// we accept any one of them decoding. A failure here means BP or
// CRC has a real bug — at clean SNR the decode should be trivial.
func TestDecode_CleanFixture(t *testing.T) {
	wavPath := filepath.Join("..", "10cq_clean.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("read wav %s: %v", wavPath, err)
	}
	cands := candidates.Find(data.Samples)
	if len(cands) == 0 {
		t.Fatal("no candidates found in clean fixture — upstream regression")
	}

	// Try each candidate in descending stage-1 score order. The 10
	// true signals occupy the top ranks at clean SNR.
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })

	successes := 0
	for i, c := range cands {
		energies := demod.Demod(data.Samples, c.Freq, c.DT)
		llrs := demod.LLRs(energies)

		// LLRs returns a fixed-size array — copy into ldpc's expected
		// shape. (Both are length 174 by construction; the conversion
		// is just an array-type rebind.)
		var input [codewordBits]float64
		for k := 0; k < codewordBits; k++ {
			input[k] = llrs[k]
		}

		_, stats := Decode(input)
		if stats.ConvergedCRC {
			successes++
			t.Logf("candidate %d (freq=%.2f dt=%.3f) decoded in %d iters", i, c.Freq, c.DT, stats.Iterations)
		} else if i < 15 {
			// Log a few non-successes for diagnostic context.
			t.Logf("candidate %d (freq=%.2f dt=%.3f) FAILED: parity=%v iters=%d syndromeFinal=%d syndromeBest=%d",
				i, c.Freq, c.DT, stats.ConvergedParity, stats.Iterations,
				stats.FinalSyndromeWeight, stats.BestSyndromeWeight)
		}
	}
	if successes == 0 {
		t.Fatalf("zero decodes on clean fixture — BP or CRC is broken (tried %d candidates)", len(cands))
	}
	t.Logf("clean fixture: %d / %d candidates decoded with CRC", successes, len(cands))
}
