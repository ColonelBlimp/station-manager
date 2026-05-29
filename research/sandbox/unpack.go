package sandbox

import (
	"fmt"
	"math/big"
	"strings"
)

// FT8 source-decoder constants per QEX paper ref [14]:
//
//   - ntokens   = 2063592   c28 offset for the standard-callsign range.
//     Values [0, ntokens) carry special tokens:
//     "DE" (0), "QRZ" (1), "CQ" (2), "CQ nnn"
//     (3..1002), "CQ aaaa" (1003..458978), with
//     the remaining slots reserved for future
//     extensions.
//
//   - max22hash = 4194304   = 2²², the 22-bit hash-placeholder range.
//     Values [ntokens, ntokens+max22hash) mean
//     "this is a callsign known only by its
//     22-bit hash; resolve via the rolling hash
//     table or render as a placeholder".
//
//   - callBase  = ntokens+max22hash = 6_257_896.
//     Standard callsigns occupy [callBase, 2²⁸).
//
//   - maxGrid4  = 32400     g15 boundary. Values [0, maxGrid4) carry a
//     4-character Maidenhead grid; values
//     [maxGrid4, …) carry signal reports / Roger
//     / 73 tokens.
const (
	ntokens   = 2063592
	max22hash = 4194304
	callBase  = ntokens + max22hash
	maxGrid4  = 32400
)

// Callsign mixed-radix alphabets per QEX ref [14] std_call_to_c28.f90.
// A standard FT8 callsign occupies 6 character slots: pos1 from callA1
// (leading space allowed), pos2 from callA2 (letter or digit, no
// space), pos3 from callA3 (mandatory digit), pos4-6 from callA4
// (letter or space). Total addressable space:
// 37 × 36 × 10 × 27³ = 262 177 560.
const (
	callA1 = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" // 37 chars
	callA2 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"  // 36 chars
	callA3 = "0123456789"                            // 10 chars
	callA4 = " ABCDEFGHIJKLMNOPQRSTUVWXYZ"           // 27 chars
)

// freeTextAlphabet is the 42-character base used for Type 0.0 free
// text, per QEX ref [14] free_text_to_f71.f90. Includes a leading
// space for left-padding short messages (right-justified, so trailing
// content; leading spaces are stripped on display).
const freeTextAlphabet = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ+-./?"

// UnpackResult is the outcome of unpacking a 77-bit FT8 payload into
// human-readable text. OK == true means the message type was supported
// and the decoded callsigns were resolvable (no hash-placeholder
// fallbacks). Detail carries diagnostic information for unsupported
// types and for partial decodes that hit a hash placeholder.
type UnpackResult struct {
	OK     bool
	Text   string
	I3     int
	N3     int
	Detail string
}

// Unpack77 decodes the 77-bit FT8 payload (msg91[0:77] from
// BPDecode) into a human-readable message string. Type 4 messages
// and any Type 1/2 with hashed callsigns will surface placeholders
// for the hashed addressee. Use Unpack77WithHashes to resolve them
// via a CallsignHashTable.
//
// Dispatch is on the trailing 3 bits of the payload (i3); for the
// i3=0 family the 3 bits immediately preceding i3 (n3) select the
// sub-type.
//
// Currently implemented:
//   - i3=1, i3=2 (Type 1, Type 2 — standard QSO with /R or /P suffix)
//   - i3=0 / n3=0 (Type 0.0 free text)
//   - i3=4 (Type 4, nonstandard callsigns)
//
// Deferred:
//   - Other i3=0 sub-types (DXpedition, Field Day, telemetry) and
//     i3=3/5 (contest exchanges) — implement when surfaced by a
//     fixture that contains them.
func Unpack77(payload [LDPCPayloadBits]uint8) UnpackResult {
	return Unpack77WithHashes(payload, nil)
}

