package codec

import (
	"errors"
	"testing"
)

// TestFormatMessage_Type1_BasicShapes pins the human-readable output
// for the canonical Type 1 layouts. Each case maps a Message struct
// to its on-air text form per the FT8 display convention (matching
// WSJT-X for the cases the QEX paper enumerates).
func TestFormatMessage_Type1_BasicShapes(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want string
	}{
		{
			"cq_callsign_grid",
			Message{Type: MessageTypeStd, Call1: "CQ", Call2: "G4ABC", Grid: "IO91"},
			"CQ G4ABC IO91",
		},
		{
			"cq_callsign_no_grid",
			Message{Type: MessageTypeStd, Call1: "CQ", Call2: "G4ABC"},
			"CQ G4ABC",
		},
		{
			"cq_dx_callsign_grid",
			Message{Type: MessageTypeStd, Call1: "CQ DX", Call2: "G4ABC", Grid: "IO91"},
			"CQ DX G4ABC IO91",
		},
		{
			"cq_pota_callsign_grid",
			Message{Type: MessageTypeStd, Call1: "CQ POTA", Call2: "G4ABC", Grid: "IO91"},
			"CQ POTA G4ABC IO91",
		},
		{
			"cq_100_callsign_grid",
			Message{Type: MessageTypeStd, Call1: "CQ 100", Call2: "G4ABC", Grid: "IO91"},
			"CQ 100 G4ABC IO91",
		},
		{
			"de_callsign_grid",
			Message{Type: MessageTypeStd, Call1: "DE", Call2: "G4ABC", Grid: "IO91"},
			"DE G4ABC IO91",
		},
		{
			"qrz_callsign_grid",
			Message{Type: MessageTypeStd, Call1: "QRZ", Call2: "G4ABC", Grid: "IO91"},
			"QRZ G4ABC IO91",
		},
		{
			"plain_pair_grid",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "FN20"},
			"K1ABC G4ABC FN20",
		},
		{
			"plain_pair_report",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "-11"},
			"K1ABC G4ABC -11",
		},
		{
			"plain_pair_ack_report",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", AckBit: true, Grid: "-09"},
			"K1ABC G4ABC R-09",
		},
		{
			"plain_pair_ack_grid",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", AckBit: true, Grid: "FN20"},
			"K1ABC G4ABC R FN20",
		},
		{
			"plain_pair_reserved_rr73",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "RR73"},
			"K1ABC G4ABC RR73",
		},
		{
			"plain_pair_reserved_73",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "73"},
			"K1ABC G4ABC 73",
		},
		{
			"plain_pair_blank_grid",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC"},
			"K1ABC G4ABC",
		},
		{
			"rover1_set",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Rover1: true, Grid: "FN20"},
			"K1ABC/R G4ABC FN20",
		},
		{
			"rover2_set",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Rover2: true, Grid: "FN20"},
			"K1ABC G4ABC/R FN20",
		},
		{
			"both_rovers_set",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Rover1: true, Rover2: true, Grid: "FN20"},
			"K1ABC/R G4ABC/R FN20",
		},
		{
			"ack_bare",
			Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", AckBit: true},
			"K1ABC G4ABC R",
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

// TestFormatMessage_UnsupportedType pins the rollout sentinel for
// types whose formatter hasn't landed.
func TestFormatMessage_UnsupportedType(t *testing.T) {
	cases := []MessageType{
		MessageTypeEUVHFP,
		MessageTypeNonStdCall,
		MessageTypeFreeText,
		MessageTypeTelemetry,
	}
	for _, mt := range cases {
		t.Run(mt.testName(), func(t *testing.T) {
			_, err := FormatMessage(Message{Type: mt, Call1: "K1ABC", Call2: "G4ABC"})
			if !errors.Is(err, ErrUnsupportedMessageType) {
				t.Errorf("FormatMessage(Type=%d) err=%v, want ErrUnsupportedMessageType", mt, err)
			}
		})
	}
}

// TestFormatMessage_RejectsTokenWithRover pins the validation guard:
// a token in Call1/Call2 with the rover bit set is nonsensical (the
// /R suffix means "rover callsign" — tokens aren't callsigns).
func TestFormatMessage_RejectsTokenWithRover(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"cq_with_rover1", Message{Type: MessageTypeStd, Call1: "CQ", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"de_with_rover1", Message{Type: MessageTypeStd, Call1: "DE", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"cq_dx_with_rover1", Message{Type: MessageTypeStd, Call1: "CQ DX", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"cq_token_in_call2_with_rover2", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "CQ", Rover2: true, Grid: "FN20"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FormatMessage(tc.msg); err == nil {
				t.Errorf("FormatMessage(%+v) = nil err, want validation error", tc.msg)
			}
		})
	}
}

// TestFormatMessage_RejectsBadCallsign verifies that format validates
// using the same gate as the encoder — a Message that won't encode
// also won't format.
func TestFormatMessage_RejectsBadCallsign(t *testing.T) {
	cases := []Message{
		{Type: MessageTypeStd, Call1: "k1abc", Call2: "G4ABC", Grid: "FN20"},   // lowercase
		{Type: MessageTypeStd, Call1: "AB", Call2: "G4ABC", Grid: "FN20"},      // too short
		{Type: MessageTypeStd, Call1: "K1ABC", Call2: "ABCDEFG", Grid: "FN20"}, // too long
		{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "fn20"},   // bad grid case
	}
	for _, msg := range cases {
		if _, err := FormatMessage(msg); err == nil {
			t.Errorf("FormatMessage(%+v) = nil err, want validation error", msg)
		}
	}
}
