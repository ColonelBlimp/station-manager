package codec

// Layer 2 wire-format constants per QEX paper Table 1 + Table 2.
// Centralised here so encode.go (write side) and decode.go (read
// side) reference one source of truth — the wire shape is an
// invariant shared by both directions, not a per-file detail.
//
// Per-primitive Layer 1 widths (CallsignBits, G15Bits, F71Bits,
// C58Bits) stay with their primitive files — they belong to the
// primitive's algorithm, not the message-format layer.

// MessageBits is the number of information bits in an FT8 message.
// Per QEX paper §2: "FT4 and FT8 transmissions always convey
// exactly 77 bits of user information."
const MessageBits = 77

// i3 tag — message-type discriminator written into the lowest 3
// bits of the 77-bit body. i3Offset = MessageBits - i3Width so the
// layout reads as "i3 spans [i3Offset, MessageBits)" at call sites.
const (
	i3Width  = 3
	i3Offset = MessageBits - i3Width
)

// Implemented i3 tag values. Per QEX paper Table 1 column 1 ("Type
// i3.n3"), the dotted-number IS the wire i3 for top-level types —
// Type N. → i3=N. Tags for unimplemented types (i3=3 RTTY RU, plus
// i3=6/7 which are unassigned per QEX Table 1) land alongside their
// encoders when they ship.
const (
	i3Zero       = 0 // i3=0 family (n3 sub-dispatch; Phase 3A wires n3=0 only)
	i3Std        = 1 // Type 1 (Std Msg)
	i3EUVHFP     = 2 // Type 2 (EU VHF /P)
	i3NonStdCall = 4 // Type 4 (NonStd Call)
	i3EUVHFHash  = 5 // Type 5 (EU VHF hashes+g25)
)

// QEX Table 1 i3.n3 tags for the i3=0 message-type family. n3
// occupies bits 71..73 (the 3 bits immediately above f71's 71-bit
// payload); i3=0 occupies bits 74..76. The family multiplexes:
//
//	n3 = 0: Free Text (Type 0.0)         — implemented
//	n3 = 1: DXpedition (Type 0.1)        — Phase 4
//	n3 = 3: Field Day Class A-F (0.3)    — Phase 4
//	n3 = 4: Field Day (0.4)              — Phase 4
//	n3 = 5: Telemetry (0.5)              — Phase 4
//	n3 = 2, 6, 7 are unassigned per QEX paper Table 1.
const (
	n3FreeText  = 0
	n3FieldBits = 3
)

// Type 4 (NonStd Call) bit-field widths per QEX Table 1 layout
//
//	h12 c58 h1 r2 c1 i3=4
//	 12  58  1  2  1   3   = 77 bits
//
// c58 itself is C58Bits from nonstdcall.go.
const (
	h12Bits = 12
	h1Bits  = 1
	r2Bits  = 2
	c1Bits  = 1
)

// Type 4 r2 token slot values per QEX Table 2. The 2-bit field
// encodes one of four reserved tokens. gridToR2 / r2ToGrid in
// encode.go map between these wire values and the canonical Grid
// string form.
const (
	r2Blank = 0
	r2RRR   = 1
	r2RR73  = 2
	r2_73   = 3
)

// Type 5 (EU VHF hashes+g25) bit-field widths per QEX Table 1 layout:
//
//	h12 h22 R1 r3 s11 g25 i3=5
//	 12  22  1  3  11  25   3   = 77 bits
//
// h12Bits + HashBits22 (hashcodes.go) + r3Bits + s11Bits + G25Bits
// (grid.go) + i3Width = 12+22+1+3+11+25+3 = 77.
const (
	r3Bits  = 3
	s11Bits = 11
)

// r3 maps a 3-bit code (0..7) to a 2-digit signal report string per
// QEX paper Table 2 ("Values 0 – 7 encode a signal report displayed
// as 52, 53, … 59"). r3Bias = 52 lifts the code to its display value.
const (
	r3Min  = 0
	r3Max  = 7
	r3Bias = 52
)

// s11Max bounds the Type 5 serial-number slot — 11 bits → 0..2047
// per QEX Table 2 ("Serial number 0 – 2047").
const s11Max = (1 << s11Bits) - 1
