package codec

import (
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

// TestEncodeMessage_Type2_AcceptsToken is the regression pin for
// finding #2: per QEX paper Table 2, the c28 field — used in BOTH
// Type 1 and Type 2 Call slots — accepts "Standard callsign, CQ,
// DE, QRZ, or 22-bit hash". The earlier validateType2Call carve-
// out that rejected tokens contradicted Table 2. Token-bearing
// Type 2 messages like "CQ G4ABC/P JO22" are valid and round-trip.
func TestEncodeMessage_Type2_AcceptsToken(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"cq_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "CQ", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"de_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "DE", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"qrz_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "QRZ", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"cq_dx_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "CQ DX", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"cq_100_in_call1", Message{Type: MessageTypeEUVHFP, Call1: "CQ 100", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bits, err := EncodeMessage(tc.msg)
			if err != nil {
				t.Fatalf("EncodeMessage: %v — Type 2 c28 must accept tokens per QEX Table 2", err)
			}
			got, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if got.Type != MessageTypeEUVHFP {
				t.Errorf("decoded Type = %d, want MessageTypeEUVHFP", got.Type)
			}
			if got.Call1 != tc.msg.Call1 {
				t.Errorf("Call1 round-trip: got %q, want %q", got.Call1, tc.msg.Call1)
			}
			if got.Call2 != tc.msg.Call2 {
				t.Errorf("Call2 round-trip: got %q, want %q", got.Call2, tc.msg.Call2)
			}
			if got.Suffix2 != tc.msg.Suffix2 {
				t.Errorf("Suffix2 round-trip: got %v, want %v", got.Suffix2, tc.msg.Suffix2)
			}
		})
	}
}

// TestEncodeMessage_Type2_RejectsSuffixOnToken pins the encode-side
// guard: validateType2Suffix rejects Suffix1=true when Call1 is a
// token (tokens like CQ / DE / QRZ are not callsigns and cannot
// take /P portable). Symmetric with Type 1's /R-on-token gate.
func TestEncodeMessage_Type2_RejectsSuffixOnToken(t *testing.T) {
	cases := []Message{
		{Type: MessageTypeEUVHFP, Call1: "CQ", Suffix1: true, Call2: "G4ABC", Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "DE", Suffix1: true, Call2: "G4ABC", Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "CQ", Suffix2: true, Grid: "JO22"},
	}
	for _, msg := range cases {
		if _, err := EncodeMessage(msg); err == nil {
			t.Errorf("EncodeMessage(%+v) = nil err, want validateType2Suffix rejection", msg)
		}
	}
}

// TestEncodeMessage_Type2_RejectsBadCallsign verifies the std-callsign
// shape gate. Mirrors the Type 1 rejection set; bad-shape callsigns
// (lowercase, wrong length, bad grid) fail at validateStdCallsign or
// validateG15Slot — these are not tokens, so the validateType1Call
// fallthrough to validateStdCallsign rejects them.
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

// TestDecodeMessage_Type2_AcceptsTokenInWire is the regression pin
// for finding #2: per QEX Table 2, a token (CQ / DE / QRZ / "CQ
// <suffix>") in a Type 2 c28 slot is valid. The decoder must
// recover the token name, not return ErrTokenInGap. This test
// plants the wire bits directly to verify the decode path
// independently of the encoder.
func TestDecodeMessage_Type2_AcceptsTokenInWire(t *testing.T) {
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
	// /P bit for Call2.
	bits[57] = 1
	g15 := Grid4ToG15("JO22")
	for i := range G15Bits {
		bits[59+i] = byte((g15 >> (G15Bits - 1 - i)) & 1)
	}
	// i3 = 2.
	bits[75] = 1

	got, err := DecodeMessage(bits)
	if err != nil {
		t.Fatalf("DecodeMessage(Type 2 wire with token c28): %v — tokens are valid in Type 2 c28 per QEX Table 2", err)
	}
	if got.Type != MessageTypeEUVHFP {
		t.Errorf("decoded Type = %d, want MessageTypeEUVHFP", got.Type)
	}
	if got.Call1 != "CQ" {
		t.Errorf("Call1 = %q, want %q", got.Call1, "CQ")
	}
	if got.Call2 != "G4ABC" {
		t.Errorf("Call2 = %q, want %q", got.Call2, "G4ABC")
	}
	if !got.Suffix2 {
		t.Errorf("Suffix2 = false, want true (/P bit was planted)")
	}
}

