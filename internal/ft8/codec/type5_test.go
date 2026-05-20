package codec

import (
	"errors"
	"slices"
	"testing"
)

// TestEncodeMessage_Type5_LayoutMatchesQEXTable1 verifies that
// EncodeMessage assembles a Type 5 (EU VHF hashes+g25) body in the
// exact bit layout QEX paper Table 1 specifies:
//
//	h12 | h22 | R1 | r3 | s11 | g25 | i3=5
//	 12    22    1    3    11    25     3
//
// The expected bit slice is built from Layer 1 primitives so a width
// or offset regression surfaces here rather than producing wire
// output that nothing else can decode.
func TestEncodeMessage_Type5_LayoutMatchesQEXTable1(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			"qex_paper_example",
			Message{
				Type:    MessageTypeEUVHFHash,
				Call1:   "G4ABC",
				Call2:   "PA9XYZ",
				AckBit:  true,
				Report3: 5, // → display "57"
				Serial:  7, // → "0007"
				Grid6:   "JO22DB",
			},
		},
		{
			"min_report_min_serial_no_ack",
			Message{
				Type:    MessageTypeEUVHFHash,
				Call1:   "K1JT",
				Call2:   "G4ABC",
				AckBit:  false,
				Report3: 0,
				Serial:  0,
				Grid6:   "AA00AA",
			},
		},
		{
			"max_report_max_serial",
			Message{
				Type:    MessageTypeEUVHFHash,
				Call1:   "W9XYZ",
				Call2:   "PA9XYZ",
				AckBit:  true,
				Report3: 7,
				Serial:  2047,
				Grid6:   "RR99XX",
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

			_, h12, _ := HashCodes(tc.msg.Call1)
			_, _, h22 := HashCodes(tc.msg.Call2)
			g25 := Grid6ToG25(tc.msg.Grid6)

			want := make([]byte, 0, MessageBits)
			for i := range h12Bits {
				want = append(want, byte((h12>>(h12Bits-1-i))&1))
			}
			for i := range HashBits22 {
				want = append(want, byte((h22>>(HashBits22-1-i))&1))
			}
			want = append(want, boolByte(tc.msg.AckBit))
			for i := range r3Bits {
				want = append(want, byte((uint64(tc.msg.Report3)>>(r3Bits-1-i))&1))
			}
			for i := range s11Bits {
				want = append(want, byte((uint64(tc.msg.Serial)>>(s11Bits-1-i))&1))
			}
			for i := range G25Bits {
				want = append(want, byte((g25>>(G25Bits-1-i))&1))
			}
			// i3 = 3 → bits 011 MSB-first.
			want = append(want, 0, 1, 1)

			if !slices.Equal(got, want) {
				t.Errorf("layout mismatch\n got=%v\nwant=%v", got, want)
			}
		})
	}
}

// TestEncodeMessage_Type5_I3TagIs3 pins the i3 tag at the wire's
// lowest 3 bits to 3 (011 MSB-first). A regression that swapped tag
// values across Type 1/2/3/4/5 would land here.
func TestEncodeMessage_Type5_I3TagIs3(t *testing.T) {
	got, err := EncodeMessage(Message{
		Type:    MessageTypeEUVHFHash,
		Call1:   "G4ABC",
		Call2:   "PA9XYZ",
		Report3: 5,
		Serial:  7,
		Grid6:   "JO22DB",
	})
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	if got[74] != 0 || got[75] != 1 || got[76] != 1 {
		t.Errorf("i3 bits at 74..76 = %d %d %d, want 0 1 1 (i3=3)", got[74], got[75], got[76])
	}
}

// TestEncodeMessage_Type5_Rejects pins the validateType5* gates. Each
// rejection case is a Message shape that violates one of the rules in
// encodeEUVHFHash's doc.
func TestEncodeMessage_Type5_Rejects(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"token_in_call1", Message{Type: MessageTypeEUVHFHash, Call1: "CQ", Call2: "G4ABC", Grid6: "JO22DB"}},
		{"token_in_call2", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "DE", Grid6: "JO22DB"}},
		{"empty_call1", Message{Type: MessageTypeEUVHFHash, Call2: "G4ABC", Grid6: "JO22DB"}},
		{"nonstd_call1", Message{Type: MessageTypeEUVHFHash, Call1: "PJ4/K1ABC", Call2: "G4ABC", Grid6: "JO22DB"}},
		{"report_out_of_range", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Report3: 8, Grid6: "JO22DB"}},
		{"serial_out_of_range", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Serial: 2048, Grid6: "JO22DB"}},
		{"empty_grid", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ"}},
		{"grid4_only", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Grid6: "JO22"}},
		{"grid_bad_subsquare", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Grid6: "JO22YY"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EncodeMessage(tc.msg); err == nil {
				t.Errorf("EncodeMessage(%+v) = nil err, want validation error", tc.msg)
			}
		})
	}
}

