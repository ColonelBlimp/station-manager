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
	// Phase 4.
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

	// Rover flags (r1) per call. Type 2 EU VHF /P uses these to
	// signal /P "rover" operation on either side; other types treat
	// them as reserved zero.
	Rover1 bool
	Rover2 bool

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
}
