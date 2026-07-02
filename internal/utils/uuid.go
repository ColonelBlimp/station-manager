package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewUUIDv7 returns a UUID version 7 string per RFC 9562, time-ordered
// using the current wall-clock time. The canonical 36-character form is
// returned with lowercase hex digits and standard hyphenation.
//
// Layout (RFC 9562 §5.7):
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                       unix_ts_ms (48 bits)                    |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|        unix_ts_ms (cont.)     |  ver  |       rand_a          |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|var|                       rand_b                              |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                       rand_b (cont.)                          |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
// version nibble = 0b0111 (7); variant bits = 0b10. The remaining 74
// bits are filled from crypto/rand.
func NewUUIDv7() string {
	return NewUUIDv7At(time.Now())
}

// NewUUIDv7At is the same as NewUUIDv7 but uses t as the timestamp
// source. Used by the historical-row backfill so pre-migration QSOs
// keep their creation-time order in the time-prefix.
//
// Caller's responsibility to pass a meaningful t. Negative or
// pre-epoch times are coerced to zero (the spec leaves this
// undefined; zero is the least-bad observable behaviour).
func NewUUIDv7At(t time.Time) string {
	ms := t.UnixMilli()
	if ms < 0 {
		ms = 0
	}

	var b [16]byte

	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	if _, err := rand.Read(b[6:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failure: %v", err))
	}

	b[6] = (b[6] & 0x0F) | 0x70
	b[8] = (b[8] & 0x3F) | 0x80

	return formatUUID(b)
}

func formatUUID(b [16]byte) string {
	const hexdig = "0123456789abcdef"
	out := make([]byte, 36)
	out[8] = '-'
	out[13] = '-'
	out[18] = '-'
	out[23] = '-'

	for i, j := 0, 0; i < 16; i++ {
		switch j {
		case 8, 13, 18, 23:
			j++
		}
		out[j] = hexdig[b[i]>>4]
		out[j+1] = hexdig[b[i]&0x0F]
		j += 2
	}
	return string(out)
}

// IsValidUUIDv7 reports whether s is a canonical-form UUIDv7 string:
// 36 characters, hex (either case — hex.DecodeString accepts upper or
// lower), standard hyphenation, version nibble = 7, and variant bits
// = 0b10.
//
// Used by tests and by the API edge to reject obviously-malformed IDs
// before a DB lookup. Not authoritative for "is this a UUID *we*
// generated" — that requires a DB query.
func IsValidUUIDv7(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	stripped := s[:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
	raw, err := hex.DecodeString(stripped)
	if err != nil {
		return false
	}
	if (raw[6] & 0xF0) != 0x70 {
		return false
	}
	if (raw[8] & 0xC0) != 0x80 {
		return false
	}
	return true
}
