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

// TestPackUnpackRoundTrip_Type4 pins that Pack77+Unpack77 are
// inverses for Type 4 messages too: with the addressee pre-registered
// in the hash table, the round-tripped text equals the original.
func TestPackUnpackRoundTrip_Type4(t *testing.T) {
	ht := NewCallsignHashTable()
	ht.Add("W1AW") // addressee

	cases := []struct {
		addressee, sender string
		report            PackType4Report
		swap              bool
		want              string
	}{
		{"W1AW", "PJ4/K1ABC", PackType4_73, false, "W1AW PJ4/K1ABC 73"},
		{"W1AW", "YW18FIFA", PackType4RR73, false, "W1AW YW18FIFA RR73"},
		{"W1AW", "OH/W1AW", PackType4Blank, true, "OH/W1AW W1AW"},
		{"W1AW", "PJ4/K1ABC", PackType4RRR, false, "W1AW PJ4/K1ABC RRR"},
	}
	for _, tc := range cases {
		payload, err := PackType4(tc.addressee, tc.sender, tc.report, tc.swap)
		if err != nil {
			t.Errorf("PackType4(%q, %q, %d, %v) error: %v",
				tc.addressee, tc.sender, tc.report, tc.swap, err)
			continue
		}
		res := Unpack77WithHashes(payload, ht)
		if !res.OK {
			t.Errorf("Unpack77WithHashes of packed %q: not OK (detail=%q)",
				tc.want, res.Detail)
			continue
		}
		if res.Text != tc.want {
			t.Errorf("Type4 roundtrip: pack(%q, %q, %d, %v) → %q, want %q",
				tc.addressee, tc.sender, tc.report, tc.swap, res.Text, tc.want)
		}
	}
}

// TestHashCallsign_Consistent pins that hashing is deterministic and
// that the three returned widths match each other (h10 is the top
// 10 of the same product, h12 the top 12, h22 the top 22).
func TestHashCallsign_Consistent(t *testing.T) {
	calls := []string{"K1JT", "W1AW", "PJ4/K1ABC", "YW18FIFA", "OH/W1AW"}
	for _, c := range calls {
		h10a, h12a, h22a := HashCallsign(c)
		h10b, h12b, h22b := HashCallsign(c)
		if h10a != h10b || h12a != h12b || h22a != h22b {
			t.Errorf("HashCallsign(%q) not deterministic", c)
		}
		// Cross-check: h10 should be the top 10 bits of h22 shifted
		// (h22 = top 22, h10 = top 10, so h10 == h22 >> 12).
		if h10a != h22a>>12 {
			t.Errorf("HashCallsign(%q): h10=%d ≠ h22>>12 = %d", c, h10a, h22a>>12)
		}
		// h12 == h22 >> 10 (top 12 vs top 22, diff = 10).
		if h12a != h22a>>10 {
			t.Errorf("HashCallsign(%q): h12=%d ≠ h22>>10 = %d", c, h12a, h22a>>10)
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
