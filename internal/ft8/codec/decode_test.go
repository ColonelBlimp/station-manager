package codec

import (
	"errors"
	"slices"
	"testing"
)

// ---- C28ToCallsign (Layer 1 inverse) ---------------------------------------

func TestC28ToCallsign_Partitions(t *testing.T) {
	cases := []struct {
		name     string
		c28      uint32
		wantKind C28Kind
	}{
		{"min_token", 0, C28KindToken},
		{"mid_token", 100, C28KindToken},
		{"max_token", nTokens - 1, C28KindToken},
		{"min_hash", nTokens, C28KindHash22},
		{"k1jt_via_callsign_c28_lands_in_hash", 6040944, C28KindHash22},
		{"k1jt_via_hashed_c28", 4159881, C28KindHash22},
		{"max_hash", stdCallOffset - 1, C28KindHash22},
		{"min_std", stdCallOffset, C28KindStdCall},
		{"g4abc", 9486694, C28KindStdCall},
		{"max_std", (1 << CallsignBits) - 1, C28KindStdCall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, gotKind := C28ToCallsign(tc.c28)
			if gotKind != tc.wantKind {
				t.Errorf("C28ToCallsign(%d).kind = %v, want %v", tc.c28, gotKind, tc.wantKind)
			}
		})
	}
}

// TestC28ToCallsign_StdCall_RoundTrip verifies that CallsignC28 →
// C28ToCallsign recovers the original callsign exactly, for the
// long-format std-call shapes (5-char-1-prefix and 6-char-2-prefix).
func TestC28ToCallsign_StdCall_RoundTrip(t *testing.T) {
	cases := []string{
		// 5-char-1-prefix: [letter][digit][letter]{3}
		"G4ABC", "K1ABC", "W1ABC", "F5RXL", "K1JTR",
		// 6-char-2-prefix: [alnum]{2}≥1-letter [digit] [letter]{3}
		"AB1CDE", "2E0XYZ", "9V1BCD", "KH1ABC", "JA1XYZ", "ZZ0ZZZ",
		// NB: VK7MO is 5-char-2-prefix (VK + 7 + MO) — routes
		// through HashedCallC28, not CallsignC28; covered by the
		// stdCallToC28 routing tests, not here.
	}
	for _, call := range cases {
		t.Run(call, func(t *testing.T) {
			c28 := CallsignC28(call)
			got, kind := C28ToCallsign(c28)
			if kind != C28KindStdCall {
				t.Fatalf("C28ToCallsign(%d).kind = %v, want C28KindStdCall", c28, kind)
			}
			if got != call {
				t.Errorf("C28ToCallsign(%d) = %q, want %q", c28, got, call)
			}
		})
	}
}

// TestC28ToCallsign_StdCall_PartitionBoundary covers c28 values at
// the std-call partition edges to verify the divmod doesn't drift.
// Boundary tuples don't correspond to real callsigns — the test
// pins the arithmetic, not the realism.
func TestC28ToCallsign_StdCall_PartitionBoundary(t *testing.T) {
	// At stdCallOffset all indices are 0: pos1=' ', pos2='0',
	// pos3='0', pos4..6=' '. Padded " 00   "; TrimLeft strips the
	// one leading space; result has trailing spaces (they're valid
	// alphabet entries at pos4..6, not padding).
	got, kind := C28ToCallsign(stdCallOffset)
	if kind != C28KindStdCall {
		t.Errorf("c28=stdCallOffset kind=%v, want StdCall", kind)
	}
	if got != "00   " {
		t.Errorf("c28=stdCallOffset call=%q, want %q", got, "00   ")
	}

	// At max c28 (= 2^28-1) all indices are alphabet-max:
	// pos1[36]='Z', pos2[35]='Z', pos3[9]='9', pos4..6[26]='Z'.
	// Padded "ZZ9ZZZ" — no leading spaces.
	got, kind = C28ToCallsign((1 << CallsignBits) - 1)
	if kind != C28KindStdCall {
		t.Errorf("c28=2^28-1 kind=%v, want StdCall", kind)
	}
	if got != "ZZ9ZZZ" {
		t.Errorf("c28=2^28-1 call=%q, want %q", got, "ZZ9ZZZ")
	}
}

