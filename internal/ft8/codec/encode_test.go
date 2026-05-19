package codec

import (
	"errors"
	"slices"
	"testing"
)

// TestEncodeMessage_Type1_LayoutMatchesQEXTable1 verifies that
// EncodeMessage assembles a Type 1 (Std Msg) body in the exact bit
// layout QEX paper Table 1 specifies:
//
//	c28(Call1) | r1(Rover1) | c28(Call2) | r1(Rover2) | R1(AckBit) | g15(Grid) | i3=1
//	    28          1            28          1            1            15        3
//
// The expected bits are constructed by re-using the Layer 1 codec
// primitives (CallsignC28, Grid4ToG15) which have their own
// per-primitive spec-vector tests against QEX ref [14] reference
// programs. This test owns the LAYOUT (offsets, widths, field
// order) — Layer 1's tests own each primitive's value correctness.
// A bug here would surface as a wrong-offset or wrong-width: the
// kind of mistake that produces a "decode fails" symptom several
// layers deeper without pointing at the source.
func TestEncodeMessage_Type1_LayoutMatchesQEXTable1(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			name: "k1jt_calls_g4abc_fn20",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "K1JT",
				Call2: "G4ABC",
				Grid:  "FN20",
			},
		},
		{
			name: "rover1_set",
			msg: Message{
				Type:   MessageTypeStd,
				Call1:  "K1JT",
				Call2:  "G4ABC",
				Rover1: true,
				Grid:   "FN20",
			},
		},
		{
			name: "rover2_set",
			msg: Message{
				Type:   MessageTypeStd,
				Call1:  "K1JT",
				Call2:  "G4ABC",
				Rover2: true,
				Grid:   "FN20",
			},
		},
		{
			name: "ack_set",
			msg: Message{
				Type:   MessageTypeStd,
				Call1:  "K1JT",
				Call2:  "G4ABC",
				AckBit: true,
				Grid:   "FN20",
			},
		},
		{
			name: "all_rover_and_ack_set",
			msg: Message{
				Type:   MessageTypeStd,
				Call1:  "K1JT",
				Call2:  "G4ABC",
				Rover1: true,
				Rover2: true,
				AckBit: true,
				Grid:   "FN20",
			},
		},
		{
			name: "grid_io91",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "G4ABC",
				Call2: "K1JT",
				Grid:  "IO91",
			},
		},
		{
			name: "report_minus11",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "K1JT",
				Call2: "G4ABC",
				Grid:  "-11",
			},
		},
		{
			name: "report_plus02",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "K1JT",
				Call2: "G4ABC",
				Grid:  "+02",
			},
		},
		{
			name: "reserved_rr73",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "K1JT",
				Call2: "G4ABC",
				Grid:  "RR73",
			},
		},
		{
			name: "reserved_blank",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "K1JT",
				Call2: "G4ABC",
				Grid:  "",
			},
		},
		{
			// Lower-bound boundary of the FT8 protocol report range
			// [-30, +99]. The range guard must accept this value;
			// "-31" is rejected by the bad-grid suite below.
			name: "report_at_protocol_min",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "K1JT",
				Call2: "G4ABC",
				Grid:  "-30",
			},
		},
		{
			// Upper-bound boundary at +99. "+99" the largest 2-digit
			// signed-report magnitude isSignedReport accepts.
			name: "report_at_protocol_max",
			msg: Message{
				Type:  MessageTypeStd,
				Call1: "K1JT",
				Call2: "G4ABC",
				Grid:  "+99",
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
				t.Fatalf("len(got) = %d, want %d", len(got), MessageBits)
			}

			// Independent re-assembly via Layer 1 primitives — each
			// field expanded MSB-first into the position QEX Table 1
			// specifies. Uses stdCallToC28 (not CallsignC28 directly)
			// because the encoder routes 3-4 char and 5-char-2-prefix
			// calls through HashedCallC28; bypassing the routing in
			// the expected output would mis-spec the test post the
			// Phase 2C encoder fix.
			want := make([]byte, 0, MessageBits)
			want = append(want, bitsOfValue(uint64(stdCallToC28(tc.msg.Call1)), CallsignBits)...)
			want = append(want, boolBitByte(tc.msg.Rover1))
			want = append(want, bitsOfValue(uint64(stdCallToC28(tc.msg.Call2)), CallsignBits)...)
			want = append(want, boolBitByte(tc.msg.Rover2))
			want = append(want, boolBitByte(tc.msg.AckBit))
			want = append(want, bitsOfValue(uint64(Grid4ToG15(tc.msg.Grid)), G15Bits)...)
			want = append(want, bitsOfValue(i3Std, 3)...)

			if !slices.Equal(got, want) {
				t.Errorf("layout mismatch:\n got=%v\nwant=%v", got, want)
			}
		})
	}
}

