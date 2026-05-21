package codec

// MessageType discriminates the ten i3.n3 type codes QEX paper
// Table 1 enumerates (i3=1..5 plus i3=0.{0,1,3,4,5}). Stored as a
// numeric tag inside Message so encoders can dispatch on it.
//
// The i3.n3 encoding on the wire (the low 3 or 6 bits of the 77-bit
// body) is a consequence of MessageType, not its definition — the
// packer per type writes the right tag at the right offset.
type MessageType uint8

const (
	// MessageTypeStd is QEX Table 1 Type 1 ("Std Msg"): two standard
	// callsigns + r1 rover flags + R1 ack + g15 grid/report + i3=1.
	// The everyday "K1JT G4ABC FN20" exchange.
	MessageTypeStd MessageType = iota + 1

	// MessageTypeEUVHFP is QEX Table 1 Type 2 ("EU VHF /P").
	// Phase 3.
	MessageTypeEUVHFP

	// MessageTypeRTTYRU is QEX Table 1 Type 3 ("RTTY Roundup").
	MessageTypeRTTYRU

	// MessageTypeNonStdCall is QEX Table 1 Type 4 ("NonStd Call").
	// Phase 3.
	MessageTypeNonStdCall

	// MessageTypeEUVHFHash is QEX Table 1 Type 5 ("EU VHF hashes+g25").
	// Phase 3.
	MessageTypeEUVHFHash

	// MessageTypeFreeText is QEX Table 1 Type 0.0 ("Free Text").
	// Phase 3.
	MessageTypeFreeText

	// MessageTypeDXpedition is QEX Table 1 Type 0.1 ("DXpedition").
	// Phase 4.
	MessageTypeDXpedition

	// MessageTypeFieldDayANSI is QEX Table 1 Type 0.3 ("Field Day").
	// Phase 4.
	MessageTypeFieldDayANSI

	// MessageTypeFieldDayRTTY is QEX Table 1 Type 0.4 ("Field Day RTTY").
	// Phase 4.
	MessageTypeFieldDayRTTY

	// MessageTypeTelemetry is QEX Table 1 Type 0.5 ("Telemetry").
	// Phase 4.
	MessageTypeTelemetry
)