// TestC28ToCallsign_Hash_Token_EmptyString verifies the
// non-StdCall partitions return ("", kind) per contract.
func TestC28ToCallsign_Hash_Token_EmptyString(t *testing.T) {
	for c28 := uint32(0); c28 < nTokens; c28 += 100000 {
		if got, _ := C28ToCallsign(c28); got != "" {
			t.Errorf("Token partition c28=%d call=%q, want empty", c28, got)
		}
	}
	for c28 := uint32(nTokens); c28 < stdCallOffset; c28 += 100000 {
		if got, _ := C28ToCallsign(c28); got != "" {
			t.Errorf("Hash partition c28=%d call=%q, want empty", c28, got)
		}
	}
}

// ---- G15ToGrid4 (Layer 1 inverse) ------------------------------------------

func TestG15ToGrid4_Partitions(t *testing.T) {
	cases := []struct {
		name     string
		g15      uint16
		wantStr  string
		wantKind G15Kind
	}{
		{"grid_aa00", 0, "AA00", G15KindGrid4},
		{"grid_fn20", 10320, "FN20", G15KindGrid4},
		{"grid_io91", uint16(8*18*100 + 14*100 + 9*10 + 1), "IO91", G15KindGrid4},
		{"grid_rr99", maxGrid4 - 1, "RR99", G15KindGrid4},
		{"reserved_blank", g15Empty, "", G15KindReserved},
		{"reserved_rrr", g15RRR, "RRR", G15KindReserved},
		{"reserved_rr73", g15RR73, "RR73", G15KindReserved},
		{"reserved_73", g15_73, "73", G15KindReserved},
		{"report_minus_30", uint16(maxGrid4 + g15ReportBias - 30), "-30", G15KindReport},
		{"report_minus_11", uint16(maxGrid4 + g15ReportBias - 11), "-11", G15KindReport},
		{"report_zero", uint16(maxGrid4 + g15ReportBias), "+00", G15KindReport},
		{"report_plus_2", uint16(maxGrid4 + g15ReportBias + 2), "+02", G15KindReport},
		{"report_plus_99", uint16(maxGrid4 + g15ReportBias + 99), "+99", G15KindReport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStr, gotKind := G15ToGrid4(tc.g15)
			if gotKind != tc.wantKind {
				t.Errorf("G15ToGrid4(%d).kind = %v, want %v", tc.g15, gotKind, tc.wantKind)
			}
			if gotStr != tc.wantStr {
				t.Errorf("G15ToGrid4(%d).str = %q, want %q", tc.g15, gotStr, tc.wantStr)
			}
		})
	}
}

// TestG15ToGrid4_RoundTrip verifies Grid4ToG15 → G15ToGrid4
// recovers the original for grids, reserved tokens, and reports.
func TestG15ToGrid4_RoundTrip(t *testing.T) {
	cases := []string{
		"AA00", "FN20", "IO91", "JO22", "RR99", "BB55",
		"", "RRR", "RR73", "73",
		"+00", "+02", "-11", "-30", "+99",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			g15 := Grid4ToG15(in)
			got, kind := G15ToGrid4(g15)
			if got != in {
				t.Errorf("Grid4ToG15(%q) → G15ToGrid4(%d) = %q, want %q (kind=%v)", in, g15, got, in, kind)
			}
		})
	}
}

// TestG15ToGrid4_PanicsOutOfRange covers the upper-bit guard.
func TestG15ToGrid4_PanicsOutOfRange(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("G15ToGrid4(0x8000) should panic, did not")
		}
	}()
	G15ToGrid4(0x8000)
}

// ---- stdCallToC28 (Phase 2C encoder fix) -----------------------------------