// TestEncodeMessage_Type1_BitPositions pins down each slot's
// position with absolute bit indices. If a future refactor reorders
// the BitBuilder appends but the layout-equality test above happens
// to pass (e.g. due to symmetric inputs), these spot-checks catch
// the drift.
func TestEncodeMessage_Type1_BitPositions(t *testing.T) {
	// Construct a Message where every flag bit is set so we can read
	// them out by index without ambiguity. The c28/g15 values come
	// from real inputs to keep them inside the documented ranges.
	msg := Message{
		Type:   MessageTypeStd,
		Call1:  "K1JT",
		Call2:  "G4ABC",
		Rover1: true,
		Rover2: true,
		AckBit: true,
		Grid:   "FN20",
	}
	got, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}

	// Per Table 1 the layout puts:
	//   bits[0..27]  = c28(Call1)
	//   bits[28]     = Rover1 (= 1)
	//   bits[29..56] = c28(Call2)
	//   bits[57]     = Rover2 (= 1)
	//   bits[58]     = AckBit (= 1)
	//   bits[59..73] = g15(Grid)
	//   bits[74..76] = i3 = 001 MSB-first
	if got[28] != 1 {
		t.Errorf("Rover1 at bits[28] = %d, want 1", got[28])
	}
	if got[57] != 1 {
		t.Errorf("Rover2 at bits[57] = %d, want 1", got[57])
	}
	if got[58] != 1 {
		t.Errorf("AckBit at bits[58] = %d, want 1", got[58])
	}
	if got[74] != 0 || got[75] != 0 || got[76] != 1 {
		t.Errorf("i3 at bits[74..76] = %d%d%d, want 001", got[74], got[75], got[76])
	}
}

// TestEncodeMessage_Type1_ZeroValueFlags confirms unset flag bits
// encode as 0 — pins down the boolBit zero path.
func TestEncodeMessage_Type1_ZeroValueFlags(t *testing.T) {
	msg := Message{
		Type:  MessageTypeStd,
		Call1: "K1JT",
		Call2: "G4ABC",
		Grid:  "FN20",
	}
	got, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	if got[28] != 0 {
		t.Errorf("Rover1 (unset) at bits[28] = %d, want 0", got[28])
	}
	if got[57] != 0 {
		t.Errorf("Rover2 (unset) at bits[57] = %d, want 0", got[57])
	}
	if got[58] != 0 {
		t.Errorf("AckBit (unset) at bits[58] = %d, want 0", got[58])
	}
}

