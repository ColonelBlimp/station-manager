package codec

import (
	"errors"
	"slices"
	"testing"
)

// TestParseMessage_Type1_BasicShapes mirrors TestFormatMessage's
// basic-shape table, asserting that ParseMessage inverts FormatMessage
// for the canonical Type 1 layouts.
func TestParseMessage_Type1_BasicShapes(t *testing.T) {
	cases := []struct {
		text string
		want Message
	}{
		{"CQ G4ABC IO91", Message{Type: MessageTypeStd, Call1: "CQ", Call2: "G4ABC", Grid: "IO91"}},
		{"CQ G4ABC", Message{Type: MessageTypeStd, Call1: "CQ", Call2: "G4ABC"}},
		{"CQ DX G4ABC IO91", Message{Type: MessageTypeStd, Call1: "CQ DX", Call2: "G4ABC", Grid: "IO91"}},
		{"CQ POTA G4ABC IO91", Message{Type: MessageTypeStd, Call1: "CQ POTA", Call2: "G4ABC", Grid: "IO91"}},
		{"CQ 100 G4ABC IO91", Message{Type: MessageTypeStd, Call1: "CQ 100", Call2: "G4ABC", Grid: "IO91"}},
		{"DE G4ABC IO91", Message{Type: MessageTypeStd, Call1: "DE", Call2: "G4ABC", Grid: "IO91"}},
		{"QRZ G4ABC IO91", Message{Type: MessageTypeStd, Call1: "QRZ", Call2: "G4ABC", Grid: "IO91"}},
		{"K1ABC G4ABC FN20", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "FN20"}},
		{"K1ABC G4ABC -11", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "-11"}},
		{"K1ABC G4ABC R-09", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", AckBit: true, Grid: "-09"}},
		{"K1ABC G4ABC R FN20", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", AckBit: true, Grid: "FN20"}},
		{"K1ABC G4ABC RR73", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "RR73"}},
		{"K1ABC G4ABC 73", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "73"}},
		{"K1ABC G4ABC", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC"}},
		{"K1ABC/R G4ABC FN20", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Suffix1: true, Grid: "FN20"}},
		{"K1ABC G4ABC/R FN20", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Suffix2: true, Grid: "FN20"}},
		{"K1ABC/R G4ABC/R FN20", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Suffix1: true, Suffix2: true, Grid: "FN20"}},
		{"K1ABC G4ABC R", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", AckBit: true}},
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

// TestParseMessage_LowercaseNormalisation verifies the parser
// upper-cases ASCII letters before classification, so operator
// input "cq k1abc fn20" parses identically to "CQ K1ABC FN20".
func TestParseMessage_LowercaseNormalisation(t *testing.T) {
	cases := []struct {
		text string
		want Message
	}{
		{"cq k1abc fn20", Message{Type: MessageTypeStd, Call1: "CQ", Call2: "K1ABC", Grid: "FN20"}},
		{"K1ABC g4abc -11", Message{Type: MessageTypeStd, Call1: "K1ABC", Call2: "G4ABC", Grid: "-11"}},
		{"cq dx g4abc io91", Message{Type: MessageTypeStd, Call1: "CQ DX", Call2: "G4ABC", Grid: "IO91"}},
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

// TestParseMessage_WhitespaceCollapsing verifies that any sequence of
// whitespace separates fields equivalently — leading, trailing, and
// internal multi-space all collapse via strings.Fields.
func TestParseMessage_WhitespaceCollapsing(t *testing.T) {
	cases := []string{
		"CQ G4ABC IO91",
		"  CQ G4ABC IO91  ",
		"CQ\tG4ABC IO91",
		"CQ  G4ABC   IO91",
		"\nCQ G4ABC IO91\n",
	}
	want := Message{Type: MessageTypeStd, Call1: "CQ", Call2: "G4ABC", Grid: "IO91"}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			got, err := ParseMessage(text)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", text, err)
			}
			if got != want {
				t.Errorf("ParseMessage(%q):\n got=%+v\nwant=%+v", text, got, want)
			}
		})
	}
}