// TestStdCallToC28_RoutingBasedOnShape pins down the routing
// decision: long-format calls go through CallsignC28, others
// through HashedCallC28. The choice is load-bearing for
// round-trippability and for hash-table lookup at the receiver.
func TestStdCallToC28_RoutingBasedOnShape(t *testing.T) {
	cases := []struct {
		name     string
		call     string
		wantPath string // "callsign" or "hash"
	}{
		// Long-format (5-char-1-prefix): routes through CallsignC28.
		{"5char_1prefix_G4ABC", "G4ABC", "callsign"},
		{"5char_1prefix_K1JTR", "K1JTR", "callsign"},
		{"5char_1prefix_W9XYZ", "W9XYZ", "callsign"},
		// Long-format (6-char-2-prefix): routes through CallsignC28.
		{"6char_2prefix_KH1ABC", "KH1ABC", "callsign"},
		{"6char_2prefix_2E0XYZ", "2E0XYZ", "callsign"},
		{"6char_2prefix_9V1BCD", "9V1BCD", "callsign"},
		// Short formats: route through HashedCallC28 — produce the
		// real 22-bit hash + nTokens, not the negative-index
		// arithmetic byproduct of CallsignC28.
		{"3char_G3X", "G3X", "hash"},
		{"4char_1prefix_K1JT", "K1JT", "hash"},
		{"4char_2prefix_AB1C", "AB1C", "hash"},
		{"5char_2prefix_AB1CD", "AB1CD", "hash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stdCallToC28(tc.call)
			switch tc.wantPath {
			case "callsign":
				want := CallsignC28(tc.call)
				if got != want {
					t.Errorf("stdCallToC28(%q) = %d, want CallsignC28 result %d", tc.call, got, want)
				}
				if got < stdCallOffset {
					t.Errorf("stdCallToC28(%q) = %d should be in std-call range [stdCallOffset, 2^28)", tc.call, got)
				}
			case "hash":
				want := HashedCallC28(tc.call)
				if got != want {
					t.Errorf("stdCallToC28(%q) = %d, want HashedCallC28 result %d", tc.call, got, want)
				}
				if got < nTokens || got >= stdCallOffset {
					t.Errorf("stdCallToC28(%q) = %d should be in hash range [nTokens, stdCallOffset)", tc.call, got)
				}
			}
		})
	}
}

// TestStdCallToC28_K1JTUsesActualHash pins the specific failure mode
// that motivated the Phase 2C encoder fix: K1JT MUST NOT encode via
// CallsignC28's negative-index byproduct (6040944), which would be
// indistinguishable from A1JT, B1JT, etc. The correct encoding is
// HashedCallC28("K1JT") = 4159881 — K1JT's actual 22-bit hash with
// the nTokens bias.
func TestStdCallToC28_K1JTUsesActualHash(t *testing.T) {
	got := stdCallToC28("K1JT")
	if got != 4159881 {
		t.Errorf("stdCallToC28(%q) = %d, want %d (HashedCallC28 path)", "K1JT", got, 4159881)
	}
	if got == 6040944 {
		t.Errorf("stdCallToC28(%q) = 6040944 — that's CallsignC28's negative-index byproduct, NOT K1JT's real hash. Encoder fix regressed.", "K1JT")
	}
}

// ---- DecodeMessage (Layer 2) -----------------------------------------------

// TestDecodeMessage_RoundTripLongCalls is the headline Phase 2C
// test: every long-format std-call shape round-trips bit-for-bit
// through EncodeMessage → DecodeMessage → EncodeMessage. Both
// directions are exercised under realistic Message values.
func TestDecodeMessage_RoundTripLongCalls(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			"G4ABC_calls_K1ABC_FN20",
			Message{
				Type:  MessageTypeStd,
				Call1: "G4ABC",
				Call2: "K1ABC",
				Grid:  "FN20",
			},
		},
		{
			"6char_2prefix_pair",
			Message{
				Type:  MessageTypeStd,
				Call1: "AB1CDE",
				Call2: "2E0XYZ",
				Grid:  "IO91",
			},
		},
		{
			"rover_and_ack_set",
			Message{
				Type:   MessageTypeStd,
				Call1:  "G4ABC",
				Call2:  "K1ABC",
				Rover1: true,
				Rover2: true,
				AckBit: true,
				Grid:   "FN20",
			},
		},
		{
			"report_grid_slot",
			Message{
				Type:  MessageTypeStd,
				Call1: "G4ABC",
				Call2: "K1ABC",
				Grid:  "-11",
			},
		},
		{
			"reserved_rr73_slot",
			Message{
				Type:  MessageTypeStd,
				Call1: "G4ABC",
				Call2: "K1ABC",
				Grid:  "RR73",
			},
		},
		{
			"blank_grid_slot",
			Message{
				Type:  MessageTypeStd,
				Call1: "G4ABC",
				Call2: "K1ABC",
				Grid:  "",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bits, err := EncodeMessage(tc.msg)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			decoded, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if decoded != tc.msg {
				t.Errorf("round-trip mismatch:\n got=%+v\nwant=%+v", decoded, tc.msg)
			}
			// Triple-check: re-encode the decoded message and assert
			// the bits match. Catches a class of bug where the
			// decoder maps differently-encoded bits to the same
			// Message struct value.
			rebits, err := EncodeMessage(decoded)
			if err != nil {
				t.Fatalf("EncodeMessage(decoded): %v", err)
			}
			if !slices.Equal(rebits, bits) {
				t.Errorf("re-encode mismatch:\n got=%v\nwant=%v", rebits, bits)
			}
		})
	}
}

