// Package unpack converts a 91-bit FT8 info word into the human-
// readable text message per QEX paper §2 + Appendix A (Franke,
// Somerville, Taylor — "The FT4 and FT8 Communication Protocols,"
// QEX July/August 2020).
//
// Layered message types (i3 = last 3 bits of the 77-bit payload):
//
//	i3 = 1  Standard:        c28 p1 c28 p1 R1 g15      (this package)
//	i3 = 2  EU VHF contest:  c28 p1 c28 p1 R1 g25      (TODO; uses 6-char grid)
//	i3 = 3  ARRL RTTY:                                 (TODO)
//	i3 = 4  Nonstandard call:  h1 c58 c58 r3 ... etc.  (TODO; needs hashtable)
//	i3 = 5  Telemetry:         t71                     (TODO)
//	i3 = 0  Special / free text                        (TODO)
//
// Current corpus is entirely Type 1, so that's the only type
// implemented in this first cut. Other types return an
// "unsupported" error; the caller (decode-eval) classifies these as
// "valid-CRC but unknown-type" rather than "malformed."
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
