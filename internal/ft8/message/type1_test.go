package message

import (
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- Pack Type 1 -------------------------------------------------

// TestPackType1_Vectors packs each test vector's human-readable text via Pack
// and verifies the result matches the expected hex payload.
func TestPackType1_Vectors(t *testing.T) {
	vectors := loadType1VectorsMsg(t)
	for _, v := range vectors {
		t.Run(v.Text, func(t *testing.T) {
			msg := parseType1Text(t, v.Text)
			msg.MsgType = TypeStandard

			got, err := Pack(msg)
			require.NoError(t, err)
			require.Equal(t, v.Hex, hex.EncodeToString(got[:]),
				"packed payload mismatch for %q", v.Text)
		})
	}
}

// TestUnpackType1_Vectors unpacks each test vector's hex payload via Unpack
// and verifies the resulting Message.String() matches the expected text.
func TestUnpackType1_Vectors(t *testing.T) {
	vectors := loadType1VectorsMsg(t)
	for _, v := range vectors {
		t.Run(v.Text, func(t *testing.T) {
			payload := hexToPayload(t, v.Hex)
			msg, err := Unpack(payload)
			require.NoError(t, err)
			require.Equal(t, TypeStandard, msg.MsgType)
			require.Equal(t, v.Text, msg.String())
		})
	}
}

// TestType1_RoundTrip verifies that Pack → Unpack → String() reproduces the
// original text for every Type 1 test vector.
func TestType1_RoundTrip(t *testing.T) {
	vectors := loadType1VectorsMsg(t)
	for _, v := range vectors {
		t.Run(v.Text, func(t *testing.T) {
			msg := parseType1Text(t, v.Text)
			msg.MsgType = TypeStandard

			payload, err := Pack(msg)
			require.NoError(t, err)

			decoded, err := Unpack(payload)
			require.NoError(t, err)
			require.Equal(t, v.Text, decoded.String())
		})
	}
}

// TestUnpackType1_FieldValues unpacks each vector and verifies the encoded
// field values (N28a, N28b, P1, P2, IR, IGrid4) match the expected values.
func TestUnpackType1_FieldValues(t *testing.T) {
	vectors := loadType1VectorsMsg(t)
	for _, v := range vectors {
		t.Run(v.Text, func(t *testing.T) {
			payload := hexToPayload(t, v.Hex)
			msg, err := Unpack(payload)
			require.NoError(t, err)

			require.Equal(t, v.C28a, msg.N28a, "N28a")
			require.Equal(t, v.P1 != 0, msg.P1, "P1")
			require.Equal(t, v.C28b, msg.N28b, "N28b")
			require.Equal(t, v.P2 != 0, msg.P2, "P2")
			require.Equal(t, v.IR != 0, msg.IR, "IR")
			require.Equal(t, v.G15, msg.IGrid4, "IGrid4")
		})
	}
}

// TestPackDoesNotMutateMessage verifies that Pack does not modify the input Message.
func TestPackDoesNotMutateMessage(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}

	// Take a snapshot of zero-value encoded fields before Pack.
	require.Equal(t, uint32(0), msg.N28a)
	require.Equal(t, uint32(0), msg.N28b)
	require.False(t, msg.P1)
	require.False(t, msg.P2)
	require.False(t, msg.IR)
	require.Equal(t, uint16(0), msg.IGrid4)

	_, err := Pack(msg)
	require.NoError(t, err)

	// Encoded fields must remain zero — Pack must not write back.
	require.Equal(t, uint32(0), msg.N28a, "Pack must not mutate N28a")
	require.Equal(t, uint32(0), msg.N28b, "Pack must not mutate N28b")
	require.False(t, msg.P1, "Pack must not mutate P1")
	require.False(t, msg.P2, "Pack must not mutate P2")
	require.False(t, msg.IR, "Pack must not mutate IR")
	require.Equal(t, uint16(0), msg.IGrid4, "Pack must not mutate IGrid4")
}

// --------------- Pack Type 1 — manual messages --------------------------------