// Unpack77WithHashes is the hash-aware variant: it resolves Type 4
// h12 addressees and Type 1/2 h22 hash placeholders from the
// supplied CallsignHashTable. Pass nil for the table to fall back
// to the placeholder behaviour of Unpack77.
//
// The table is read-only inside this call; callers (e.g.
// MultiPassDecode) are responsible for Add'ing newly-decoded
// callsigns back to the table for future references.
func Unpack77WithHashes(payload [LDPCPayloadBits]uint8, ht *CallsignHashTable) UnpackResult {
	bits := payload[:]
	i3 := int(extractBits(bits, 74, 3))

	switch i3 {
	case 1, 2:
		return unpackType12(bits, i3, ht)
	case 0:
		n3 := int(extractBits(bits, 71, 3))
		if n3 == 0 {
			return unpackFreeText(bits)
		}
		return UnpackResult{
			I3: 0, N3: n3,
			Detail: fmt.Sprintf("unsupported i3=0 sub-type n3=%d", n3),
		}
	case 4:
		return unpackType4(bits, ht)
	default:
		return UnpackResult{
			I3:     i3,
			Detail: fmt.Sprintf("unsupported message type i3=%d", i3),
		}
	}
}

// extractBits reads n bits starting at offset from a 0/1 bit slice
// and returns them as a uint64, MSB-first.
func extractBits(bits []uint8, offset, n int) uint64 {
	var v uint64
	for i := 0; i < n; i++ {
		v = (v << 1) | uint64(bits[offset+i])
	}
	return v
}

// unpackType12 decodes the Type 1 / Type 2 message layout:
//
//	bits  0..27  c28 callsign 1
//	bit  28      p1     (suffix flag for callsign 1)
//	bits 29..56  c28 callsign 2
//	bit  57      p2     (suffix flag for callsign 2)
//	bit  58      r1     (R prefix on grid/report)
//	bits 59..73  g15    (grid or report)
//	bits 74..76  i3     (1 = Type 1 /R variant, 2 = Type 2 /P variant)
//
// p1/p2 interpretation is suffix "/R" when i3=1, "/P" when i3=2.
// r1=1 prefixes the grid/report with "R " (e.g. "R FN20", "R-12").
func unpackType12(bits []uint8, i3 int, ht *CallsignHashTable) UnpackResult {
	c28a := uint32(extractBits(bits, 0, 28))
	p1 := bits[28]
	c28b := uint32(extractBits(bits, 29, 28))
	p2 := bits[57]
	r1 := bits[58]
	g15 := uint32(extractBits(bits, 59, 15))

	suffix := "/R"
	if i3 == 2 {
		suffix = "/P"
	}

	call1, ok1 := unpackCallsign28WithHashes(c28a, ht)
	call2, ok2 := unpackCallsign28WithHashes(c28b, ht)
	grid, okG := unpackGrid15(g15)

	res := UnpackResult{I3: i3}
	if p1 == 1 {
		call1 += suffix
	}
	if p2 == 1 {
		call2 += suffix
	}
	text := call1 + " " + call2
	if grid != "" {
		if r1 == 1 {
			text += " R " + grid
		} else {
			text += " " + grid
		}
	} else if r1 == 1 {
		text += " R"
	}
	res.Text = strings.TrimRight(text, " ")
	res.OK = ok1 && ok2 && okG
	if !res.OK {
		res.Detail = fmt.Sprintf("unresolved: call1ok=%v call2ok=%v gridok=%v",
			ok1, ok2, okG)
	}
	return res
}

// unpackCallsign28 decodes a 28-bit FT8 callsign token. Returns the
// decoded text and an OK flag — OK is false for hash-placeholder
// values that cannot be resolved without the rolling hash table.
func unpackCallsign28(n uint32) (string, bool) {
	return unpackCallsign28WithHashes(n, nil)
}

// unpackCallsign28WithHashes is the hash-aware variant: hash-
// placeholder c28 values (NTOKENS ≤ n < NTOKENS+MAX22) are looked
// up in the supplied hash table. Returns OK=true when the table
// resolves the hash; OK=false (with a <...> placeholder) otherwise.
func unpackCallsign28WithHashes(n uint32, ht *CallsignHashTable) (string, bool) {
	switch {
	case n < ntokens:
		return decodeCallsignToken(n)
	case n < callBase:
		hash22 := n - ntokens
		if call, ok := ht.LookupH22(hash22); ok {
			return call, true
		}
		return fmt.Sprintf("<...%d>", hash22), false
	default:
		return decodeStandardCall(n - callBase), true
	}
}

