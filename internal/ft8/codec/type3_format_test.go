package codec

import (
	"testing"
)

// TestFormatMessage_Type3_BasicShapes pins the rendered output for
// the canonical Type 3 (RTTY Roundup) layouts. Mirrors the format
// shape: "[TU; ]<call1> <call2> [R ]<5N9> <exchange>" where exchange
// is a 4-digit zero-padded serial or a state/province abbreviation.
func TestFormatMessage_Type3_BasicShapes(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want string
	}{
		{"serial_basic", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 7}, "K1ABC W9XYZ 579 0007"},
		{"state_basic", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "WI"}, "K1ABC W9XYZ 579 WI"},
		{"with_ack", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", AckBit: true, Report3: 5, Serial: 7}, "K1ABC W9XYZ R 579 0007"},
		{"with_tu", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", TU: true, Report3: 5, Serial: 7}, "TU; K1ABC W9XYZ 579 0007"},
		{"with_tu_and_ack", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", TU: true, AckBit: true, Report3: 7, StateProvince: "NY"}, "TU; K1ABC W9XYZ R 599 NY"},
		{"min_report", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 0, Serial: 1}, "K1ABC W9XYZ 529 0001"},
		{"max_report", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 7, Serial: 7999}, "K1ABC W9XYZ 599 7999"},
		{"three_char_state", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "NWT"}, "K1ABC W9XYZ 579 NWT"},
		{"three_char_state_pei", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "PEI"}, "K1ABC W9XYZ 579 PEI"},
		// Token in Call1 — per QEX Table 2 c28 accepts tokens
		{"cq_in_call1", Message{Type: MessageTypeRTTYRU, Call1: "CQ", Call2: "K1ABC", Report3: 0, StateProvince: "NY"}, "CQ K1ABC 529 NY"},
		{"cq_dx_in_call1", Message{Type: MessageTypeRTTYRU, Call1: "CQ DX", Call2: "K1ABC", Report3: 0, Serial: 1}, "CQ DX K1ABC 529 0001"},
		{"de_in_call1", Message{Type: MessageTypeRTTYRU, Call1: "DE", Call2: "K1ABC", Report3: 5, StateProvince: "TX"}, "DE K1ABC 579 TX"},
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

// TestFormatMessage_Type3_Rejects pins the format-side validation
// gates — same as the encoder's.
func TestFormatMessage_Type3_Rejects(t *testing.T) {
	cases := []Message{
		{Type: MessageTypeRTTYRU, Call1: "g4abc", Call2: "K1ABC", Report3: 5, Serial: 1},
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 8, Serial: 1},
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 8000},
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "ZZ"},
		{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 42, StateProvince: "NY"},
	}
	for _, msg := range cases {
		if _, err := FormatMessage(msg); err == nil {
			t.Errorf("FormatMessage(%+v) = nil err, want validation error", msg)
		}
	}
}

// TestParseMessage_Type3_BasicShapes pins the parse-side recovery
// for every Type 3 layout. The classifier routes "TU;" prefix OR
// a "5N9" 3-digit report token to parseRTTYRoundup.
func TestParseMessage_Type3_BasicShapes(t *testing.T) {
	cases := []struct {
		text string
		want Message
	}{
		{"K1ABC W9XYZ 579 0007", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 7}},
		{"K1ABC W9XYZ 579 WI", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "WI"}},
		{"K1ABC W9XYZ R 579 0007", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", AckBit: true, Report3: 5, Serial: 7}},
		{"TU; K1ABC W9XYZ 579 0007", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", TU: true, Report3: 5, Serial: 7}},
		{"TU; K1ABC W9XYZ R 599 NY", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", TU: true, AckBit: true, Report3: 7, StateProvince: "NY"}},
		{"K1ABC W9XYZ 529 0001", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 0, Serial: 1}},
		{"K1ABC W9XYZ 599 7999", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 7, Serial: 7999}},
		{"K1ABC W9XYZ 579 NWT", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "NWT"}},
		{"K1ABC W9XYZ 579 PEI", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, StateProvince: "PEI"}},
		{"CQ K1ABC 529 NY", Message{Type: MessageTypeRTTYRU, Call1: "CQ", Call2: "K1ABC", Report3: 0, StateProvince: "NY"}},
		{"CQ DX K1ABC 529 0001", Message{Type: MessageTypeRTTYRU, Call1: "CQ DX", Call2: "K1ABC", Report3: 0, Serial: 1}},
		{"DE K1ABC 579 TX", Message{Type: MessageTypeRTTYRU, Call1: "DE", Call2: "K1ABC", Report3: 5, StateProvince: "TX"}},
		// Lowercase normalises through the same path.
		{"k1abc w9xyz 579 0007", Message{Type: MessageTypeRTTYRU, Call1: "K1ABC", Call2: "W9XYZ", Report3: 5, Serial: 7}},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got, err := ParseMessage(tc.text)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", tc.text, err)
			}
			if got != tc.want {
				t.Errorf("ParseMessage(%q):\n got=%+v\nwant=%+v", tc.text, got, tc.want)
			}
		})
	}
}

// TestParseMessage_Type3_Rejects pins the parser's error paths.
func TestParseMessage_Type3_Rejects(t *testing.T) {
	cases := []string{
		"TU;",                      // only TU; — no body
		"TU; K1ABC W9XYZ",          // missing report + exchange
		"TU; K1ABC W9XYZ 579",      // missing exchange
		"K1ABC W9XYZ 519 NY",       // 519 invalid report (middle digit 1 outside 2..9)
		"K1ABC W9XYZ 589 ZZ",       // ZZ not a state and not all digits
		"K1ABC W9XYZ 579 8000",     // serial > 7999
		"K1ABC W9XYZ 579 NY EXTRA", // trailing junk
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			if _, err := ParseMessage(text); err == nil {
				t.Errorf("ParseMessage(%q) = nil err, want error", text)
			}
		})
	}
}

// TestType3_TextRoundTrip verifies the full text-layer loop:
// ParseMessage → EncodeMessage → DecodeMessage → FormatMessage
// returns the original input.
func TestType3_TextRoundTrip(t *testing.T) {
	cases := []string{
		"K1ABC W9XYZ 579 0007",
		"K1ABC W9XYZ 579 WI",
		"K1ABC W9XYZ R 579 0007",
		"K1ABC W9XYZ R 579 NY",
		"TU; K1ABC W9XYZ 579 0007",
		"TU; K1ABC W9XYZ R 599 NY",
		"K1ABC W9XYZ 529 0001",
		"K1ABC W9XYZ 599 7999",
		"K1ABC W9XYZ 579 NWT",
		"K1ABC W9XYZ 579 PEI",
		"CQ K1ABC 529 NY",
		"CQ DX K1ABC 529 0001",
		"DE K1ABC 579 TX",
		"QRZ K1ABC 579 CA",
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
