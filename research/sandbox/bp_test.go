package sandbox

import (
	"testing"
)

// TestCRC14_AllZero pins the property that CRC14 over 77 zero bits
// is 14 zero bits. This makes the all-zero codeword a valid 91-bit
// info word — convenient for BP synthetic tests.
func TestCRC14_AllZero(t *testing.T) {
	zeros := make([]uint8, LDPCPayloadBits)
	crc := computeCRC14(zeros)
	for i, b := range crc {
		if b != 0 {
			t.Errorf("CRC14(0…0)[%d] = %d, want 0", i, b)
		}
	}
}

// TestCRC14_VerifyAllZeroMessage pins that VerifyCRC14 accepts the
// all-zero 91-bit info word.
func TestCRC14_VerifyAllZeroMessage(t *testing.T) {
	var msg [LDPCInfoBits]uint8
	if !VerifyCRC14(msg) {
		t.Error("VerifyCRC14 rejected the all-zero info word (CRC should be 0)")
	}
}

// TestBPDecode_StrongAllZeroLLRsDecodeIteration0 covers acceptance
// criterion #1: a synthetic valid codeword (all zeros, which is always
// in the LDPC null space) with strong positive LLRs decodes on the
// first syndrome check, before any BP messages are exchanged.
func TestBPDecode_StrongAllZeroLLRsDecodeIteration0(t *testing.T) {
	var llrs [LDPCCodewordBits]float64
	for i := range llrs {
		llrs[i] = 1e6 // strongly favours bit 0
	}
	res := BPDecode(llrs, DefaultBPOptions())
	if !res.OK {
		t.Fatalf("BPDecode failed on strong all-zero LLRs (syndrome=%v, crc=%v)",
			res.SyndromeClean, res.CRCValid)
	}
	if res.Iterations != 1 {
		t.Errorf("expected decode in 1 iteration, got %d", res.Iterations)
	}
	for i, b := range res.Codeword {
		if b != 0 {
			t.Errorf("codeword[%d] = %d, want 0", i, b)
			break
		}
	}
}

// TestBPDecode_RecoversFromFlippedLLRs covers acceptance criterion #2:
// the same all-zero codeword with a handful of flipped LLR signs (i.e.
// channel-induced "bit errors" in 8 of 174 positions) should be
// corrected by BP within the default iteration budget.
//
// 8 errors is comfortably inside the FT8 LDPC code's correction range
// (the published code corrects up to ~13 random errors with high
// probability under sum-product BP).
func TestBPDecode_RecoversFromFlippedLLRs(t *testing.T) {
	var llrs [LDPCCodewordBits]float64
	for i := range llrs {
		llrs[i] = 5.0
	}
	// Flip 8 LLRs to -5: bits that look like 1 to the decoder but
	// "true" codeword is 0 everywhere.
	flipped := []int{3, 17, 42, 71, 88, 105, 134, 161}
	for _, i := range flipped {
		llrs[i] = -5.0
	}

	res := BPDecode(llrs, DefaultBPOptions())
	if !res.OK {
		t.Fatalf("BPDecode did not recover (syndrome=%v, crc=%v, iters=%d)",
			res.SyndromeClean, res.CRCValid, res.Iterations)
	}
	for i, b := range res.Codeword {
		if b != 0 {
			t.Errorf("codeword[%d] = %d after BP, want 0 (BP failed to correct flip)", i, b)
			break
		}
	}
}

// TestBPDecode_RejectsRandomNoise pins that BP doesn't false-positive
// on uncorrelated noise LLRs: a vector of small random-sign values
// should produce a syndrome-fail (or at worst a CRC-fail) result, not
// OK. This mirrors the "spurious candidate" behaviour seen on real
// fixtures.
func TestBPDecode_RejectsRandomNoise(t *testing.T) {
	var llrs [LDPCCodewordBits]float64
	// Deterministic alternating pattern: weak signal, alternating sign.
	for i := range llrs {
		if i%2 == 0 {
			llrs[i] = 0.3
		} else {
			llrs[i] = -0.3
		}
	}
	res := BPDecode(llrs, DefaultBPOptions())
	if res.OK {
		t.Error("BPDecode accepted uncorrelated noise as a valid codeword (false positive)")
	}
}