// TestParseMessage_Rejects covers the error paths.
func TestParseMessage_Rejects(t *testing.T) {
	// Inputs here must fail BOTH structured parse AND the Free Text
	// fallback (finding #3). The fallback accepts any input ≤ 13
	// chars composed entirely of f71-alphabet chars, so the rejection
	// cases below are inputs that violate one of those: out-of-alphabet
	// chars, or oversized after a structured-shape failure.
	cases := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"whitespace_only", "   \t\n"},
		{"call_with_garbage", "K1AB! G4ABC FN20"},        // '!' not in f71 alphabet
		{"too_many_fields", "K1ABC G4ABC FN20 EXTRA"},    // 22 chars > Free Text max 13
		{"r_separates_report", "K1ABC G4ABC R -11"},      // 17 chars > 13
		{"bare_r_alone_with_grid", "K1ABC G4ABC X FN20"}, // 18 chars > 13
		{"unknown_token_at_head", "FOO G4ABC FN20"},      // 14 chars > 13
		{"bad_grid_value", "K1ABC G4ABC GARBAGE"},        // 19 chars > 13
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseMessage(tc.text); err == nil {
				t.Errorf("ParseMessage(%q) = nil err, want error", tc.text)
			}
		})
	}
}

// TestParseMessage_EmptyReturnsSentinel pins the ErrEmptyMessage
// sentinel for empty/whitespace-only inputs.
func TestParseMessage_EmptyReturnsSentinel(t *testing.T) {
	for _, text := range []string{"", "  ", "\t", "\n"} {
		_, err := ParseMessage(text)
		if !errors.Is(err, ErrEmptyMessage) {
			t.Errorf("ParseMessage(%q) err=%v, want ErrEmptyMessage", text, err)
		}
	}
}

// ---- Free Text classifier + parsing (Phase 3A Step C) ----------------------

// TestParseMessage_FreeText_Dispatch pins the out-of-alphabet
// classifier rule: '.' or '?' in the input routes to Type 0.0,
// regardless of what else is in the input. Per the Phase 3A Step A
// design choice #3.
func TestParseMessage_FreeText_Dispatch(t *testing.T) {
	cases := []struct {
		name string
		text string
		want Message
	}{
		{"period_alone", "HELLO.", Message{Type: MessageTypeFreeText, FreeText: "HELLO."}},
		{"question_alone", "TEST?", Message{Type: MessageTypeFreeText, FreeText: "TEST?"}},
		{"period_with_spaces", "TNX 73.", Message{Type: MessageTypeFreeText, FreeText: "TNX 73."}},
		{"max_length_with_dot", "ABCDEFGHIJKL.", Message{Type: MessageTypeFreeText, FreeText: "ABCDEFGHIJKL."}},
		// Leading/trailing whitespace is trimmed; internal whitespace
		// preserved.
		{"trim_outer_whitespace", "  HELLO.  ", Message{Type: MessageTypeFreeText, FreeText: "HELLO."}},
		// Lowercase normalises through the same path.
		{"lowercase_normalises", "hello.", Message{Type: MessageTypeFreeText, FreeText: "HELLO."}},
		// A trigger char anywhere in the input dispatches to Free Text,
		// even if the rest looks like a structured Type 1 message.
		// Operator's call: punctuation is the explicit Free Text signal.
		{"callsign_shape_with_dot", "K1JT FN20.", Message{Type: MessageTypeFreeText, FreeText: "K1JT FN20."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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

// TestParseMessage_FreeText_RejectsOversized pins the validateFreeText
// gate inside parseFreeText. Inputs with a trigger char but more than
// 13 chars after trimming surface a parse error.
func TestParseMessage_FreeText_RejectsOversized(t *testing.T) {
	oversized := []string{
		"ABCDEFGHIJKLM.",    // 14 chars
		"HELLO WORLD?",      // 12 chars — should succeed
		"K1ABC G4ABC FN20.", // 17 chars — too long
	}
	for _, text := range oversized {
		t.Run(text, func(t *testing.T) {
			_, err := ParseMessage(text)
			// "HELLO WORLD?" is exactly 12 chars and should succeed.
			if text == "HELLO WORLD?" {
				if err != nil {
					t.Errorf("ParseMessage(%q): unexpected err %v (12 chars should succeed)", text, err)
				}
				return
			}
			if err == nil {
				t.Errorf("ParseMessage(%q) = nil err, want validation error (oversized Free Text)", text)
			}
		})
	}
}

// TestParseMessage_FreeText_NoTriggerFallback is the regression pin
// for finding #3: per QEX paper Table 1 the Type 0.0 example is
// "TNX BOB 73 GL", which contains no '.' or '?' — so the eager
// trigger-char dispatch wouldn't fire. The parser must fall back
// to Free Text when structured Type 1 parse fails AND the input
// is f71-compatible (≤ 13 chars, all chars in f71 alphabet).
//
// The earlier "Phase 3A design choice #3" (no implicit fallback)
// was spec-incorrect — Free Text without punctuation IS valid per
// the QEX paper.
func TestParseMessage_FreeText_NoTriggerFallback(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"TNX BOB 73 GL", "TNX BOB 73 GL"}, // QEX Table 1 canonical example
		{"HELLO", "HELLO"},                 // single token, in-alphabet
		{"73 OM", "73 OM"},                 // ham shorthand without punctuation
		{"FB 73", "FB 73"},                 // ham shorthand
		{"TNX 73", "TNX 73"},
		{"CQ", "CQ"},   // bare CQ doesn't fit "CQ <call>"; treated as Free Text
		{"DE", "DE"},   // bare DE
		{"QRZ", "QRZ"}, // bare QRZ
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got, err := ParseMessage(tc.text)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v — expected Free Text fallback per QEX Table 1", tc.text, err)
			}
			if got.Type != MessageTypeFreeText {
				t.Errorf("ParseMessage(%q).Type = %d, want MessageTypeFreeText", tc.text, got.Type)
			}
			if got.FreeText != tc.want {
				t.Errorf("ParseMessage(%q).FreeText = %q, want %q", tc.text, got.FreeText, tc.want)
			}
		})
	}
}

