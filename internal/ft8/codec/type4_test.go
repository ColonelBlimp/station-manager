package codec

import (
	"errors"
	"slices"
	"testing"
)

// TestEncodeMessage_Type4_LayoutMatchesQEXTable1 verifies that
// EncodeMessage assembles a Type 4 (NonStd Call) body in the exact
// bit layout QEX paper Table 1 specifies:
//
//	h12(hash) | c58(nonstd) | h1 | r2 | c1 | i3=4
//	   12         58          1    2    1    3
//
// Builds the expected bit slice from Layer 1 primitives so a width
// or offset regression surfaces here rather than producing wire
// output that nothing else can decode.
func TestEncodeMessage_Type4_LayoutMatchesQEXTable1(t *testing.T) {
	cases := []struct {
		name   string
		msg    Message
		wantH1 byte
		wantC1 byte
	}{
		{
			name:   "std_call1_nonstd_call2_rrr",
			msg:    Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "PJ4/K1ABC", Grid: "RRR"},
			wantH1: 0,
			wantC1: 0,
		},
		{
			name:   "nonstd_call1_std_call2_rr73",
			msg:    Message{Type: MessageTypeNonStdCall, Call1: "PJ4/K1ABC", Call2: "W9XYZ", Grid: "RR73"},
			wantH1: 1,
			wantC1: 0,
		},
		{
			name:   "cq_from_nonstd_blank",
			msg:    Message{Type: MessageTypeNonStdCall, Call1: "CQ", Call2: "PJ4/K1ABC"},
			wantH1: 0,
			wantC1: 1,
		},
		{
			name:   "cq_from_nonstd_73",
			msg:    Message{Type: MessageTypeNonStdCall, Call1: "CQ", Call2: "YW18FIFA", Grid: "73"},
			wantH1: 0,
			wantC1: 1,
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

			var h12 uint32
			var c58 uint64
			switch {
			case tc.msg.Call1 == "CQ":
				h12 = 0
				c58 = CallsignC58(tc.msg.Call2)
			case tc.wantH1 == 0:
				_, h12, _ = HashCodes(tc.msg.Call1)
				c58 = CallsignC58(tc.msg.Call2)
			default:
				_, h12, _ = HashCodes(tc.msg.Call2)
				c58 = CallsignC58(tc.msg.Call1)
			}
			r2, ok := gridToR2(tc.msg.Grid)
			if !ok {
				t.Fatalf("test setup: Grid %q has no r2 mapping", tc.msg.Grid)
			}

			want := make([]byte, 0, MessageBits)
			for i := range h12Bits {
				want = append(want, byte((h12>>(h12Bits-1-i))&1))
			}
			for i := range C58Bits {
				want = append(want, byte((c58>>(C58Bits-1-i))&1))
			}
			want = append(want, tc.wantH1)
			for i := range r2Bits {
				want = append(want, byte((r2>>(r2Bits-1-i))&1))
			}
			want = append(want, tc.wantC1)
			// i3 = 4 → bits 100 MSB-first.
			want = append(want, 1, 0, 0)

			if !slices.Equal(got, want) {
				t.Errorf("layout mismatch\n got=%v\nwant=%v", got, want)
			}
		})
	}
}

// TestEncodeMessage_Type4_I3TagIs4 pins the i3 tag at the wire's
// lowest 3 bits to 4 (100 MSB-first). A regression that swapped tag
// values across Type 1/2/4 would land here.
func TestEncodeMessage_Type4_I3TagIs4(t *testing.T) {
	got, err := EncodeMessage(Message{
		Type:  MessageTypeNonStdCall,
		Call1: "W9XYZ",
		Call2: "PJ4/K1ABC",
		Grid:  "RRR",
	})
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	if got[74] != 1 || got[75] != 0 || got[76] != 0 {
		t.Errorf("i3 bits at 74..76 = %d %d %d, want 1 0 0 (i3=4)", got[74], got[75], got[76])
	}
}

