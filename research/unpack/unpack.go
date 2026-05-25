// Package unpack converts a 91-bit FT8 info word into the human-
// readable text message per QEX paper §2 + Appendix A (Franke,
// Somerville, Taylor — "The FT4 and FT8 Communication Protocols,"
// QEX July/August 2020).
//
// Layered message types (i3 = last 3 bits of the 77-bit payload):
//
//	i3 = 1  Standard:         c28 p1 c28 p1 R1 g15      (implemented)
//	i3 = 2  EU VHF contest:   c28 p1 c28 p1 R1 g25      (TODO; 6-char grid)
//	i3 = 3  ARRL RTTY:                                  (TODO)
//	i3 = 4  Nonstandard call: h12 c58 h1 r2 c1          (implemented; hashed call shown as "<...>")
//	i3 = 5  Telemetry:        t71                       (TODO)
//	i3 = 0  Special / free text                         (TODO)
//
// Types 1 and 4 cover the full real-capture corpus (Sessions 88-91).
// Other types return an "unsupported" error; the caller
// (decode-eval) classifies these as "valid-CRC but unknown-type"
// rather than "malformed."
//
// Imports stdlib only — by rule the research tree must not depend
// on internal/ft8/*. The encoding tables (c28 charsets, NTOKENS /
// MAX22 constants, MAXGRID4) are reproduced from the QEX ref [14]
// public-domain Fortran encoders (std_call_to_c28.f90,
// grid4_to_g15.f90).
package unpack

import (
	"fmt"
	"strings"
)

// Result is the parsed message plus diagnostic flags. Text is the
// canonical "CALL1 CALL2 EXTRA" rendering matching jt9's output
// format; MsgType is the i3 byte for routing.
type Result struct {
	Text    string // formatted message, e.g. "CQ K1JT FN20"
	MsgType uint8  // i3 — 0..7 per QEX §2
}

// Unpack returns the message text for a 91-bit FT8 info word.
// The first 77 bits carry the payload (last 3 of which are i3);
// the trailing 14 bits are CRC and are ignored here (the caller
// is expected to have already verified CRC).
//
// On supported types, returns Result with Text populated. On
// unsupported types or malformed payloads, returns a non-nil
// error with Result.MsgType set so the caller can classify.
func Unpack(info [91]uint8) (Result, error) {
	i3 := uint8(bitsToUint(info[74:77]))
	switch i3 {
	case 1:
		text, err := unpackType1(info[:77])
		return Result{Text: text, MsgType: i3}, err
	case 4:
		text, err := unpackType4(info[:77])
		return Result{Text: text, MsgType: i3}, err
	default:
		return Result{MsgType: i3}, fmt.Errorf("unpack: i3=%d not implemented", i3)
	}
}

// bitsToUint reads a slice of 0/1 bytes as a big-endian (MSB first)
// unsigned integer. Standard FT8 bit-ordering convention.
func bitsToUint(bits []uint8) uint64 {
	var n uint64
	for _, b := range bits {
		n = (n << 1) | uint64(b&1)
	}
	return n
}

// unpackType1 decodes the 77-bit payload of a Type 1 standard
// message: c28 p1 c28 p1 R1 g15 i3.
//
//	bits[0..27]  c28 — first callsign (CQ/DE/QRZ, hash, or standard)
//	bits[28]     p1  — first call /P suffix flag
//	bits[29..56] c28 — second callsign
//	bits[57]     p1  — second call /P suffix flag
//	bits[58]     R1  — leading-R flag on the report ("R+02" vs "+02")
//	bits[59..73] g15 — grid / report / RRR / RR73 / 73 / blank
//	bits[74..76] i3  — message type (always 1 for this path)
//
// Output format mirrors jt9: "CALL1 CALL2 EXTRA", with /P appended
// to either call when its p1 bit is set, and R prepended to EXTRA
// when R1 is set.
func unpackType1(p []uint8) (string, error) {
	if len(p) != 77 {
		return "", fmt.Errorf("unpack: type1 expects 77-bit payload, got %d", len(p))
	}
	c1 := uint32(bitsToUint(p[0:28]))
	p1 := p[28]
	c2 := uint32(bitsToUint(p[29:57]))
	p2 := p[57]
	r1 := p[58]
	g := uint16(bitsToUint(p[59:74]))

	call1, err := decodeC28(c1)
	if err != nil {
		return "", fmt.Errorf("call1: %w", err)
	}
	call2, err := decodeC28(c2)
	if err != nil {
		return "", fmt.Errorf("call2: %w", err)
	}
	extra, err := decodeG15(g)
	if err != nil {
		return "", fmt.Errorf("g15: %w", err)
	}

	if p1 == 1 {
		call1 += "/P"
	}
	if p2 == 1 {
		call2 += "/P"
	}

	var sb strings.Builder
	sb.WriteString(call1)
	sb.WriteByte(' ')
	sb.WriteString(call2)
	if extra != "" {
		sb.WriteByte(' ')
		if r1 == 1 {
			sb.WriteByte('R')
		}
		sb.WriteString(extra)
	} else if r1 == 1 {
		// R1 set but g15 blank — empty "R" report. Rare; emit "R" alone.
		sb.WriteString(" R")
	}
	return sb.String(), nil
}

// unpackType4 decodes the 77-bit payload of a Type 4 (nonstandard
// callsign) message per QEX paper §2. Layout MSB-first:
//
//	bits[0..11]  h12 — 12-bit hash of the OTHER callsign (the
//	                   standard one in a "<call> nonstd ..." exchange).
//	                   Displayed as "<...>" since this offline decoder
//	                   has no running hashtable to resolve it.
//	bits[12..69] c58 — 58-bit nonstandard callsign (up to 11 chars).
//	bits[70]     h1  — flag: 0 = hashed call is FIRST, 1 = SECOND.
//	bits[71..72] r2  — 0=blank, 1=RRR, 2=RR73, 3=73.
//	bits[73]     c1  — flag: 1 = message is "CQ <nonstd>" (h12 ignored).
//	bits[74..76] i3  — message type (always 4 for this path).
//
// Output format mirrors jt9:
//
//	c1=1            → "CQ <nonstd>"
//	c1=0, h1=0      → "<...> <nonstd>" + " r2"
//	c1=0, h1=1      → "<nonstd> <...>" + " r2"
//
// where r2 is appended only when non-blank.
func unpackType4(p []uint8) (string, error) {
	if len(p) != 77 {
		return "", fmt.Errorf("unpack: type4 expects 77-bit payload, got %d", len(p))
	}
	// h12 ignored except for the placeholder; we don't have a hashtable.
	c58 := bitsToUint(p[12:70])
	h1 := p[70]
	r2 := uint8(bitsToUint(p[71:73]))
	c1Flag := p[73]

	nonstd := decodeC58(c58)
	if nonstd == "" {
		nonstd = "<empty>"
	}

	var r2Text string
	switch r2 {
	case 0:
		r2Text = ""
	case 1:
		r2Text = "RRR"
	case 2:
		r2Text = "RR73"
	case 3:
		r2Text = "73"
	}

	var sb strings.Builder
	switch {
	case c1Flag == 1:
		sb.WriteString("CQ ")
		sb.WriteString(nonstd)
	case h1 == 0:
		sb.WriteString("<...>")
		sb.WriteByte(' ')
		sb.WriteString(nonstd)
	default:
		sb.WriteString(nonstd)
		sb.WriteString(" <...>")
	}
	if r2Text != "" {
		sb.WriteByte(' ')
		sb.WriteString(r2Text)
	}
	return sb.String(), nil
}