// TestType5_RoundTrip exercises Encode→Decode equivalence. Both call
// slots decode to the "<...>" sentinel + the matching Hash12 / Hash22
// values; the report, serial, grid, and ack all round-trip exactly.
func TestType5_RoundTrip(t *testing.T) {
	cases := []Message{
		{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", AckBit: true, Report3: 5, Serial: 7, Grid6: "JO22DB"},
		{Type: MessageTypeEUVHFHash, Call1: "K1JT", Call2: "W9XYZ", AckBit: false, Report3: 0, Serial: 0, Grid6: "FN20AA"},
		{Type: MessageTypeEUVHFHash, Call1: "G4WJS", Call2: "K1ABC", AckBit: true, Report3: 7, Serial: 2047, Grid6: "IO91MM"},
	}
	for _, in := range cases {
		t.Run(in.Call1+"_"+in.Call2, func(t *testing.T) {
			bits, err := EncodeMessage(in)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			got, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if got.Type != MessageTypeEUVHFHash {
				t.Errorf("Type = %d, want MessageTypeEUVHFHash", got.Type)
			}
			if got.Call1 != hashedCallSentinel || got.Call2 != hashedCallSentinel {
				t.Errorf("calls = (%q, %q), want both %q", got.Call1, got.Call2, hashedCallSentinel)
			}
			_, wantH12, _ := HashCodes(in.Call1)
			_, _, wantH22 := HashCodes(in.Call2)
			if got.Hash12 != uint16(wantH12) {
				t.Errorf("Hash12 = %d, want %d", got.Hash12, wantH12)
			}
			if got.Hash22 != wantH22 {
				t.Errorf("Hash22 = %d, want %d", got.Hash22, wantH22)
			}
			if got.AckBit != in.AckBit {
				t.Errorf("AckBit = %t, want %t", got.AckBit, in.AckBit)
			}
			if got.Report3 != in.Report3 {
				t.Errorf("Report3 = %d, want %d", got.Report3, in.Report3)
			}
			if got.Serial != in.Serial {
				t.Errorf("Serial = %d, want %d", got.Serial, in.Serial)
			}
			if got.Grid6 != in.Grid6 {
				t.Errorf("Grid6 = %q, want %q", got.Grid6, in.Grid6)
			}
		})
	}
}

// TestDecodeMessage_Type5_NotUnknown sanity-checks that i3=3 dispatch
// is wired up (DecodeMessage routes to decodeEUVHFHash rather than
// returning ErrUnknownMessageType).
func TestDecodeMessage_Type5_NotUnknown(t *testing.T) {
	in := Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Report3: 5, Serial: 7, Grid6: "JO22DB"}
	bits, err := EncodeMessage(in)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	_, err = DecodeMessage(bits)
	if errors.Is(err, ErrUnknownMessageType) {
		t.Error("DecodeMessage(Type 5 wire) returned ErrUnknownMessageType, want successful decode")
	}
}

// ---- Text layer ------------------------------------------------------------