// TestEncodeMessage_UnsupportedType returns the sentinel for any
// MessageType whose packer hasn't landed yet. Pins the rollout
// behaviour so tests during Phase 3/4 can assert their type is now
// implemented (i.e. no longer returns ErrUnsupportedMessageType).
func TestEncodeMessage_UnsupportedType(t *testing.T) {
	// MessageTypeStd (Phase 2) and MessageTypeFreeText (Phase 3A) are
	// implemented; the remaining types stay unsupported until Phase
	// 3B / 3C / 3D and Phase 4 land their packers.
	cases := []MessageType{
		MessageTypeEUVHFP,
		MessageTypeRTTYRU,
		MessageTypeNonStdCall,
		MessageTypeEUVHFHash,
		MessageTypeDXpedition,
		MessageTypeFieldDayANSI,
		MessageTypeFieldDayRTTY,
		MessageTypeTelemetry,
	}
	for _, mt := range cases {
		t.Run(mt.testName(), func(t *testing.T) {
			_, err := EncodeMessage(Message{Type: mt})
			if !errors.Is(err, ErrUnsupportedMessageType) {
				t.Errorf("got err=%v, want ErrUnsupportedMessageType", err)
			}
		})
	}
}

// TestEncodeMessage_ZeroValueType — a Message with no Type set is
// MessageType(0), which is not a valid type. EncodeMessage rejects
// it with ErrUnsupportedMessageType (matches the unimplemented-type
// path; both end up in the default switch case).
func TestEncodeMessage_ZeroValueType(t *testing.T) {
	_, err := EncodeMessage(Message{Call1: "K1JT", Call2: "G4ABC", Grid: "FN20"})
	if !errors.Is(err, ErrUnsupportedMessageType) {
		t.Errorf("got err=%v, want ErrUnsupportedMessageType", err)
	}
}

// TestEncodeMessage_Type1_BadCallsign covers the validation paths
// that return errors (rather than letting CallsignC28 panic).
func TestEncodeMessage_Type1_BadCallsign(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"call1_too_short", Message{Type: MessageTypeStd, Call1: "AB", Call2: "K1JT", Grid: "FN20"}},
		{"call1_too_long", Message{Type: MessageTypeStd, Call1: "ABCDEFG", Call2: "K1JT", Grid: "FN20"}},
		{"call1_lowercase", Message{Type: MessageTypeStd, Call1: "k1jt", Call2: "K1JT", Grid: "FN20"}},
		{"call1_punct", Message{Type: MessageTypeStd, Call1: "K1J/T", Call2: "K1JT", Grid: "FN20"}},
		{"call1_no_digit", Message{Type: MessageTypeStd, Call1: "ABCDEF", Call2: "K1JT", Grid: "FN20"}},
		{"call1_no_letter_prefix", Message{Type: MessageTypeStd, Call1: "11ABC", Call2: "K1JT", Grid: "FN20"}},
		{"call1_long_suffix", Message{Type: MessageTypeStd, Call1: "K1ABCD", Call2: "K1JT", Grid: "FN20"}},
		{"call2_lowercase", Message{Type: MessageTypeStd, Call1: "K1JT", Call2: "g4abc", Grid: "FN20"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EncodeMessage(tc.msg); err == nil {
				t.Errorf("EncodeMessage(%+v) = nil err, want validation error", tc.msg)
			}
		})
	}
}