// TestDecodeMessage_Type2_HashCallSurfaces verifies finding #2's
// behaviour for Type 2: a c28 in the [nTokens, stdCallOffset) hash
// partition decodes as the "<...>" sentinel with the raw 22-bit
// hash exposed via Hash22Call1 / Hash22Call2. Symmetric with the
// Type 1 path verified in TestDecodeMessage_HashRangeC28SurfacesSentinel.
func TestDecodeMessage_Type2_HashCallSurfaces(t *testing.T) {
	bits := make([]byte, MessageBits)
	// Plant a hash-range c28 (mid-partition) in Call1. nTokens=2063592;
	// 3000000 is safely inside the hash partition. Raw hash22 value
	// is c28 - nTokens = 936408.
	hashC28 := uint32(3000000)
	const wantHash22 = 936408
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

	msg, err := DecodeMessage(bits)
	if err != nil {
		t.Fatalf("DecodeMessage(Type 2 wire with hash c28): unexpected err = %v", err)
	}
	if msg.Call1 != hashedCallSentinel {
		t.Errorf("Call1 = %q, want %q", msg.Call1, hashedCallSentinel)
	}
	if msg.Hash22Call1 != wantHash22 {
		t.Errorf("Hash22Call1 = %d, want %d", msg.Hash22Call1, wantHash22)
	}
	if msg.Call2 != "G4ABC" {
		t.Errorf("Call2 = %q, want G4ABC", msg.Call2)
	}
}

// TestFormatMessage_Type2_BasicShapes pins the human-readable
// output for the Type 2 (EU VHF /P) layouts. Mirrors the Type 1
// formatter test, with /P in place of /R.
func TestFormatMessage_Type2_BasicShapes(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want string
	}{
		{
			"plain_pair_grid",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Grid: "JO22"},
			"G4ABC P9XYZ JO22",
		},
		{
			"plain_pair_no_grid",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ"},
			"G4ABC P9XYZ",
		},
		{
			"suffix1_set",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Grid: "JO22"},
			"G4ABC/P P9XYZ JO22",
		},
		{
			"suffix2_set",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix2: true, Grid: "JO22"},
			"G4ABC P9XYZ/P JO22",
		},
		{
			"both_suffix_set",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Suffix2: true, Grid: "JO22"},
			"G4ABC/P P9XYZ/P JO22",
		},
		{
			"report",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Grid: "-11"},
			"G4ABC P9XYZ -11",
		},
		{
			"ack_report",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", AckBit: true, Grid: "-09"},
			"G4ABC P9XYZ R-09",
		},
		{
			"ack_grid",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", AckBit: true, Grid: "JO22"},
			"G4ABC P9XYZ R JO22",
		},
		{
			"reserved_rr73",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Grid: "RR73"},
			"G4ABC P9XYZ RR73",
		},
		{
			"suffix1_with_ack",
			Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, AckBit: true, Grid: "JO22"},
			"G4ABC/P P9XYZ R JO22",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FormatMessage(tc.msg)
			if err != nil {
				t.Fatalf("FormatMessage: %v", err)
			}
			if got != tc.want {
				t.Errorf("FormatMessage(%+v) = %q, want %q", tc.msg, got, tc.want)
			}
		})
	}
}

