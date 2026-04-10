// validate_test.go — tests for post-decode plausibility checks.

package message

import "testing"

func TestPlausibleCallsign(t *testing.T) {
	tests := []struct {
		call string
		want bool
	}{
		// Valid standard callsigns.
		{"W1AW", true},
		{"VK2XYZ", true},
		{"KB7THX", true},
		{"3DA0XYZ", true},
		{"A61CK", true},
		{"SV2SIH", true},
		{"HZ1TT", true},

		// Tokens — always plausible.
		{"CQ", true},
		{"DE", true},
		{"QRZ", true},
		{"CQ DX", true},
		{"CQ 350", true},
		{"CQ POTA", true},

		// Hash references — always plausible.
		{"<...>", true},
		{"<W1AW>", true},

		// Empty/whitespace — implausible.
		{"", false},
		{"   ", false},

		// All digits — implausible (no letter).
		{"12345", false},
		{"007", false},

		// All letters — implausible (no digit).
		{"ABCDEF", false},
		{"XYZ", false},

		// Single character — could be edge cases.
		{"A", false}, // no digit
		{"1", false}, // no letter
		{"A1", true}, // minimal valid

		// Callsigns with slash (Type 4 style).
		{"VK/ZL4XZ", true},
	}

	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			got := PlausibleCallsign(tt.call)
			if got != tt.want {
				t.Errorf("PlausibleCallsign(%q) = %v, want %v", tt.call, got, tt.want)
			}
		})
	}
}

func TestPlausibleMessage_Standard(t *testing.T) {
	// Pack a known valid Type 1 message and verify it's plausible.
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	packed, err := Pack(msg)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	if !PlausibleMessage(packed) {
		t.Error("PlausibleMessage returned false for valid CQ W1AW FN31")
	}
}

func TestPlausibleMessage_FreeText(t *testing.T) {
	// Free text messages have no callsign fields — should be plausible.
	msg := &Message{
		MsgType:  TypeFreeText,
		FreeText: "TNX BOB 73 GL",
	}
	packed, err := Pack(msg)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	if !PlausibleMessage(packed) {
		t.Error("PlausibleMessage returned false for valid free text message")
	}
}

func TestPlausibleMessage_AllZero(t *testing.T) {
	// All-zero payload — should be plausible at the message level (since
	// it unpacks to some Type 0 free text). The all-zero codeword is caught
	// by the codec's verifyAndExtract, not here.
	var zero [MsgBytes]byte
	// This may or may not unpack, but PlausibleMessage should not panic.
	_ = PlausibleMessage(zero)
}