// decodeCallsignToken handles the [0, ntokens) special-token range:
// DE, QRZ, CQ, "CQ nnn" (3-digit numeric), "CQ aaaa" (4-letter).
// Unrecognised slots beyond CQ_abcd fall back to a placeholder so
// downstream display still has something readable to show.
func decodeCallsignToken(n uint32) (string, bool) {
	switch {
	case n == 0:
		return "DE", true
	case n == 1:
		return "QRZ", true
	case n == 2:
		return "CQ", true
	case n >= 3 && n <= 1002:
		return fmt.Sprintf("CQ %03d", n-3), true
	case n >= 1003 && n < 1003+26*26*26*26:
		return decodeCQAbcd(n - 1003), true
	default:
		return fmt.Sprintf("<reserved-token %d>", n), false
	}
}

// decodeCQAbcd produces "CQ aaaa" for n in [0, 26⁴), with the four
// letters MSB-first (a, b, c, d ↦ base-26 digits). Trailing 'A'
// characters are stripped from the displayed modifier to match the
// FT8 convention used by jt9 and other oracles: short modifiers
// like "DX" or "POTA " are stored with right-side 'A' padding (A=0
// in the base-26 encoding) and rendered without the padding. So
// n=68276 stores "DXAA" and renders as "CQ DX"; n=273598 stores
// "POTA" and renders as "CQ POTA".
//
// Edge case: a literal "CQ AAAA" payload (n=0) would render as the
// empty modifier "CQ" — operationally indistinguishable from the
// bare-CQ token (c28_1=2). Treated as "CQ" for display consistency.
func decodeCQAbcd(n uint32) string {
	var c [4]byte
	for i := 3; i >= 0; i-- {
		c[i] = byte('A' + n%26)
		n /= 26
	}
	modifier := strings.TrimRight(string(c[:]), "A")
	if modifier == "" {
		return "CQ"
	}
	return "CQ " + modifier
}

// decodeStandardCall inverts the mixed-radix standard-callsign
// encoding from std_call_to_c28.f90:
//
//	n = i1·(36·10·27³) + i2·(10·27³) + i3·(27³) + i4·(27²) + i5·27 + i6
//
// where (i1, i2, i3, i4, i5, i6) index into (callA1, callA2, callA3,
// callA4, callA4, callA4) respectively. The 6 characters are
// reconstructed in slot order, then both leading and trailing spaces
// are trimmed — short callsigns (e.g. "K1JT") are stored with leading
// and trailing space padding so the digit lands at slot 3.
func decodeStandardCall(n uint32) string {
	i6 := n % 27
	n /= 27
	i5 := n % 27
	n /= 27
	i4 := n % 27
	n /= 27
	i3 := n % 10
	n /= 10
	i2 := n % 36
	n /= 36
	i1 := n % 37

	c := []byte{
		callA1[i1],
		callA2[i2],
		callA3[i3],
		callA4[i4],
		callA4[i5],
		callA4[i6],
	}
	return strings.Trim(string(c), " ")
}

// unpackGrid15 decodes the g15 grid/report field. Returns the decoded
// text and an OK flag.
//
//	[0, maxGrid4)        4-character Maidenhead grid
//	maxGrid4 + 1         "" (empty — used when the grid slot is unused)
//	maxGrid4 + 2         "RRR"
//	maxGrid4 + 3         "RR73"
//	maxGrid4 + 4         "73"
//	maxGrid4 + (irpt)    "+NN" / "-NN" report where dB = irpt - 35
//	                     (irpt ≥ 5)
func unpackGrid15(n uint32) (string, bool) {
	if n < maxGrid4 {
		j1 := n / (18 * 10 * 10)
		j2 := (n / (10 * 10)) % 18
		j3 := (n / 10) % 10
		j4 := n % 10
		return string([]byte{
			byte('A' + j1),
			byte('A' + j2),
			byte('0' + j3),
			byte('0' + j4),
		}), true
	}
	irpt := n - maxGrid4
	switch irpt {
	case 1:
		return "", true
	case 2:
		return "RRR", true
	case 3:
		return "RR73", true
	case 4:
		return "73", true
	}
	if irpt >= 5 {
		db := int(irpt) - 35
		return fmt.Sprintf("%+03d", db), true
	}
	return fmt.Sprintf("<grid15 reserved %d>", n), false
}