// TestDecodeMessage_ShortCallReturnsHashError documents the Phase
// 2C limitation: a Type 1 message with a short callsign (encoded
// via HashedCallC28) decodes to ErrCallsignNeedsHashLookup. The
// FT8 service layer (Phase 4) will catch this sentinel and resolve
// the call via its running hash table.
func TestDecodeMessage_ShortCallReturnsHashError(t *testing.T) {
	msg := Message{
		Type:  MessageTypeStd,
		Call1: "K1JT", // 4-char short call → HashedCallC28 → hash range
		Call2: "G4ABC",
		Grid:  "FN20",
	}
	bits, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	_, err = DecodeMessage(bits)
	if !errors.Is(err, ErrCallsignNeedsHashLookup) {
		t.Errorf("DecodeMessage(short-call msg) err=%v, want ErrCallsignNeedsHashLookup", err)
	}
}

// TestDecodeMessage_RejectsShortBody verifies the length guard.
func TestDecodeMessage_RejectsShortBody(t *testing.T) {
	for _, n := range []int{0, 1, 76, 78, 91, 174} {
		if _, err := DecodeMessage(make([]byte, n)); !errors.Is(err, ErrShortBody) {
			t.Errorf("DecodeMessage(len=%d) err=%v, want ErrShortBody", n, err)
		}
	}
}

// TestDecodeMessage_RejectsUnknownI3 verifies the i3-tag dispatch.
// Phase 2C only implements i3=1; the other tags return the
// unsupported-type sentinel.
func TestDecodeMessage_RejectsUnknownI3(t *testing.T) {
	for i3 := 0; i3 < 8; i3++ {
		if i3 == i3Std {
			continue
		}
		bits := make([]byte, MessageBits)
		// Write i3 into the lowest 3 bits (positions 74..76).
		bits[74] = byte((i3 >> 2) & 1)
		bits[75] = byte((i3 >> 1) & 1)
		bits[76] = byte(i3 & 1)
		if _, err := DecodeMessage(bits); !errors.Is(err, ErrUnknownMessageType) {
			t.Errorf("DecodeMessage(i3=%d) err=%v, want ErrUnknownMessageType", i3, err)
		}
	}
}

// TestDecodeMessage_TokenReturnsTokenError covers the C28KindToken
// path: a Type 1 message with c28 in the token range surfaces
// ErrCallsignIsToken. Phase 2D adds token decoding. Both slots are
// exercised so a future regression that swaps the c28 reads in
// decodeStd surfaces here rather than only when Call1 has a token.
func TestDecodeMessage_TokenReturnsTokenError(t *testing.T) {
	g4abc := CallsignC28("G4ABC")

	writeC28 := func(bits []byte, offset int, c28 uint32) {
		for i := range CallsignBits {
			bits[offset+i] = byte((c28 >> (CallsignBits - 1 - i)) & 1)
		}
	}

	cases := []struct {
		name      string
		tokenSlot int // 0 for Call1, 1 for Call2
	}{
		{"token_in_Call1", 0},
		{"token_in_Call2", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bits := make([]byte, MessageBits)
			if tc.tokenSlot == 0 {
				writeC28(bits, 0, 2) // CQ token
				writeC28(bits, 29, g4abc)
			} else {
				writeC28(bits, 0, g4abc)
				writeC28(bits, 29, 2)
			}
			// i3 = 1 at bits[74..76]
			bits[76] = 1
			if _, err := DecodeMessage(bits); !errors.Is(err, ErrCallsignIsToken) {
				t.Errorf("DecodeMessage(%s) err=%v, want ErrCallsignIsToken", tc.name, err)
			}
		})
	}
}

// ---- Internal helpers ------------------------------------------------------

// TestReadBitsUint64_InvertsBitBuilder pins down the bit-extraction
// helper as the precise inverse of BitBuilder.Append for the widths
// Layer 2 uses.
func TestReadBitsUint64_InvertsBitBuilder(t *testing.T) {
	cases := []struct {
		value uint64
		nbits int
	}{
		{0x0123456, 28},
		{0x1234567, 28},
		{0, 1},
		{1, 1},
		{0x5AAA, 15},
		{0x1234, 15},
		{5, 3},
	}
	for _, tc := range cases {
		var b BitBuilder
		b.Append(tc.value, tc.nbits)
		got := readBitsUint64(b.Bits(), 0, tc.nbits)
		if got != tc.value {
			t.Errorf("readBitsUint64(BitBuilder(%#x, %d)) = %#x, want %#x", tc.value, tc.nbits, got, tc.value)
		}
	}
}
