package codec

import (
	"errors"
	"slices"
	"testing"
)

// TestEncodeMessage_Type3_LayoutMatchesQEXTable1 verifies the
// Type 3 (RTTY Roundup) bit layout per QEX paper Table 1:
//
//	t1 | c28(Call1) | c28(Call2) | R1 | r3 | s13 | i3=3
//	 1      28           28        1    3    13     3
//
// The expected bit slice is built from Layer 1 primitives so a
// width or offset regression surfaces here rather than producing
// wire output that nothing else can decode.
func TestEncodeMessage_Type3_LayoutMatchesQEXTable1(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			"qex_paper_example_serial",
			Message{
				Type:    MessageTypeRTTYRU,
				Call1:   "K1ABC",
				Call2:   "W9XYZ",
				Report3: 5,
				Serial:  7,
			},
		},
		{
			"state_form",
			Message{
				Type:          MessageTypeRTTYRU,
				Call1:         "K1ABC",
				Call2:         "W9XYZ",
				Report3:       5,
				StateProvince: "WI",
			},
		},
		{
			"with_tu_and_ack",
			Message{
				Type:          MessageTypeRTTYRU,
				Call1:         "K1ABC",
				Call2:         "W9XYZ",
				TU:            true,
				AckBit:        true,
				Report3:       7,
				StateProvince: "NY",
			},
		},
		{
			"token_in_call1",
			Message{
				Type:    MessageTypeRTTYRU,
				Call1:   "CQ",
				Call2:   "K1ABC",
				Report3: 0,
				Serial:  4242,
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

			var s13 uint16
			if tc.msg.StateProvince != "" {
				s13, _ = StateToS13(tc.msg.StateProvince)
			} else {
				s13, _ = SerialToS13(tc.msg.Serial)
			}

			want := make([]byte, 0, MessageBits)
			want = append(want, boolByte(tc.msg.TU))
			want = append(want, bitsOfValue(uint64(type1CallToC28(tc.msg.Call1)), CallsignBits)...)
			want = append(want, bitsOfValue(uint64(type1CallToC28(tc.msg.Call2)), CallsignBits)...)
			want = append(want, boolByte(tc.msg.AckBit))
			for i := range r3Bits {
				want = append(want, byte((uint64(tc.msg.Report3)>>(r3Bits-1-i))&1))
			}
			for i := range s13Bits {
				want = append(want, byte((uint64(s13)>>(s13Bits-1-i))&1))
			}
			// i3 = 3 → bits 011 MSB-first.
			want = append(want, 0, 1, 1)

			if !slices.Equal(got, want) {
				t.Errorf("layout mismatch\n got=%v\nwant=%v", got, want)
			}
		})
	}
}

// TestEncodeMessage_Type3_I3TagIs3 pins the i3 tag at the wire's
// lowest 3 bits to 3 (011 MSB-first). A regression that swapped
// i3=3 (RTTY RU) with another type's tag would land here.
func TestEncodeMessage_Type3_I3TagIs3(t *testing.T) {
	got, err := EncodeMessage(Message{
		Type:    MessageTypeRTTYRU,
		Call1:   "K1ABC",
		Call2:   "W9XYZ",
		Report3: 5,
		Serial:  7,
	})
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	if got[74] != 0 || got[75] != 1 || got[76] != 1 {
		t.Errorf("i3 bits at 74..76 = %d %d %d, want 0 1 1 (i3=3)", got[74], got[75], got[76])
	}
}

// TestEncodeMessage_Type3_Rejects pins the validation gates.
func TestEncodeMessage_Type3_Rejects(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"bad_call1", Message{Type: MessageTypeRTTYRU, Call1: "g4abc", Call2: "K1ABC", Report3: 5, Serial: 1}},
		{"bad_call2", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "TOO_LONG_CALL", Report3: 5, Serial: 1}},
		{"report_out_of_range", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 8, Serial: 1}},
		{"serial_out_of_range", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 8000}},
		{"unknown_state", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "ZZ"}},
		{"both_serial_and_state", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 42, StateProvince: "NY"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EncodeMessage(tc.msg); err == nil {
				t.Errorf("EncodeMessage(%+v) = nil err, want validation error", tc.msg)
			}
		})
	}
}