// TestEncodeMessage_Type4_Rejects pins the validateType4Calls gate.
// Each rejection case is a Message shape that violates one of the
// rules in encodeNonStdCall's doc.
func TestEncodeMessage_Type4_Rejects(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"both_std", Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "G4ABC", Grid: "RRR"}},
		{"both_nonstd", Message{Type: MessageTypeNonStdCall, Call1: "PJ4/K1ABC", Call2: "VK7ABC/P", Grid: "RRR"}},
		{"cq_with_suffix_in_call1", Message{Type: MessageTypeNonStdCall, Call1: "CQ DX", Call2: "PJ4/K1ABC", Grid: "RRR"}},
		{"de_token_in_call1", Message{Type: MessageTypeNonStdCall, Call1: "DE", Call2: "PJ4/K1ABC", Grid: "RRR"}},
		{"qrz_token_in_call2", Message{Type: MessageTypeNonStdCall, Call1: "PJ4/K1ABC", Call2: "QRZ", Grid: "RRR"}},
		{"illegal_grid_locator", Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "PJ4/K1ABC", Grid: "FN20"}},
		{"illegal_signed_report", Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "PJ4/K1ABC", Grid: "-11"}},
		{"bad_nonstd_call", Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "TOOLONGCALL12", Grid: "RRR"}},
		{"empty_nonstd_call", Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "", Grid: "RRR"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EncodeMessage(tc.msg); err == nil {
				t.Errorf("EncodeMessage(%+v) = nil err, want validation error", tc.msg)
			}
		})
	}
}

// TestType4_RoundTrip exercises Encode→Decode equivalence for the
// Type 4 shapes. The std side decodes to "<...>" + the matching
// Hash12 value; the nonstd side decodes to its literal string. The
// CQ-from-nonstd path leaves Hash12 zero (the wire ignores h12 when
// c1=1).
func TestType4_RoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		in       Message
		wantHash uint16 // 0 when c1=1
	}{
		{
			"std_call1_nonstd_call2_rrr",
			Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "PJ4/K1ABC", Grid: "RRR"},
			func() uint16 { _, h12, _ := HashCodes("W9XYZ"); return uint16(h12) }(),
		},
		{
			"nonstd_call1_std_call2_rr73",
			Message{Type: MessageTypeNonStdCall, Call1: "PJ4/K1ABC", Call2: "W9XYZ", Grid: "RR73"},
			func() uint16 { _, h12, _ := HashCodes("W9XYZ"); return uint16(h12) }(),
		},
		{
			"std_nonstd_73",
			Message{Type: MessageTypeNonStdCall, Call1: "K1ABC", Call2: "YW18FIFA", Grid: "73"},
			func() uint16 { _, h12, _ := HashCodes("K1ABC"); return uint16(h12) }(),
		},
		{
			"std_nonstd_blank",
			Message{Type: MessageTypeNonStdCall, Call1: "G4ABC", Call2: "M/K1ABC"},
			func() uint16 { _, h12, _ := HashCodes("G4ABC"); return uint16(h12) }(),
		},
		{
			"cq_from_nonstd_rrr",
			Message{Type: MessageTypeNonStdCall, Call1: "CQ", Call2: "PJ4/K1ABC", Grid: "RRR"},
			0,
		},
		{
			"cq_from_nonstd_blank",
			Message{Type: MessageTypeNonStdCall, Call1: "CQ", Call2: "YW18FIFA"},
			0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bits, err := EncodeMessage(tc.in)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			got, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if got.Type != MessageTypeNonStdCall {
				t.Errorf("Type = %d, want MessageTypeNonStdCall", got.Type)
			}
			if got.Grid != tc.in.Grid {
				t.Errorf("Grid = %q, want %q", got.Grid, tc.in.Grid)
			}
			if got.Hash12 != tc.wantHash {
				t.Errorf("Hash12 = %d, want %d", got.Hash12, tc.wantHash)
			}

			// Verify the nonstd side and the hashed side land in the
			// right slots per the encoder's std-vs-nonstd routing.
			switch {
			case tc.in.Call1 == "CQ":
				if got.Call1 != "CQ" || got.Call2 != tc.in.Call2 {
					t.Errorf("CQ branch: got (%q, %q), want (\"CQ\", %q)", got.Call1, got.Call2, tc.in.Call2)
				}
			case isStdCallsignShape(tc.in.Call1):
				// h1=0: hash side is Call1, nonstd is Call2.
				if got.Call1 != hashedCallSentinel || got.Call2 != tc.in.Call2 {
					t.Errorf("h1=0 branch: got (%q, %q), want (%q, %q)", got.Call1, got.Call2, hashedCallSentinel, tc.in.Call2)
				}
			default:
				// h1=1: hash side is Call2, nonstd is Call1.
				if got.Call1 != tc.in.Call1 || got.Call2 != hashedCallSentinel {
					t.Errorf("h1=1 branch: got (%q, %q), want (%q, %q)", got.Call1, got.Call2, tc.in.Call1, hashedCallSentinel)
				}
			}
		})
	}
}