// unpackType4 decodes the Type 4 message layout (nonstandard
// callsigns):
//
//	bits  0..11   h12  (12-bit hash of the addressee callsign)
//	bits 12..69   c58  (58-bit free-form callsign of the sender)
//	bit  70       h1   (0 = "no hash flip", 1 = swap interpretation;
//	                    matches the WSJT-X type-4 spec)
//	bits 71..72   r2   (2-bit report: 0=blank, 1=RRR, 2=RR73, 3=73)
//	bit  73       c1   (0: addressee, then sender; 1: swapped order)
//	bits 74..76   i3   (= 4)
//
// The h12 addressee is resolved against the supplied hash table; on
// miss, surfaces as "<…h12>" placeholder (decode OK=false). The
// c58 callsign is always decoded from bits (never hashed).
//
// Output text shape: "<addressee> <sender> <report>". Empty report
// is omitted.
//
// h1 semantics per QEX paper § 7: when h1=1, the "<...>" hashed-call
// placeholder in the output is placed in front of the c58 call
// regardless of c1. Treated as informational here; the actual
// output sequence follows c1.
func unpackType4(bits []uint8, ht *CallsignHashTable) UnpackResult {
	h12 := uint32(extractBits(bits, 0, 12))
	c58 := extractBits(bits, 12, 58)
	_ = bits[70] // h1; reserved for future semantic refinements
	r2 := int(extractBits(bits, 71, 2))
	c1 := bits[73]

	addressee, okAddr := ht.LookupH12(h12)
	if !okAddr {
		addressee = fmt.Sprintf("<...%d>", h12)
	}
	sender := decodeC58(c58)

	var reportText string
	switch r2 {
	case 0:
		reportText = ""
	case 1:
		reportText = "RRR"
	case 2:
		reportText = "RR73"
	case 3:
		reportText = "73"
	}

	var text string
	if c1 == 0 {
		text = addressee + " " + sender
	} else {
		text = sender + " " + addressee
	}
	if reportText != "" {
		text += " " + reportText
	}

	res := UnpackResult{
		Text: text,
		I3:   4,
		OK:   okAddr,
	}
	if !okAddr {
		res.Detail = fmt.Sprintf("Type 4: h12=%d not in hash table", h12)
	}
	return res
}

// decodeC58 inverts the 58-bit nonstandard-callsign encoding from
// QEX ref [14] nonstd_to_c58.f90:
//
//	n58 = Σ char_index[i] × 38^(10−i)
//
// for an 11-character base-38 string (space-padded). Trailing spaces
// are trimmed on output.
func decodeC58(n58 uint64) string {
	var chars [11]byte
	for i := 10; i >= 0; i-- {
		idx := int(n58 % 38)
		chars[i] = c58Alphabet[idx]
		n58 /= 38
	}
	return strings.TrimRight(string(chars[:]), " ")
}

// unpackFreeText decodes a Type 0.0 free-text message. The 71 message
// bits (payload[0:71]) form a base-42 integer that decodes to 13
// characters from freeTextAlphabet (' '+0-9+A-Z+'+-./?'). Leading
// space-padding from right-justification is stripped on output.
//
// Uses math/big since 71 bits exceeds uint64. The conversion is
// straightforward base-42 long division.
func unpackFreeText(bits []uint8) UnpackResult {
	n := new(big.Int)
	for i := 0; i < 71; i++ {
		n.Lsh(n, 1)
		if bits[i] == 1 {
			n.Or(n, big.NewInt(1))
		}
	}
	base := big.NewInt(42)
	rem := new(big.Int)
	chars := make([]byte, 13)
	for i := 12; i >= 0; i-- {
		n.QuoRem(n, base, rem)
		idx := rem.Int64()
		if idx < 0 || idx >= int64(len(freeTextAlphabet)) {
			return UnpackResult{
				I3: 0, N3: 0,
				Detail: fmt.Sprintf("free-text base-42 digit out of range: %d", idx),
			}
		}
		chars[i] = freeTextAlphabet[idx]
	}
	return UnpackResult{
		OK:   true,
		Text: strings.TrimLeft(string(chars), " "),
		I3:   0,
		N3:   0,
	}
}
