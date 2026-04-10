package message

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnpackType4(t *testing.T) {
	tests := []struct {
		name    string
		call    string // expected non-standard callsign
		grid    string // expected report
		icq     bool   // is CQ message
		iflip   bool   // flip order
		nrpt    int    // report code
		wantStr string // expected String() output
	}{
		{
			name:    "VK/ZL4XZ with RR73 (iflip=1)",
			call:    "VK/ZL4XZ",
			grid:    "RR73",
			icq:     false,
			iflip:   true,
			nrpt:    2,
			wantStr: "VK/ZL4XZ <...> RR73",
		},
		{
			name:    "PJ4/KA1ABC with 73 (iflip=0)",
			call:    "PJ4/KA1ABC",
			grid:    "73",
			icq:     false,
			iflip:   false,
			nrpt:    3,
			wantStr: "<...> PJ4/KA1ABC 73",
		},
		{
			name:    "CQ with non-standard call",
			call:    "VK/ZL4XZ",
			grid:    "",
			icq:     true,
			iflip:   false,
			nrpt:    0,
			wantStr: "CQ VK/ZL4XZ",
		},
		{
			name:    "RRR report",
			call:    "DL/W1AW",
			grid:    "RRR",
			icq:     false,
			iflip:   true,
			nrpt:    1,
			wantStr: "DL/W1AW <...> RRR",
		},
		{
			name:    "no report",
			call:    "VE3/W1AW",
			grid:    "",
			icq:     false,
			iflip:   false,
			nrpt:    0,
			wantStr: "<...> VE3/W1AW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode a Type 4 payload manually.
			payload := encodeType4ForTest(t, tt.call, tt.iflip, tt.nrpt, tt.icq)

			// Verify i3 = 4.
			i3 := int(UnpackBits(payload[:], 74, 3))
			assert.Equal(t, 4, i3, "i3 field should be 4")

			// Unpack via the public Unpack function.
			msg, err := Unpack(payload)
			require.NoError(t, err)
			assert.Equal(t, TypeNonStandard, msg.MsgType)
			assert.Equal(t, tt.wantStr, msg.String())
		})
	}
}

func TestDecodeCallsign58(t *testing.T) {
	tests := []struct {
		call string
	}{
		{"VK/ZL4XZ"},
		{"PJ4/KA1ABC"},
		{"DL/W1AW"},
		{"VE3/W1AW"},
		{"W1AW"},
		{"A"},
	}

	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			n58 := encodeCallsign58(tt.call)
			got := decodeCallsign58(n58)
			assert.Equal(t, tt.call, got)
		})
	}
}

// --- Test helpers ---

// encodeCallsign58 encodes a callsign into a 58-bit value using base-38.
// This is the inverse of decodeCallsign58 (pack58 in ft8_lib).
func encodeCallsign58(call string) uint64 {
	var n58 uint64
	for i := 0; i < len(call); i++ {
		c := call[i]
		var idx int
		switch {
		case c == ' ':
			idx = 0
		case c >= '0' && c <= '9':
			idx = int(c-'0') + 1
		case c >= 'A' && c <= 'Z':
			idx = int(c-'A') + 11
		case c == '/':
			idx = 37
		default:
			idx = 0
		}
		n58 = n58*38 + uint64(idx)
	}
	return n58
}

// encodeType4ForTest builds a Type 4 payload from the given parameters.
func encodeType4ForTest(t *testing.T, call string, iflip bool, nrpt int, icq bool) [MsgBytes]byte {
	t.Helper()

	var payload [MsgBytes]byte

	// n12: use a dummy 12-bit hash.
	PackBits(payload[:], type4OffN12, 12, 0x123)

	// n58: encode the non-standard callsign.
	n58 := encodeCallsign58(call)
	PackBits(payload[:], type4OffN58, 58, n58)

	// iflip
	if iflip {
		PackBits(payload[:], type4OffIflip, 1, 1)
	}

	// nrpt
	PackBits(payload[:], type4OffNrpt, 2, uint64(nrpt))

	// icq
	if icq {
		PackBits(payload[:], type4OffICQ, 1, 1)
	}

	// i3 = 4
	PackBits(payload[:], 74, 3, 4)

	return payload
}