// TestType3_RoundTrip exercises Encode → Decode equivalence over
// both exchange forms (serial + state), the TU and AckBit flags,
// and token-in-Call1 inputs. Every field must round-trip exactly.
func TestType3_RoundTrip(t *testing.T) {
	cases := []Message{
		// Serial form
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 7},
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", AckBit: true, Report3: 7, Serial: 7999},
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", TU: true, AckBit: true, Report3: 0, Serial: 0},
		// State form — exercise table boundaries
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "AL"}, // first
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "WI"},
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "NWT"}, // 3-char
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "PEI"}, // 3-char
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "DC"},  // last
		// Token in Call1 — per QEX Table 2 c28 accepts tokens
		{Type: MessageTypeRTTYRU, Call1: "CQ", Call2: "K1ABC", Report3: 0, Serial: 1},
		{Type: MessageTypeRTTYRU, Call1: "DE", Call2: "K1ABC", Report3: 0, StateProvince: "NY"},
		{Type: MessageTypeRTTYRU, Call1: "QRZ", Call2: "K1ABC", Report3: 0, Serial: 1},
		{Type: MessageTypeRTTYRU, Call1: "CQ DX", Call2: "K1ABC", Report3: 0, Serial: 1},
	}
	for _, in := range cases {
		t.Run(in.Call1+"_"+in.Call2+"_"+in.StateProvince, func(t *testing.T) {
			bits, err := EncodeMessage(in)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			got, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if got.Type != MessageTypeRTTYRU {
				t.Errorf("Type = %d, want MessageTypeRTTYRU", got.Type)
			}
			if got.Call1 != in.Call1 || got.Call2 != in.Call2 {
				t.Errorf("calls: got (%q, %q), want (%q, %q)", got.Call1, got.Call2, in.Call1, in.Call2)
			}
			if got.TU != in.TU {
				t.Errorf("TU: got %v, want %v", got.TU, in.TU)
			}
			if got.AckBit != in.AckBit {
				t.Errorf("AckBit: got %v, want %v", got.AckBit, in.AckBit)
			}
			if got.Report3 != in.Report3 {
				t.Errorf("Report3: got %d, want %d", got.Report3, in.Report3)
			}
			if got.Serial != in.Serial {
				t.Errorf("Serial: got %d, want %d", got.Serial, in.Serial)
			}
			if got.StateProvince != in.StateProvince {
				t.Errorf("StateProvince: got %q, want %q", got.StateProvince, in.StateProvince)
			}
		})
	}
}

// TestDecodeMessage_Type3_NotUnknown sanity-checks that i3=3
// dispatch is wired up — DecodeMessage routes to decodeRTTYRoundup
// rather than returning ErrUnknownMessageType.
func TestDecodeMessage_Type3_NotUnknown(t *testing.T) {
	in := Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 7}
	bits, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	_, err = DecodeMessage(bits)
	if errors.Is(err, ErrUnknownMessageType) {
		t.Error("DecodeMessage(Type 3 wire) returned ErrUnknownMessageType, want successful decode")
	}
}

// TestDecodeMessage_Type3_RejectsInvalidS13 pins the s13-gap path.
// The encoder never emits an s13 in the unassigned codepoints
// (8000, or > 8065), but corruption / fuzz / hostile input can.
// The decoder must surface ErrInvalidS13 cleanly — no panic.
func TestDecodeMessage_Type3_RejectsInvalidS13(t *testing.T) {
	cases := []uint16{
		8000,               // gap between serial (≤7999) and state (≥8001)
		8001 + 65,          // first above state range
		(1 << s13Bits) - 1, // top of 13-bit field
	}
	for _, s13 := range cases {
		bits := make([]byte, MessageBits)
		// t1 = 0
		// c28(Call1) = CallsignC28("K1ABC")
		c28a := CallsignC28("K1ABC")
		for i := range CallsignBits {
			bits[t1Bits+i] = byte((c28a >> (CallsignBits - 1 - i)) & 1)
		}
		// c28(Call2) = CallsignC28("W9XYZ")
		c28b := CallsignC28("W9XYZ")
		for i := range CallsignBits {
			bits[t1Bits+CallsignBits+i] = byte((c28b >> (CallsignBits - 1 - i)) & 1)
		}
		// R1 = 0, r3 = 0
		off := t1Bits + 2*CallsignBits + 1 + r3Bits
		// s13 = invalid codepoint
		for i := range s13Bits {
			bits[off+i] = byte((uint64(s13) >> (s13Bits - 1 - i)) & 1)
		}
		// i3 = 3 → 011 MSB-first
		bits[74] = 0
		bits[75] = 1
		bits[76] = 1

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("DecodeMessage panicked on s13=%d: %v — must return error instead", s13, r)
			}
		}()

		_, err := DecodeMessage(bits)
		if !errors.Is(err, ErrInvalidS13) {
			t.Errorf("DecodeMessage(s13=%d) err=%v, want ErrInvalidS13", s13, err)
		}
	}
}