// TestFormatMessage_Type2_RendersToken is the regression pin for
// finding #2 at the format layer: token-bearing Type 2 messages
// render with the token verbatim plus the /P-suffix-as-set rendering
// rules. Tokens themselves never carry /P (validateType2Suffix gates
// that combination); the /P appears only on the std-callsign slot.
func TestFormatMessage_Type2_RendersToken(t *testing.T) {
	cases := []struct {
		msg  Message
		want string
	}{
		{Message{Type: MessageTypeEUVHFP, Call1: "CQ", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}, "CQ G4ABC/P JO22"},
		{Message{Type: MessageTypeEUVHFP, Call1: "DE", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}, "DE G4ABC/P JO22"},
		{Message{Type: MessageTypeEUVHFP, Call1: "QRZ", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}, "QRZ G4ABC/P JO22"},
		{Message{Type: MessageTypeEUVHFP, Call1: "CQ DX", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}, "CQ DX G4ABC/P JO22"},
		{Message{Type: MessageTypeEUVHFP, Call1: "CQ 100", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}, "CQ 100 G4ABC/P JO22"},
	}
	for _, tc := range cases {
		got, err := FormatMessage(tc.msg)
		if err != nil {
			t.Errorf("FormatMessage(%+v): %v", tc.msg, err)
			continue
		}
		if got != tc.want {
			t.Errorf("FormatMessage(%+v) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

// TestFormatMessage_Type2_RejectsSuffixOnToken pins the format-side
// guard: validateType2Suffix rejects /P on a token, mirroring the
// encode-side gate.
func TestFormatMessage_Type2_RejectsSuffixOnToken(t *testing.T) {
	cases := []Message{
		{Type: MessageTypeEUVHFP, Call1: "CQ", Suffix1: true, Call2: "G4ABC", Grid: "JO22"},
		{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "CQ", Suffix2: true, Grid: "JO22"},
	}
	for _, msg := range cases {
		if _, err := FormatMessage(msg); err == nil {
			t.Errorf("FormatMessage(%+v) = nil err, want validateType2Suffix rejection", msg)
		}
	}
}

// TestParseMessage_Type2_BasicShapes pins the parse-side classifier
// dispatch. Inputs containing /P route to parseEUVHFP; the recovered
// Message has Type == MessageTypeEUVHFP and the /P-bearing call has
// its Suffix bit set.
func TestParseMessage_Type2_BasicShapes(t *testing.T) {
	cases := []struct {
		in   string
		want Message
	}{
		{"G4ABC/P P9XYZ JO22", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Grid: "JO22"}},
		{"G4ABC P9XYZ/P JO22", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix2: true, Grid: "JO22"}},
		{"G4ABC/P P9XYZ/P JO22", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Suffix2: true, Grid: "JO22"}},
		{"G4ABC/P P9XYZ", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true}},
		{"G4ABC/P P9XYZ -11", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Grid: "-11"}},
		{"G4ABC/P P9XYZ R-09", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, AckBit: true, Grid: "-09"}},
		{"G4ABC/P P9XYZ R JO22", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, AckBit: true, Grid: "JO22"}},
		{"G4ABC/P P9XYZ RR73", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Grid: "RR73"}},
		// Lowercase input is upper-cased upstream — same round-trip
		// guarantee Type 1 provides.
		{"g4abc/p p9xyz jo22", Message{Type: MessageTypeEUVHFP, Call1: "G4ABC", Call2: "P9XYZ", Suffix1: true, Grid: "JO22"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseMessage(tc.in)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseMessage(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseMessage_Type2_RejectsMixedRoverAndPortable pins the
// per-call suffix gate: a single message cannot mix /R and /P on
// its two callsigns — the wire bit slot is single-Type. The
// classifier routes to parseEUVHFP on the /P trigger, and the
// per-call validator rejects any /R it then finds.
func TestParseMessage_Type2_RejectsMixedRoverAndPortable(t *testing.T) {
	cases := []string{
		"K1ABC/R G4ABC/P JO22",
		"K1ABC/P G4ABC/R JO22",
		"K1ABC/R G4ABC/P R JO22",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseMessage(in); err == nil {
				t.Errorf("ParseMessage(%q) = nil err, want mixed /R + /P rejection", in)
			}
		})
	}
}

// TestParseMessage_Type2_AcceptsTokenStart is the regression pin
// for finding #2 at the parse layer: token-prefixed Type 2 inputs
// ("CQ G4ABC/P JO22", "DE G4ABC/P JO22", "QRZ G4ABC/P JO22",
// "CQ DX G4ABC/P JO22") must parse to MessageTypeEUVHFP with the
// token in Call1, the /P-bearing std call in Call2, and the
// matching Suffix2 bit. The classifier routes to parseEUVHFP on
// the /P trigger; parseEUVHFP dispatches by first-token to
// parseCQEUVHFP / parseDirectedEUVHFP / parsePlainEUVHFP.
func TestParseMessage_Type2_AcceptsTokenStart(t *testing.T) {
	cases := []struct {
		in   string
		want Message
	}{
		{"CQ G4ABC/P JO22", Message{Type: MessageTypeEUVHFP, Call1: "CQ", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"CQ DX G4ABC/P JO22", Message{Type: MessageTypeEUVHFP, Call1: "CQ DX", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"CQ 100 G4ABC/P JO22", Message{Type: MessageTypeEUVHFP, Call1: "CQ 100", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"DE G4ABC/P JO22", Message{Type: MessageTypeEUVHFP, Call1: "DE", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
		{"QRZ G4ABC/P JO22", Message{Type: MessageTypeEUVHFP, Call1: "QRZ", Call2: "G4ABC", Suffix2: true, Grid: "JO22"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseMessage(tc.in)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseMessage(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// TestType2_TextRoundTrip verifies the full text-layer loop:
// ParseMessage → EncodeMessage → DecodeMessage → FormatMessage
// returns the original input. Each transition is independently
// tested elsewhere; this test is the integration point.
//
// Token-prefixed cases ("CQ ... /P ...") were added with finding
// #2 — QEX Table 2 c28 accepts tokens in Type 2, so token-bearing
// Type 2 messages must round-trip end-to-end.
func TestType2_TextRoundTrip(t *testing.T) {
	cases := []string{
		"G4ABC/P P9XYZ JO22",
		"G4ABC P9XYZ/P JO22",
		"G4ABC/P P9XYZ/P JO22",
		"G4ABC/P P9XYZ",
		"G4ABC/P P9XYZ -11",
		"G4ABC/P P9XYZ R-09",
		"G4ABC/P P9XYZ R JO22",
		"G4ABC/P P9XYZ RR73",
		"G4ABC/P P9XYZ/P R IO91",
		// Token-prefixed (finding #2)
		"CQ G4ABC/P JO22",
		"CQ DX G4ABC/P JO22",
		"CQ 100 G4ABC/P JO22",
		"DE G4ABC/P JO22",
		"QRZ G4ABC/P JO22",
		"CQ G4ABC/P -11",
		"CQ G4ABC/P R JO22",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			parsed, err := ParseMessage(in)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", in, err)
			}
			bits, err := EncodeMessage(parsed)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			decoded, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			got, err := FormatMessage(decoded)
			if err != nil {
				t.Fatalf("FormatMessage: %v", err)
			}
			if got != in {
				t.Errorf("round-trip(%q) = %q, want %q", in, got, in)
			}
		})
	}
}
