package codec

import (
	"errors"
	"slices"
	"testing"
)

// TestEncodeMessage_Type2_LayoutMatchesQEXTable1 verifies that
// EncodeMessage assembles a Type 2 (EU VHF /P) body in the exact bit
// layout QEX paper Table 1 specifies:
//
//	c28(Call1) | p1(Suffix1) | c28(Call2) | p1(Suffix2) | R1(AckBit) | g15(Grid) | i3=2
//	    28          1             28           1             1           15        3
//
// Same wire shape as Type 1 — the test parallels the Type 1 layout
// test so a regression in either path surfaces here.
func TestEncodeMessage_Type2_LayoutMatchesQEXTable1(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			name: "g4abc_calls_p9xyz_jo22",
			msg: Message{
				Type:  MessageTypeEUVHFP,
				Call1: "G4ABC",
				Call2: "P9XYZ",
				Grid:  "JO22",
			},
		},
		{
			name: "suffix1_set",
			msg: Message{
				Type:    MessageTypeEUVHFP,
				Call1:   "G4ABC",
				Call2:   "P9XYZ",
				Suffix1: true,
				Grid:    "JO22",
			},
		},
		{
			name: "suffix2_set",
			msg: Message{
				Type:    MessageTypeEUVHFP,
				Call1:   "G4ABC",
				Call2:   "P9XYZ",
				Suffix2: true,
				Grid:    "JO22",
			},
		},
		{
			name: "ack_set",
			msg: Message{
				Type:   MessageTypeEUVHFP,
				Call1:  "G4ABC",
				Call2:  "P9XYZ",
				AckBit: true,
				Grid:   "JO22",
			},
		},
		{
			name: "all_suffix_and_ack_set",
			msg: Message{
				Type:    MessageTypeEUVHFP,
				Call1:   "G4ABC",
				Call2:   "P9XYZ",
				Suffix1: true,
				Suffix2: true,
				AckBit:  true,
				Grid:    "JO22",
			},
		},
		{
			name: "report_minus_11",
			msg: Message{
				Type:  MessageTypeEUVHFP,
				Call1: "G4ABC",
				Call2: "P9XYZ",
				Grid:  "-11",
			},
		},
		{
			name: "reserved_rr73",
			msg: Message{
				Type:  MessageTypeEUVHFP,
				Call1: "G4ABC",
				Call2: "P9XYZ",
				Grid:  "RR73",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeMessage(tc.msg)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			if len(got) != MessageBits {
				t.Fatalf("got %d bits, want %d", len(got), MessageBits)
			}

			want := make([]byte, 0, MessageBits)
			c1 := CallsignC28(tc.msg.Call1)
			for i := range CallsignBits {
				want = append(want, byte((c1>>(CallsignBits-1-i))&1))
			}
			want = append(want, boolBitByte(tc.msg.Suffix1))
			c2 := CallsignC28(tc.msg.Call2)
			for i := range CallsignBits {
				want = append(want, byte((c2>>(CallsignBits-1-i))&1))
			}
			want = append(want, boolBitByte(tc.msg.Suffix2))
			want = append(want, boolBitByte(tc.msg.AckBit))
			g15 := Grid4ToG15(tc.msg.Grid)
			for i := range G15Bits {
				want = append(want, byte((g15>>(G15Bits-1-i))&1))
			}
			// i3 = 2 → bits 010 (MSB-first).
			want = append(want, 0, 1, 0)

			if !slices.Equal(got, want) {
				t.Errorf("layout mismatch\n got=%v\nwant=%v", got, want)
			}
		})
	}
}

// TestEncodeMessage_Type2_I3TagIs2 pins the i3 tag at the wire's
// lowest 3 bits to the value 2 (010 MSB-first). A regression that
// flipped Type 1's encoder to write Type 2's tag (or vice versa)
// would land here.
func TestEncodeMessage_Type2_I3TagIs2(t *testing.T) {
	got, err := EncodeMessage(Message{
		Type:  MessageTypeEUVHFP,
		Call1: "G4ABC",
		Call2: "P9XYZ",
		Grid:  "JO22",
	})
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	// i3 occupies positions 74..76, MSB-first → 0, 1, 0.
	if got[74] != 0 || got[75] != 1 || got[76] != 0 {
		t.Errorf("i3 bits at 74..76 = %d %d %d, want 0 1 0 (i3=2)", got[74], got[75], got[76])
	}
}

// TestEncodeMessage_Type2_RejectsToken verifies the validateType2Call
// gate: QEX Table 7 tokens (CQ / DE / QRZ / "CQ <suffix>") are NOT
// valid in Type 2's c28 slots. The encoder rejects them rather than
// silently routing through TokenToC28 (which would produce wire output
// indistinguishable from Type 1 except for the i3 tag).
func TestEncodeMessage_Type2_RejectsToken(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"cq_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "CQ", Call2: "G4ABC", Grid: "JO22"}},
		{"de_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "DE", Call2: "G4ABC", Grid: "JO22"}},
		{"qrz_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "QRZ", Call2: "G4ABC", Grid: "JO22"}},
		{"cq_dx_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "CQ DX", Call2: "G4ABC", Grid: "JO22"}},
		{"cq_100_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "CQ 100", Call2: "G4ABC", Grid: "JO22"}},
		{"cq_in_call2", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "CQ", Grid: "JO22"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EncodeMessage(tc.msg); err == nil {
				t.Errorf("EncodeMessage(%+v) = nil err, want validation error (token in Type 2)", tc.msg)
			}
		})
	}
}

// TestEncodeMessage_Type2_RejectsBadCallsign verifies the std-callsign
// shape gate. Mirrors the Type 1 rejection set; Type 2 is stricter
// (no token escape), so the same bad-shape inputs fail here as well.
func TestEncodeMessage_Type2_RejectsBadCallsign(t *testing.T) {
	cases := []Message{
		{Type: MessageTypeEUVHFP, Call1: "g4abc", Call2: "P9XYZ", Grid: "JO22"},   // lowercase
		{Type: MessageTypeEUVHFP, Call1: "AB", Call2: "P9XYZ", Grid: "JO22"},      // too short
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "ABCDEFG", Grid: "JO22"}, // too long
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Grid: "jo22"},   // bad grid case
	}
	for _, msg := range cases {
		if _, err := EncodeMessage(msg); err == nil {
			t.Errorf("EncodeMessage(%+v) = nil err, want validation error", msg)
		}
	}
}