// TestFormatMessage_Type5_BasicShapes pins the human-readable output
// per the QEX Table 1 example shape: "<call1> <call2> [R ]rrSSSS GRID6".
func TestFormatMessage_Type5_BasicShapes(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want string
	}{
		{
			"qex_example_with_ack",
			Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", AckBit: true, Report3: 5, Serial: 7, Grid6: "JO22DB"},
			"<G4ABC> <PA9XYZ> R 570007 JO22DB",
		},
		{
			"no_ack",
			Message{Type: MessageTypeEUVHFHash, Call1: "K1JT", Call2: "W9XYZ", Report3: 0, Serial: 0, Grid6: "FN20AA"},
			"<K1JT> <W9XYZ> 520000 FN20AA",
		},
		{
			"max_report_max_serial",
			Message{Type: MessageTypeEUVHFHash, Call1: "G4WJS", Call2: "K1ABC", AckBit: true, Report3: 7, Serial: 2047, Grid6: "IO91MM"},
			"<G4WJS> <K1ABC> R 592047 IO91MM",
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

// TestFormatMessage_Type5_SentinelAndPreBracketed pins the decoder-
// output lifecycle: an unresolved-hash decode produces a Message with
// "<...>" sentinels in both call slots; a Phase-4-resolved decode
// produces a Message with pre-bracketed strings. The formatter emits
// both forms verbatim — bracket wrapping only fires on plain
// std-shaped strings (the encoder-input case).
func TestFormatMessage_Type5_SentinelAndPreBracketed(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want string
	}{
		{
			"both_sentinels",
			Message{Type: MessageTypeEUVHFHash, Call1: "<...>", Call2: "<...>", AckBit: true, Report3: 5, Serial: 7, Grid6: "JO22DB"},
			"<...> <...> R 570007 JO22DB",
		},
		{
			"prebracketed_both",
			Message{Type: MessageTypeEUVHFHash, Call1: "<G4ABC>", Call2: "<PA9XYZ>", Report3: 5, Serial: 7, Grid6: "JO22DB"},
			"<G4ABC> <PA9XYZ> 570007 JO22DB",
		},
		{
			"mixed_sentinel_prebracketed",
			Message{Type: MessageTypeEUVHFHash, Call1: "<...>", Call2: "<PA9XYZ>", Report3: 5, Serial: 7, Grid6: "JO22DB"},
			"<...> <PA9XYZ> 570007 JO22DB",
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

// TestFormatMessage_Type5_Rejects pins the format-side validator. The
// format gate is laxer than encode's (accepts sentinel + pre-bracketed
// forms) but still catches empty slots, broken bracketing, illegal
// report/serial/grid values.
func TestFormatMessage_Type5_Rejects(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{"empty_call1", Message{Type: MessageTypeEUVHFHash, Call2: "PA9XYZ", Report3: 5, Serial: 7, Grid6: "JO22DB"}},
		{"empty_call2", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Report3: 5, Serial: 7, Grid6: "JO22DB"}},
		{"report_out_of_range", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Report3: 9, Serial: 7, Grid6: "JO22DB"}},
		{"serial_out_of_range", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Report3: 5, Serial: 3000, Grid6: "JO22DB"}},
		{"bad_grid", Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", Report3: 5, Serial: 7, Grid6: "JO22"}},
		{"broken_bracket_call1", Message{Type: MessageTypeEUVHFHash, Call1: "<abc>", Call2: "PA9XYZ", Report3: 5, Serial: 7, Grid6: "JO22DB"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FormatMessage(tc.msg); err == nil {
				t.Errorf("FormatMessage(%+v) = nil err, want validation error", tc.msg)
			}
		})
	}
}

// TestParseMessage_Type5_BasicShapes pins the classifier dispatch for
// Type 5 inputs. Angle brackets are stripped uniformly; the resulting
// Message holds plain strings (or the "<...>" sentinel when the input
// carried one).
func TestParseMessage_Type5_BasicShapes(t *testing.T) {
	cases := []struct {
		in   string
		want Message
	}{
		{
			"<G4ABC> <PA9XYZ> R 570007 JO22DB",
			Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", AckBit: true, Report3: 5, Serial: 7, Grid6: "JO22DB"},
		},
		{
			"<K1JT> <W9XYZ> 520000 FN20AA",
			Message{Type: MessageTypeEUVHFHash, Call1: "K1JT", Call2: "W9XYZ", Report3: 0, Serial: 0, Grid6: "FN20AA"},
		},
		{
			"<G4WJS> <K1ABC> R 592047 IO91MM",
			Message{Type: MessageTypeEUVHFHash, Call1: "G4WJS", Call2: "K1ABC", AckBit: true, Report3: 7, Serial: 2047, Grid6: "IO91MM"},
		},
		// Sentinel inputs (decoder output passed back through parser).
		{
			"<...> <...> R 570007 JO22DB",
			Message{Type: MessageTypeEUVHFHash, Call1: "<...>", Call2: "<...>", AckBit: true, Report3: 5, Serial: 7, Grid6: "JO22DB"},
		},
		// Lowercase normalised upstream.
		{
			"<g4abc> <pa9xyz> r 570007 jo22db",
			Message{Type: MessageTypeEUVHFHash, Call1: "G4ABC", Call2: "PA9XYZ", AckBit: true, Report3: 5, Serial: 7, Grid6: "JO22DB"},
		},
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

// TestParseMessage_Type5_RejectsBadShape pins parse-side rejections.
// Includes wrong field counts, bad report+serial token, and "R" in
// the wrong position.
func TestParseMessage_Type5_RejectsBadShape(t *testing.T) {
	cases := []string{
		"<G4ABC> <PA9XYZ> R 570007 JO22DB EXTRA", // too many tokens
		"<G4ABC> <PA9XYZ> R 570007",              // missing grid (drops below trigger)
		"<G4ABC> <PA9XYZ> X 570007 JO22DB",       // 5 tokens but middle isn't "R"
		"<G4ABC> <PA9XYZ> 51 0007 JO22DB",        // report too low (51 < 52)
		"<G4ABC> <PA9XYZ> ABC0007 JO22DB",        // non-digit report+serial token
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseMessage(in); err == nil {
				t.Errorf("ParseMessage(%q) = nil err, want rejection", in)
			}
		})
	}
}

// TestType5_TextRoundTrip exercises the parse → encode → decode →
// format chain. Both call slots become "<...>" sentinels on decode
// because the codec layer doesn't run Phase 4's hash table, so the
// formatted output differs from the input on both call sides:
// "<G4ABC> <PA9XYZ>" becomes "<...> <...>". The rest (report,
// serial, grid, ack) round-trips exactly.
func TestType5_TextRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		want string // sentinel-form expected output
	}{
		{"<G4ABC> <PA9XYZ> R 570007 JO22DB", "<...> <...> R 570007 JO22DB"},
		{"<K1JT> <W9XYZ> 520000 FN20AA", "<...> <...> 520000 FN20AA"},
		{"<G4WJS> <K1ABC> R 592047 IO91MM", "<...> <...> R 592047 IO91MM"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			parsed, err := ParseMessage(tc.in)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", tc.in, err)
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
			if got != tc.want {
				t.Errorf("round-trip(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseMessage_ClassifierOrder_Type5 pins that Type 5 dispatch
// fires BEFORE Type 4. The two-bracketed-calls + 6-char-grid shape is
// unambiguous; a missing grid OR a single bracket falls back to Type
// 4 territory.
func TestParseMessage_ClassifierOrder_Type5(t *testing.T) {
	cases := []struct {
		in   string
		want MessageType
	}{
		// Both bracketed + grid6 → Type 5.
		{"<G4ABC> <PA9XYZ> R 570007 JO22DB", MessageTypeEUVHFHash},
		// Both bracketed but no grid6 trailing → Type 4 (RRR slot fits).
		{"<G4ABC> <PA9XYZ> RRR", MessageTypeNonStdCall},
		// Single bracket → Type 4.
		{"<W9XYZ> PJ4/K1ABC RRR", MessageTypeNonStdCall},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseMessage(tc.in)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", tc.in, err)
			}
			if got.Type != tc.want {
				t.Errorf("ParseMessage(%q).Type = %d, want %d", tc.in, got.Type, tc.want)
			}
		})
	}
}

// TestG25ToGrid6_RoundTrip pins the Layer 1 inverse against Grid6ToG25
// over a representative set of 6-char grids covering each character
// position's alphabet extremes.
func TestG25ToGrid6_RoundTrip(t *testing.T) {
	cases := []string{
		"AA00AA",
		"RR99XX",
		"JO22DB",
		"FN20AA",
		"IO91MM",
		"KP20MP",
	}
	for _, grid := range cases {
		t.Run(grid, func(t *testing.T) {
			g25 := Grid6ToG25(grid)
			got := G25ToGrid6(g25)
			if got != grid {
				t.Errorf("G25ToGrid6(Grid6ToG25(%q)) = %q, want %q", grid, got, grid)
			}
		})
	}
}

// boolByte is a test-helper bool→byte converter (1 / 0), used to build
// expected bit slices for the layout assertions above.
func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