// Message is the canonical Layer 2 representation of an FT8 message
// of any type per QEX paper Table 1. One concrete struct (not an
// interface) with field overlap across types: Call1/Call2/AckBit/
// Grid are reused by every type that has them; per-type-unique
// fields sit alongside and are zero-valued for types that don't use
// them. The Type field is the discriminator the packer dispatches
// on.
//
// Construction is direct — there is no `NewStdMessage(...)` helper.
// Validation happens at Pack time. This matches the project rule
// "build specific, not generic": three fields named for what they
// hold beats a hierarchy of per-type structs hidden behind an
// interface.
//
// String fields are case-sensitive uppercase ASCII per the FT8
// spec — the encoder will reject lowercase callsigns rather than
// silently uppercasing them, since case-folding is the caller's
// concern (a UI layer might want to normalise before reaching here,
// whereas a wire-receive path would consider lowercase a bug).
type Message struct {
	Type MessageType

	// Two callsigns making up the contact pair. Almost every message
	// type uses both. For hash-bearing types (Type 4 NonStdCall,
	// Type 0.1 DXpedition), one side is a plain string and the
	// packer hashes it at pack time per QEX Table 1's hash-slot
	// semantics.
	Call1 string
	Call2 string

	// Suffix1/Suffix2 hold the per-callsign 1-bit suffix flag that
	// Types 1 and 2 share at the same bit offset but interpret
	// differently: Type 1 calls this slot "r1" and renders it as /R
	// (rover); Type 2 calls it "p1" and renders it as /P (portable).
	// FormatMessage decides /R vs /P from the Type discriminator.
	// Other types treat these bits as reserved zero.
	Suffix1 bool
	Suffix2 bool

	// Acknowledgment bit (R1) per QEX Table 1 — used by Type 1
	// (Std Msg), Type 2 (EU VHF /P), Type 3 (RTTY RU), Type 0.3 /
	// 0.4 (Field Day) to signal "I have your report" on the second
	// pass of the QSO. Zero for other types.
	AckBit bool

	// Grid is the multi-modal g15 slot for Type 1 / Type 2 per QEX
	// paper Table 2. Accepts:
	//
	//   - 4-character Maidenhead grid locator: "FN20", "IO91" etc.
	//   - Signed signal report: "-11", "+02", "-30".
	//   - Reserved tokens: "" (blank), "RRR", "RR73", "73".
	//
	// Only one of these forms fits the 15-bit slot at a time —
	// the encoder hands the string straight to Grid4ToG15 which
	// routes it. For Type 5 (EU VHF hashes+g25) use Grid6 instead.
	Grid string

	// FreeText is the 1-13 character payload for Type 0.0 (Free Text)
	// per QEX paper Table 1. Encoded into the 71-bit f71 slot via
	// FreeTextToF71's base-42 polynomial.
	//
	// Operator-typed content must already be normalised: uppercase,
	// each character in the f71 alphabet (space + 0-9 + A-Z + + - . / ?),
	// length ≤ 13. EncodeMessage rejects out-of-shape input rather than
	// silently padding or substituting.
	//
	// On round-trip through EncodeMessage → DecodeMessage, leading
	// spaces are lost (the encoder right-justifies via Fortran-style
	// adjustr — see F71ToFreeText's doc). Internal and trailing
	// spaces survive. The lost-leading-space asymmetry is inherent
	// to the encoding, not a codec bug.
	FreeText string

	// Hash12 holds the 12-bit hash value (the h12 wire field) for
	// Type 4 (NonStd Call) and Type 5 (EU VHF hashes+g25) messages
	// when the receiver-side hash table hasn't resolved it to a
	// callsign string. The hashed Call slot then holds the WSJT-X
	// "<...>" sentinel; Phase 4's hash table fills the resolved
	// string in once it sees the callsign on a later decode and the
	// wire-level h12 matches.
	//
	// Zero on encoded messages (the caller supplied the original
	// callsign string, so the encoder computes h12 fresh from
	// HashCodes) and on decoded messages where the hash has already
	// been resolved. Non-zero only on decoded Type 4 / Type 5
	// messages whose hash side is still unresolved.
	Hash12 uint16

	// Hash22Call1 holds the 22-bit hash value when Call1 is a
	// hash-partition c28 (Types 1/2/3 — Std Msg, EU VHF /P, RTTY
	// Roundup). When non-zero, Call1 carries the WSJT-X "<...>"
	// sentinel and the receiver's running hash table can resolve
	// the real callsign via HashTable.Resolve / LookupH22.
	//
	// Zero on encoded messages (the caller supplied the callsign
	// string directly) and on decoded messages where Call1 was a
	// standard callsign or a recognised token. Non-zero only when
	// Call1's c28 landed in the 22-bit-hash partition
	// [nTokens, stdCallOffset) — used by stations referencing a
	// previously-transmitted compound or special-event callsign.
	Hash22Call1 uint32

	// Hash22Call2 holds the 22-bit hash value for Call2 in two
	// distinct cases:
	//
	//   - Type 5 (EU VHF hashes+g25): Call2 ALWAYS hashes to 22
	//     bits on the wire (the h22 field per QEX Table 1). Same
	//     Phase-4-resolution contract as Hash12.
	//   - Types 1/2/3 (Std Msg, EU VHF /P, RTTY RU): Call2 is a
	//     hash-partition c28 — same semantics as Hash22Call1 but
	//     for the second call slot.
	//
	// Zero on encoded messages and on decoded messages where
	// Call2 was a standard callsign or a recognised token.
	Hash22Call2 uint32

	// Grid6 is the 6-character Maidenhead grid for Type 5 (EU VHF
	// hashes+g25). Distinct from Grid (the multi-modal g15 slot
	// shared by Types 1/2) because the g25 wire slot is strictly
	// a 6-char grid — no reserved-token or signed-report
	// multiplexing. Encoded via Grid6ToG25.
	Grid6 string

	// Report3 is the 3-bit signal-report code (0..7) for Type 5,
	// rendered as the 2-digit form "52".."59" per QEX paper
	// Table 2. Other types (3, 0.3, 0.4) reuse the same r3 slot
	// width but display ranges differ; FormatMessage handles per-
	// type rendering. Zero on types that don't carry a report.
	Report3 uint8

	// Serial is the contest-style serial number for Type 5 (s11
	// slot, 0..2047) and Type 3 (s13 slot serial form, 0..7999).
	// Per-type validators bound the range. Zero on types that
	// don't carry a serial.
	Serial uint16

	// TU is the t1 prefix bit for Type 3 (RTTY Roundup). 1 = the
	// rendered text begins with "TU;" per the WSJT-X convention
	// for ack'ing the prior exchange. Zero on other types.
	TU bool

	// StateProvince is the Type 3 (RTTY Roundup) state/province
	// exchange form. When non-empty, the encoder uses the state
	// form of the s13 slot (one of the 65 US states / Canadian
	// provinces from the QEX ref [14] states_provinces.txt
	// lookup table). When empty, the encoder uses the serial
	// form via the Serial field. Decoded Type 3 messages populate
	// either Serial (s13 ∈ [0, 7999]) or StateProvince (s13 ∈
	// [8001, 8065]) but never both.
	StateProvince string
}