func TestPackType1_CQ(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "CQ W1AW FN31", decoded.String())
}

func TestPackType1_Report(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "W1AW",
		Call2:   "VK2XYZ",
		Grid:    "-12",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "W1AW VK2XYZ -12", decoded.String())
}

func TestPackType1_RogerReport(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "VK2XYZ",
		Call2:   "W1AW",
		Grid:    "R-12",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "VK2XYZ W1AW R-12", decoded.String())
}

func TestPackType1_RR73(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "W1AW",
		Call2:   "VK2XYZ",
		Grid:    "RR73",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "W1AW VK2XYZ RR73", decoded.String())
}

func TestPackType1_EmptyGrid(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "W1AW",
		Call2:   "VK2XYZ",
		Grid:    "",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "W1AW VK2XYZ", decoded.String())
}

func TestPackType1_CQNum(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ 350",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "CQ 350 W1AW FN31", decoded.String())
}

func TestPackType1_CQDX(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ DX",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "CQ DX W1AW FN31", decoded.String())
}

func TestPackType1_DE(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "DE",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "DE W1AW FN31", decoded.String())
}

func TestPackType1_CQDXRogerGrid(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ DX",
		Call2:   "W1AW",
		Grid:    "R FN31",
	}
	payload, err := Pack(msg)
	require.NoError(t, err)

	decoded, err := Unpack(payload)
	require.NoError(t, err)
	require.Equal(t, "CQ DX W1AW R FN31", decoded.String())
}

// --------------- splitType1Text edge cases ------------------------------------

func TestSplitType1Text_CQDXRogerGrid(t *testing.T) {
	// "CQ DX W1AW R FN31" must produce ["CQ DX", "W1AW", "R FN31"],
	// not ["CQ DX", "W1AW", "R", "FN31"].
	parts := splitType1Text("CQ DX W1AW R FN31")
	require.Equal(t, []string{"CQ DX", "W1AW", "R FN31"}, parts)
}

func TestSplitType1Text_CQNumRogerGrid(t *testing.T) {
	parts := splitType1Text("CQ 350 W1AW R FN31")
	require.Equal(t, []string{"CQ 350", "W1AW", "R FN31"}, parts)
}

func TestSplitType1Text_SimpleRogerGrid(t *testing.T) {
	parts := splitType1Text("VK2XYZ W1AW R FN31")
	require.Equal(t, []string{"VK2XYZ", "W1AW", "R FN31"}, parts)
}

func TestSplitType1Text_NoCQ_NoRoger(t *testing.T) {
	parts := splitType1Text("W1AW VK2XYZ -12")
	require.Equal(t, []string{"W1AW", "VK2XYZ", "-12"}, parts)
}

func TestSplitType1Text_CQDXSimpleGrid(t *testing.T) {
	parts := splitType1Text("CQ DX W1AW FN31")
	require.Equal(t, []string{"CQ DX", "W1AW", "FN31"}, parts)
}

// --------------- Pack Type 1 — error cases -----------------------------------

func TestPackType1_EmptyCall1(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	_, err := Pack(msg)
	require.Error(t, err)
}

func TestPackType1_EmptyCall2(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "",
		Grid:    "FN31",
	}
	_, err := Pack(msg)
	require.Error(t, err)
}

func TestPackType1_InvalidCallsign(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "A", // too short
		Grid:    "FN31",
	}
	_, err := Pack(msg)
	require.Error(t, err)
}

func TestPackType1_InvalidGrid(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ",
		Call2:   "W1AW",
		Grid:    "INVALID",
	}
	_, err := Pack(msg)
	require.Error(t, err)
}

func TestPackType1_InvalidCQModifier_TwoDigits(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ 12",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	_, err := Pack(msg)
	require.Error(t, err)
	// The outer error wraps the CQ-specific error from encodeCallField.
	inner := stderrors.Unwrap(err)
	require.NotNil(t, inner)
	require.Contains(t, inner.Error(), "invalid CQ modifier")
}