// TestType2_RoundTrip pins Encode→Decode equivalence for the Type 2
// shapes. Every field that the encoder accepts must come back through
// the decoder with the same value (modulo G15ToGrid4's canonicalisation
// of equivalent signed-report forms, same as Type 1).
func TestType2_RoundTrip(t *testing.T) {
	cases := []Message{
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix2: true, Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Suffix2: true, Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", AckBit: true, Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Suffix2: true, AckBit: true, Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Grid: "-11"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Grid: "RR73"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ"},
	}
	for _, in := range cases {
		t.Run(in.Call1+"_"+in.Call2+"_"+in.Grid, func(t *testing.T) {
			bits, err := EncodeMessage(in)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			got, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if got.Type != in.Type {
				t.Errorf("Type mismatch: got %d, want %d", got.Type, in.Type)
			}
			if got.Call1 != in.Call1 || got.Call2 != in.Call2 {
				t.Errorf("calls: got (%q, %q), want (%q, %q)", got.Call1, got.Call2, in.Call1, in.Call2)
			}
			if got.Suffix1 != in.Suffix1 || got.Suffix2 != in.Suffix2 {
				t.Errorf("suffix bits: got (%v, %v), want (%v, %v)", got.Suffix1, got.Suffix2, in.Suffix1, in.Suffix2)
			}
			if got.AckBit != in.AckBit {
				t.Errorf("AckBit: got %v, want %v", got.AckBit, in.AckBit)
			}
			if got.Grid != in.Grid {
				t.Errorf("Grid: got %q, want %q", got.Grid, in.Grid)
			}
		})
	}
}

// TestDecodeMessage_Type2_RejectsTokenInWire pins the asymmetry
// versus Type 1: Type 1's decoder accepts token-range c28 values
// bit-faithfully (the bit pattern IS legal Type 1); Type 2's decoder
// rejects them as ErrTokenInGap because tokens are not legal in
// Type 2's c28 partition per QEX Table 7. A token-range c28 on a
// Type 2 wire is a spec-violating value (post-LDPC corruption or a
// remote encoder bug).
func TestDecodeMessage_Type2_RejectsTokenInWire(t *testing.T) {
	// Manually plant a token c28 (CQ = 2) in Call1 of a Type 2 body.
	bits := make([]byte, MessageBits)
	writeC28 := func(offset int, c28 uint32) {
		for i := range CallsignBits {
			bits[offset+i] = byte((c28 >> (CallsignBits - 1 - i)) & 1)
		}
	}
	tokenCQ, ok := TokenToC28("CQ")
	if !ok {
		t.Fatal("TokenToC28(\"CQ\") returned !ok — Layer 1 regression")
	}
	writeC28(0, tokenCQ)               // Call1 = CQ (token)
	writeC28(29, CallsignC28("G4ABC")) // Call2 = G4ABC
	g15 := Grid4ToG15("JO22")
	for i := range G15Bits {
		bits[59+i] = byte((g15 >> (G15Bits - 1 - i)) & 1)
	}
	// i3 = 2.
	bits[75] = 1

	_, err := DecodeMessage(bits)
	if !errors.Is(err, ErrTokenInGap) {
		t.Errorf("DecodeMessage(Type 2 wire with token c28) err=%v, want ErrTokenInGap", err)
	}
}

// TestDecodeMessage_Type2_HashCallSurfaces verifies the hash-partition
// path: a Type 2 c28 in the [nTokens, stdCallOffset) range surfaces
// ErrCallsignNeedsHashLookup just like Type 1. The FT8 service layer's
// hash table resolves the actual callsign post-decode.
func TestDecodeMessage_Type2_HashCallSurfaces(t *testing.T) {
	bits := make([]byte, MessageBits)
	// Plant a hash-range c28 (mid-partition) in Call1. The hash range
	// is [nTokens, stdCallOffset) per callsign.go; nTokens = 2063592,
	// stdCallOffset = nTokens + 2^22, so 3000000 is safely inside.
	hashC28 := uint32(3000000)
	for i := range CallsignBits {
		bits[i] = byte((hashC28 >> (CallsignBits - 1 - i)) & 1)
	}
	for i := range CallsignBits {
		bits[29+i] = byte((CallsignC28("G4ABC") >> (CallsignBits - 1 - i)) & 1)
	}
	g15 := Grid4ToG15("JO22")
	for i := range G15Bits {
		bits[59+i] = byte((g15 >> (G15Bits - 1 - i)) & 1)
	}
	bits[75] = 1 // i3 = 2

	_, err := DecodeMessage(bits)
	if !errors.Is(err, ErrCallsignNeedsHashLookup) {
		t.Errorf("DecodeMessage(Type 2 wire with hash c28) err=%v, want ErrCallsignNeedsHashLookup", err)
	}
}