// ---- Full text → struct → bits → struct → text round-trip ------------------

// TestFormatParse_RoundTrip is the headline Phase 2D test: every
// canonical Type 1 message round-trips bit-for-bit AND text-for-text
// through ParseMessage → EncodeMessage → DecodeMessage → FormatMessage.
// This is the integration that the whole token / format / parse
// stack exists to support.
func TestFormatParse_RoundTrip(t *testing.T) {
	cases := []string{
		"CQ G4ABC IO91",
		"CQ G4ABC",
		"CQ DX G4ABC IO91",
		"CQ POTA G4ABC IO91",
		"CQ 100 G4ABC IO91",
		"DE G4ABC IO91",
		"QRZ G4ABC IO91",
		"G4ABC K1ABC FN20",
		"G4ABC K1ABC -11",
		"G4ABC K1ABC R-09",
		"G4ABC K1ABC R FN20",
		"G4ABC K1ABC RR73",
		"G4ABC K1ABC 73",
		"G4ABC K1ABC",
		"G4ABC/R K1ABC FN20",
		"G4ABC K1ABC/R FN20",
		"G4ABC/R K1ABC/R FN20",
		// Type 0.0 Free Text — `.` and `?` eager triggers
		"HELLO.",
		"73 OM.",
		"TNX BOB 73.",
		"WHAT?",
		"K1JT?",
		// Type 0.0 Free Text — no-trigger fallback (finding #3).
		// QEX Table 1 canonical example plus operator shorthand.
		"TNX BOB 73 GL",
		"73 OM",
		"FB 73",
		"HELLO",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			// text → struct (parse)
			msg1, err := ParseMessage(text)
			if err != nil {
				t.Fatalf("ParseMessage(%q): %v", text, err)
			}
			// struct → text (format) — round-trip 1
			text2, err := FormatMessage(msg1)
			if err != nil {
				t.Fatalf("FormatMessage: %v", err)
			}
			if text2 != text {
				t.Errorf("text round-trip: got %q, want %q", text2, text)
			}
			// struct → bits (encode)
			bits, err := EncodeMessage(msg1)
			if err != nil {
				t.Fatalf("EncodeMessage: %v", err)
			}
			// bits → struct (decode)
			msg2, err := DecodeMessage(bits)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if msg2 != msg1 {
				t.Errorf("struct round-trip:\n got=%+v\nwant=%+v", msg2, msg1)
			}
			// struct → bits again (re-encode) — triple-check
			bits2, err := EncodeMessage(msg2)
			if err != nil {
				t.Fatalf("EncodeMessage #2: %v", err)
			}
			if !slices.Equal(bits, bits2) {
				t.Errorf("bit round-trip differs after decode+re-encode")
			}
		})
	}
}