func TestPackType1_InvalidCQModifier_LongSuffix(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ TOOLONG",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	_, err := Pack(msg)
	require.Error(t, err)
	inner := stderrors.Unwrap(err)
	require.NotNil(t, inner)
	require.Contains(t, inner.Error(), "invalid CQ modifier")
}

func TestPackType1_InvalidCQModifier_MixedAlphaNum(t *testing.T) {
	msg := &Message{
		MsgType: TypeStandard,
		Call1:   "CQ A1",
		Call2:   "W1AW",
		Grid:    "FN31",
	}
	_, err := Pack(msg)
	require.Error(t, err)
	inner := stderrors.Unwrap(err)
	require.NotNil(t, inner)
	require.Contains(t, inner.Error(), "invalid CQ modifier")
}

// --------------- trimUpper ----------------------------------------------------

func TestTrimUpper_MultipleLeadingSpaces(t *testing.T) {
	require.Equal(t, "W1AW", trimUpper("  W1AW"))
}

func TestTrimUpper_MultipleTrailingSpaces(t *testing.T) {
	require.Equal(t, "W1AW", trimUpper("W1AW   "))
}

func TestTrimUpper_BothSides(t *testing.T) {
	require.Equal(t, "W1AW", trimUpper("  W1AW  "))
}

func TestTrimUpper_Lowercase(t *testing.T) {
	require.Equal(t, "CQ W1AW", trimUpper("  cq w1aw  "))
}

func TestTrimUpper_Tabs(t *testing.T) {
	require.Equal(t, "CQ", trimUpper("\tCQ\t"))
}

func TestTrimUpper_Empty(t *testing.T) {
	require.Equal(t, "", trimUpper(""))
	require.Equal(t, "", trimUpper("   "))
}

func TestTrimUpper_InternalSpacesPreserved(t *testing.T) {
	require.Equal(t, "CQ DX", trimUpper("  CQ DX  "))
}

// --------------- Message.String() --------------------------------------------

func TestMessageString_Standard(t *testing.T) {
	m := &Message{MsgType: TypeStandard, Call1: "CQ", Call2: "W1AW", Grid: "FN31"}
	require.Equal(t, "CQ W1AW FN31", m.String())
}

func TestMessageString_StandardNoGrid(t *testing.T) {
	m := &Message{MsgType: TypeStandard, Call1: "W1AW", Call2: "VK2XYZ"}
	require.Equal(t, "W1AW VK2XYZ", m.String())
}

func TestMessageString_FreeText(t *testing.T) {
	m := &Message{MsgType: TypeFreeText, FreeText: "TNX BOB 73 GL"}
	require.Equal(t, "TNX BOB 73 GL", m.String())
}

func TestMessageString_Unsupported(t *testing.T) {
	m := &Message{MsgType: TypeNonStandard}
	require.Contains(t, m.String(), "Non-Standard")
}

// --------------- Unpack — unsupported types ----------------------------------