// TestDecodeMessage_Type4_R2TokenSlot exercises the 2-bit r2 token
// partition end-to-end: every r2 value lands in Grid as the matching
// string on decode. Belt-and-braces against an r2/grid mapping
// regression that would silently swap, say, RRR for 73.
func TestDecodeMessage_Type4_R2TokenSlot(t *testing.T) {
	cases := []string{"", "RRR", "RR73", "73"}
	for _, want := range cases {
		t.Run("grid="+want, func(t *testing.T) {
			in := Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "PJ4/K1ABC", Grid: want}
			bits, err := EncodeMessage(in)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			got, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if got.Grid != want {
				t.Errorf("round-trip Grid = %q, want %q", got.Grid, want)
			}
		})
	}
}

// TestDecodeMessage_Type4_HashSentinelOnUnresolvedSide pins the
// "<...>" sentinel on the hashed side of a decoded Type 4 message.
// Phase 4's hash table will swap the sentinel out for the resolved
// callsign string once it has the entry; codec layer always emits
// the sentinel.
func TestDecodeMessage_Type4_HashSentinelOnUnresolvedSide(t *testing.T) {
	in := Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "PJ4/K1ABC", Grid: "RRR"}
	bits, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	got, err := DecodeMessage(bits)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if got.Call1 != "<...>" {
		t.Errorf("Call1 (hashed side) = %q, want %q", got.Call1, "<...>")
	}
	if got.Hash12 == 0 {
		t.Error("Hash12 = 0, want non-zero (the wire carried a hashed callsign)")
	}
}

// TestDecodeMessage_Type4_CQHasZeroHash12 pins the c1=1 wire
// convention: when Call1 is CQ, h12 wire bits are ignored, and the
// decoder leaves Message.Hash12 at zero. (A non-zero h12 on a CQ
// message would be a spec violation the encoder doesn't produce; the
// decoder mirrors the spec by zeroing.)
func TestDecodeMessage_Type4_CQHasZeroHash12(t *testing.T) {
	in := Message{Type: MessageTypeNonStdCall, Call1: "CQ", Call2: "PJ4/K1ABC", Grid: "RRR"}
	bits, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	got, err := DecodeMessage(bits)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if got.Call1 != "CQ" {
		t.Errorf("Call1 = %q, want CQ", got.Call1)
	}
	if got.Hash12 != 0 {
		t.Errorf("Hash12 = %d, want 0 (h12 wire ignored when c1=1)", got.Hash12)
	}
}

// TestEncodeMessage_Type4_NonStdInAnyValidShape verifies the c58
// alphabet acceptance: 1–11 chars from space + 0-9 + A-Z + /. Picks a
// few representative shapes (compound, special-event, mixed-digit)
// to spot-check that the validator + encoder agree on what c58 can
// hold.
func TestEncodeMessage_Type4_NonStdInAnyValidShape(t *testing.T) {
	cases := []string{
		"PJ4/K1ABC",
		"VK7ABC/P",
		"YW18FIFA",
		"M/K1ABC",
		"K1ABC/QRP",
	}
	for _, nonstd := range cases {
		t.Run(nonstd, func(t *testing.T) {
			in := Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: nonstd, Grid: "RRR"}
			bits, err := EncodeMessage(in)
			if err != nil {
				t.Fatalf("EncodeMessage(nonstd=%q): %v", nonstd, err)
			}
			got, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if got.Call2 != nonstd {
				t.Errorf("Call2 round-trip = %q, want %q", got.Call2, nonstd)
			}
		})
	}
}

// TestDecodeMessage_Type4_NotUnknown sanity-checks that i3=4 dispatch
// is wired up (DecodeMessage routes to decodeNonStdCall rather than
// returning ErrUnknownMessageType).
func TestDecodeMessage_Type4_NotUnknown(t *testing.T) {
	in := Message{Type: MessageTypeNonStdCall, Call1: "W9XYZ", Call2: "PJ4/K1ABC", Grid: "RRR"}
	bits, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	_, err = DecodeMessage(bits)
	if errors.Is(err, ErrUnknownMessageType) {
		t.Error("DecodeMessage(Type 4 wire) returned ErrUnknownMessageType, want successful decode")
	}
}