// TestEncodeMessage_RejectsTokenWithRover pins the encode-side
// rover guard. Symmetric with TestFormatMessage_RejectsTokenWithRover
// — both gates must reject the same nonsensical combinations so a
// Message that one rejects can't sneak through the other.
//
// The wire format DOES allow a token+rover bit pattern; DecodeMessage
// returns it bit-faithfully. The encoder rejecting it is the
// semantic gate that prevents OUR sender from emitting one. See
// validateType1Rover's doc for the full asymmetry rationale.
func TestEncodeMessage_RejectsTokenWithRover(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"cq_with_rover1", Message{Type: MessageTypeStd, Call1: "CQ", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"de_with_rover1", Message{Type: MessageTypeStd, Call1: "DE", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"qrz_with_rover1", Message{Type: MessageTypeStd, Call1: "QRZ", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"cq_dx_with_rover1", Message{Type: MessageTypeStd, Call1: "CQ DX", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"cq_100_with_rover1", Message{Type: MessageTypeStd, Call1: "CQ 100", Call2: "G4ABC", Rover1: true, Grid: "FN20"}},
		{"token_in_call2_with_rover2", Message{Type: MessageTypeStd, Call1: "K1JT", Call2: "CQ", Rover2: true, Grid: "FN20"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EncodeMessage(tc.msg); err == nil {
				t.Errorf("EncodeMessage(%+v) = nil err, want validation error (token+rover)", tc.msg)
			}
		})
	}
}

// TestEncodeMessage_Type1_BadGrid covers the g15-slot validation
// paths. Grid4ToG15 panics on unrecognised input — Layer 2 turns
// those into errors before invoking the primitive.
func TestEncodeMessage_Type1_BadGrid(t *testing.T) {
	cases := []struct {
		name string
		grid string
	}{
		{"random_garbage", "GARBAGE"},
		{"grid_lowercase", "fn20"},
		{"grid_4chars_wrong_alphabet", "FN2X"},
		{"report_three_digits", "+999"},
		{"report_leading_zero_padded", "+002"},
		{"report_no_sign", "11"},
		// Below the FT8 protocol's [-30, +99] band. Without the
		// range guard, this would store to the same g15 slot as ""
		// (blank message) — silently encoding wrong-but-wire-valid
		// data is the bad-bug class this case pins out.
		{"report_below_protocol_range", "-34"},
		// At the protocol min — must be accepted; this is the
		// boundary case for the new guard.
		{"report_at_protocol_min_minus_one", "-31"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := Message{Type: MessageTypeStd, Call1: "K1JT", Call2: "G4ABC", Grid: tc.grid}
			if _, err := EncodeMessage(msg); err == nil {
				t.Errorf("EncodeMessage(Grid=%q) = nil err, want validation error", tc.grid)
			}
		})
	}
}

// TestEncodeMessage_Type1_ResultIsIndependent confirms the returned
// slice is safe for the caller to mutate without affecting future
// calls. Important because BitBuilder.Bits() aliases internal
// storage — if EncodeMessage returned that directly without
// detachment, a caller mutating bits[28] would corrupt the next
// call's output. (Right now each call constructs a fresh
// BitBuilder, so the slices are naturally independent.)
func TestEncodeMessage_Type1_ResultIsIndependent(t *testing.T) {
	msg := Message{Type: MessageTypeStd, Call1: "K1JT", Call2: "G4ABC", Grid: "FN20"}
	first, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage #1: %v", err)
	}
	original := slices.Clone(first)
	first[28] = 99 // mutation
	second, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage #2: %v", err)
	}
	if !slices.Equal(second, original) {
		t.Errorf("second call output differs from first's pre-mutation snapshot — outputs share storage")
	}
}

// bitsOfValue expands value's low nbits into bit-per-byte form,
// MSB-first. Independent of BitBuilder's loop body — a regression
// in Append that flipped the shift direction would NOT propagate
// here.
func bitsOfValue(value uint64, nbits int) []byte {
	out := make([]byte, nbits)
	for i := range nbits {
		out[i] = byte((value >> (nbits - 1 - i)) & 1)
	}
	return out
}

func boolBitByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// testName produces a stable, lowercased label for sub-test names.
// MessageType.String() would be a nicer addition to message.go but
// is deferred — adding it now expands Phase 2B scope unnecessarily.
func (mt MessageType) testName() string {
	switch mt {
	case MessageTypeStd:
		return "type_std"
	case MessageTypeEUVHFP:
		return "type_eu_vhf_p"
	case MessageTypeRTTYRU:
		return "type_rtty_ru"
	case MessageTypeNonStdCall:
		return "type_nonstd_call"
	case MessageTypeEUVHFHash:
		return "type_eu_vhf_hash"
	case MessageTypeFreeText:
		return "type_free_text"
	case MessageTypeDXpedition:
		return "type_dxpedition"
	case MessageTypeFieldDayANSI:
		return "type_field_day_ansi"
	case MessageTypeFieldDayRTTY:
		return "type_field_day_rtty"
	case MessageTypeTelemetry:
		return "type_telemetry"
	default:
		return "type_unknown"
	}
}