func TestUnpack_UnsupportedI3(t *testing.T) {
	var payload [MsgBytes]byte
	PackBits(payload[:], 74, 3, 2) // i3=2, not supported
	_, err := Unpack(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

func TestUnpack_Type4Unsupported(t *testing.T) {
	var payload [MsgBytes]byte
	PackBits(payload[:], 74, 3, 4) // i3=4
	_, err := Unpack(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not yet supported")
}

func TestUnpack_I3_0_N3_1_Unsupported(t *testing.T) {
	var payload [MsgBytes]byte
	// i3=0, n3=1
	PackBits(payload[:], 74, 3, 0)
	PackBits(payload[:], 71, 3, 1)
	_, err := Unpack(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

// --------------- Pack — unsupported types ------------------------------------

func TestPack_UnsupportedType(t *testing.T) {
	msg := &Message{MsgType: TypeNonStandard}
	_, err := Pack(msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

// --------------- Helpers -----------------------------------------------------

// type1VectorMsg mirrors the JSON schema with uint types matching Message fields.
type type1VectorMsg struct {
	Text string `json:"text"`
	C28a uint32 `json:"c28a"`
	P1   int    `json:"p1"`
	C28b uint32 `json:"c28b"`
	P2   int    `json:"p2"`
	IR   int    `json:"ir"`
	G15  uint16 `json:"g15"`
	I3   int    `json:"i3"`
	Hex  string `json:"hex"`
	Note string `json:"note"`
}

func loadType1VectorsMsg(t *testing.T) []type1VectorMsg {
	t.Helper()
	data, err := os.ReadFile("testdata/type1_vectors.json")
	require.NoError(t, err)
	var vectors []type1VectorMsg
	require.NoError(t, json.Unmarshal(data, &vectors))
	require.NotEmpty(t, vectors)
	return vectors
}

func hexToPayload(t *testing.T, h string) [MsgBytes]byte {
	t.Helper()
	b, err := hex.DecodeString(h)
	require.NoError(t, err)
	require.Len(t, b, MsgBytes)
	var payload [MsgBytes]byte
	copy(payload[:], b)
	return payload
}

// parseType1Text splits a Type 1 human-readable text into Call1, Call2, Grid.
// Handles compound first fields like "CQ 350" and "CQ DX".
func parseType1Text(t *testing.T, text string) *Message {
	t.Helper()

	msg := &Message{}
	parts := splitType1Text(text)

	switch len(parts) {
	case 2:
		msg.Call1 = parts[0]
		msg.Call2 = parts[1]
	case 3:
		msg.Call1 = parts[0]
		msg.Call2 = parts[1]
		msg.Grid = parts[2]
	default:
		t.Fatalf("unexpected number of parts in %q: %d", text, len(parts))
	}
	return msg
}

// splitType1Text splits a Type 1 text into 2 or 3 logical fields.
// It handles compound first fields:
//
//	"CQ 350 W1AW FN31"       → ["CQ 350", "W1AW", "FN31"]
//	"CQ DX W1AW FN31"        → ["CQ DX", "W1AW", "FN31"]
//	"CQ DX W1AW R FN31"      → ["CQ DX", "W1AW", "R FN31"]
//	"CQ W1AW FN31"           → ["CQ", "W1AW", "FN31"]
//	"W1AW VK2XYZ -12"        → ["W1AW", "VK2XYZ", "-12"]
//	"W1AW VK2XYZ"            → ["W1AW", "VK2XYZ"]
//
// It also handles compound grid fields:
//
//	"VK2XYZ W1AW R FN31"     → ["VK2XYZ", "W1AW", "R FN31"]
func splitType1Text(text string) []string {
	words := splitWords(text)
	if len(words) == 0 {
		return nil
	}

	// Handle "CQ nnn" or "CQ XXXX" compound first field.
	if len(words) >= 3 && words[0] == "CQ" {
		suffix := words[1]
		// CQ nnn (3-digit freq) or CQ XXXX (1–4 letter suffix).
		if (len(suffix) == 3 && isAllDigitStr(suffix)) ||
			(len(suffix) >= 1 && len(suffix) <= 4 && isAllLetterStr(suffix)) {
			first := words[0] + " " + words[1]
			rest := words[2:]
			// Apply R-grid compounding to the remainder (e.g. "W1AW R FN31" → "W1AW", "R FN31").
			result := []string{first}
			result = append(result, compoundRGrid(rest)...)
			return result
		}
	}

	// Handle "R FN31" compound grid field (Roger + grid at end).
	return compoundRGrid(words)
}

// compoundRGrid merges a trailing "R" + grid into a single "R grid" field
// if the slice has at least 3 elements and the second-to-last word is "R".
func compoundRGrid(words []string) []string {
	if len(words) >= 3 && words[len(words)-2] == "R" {
		grid := words[len(words)-2] + " " + words[len(words)-1]
		return append(words[:len(words)-2], grid)
	}

	return words
}

func splitWords(s string) []string {
	var words []string
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}

func isAllDigitStr(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

func isAllLetterStr(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return len(s) > 0
}
