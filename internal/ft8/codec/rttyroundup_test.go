package codec

import (
	"testing"
)

// TestSerialToS13_Boundaries pins the serial-form bounds. Per QEX
// Appendix A the s13 slot serial range is [0, 7999]; SerialToS13
// returns ok=false above the upper bound (the 13-bit field has
// 8192 codepoints, but only 0..7999 are valid serial codepoints —
// the remaining ~192 are state/unassigned).
func TestSerialToS13_Boundaries(t *testing.T) {
	cases := []struct {
		serial uint16
		want   uint16
		ok     bool
	}{
		{0, 0, true},
		{1, 1, true},
		{4242, 4242, true},
		{7999, 7999, true},
		{8000, 0, false}, // first invalid: 8000 is the gap codepoint
		{8001, 0, false},
		{8191, 0, false}, // top of 13-bit field
	}
	for _, tc := range cases {
		got, ok := SerialToS13(tc.serial)
		if got != tc.want || ok != tc.ok {
			t.Errorf("SerialToS13(%d) = (%d, %v), want (%d, %v)", tc.serial, got, ok, tc.want, tc.ok)
		}
	}
}

// TestStateToS13_LookupTable covers every entry in the QEX ref [14]
// states_provinces.txt table. The index in the table IS the offset
// from s13StateBase. Pinning all 65 entries catches drift in either
// the table order or the base offset.
func TestStateToS13_LookupTable(t *testing.T) {
	for idx, state := range rttyRoundupStates {
		got, ok := StateToS13(state)
		if !ok {
			t.Errorf("StateToS13(%q) ok=false; want true (table entry %d)", state, idx)
			continue
		}
		want := uint16(s13StateBase + idx)
		if got != want {
			t.Errorf("StateToS13(%q) = %d, want %d (table index %d + base %d)", state, got, want, idx, s13StateBase)
		}
	}
}

// TestStateToS13_RejectsUnknown pins the "not in table" path.
// Lowercase and unknown codes return ok=false.
func TestStateToS13_RejectsUnknown(t *testing.T) {
	cases := []string{
		"",
		"al",   // lowercase — case-sensitive match per the package contract
		"ZZ",   // not a real state
		"FOO",  // bad shape
		"NWT ", // trailing whitespace
		"PE",   // PE is the modern Canada-Post code for Prince Edward Island, but the QEX ref [14] table uses "PEI" — pinning the exact-match contract
	}
	for _, in := range cases {
		_, ok := StateToS13(in)
		if ok {
			t.Errorf("StateToS13(%q) ok=true; want false", in)
		}
	}
}

// TestS13ToExchange_Partitions covers all three regions of the s13
// codepoint space. Pinning the boundary values (last serial, first
// state, last state, unassigned codepoints) catches drift in either
// SerialToS13 / StateToS13 forward functions or the inverse.
func TestS13ToExchange_Partitions(t *testing.T) {
	cases := []struct {
		s13     uint16
		serial  uint16
		state   string
		kind    S13Kind
		comment string
	}{
		{0, 0, "", S13KindSerial, "serial range low"},
		{1, 1, "", S13KindSerial, ""},
		{7999, 7999, "", S13KindSerial, "serial range high"},
		{8000, 0, "", S13KindUnassigned, "gap between serial and state"},
		{8001, 0, "AL", S13KindState, "state range low (first table entry)"},
		{8001 + 64, 0, "DC", S13KindState, "state range high (last table entry, 'DC')"},
		{8001 + 65, 0, "", S13KindUnassigned, "first above state range"},
		{8191, 0, "", S13KindUnassigned, "top of 13-bit field"},
	}
	for _, tc := range cases {
		gotSerial, gotState, gotKind := S13ToExchange(tc.s13)
		if gotSerial != tc.serial || gotState != tc.state || gotKind != tc.kind {
			t.Errorf("S13ToExchange(%d) = (%d, %q, %v), want (%d, %q, %v) [%s]",
				tc.s13, gotSerial, gotState, gotKind, tc.serial, tc.state, tc.kind, tc.comment)
		}
	}
}

// TestS13ToExchange_RoundTripSerial pins serial → s13 → serial.
func TestS13ToExchange_RoundTripSerial(t *testing.T) {
	for serial := uint16(0); serial <= s13SerialMax; serial += 137 {
		s13, ok := SerialToS13(serial)
		if !ok {
			t.Fatalf("SerialToS13(%d) ok=false; in-range serial must round-trip", serial)
		}
		gotSerial, gotState, kind := S13ToExchange(s13)
		if kind != S13KindSerial {
			t.Errorf("S13ToExchange(%d) kind=%v, want S13KindSerial (from serial %d)", s13, kind, serial)
		}
		if gotSerial != serial || gotState != "" {
			t.Errorf("S13ToExchange(%d) = (%d, %q), want (%d, \"\")", s13, gotSerial, gotState, serial)
		}
	}
}

// TestS13ToExchange_RoundTripState pins state → s13 → state for
// every entry in the lookup table.
func TestS13ToExchange_RoundTripState(t *testing.T) {
	for _, state := range rttyRoundupStates {
		s13, ok := StateToS13(state)
		if !ok {
			t.Fatalf("StateToS13(%q) ok=false; table entry must round-trip", state)
		}
		gotSerial, gotState, kind := S13ToExchange(s13)
		if kind != S13KindState {
			t.Errorf("S13ToExchange(%d) kind=%v, want S13KindState (from state %q)", s13, kind, state)
		}
		if gotState != state || gotSerial != 0 {
			t.Errorf("S13ToExchange(%d) = (%d, %q), want (0, %q)", s13, gotSerial, gotState, state)
		}
	}
}
