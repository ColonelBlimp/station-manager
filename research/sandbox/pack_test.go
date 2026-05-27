package sandbox

import (
	"testing"
)

// TestPackUnpackRoundTrip_Type1 pins that Pack77 + Unpack77 are
// inverses: packing "CQ K1JT FN20" then unpacking produces the same
// string. Exercises the c28 / g15 forward and reverse paths
// end-to-end on the messages we use for fixture construction.
func TestPackUnpackRoundTrip_Type1(t *testing.T) {
	cases := []struct {
		call1, call2, grid string
		want               string
	}{
		{"CQ", "K1JT", "FN20", "CQ K1JT FN20"},
		{"CQ", "W1AW", "FN31", "CQ W1AW FN31"},
		{"CQ", "OH8X", "KP43", "CQ OH8X KP43"},
		{"CQ", "7Q5MLV", "KH71", "CQ 7Q5MLV KH71"},
		{"CQ", "G3XTT", "IO91", "CQ G3XTT IO91"},
		{"K1JT", "W1AW", "FN31", "K1JT W1AW FN31"},
		{"DE", "K1JT", "FN20", "DE K1JT FN20"},
	}
	for _, tc := range cases {
		payload, err := PackType1(tc.call1, tc.call2, tc.grid)
		if err != nil {
			t.Errorf("PackType1(%q, %q, %q) error: %v", tc.call1, tc.call2, tc.grid, err)
			continue
		}
		res := Unpack77(payload)
		if !res.OK {
			t.Errorf("Unpack77 of packed %q: not OK (detail=%q)", tc.want, res.Detail)
			continue
		}
		if res.Text != tc.want {
			t.Errorf("roundtrip: pack(%q, %q, %q) → unpack → %q, want %q",
				tc.call1, tc.call2, tc.grid, res.Text, tc.want)
		}
	}
}

// TestPackUnpackRoundTrip_CodewordAndDecode pins that the full
// fixture-generation chain (Pack77 → CRC14 → EncodeLDPC →
// CodewordToTones → SoftLLRs of clean tone observations →
// BPDecode → Unpack77) round-trips end-to-end. This is the
// invariant the fixture generator needs: a constructed signal
// at strong LLR magnitude must decode back to the original text.
func TestPackUnpackRoundTrip_CodewordAndDecode(t *testing.T) {
	payload, err := PackType1("CQ", "K1JT", "FN20")
	if err != nil {
		t.Fatal(err)
	}
	info := PayloadToInfo91(payload)
	cw := EncodeLDPC(info)
	tones := CodewordToTones(cw)

	// Build strong LLRs from the tone sequence: each data symbol's
	// expected bits get a strong positive (favouring 0) or strong
	// negative (favouring 1) signal.
	var llrs [LDPCCodewordBits]float64
	for d := 0; d < 58; d++ {
		sym := dataSymbolIndices[d]
		tone := tones[sym]
		bits := inverseGrayMap[tone]
		for bitPos := 2; bitPos >= 0; bitPos-- {
			cbi := 3*d + (2 - bitPos)
			if (bits>>bitPos)&1 == 0 {
				llrs[cbi] = 5.0
			} else {
				llrs[cbi] = -5.0
			}
		}
	}

	br := BPDecode(llrs, DefaultBPOptions())
	if !br.OK {
		t.Fatalf("BPDecode on synthesised LLRs: not OK (method=%q)", br.DecodeMethod)
	}
	var msg [LDPCPayloadBits]uint8
	copy(msg[:], br.Message91[:LDPCPayloadBits])
	res := Unpack77(msg)
	if !res.OK || res.Text != "CQ K1JT FN20" {
		t.Errorf("end-to-end: decoded = %q (ok=%v), want %q", res.Text, res.OK, "CQ K1JT FN20")
	}
}
