package message

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- PackBits ----------------------------------------------------

func TestPackBits_SingleBitAtStart(t *testing.T) {
	buf := make([]byte, 1)
	PackBits(buf, 0, 1, 1)
	require.Equal(t, byte(0x80), buf[0])
}

func TestPackBits_SingleBitAtEnd(t *testing.T) {
	buf := make([]byte, 1)
	PackBits(buf, 7, 1, 1)
	require.Equal(t, byte(0x01), buf[0])
}

func TestPackBits_FullByte(t *testing.T) {
	buf := make([]byte, 1)
	PackBits(buf, 0, 8, 0xAB)
	require.Equal(t, byte(0xAB), buf[0])
}

func TestPackBits_28BitField(t *testing.T) {
	// Pack callBase + 6319593 = 12577489 (W1AW) into 28 bits at offset 0.
	buf := make([]byte, 4)
	PackBits(buf, 0, 28, 12577489)
	// 12577489 = 0x0BFEAD1 → in 28 bits: 0000 1011 1111 1110 1010 1101 0001
	// Bytes: 0x0B 0xFE 0xAD (first 24 bits), then 4 bits 0001 → 0x10 (upper nibble of byte 3)
	require.Equal(t, byte(0x0B), buf[0])
	require.Equal(t, byte(0xFE), buf[1])
	require.Equal(t, byte(0xAD), buf[2])
	require.Equal(t, byte(0x10), buf[3]) // only upper 4 bits written
}

func TestPackBits_SpansBytesBoundary(t *testing.T) {
	// Write 4 bits at offset 6 → spans bytes 0 and 1.
	buf := make([]byte, 2)
	PackBits(buf, 6, 4, 0b1010)
	// Bits 6-7 of byte 0 = 10, bits 0-1 of byte 1 = 10 (MSB-first within the field)
	// byte 0: ------10 = 0x02
	// byte 1: 10------ = 0x80
	require.Equal(t, byte(0x02), buf[0])
	require.Equal(t, byte(0x80), buf[1])
}

func TestPackBits_PreservesExistingBits(t *testing.T) {
	buf := []byte{0xFF}
	// Write 4 zero bits at offset 2 → bits 2-5 become 0.
	PackBits(buf, 2, 4, 0)
	// byte: 11 0000 11 = 0xC3
	require.Equal(t, byte(0xC3), buf[0])
}

func TestPackBits_ZeroWidth(t *testing.T) {
	buf := []byte{0xFF}
	PackBits(buf, 0, 0, 0)
	require.Equal(t, byte(0xFF), buf[0], "zero-width write must not modify buffer")
}

func TestPackBits_MaxWidth64(t *testing.T) {
	buf := make([]byte, 8)
	PackBits(buf, 0, 64, 0xDEADBEEFCAFEBABE)
	require.Equal(t, "deadbeefcafebabe", hex.EncodeToString(buf))
}

func TestPackBits_TwoAdjacentFields(t *testing.T) {
	// Simulate packing c28a(28) + p1(1) = 29 bits.
	buf := make([]byte, 4)
	PackBits(buf, 0, 28, 2) // CQ token
	PackBits(buf, 28, 1, 0) // p1 = 0
	// c28a=2 in 28 bits: 0000 0000 0000 0000 0000 0000 0010
	// p1=0 in 1 bit: 0
	// Bits 0-28: 00000000 00000000 00000000 00100
	// byte 3 = 0010 0___ = 0x20 (bit 28 is 0, packed into position 3 of byte 3)
	require.Equal(t, byte(0x00), buf[0])
	require.Equal(t, byte(0x00), buf[1])
	require.Equal(t, byte(0x00), buf[2])
	require.Equal(t, byte(0x20), buf[3])
}

// --------------- UnpackBits --------------------------------------------------

func TestUnpackBits_SingleBitAtStart(t *testing.T) {
	buf := []byte{0x80}
	require.Equal(t, uint64(1), UnpackBits(buf, 0, 1))
}

func TestUnpackBits_SingleBitAtEnd(t *testing.T) {
	buf := []byte{0x01}
	require.Equal(t, uint64(1), UnpackBits(buf, 7, 1))
}

func TestUnpackBits_FullByte(t *testing.T) {
	buf := []byte{0xAB}
	require.Equal(t, uint64(0xAB), UnpackBits(buf, 0, 8))
}

func TestUnpackBits_28BitField(t *testing.T) {
	// 12577489 = 0x0BFEAD1 packed into first 28 bits.
	buf := []byte{0x0B, 0xFE, 0xAD, 0x10}
	require.Equal(t, uint64(12577489), UnpackBits(buf, 0, 28))
}

func TestUnpackBits_SpansBytesBoundary(t *testing.T) {
	buf := []byte{0x02, 0x80}
	require.Equal(t, uint64(0b1010), UnpackBits(buf, 6, 4))
}

func TestUnpackBits_ZeroWidth(t *testing.T) {
	buf := []byte{0xFF}
	require.Equal(t, uint64(0), UnpackBits(buf, 0, 0))
}

func TestUnpackBits_MaxWidth64(t *testing.T) {
	buf, _ := hex.DecodeString("deadbeefcafebabe")
	require.Equal(t, uint64(0xDEADBEEFCAFEBABE), UnpackBits(buf, 0, 64))
}

func TestUnpackBits_MiddleOfByte(t *testing.T) {
	// byte 0xF0 = 11110000, extract bits 2-5 (4 bits) → 1100 = 12
	buf := []byte{0xF0}
	require.Equal(t, uint64(0b1100), UnpackBits(buf, 2, 4))
}

// --------------- Round-trip --------------------------------------------------

func TestBits_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		width  int
		value  uint64
		bufLen int
	}{
		{"1-bit at 0", 0, 1, 1, 1},
		{"1-bit at 7", 7, 1, 1, 1},
		{"8-bit at 0", 0, 8, 0xAB, 1},
		{"8-bit at 4", 4, 8, 0xCD, 2},
		{"15-bit at 0", 0, 15, 10331, 2},       // FN31 grid
		{"15-bit at 59", 59, 15, 32423, 10},    // -12 dB report at Type 1 g15 offset
		{"28-bit at 0", 0, 28, 12577489, 4},    // W1AW at c28a offset
		{"28-bit at 29", 29, 28, 237000219, 8}, // VK2XYZ at c28b offset
		{"3-bit at 74", 74, 3, 1, 10},          // i3=1 at Type 1 i3 offset
		{"32-bit at 0", 0, 32, 0xDEADBEEF, 4},
		{"64-bit at 0", 0, 64, 0xDEADBEEFCAFEBABE, 8},
		{"1-bit zero", 0, 1, 0, 1},
		{"zero width", 0, 0, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.bufLen)
			PackBits(buf, tt.offset, tt.width, tt.value)
			got := UnpackBits(buf, tt.offset, tt.width)
			require.Equal(t, tt.value, got)
		})
	}
}

// TestBits_RoundTrip_DoesNotCorruptAdjacent verifies that PackBits at a given
// offset/width does not alter bits outside the written range.
func TestBits_RoundTrip_DoesNotCorruptAdjacent(t *testing.T) {
	// Fill buffer with ones, write zeros into the middle, verify surroundings.
	buf := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	PackBits(buf, 10, 12, 0) // clear bits 10-21

	// Bits 0-9 should still be 1.
	require.Equal(t, uint64(0x3FF), UnpackBits(buf, 0, 10))
	// Bits 10-21 should be 0.
	require.Equal(t, uint64(0), UnpackBits(buf, 10, 12))
	// Bits 22-31 should still be 1.
	require.Equal(t, uint64(0x3FF), UnpackBits(buf, 22, 10))
}

// --------------- Panic guards ------------------------------------------------

func TestPackBits_PanicsOnNegativeOffset(t *testing.T) {
	require.PanicsWithValue(t,
		"message: PackBits offset must be non-negative",
		func() { PackBits(make([]byte, 1), -1, 1, 0) },
	)
}

// Regression: offset=-8, width=8 passes the endBit<=len*8 check (0<=8)
// but would compute byteIdx=-1 in the loop without the offset guard.
func TestPackBits_PanicsOnNegativeOffsetBypassingEndBit(t *testing.T) {
	require.PanicsWithValue(t,
		"message: PackBits offset must be non-negative",
		func() { PackBits(make([]byte, 1), -8, 8, 0) },
	)
}

func TestPackBits_PanicsOnNegativeWidth(t *testing.T) {
	require.Panics(t, func() { PackBits(make([]byte, 1), 0, -1, 0) })
}

func TestPackBits_PanicsOnWidth65(t *testing.T) {
	require.Panics(t, func() { PackBits(make([]byte, 9), 0, 65, 0) })
}

func TestPackBits_PanicsOnOverflow(t *testing.T) {
	// 1-byte buffer can hold 8 bits; writing 1 bit at offset 8 overflows.
	require.Panics(t, func() { PackBits(make([]byte, 1), 8, 1, 1) })
}

func TestUnpackBits_PanicsOnNegativeOffset(t *testing.T) {
	require.PanicsWithValue(t,
		"message: UnpackBits offset must be non-negative",
		func() { UnpackBits(make([]byte, 1), -1, 1) },
	)
}

// Regression: offset=-8, width=8 passes the endBit<=len*8 check (0<=8)
// but would compute byteIdx=-1 in the loop without the offset guard.
func TestUnpackBits_PanicsOnNegativeOffsetBypassingEndBit(t *testing.T) {
	require.PanicsWithValue(t,
		"message: UnpackBits offset must be non-negative",
		func() { UnpackBits(make([]byte, 1), -8, 8) },
	)
}

func TestUnpackBits_PanicsOnNegativeWidth(t *testing.T) {
	require.Panics(t, func() { UnpackBits(make([]byte, 1), 0, -1) })
}

func TestUnpackBits_PanicsOnWidth65(t *testing.T) {
	require.Panics(t, func() { UnpackBits(make([]byte, 9), 0, 65) })
}

func TestUnpackBits_PanicsOnOverflow(t *testing.T) {
	require.Panics(t, func() { UnpackBits(make([]byte, 1), 8, 1) })
}

// --------------- Integration: Type 1 test vectors ----------------------------

// type1Vector mirrors the JSON schema in testdata/type1_vectors.json.
type type1Vector struct {
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

func loadType1Vectors(t *testing.T) []type1Vector {
	t.Helper()
	data, err := os.ReadFile("testdata/type1_vectors.json")
	require.NoError(t, err)
	var vectors []type1Vector
	require.NoError(t, json.Unmarshal(data, &vectors))
	require.NotEmpty(t, vectors)
	return vectors
}

// TestPackBits_Type1Vectors packs each test vector's field values using
// PackBits and verifies the result matches the expected hex payload.
// This validates PackBits against independently-generated test vectors.
func TestPackBits_Type1Vectors(t *testing.T) {
	vectors := loadType1Vectors(t)
	for _, v := range vectors {
		t.Run(v.Text, func(t *testing.T) {
			var buf [MsgBytes]byte
			off := 0
			PackBits(buf[:], off, 28, uint64(v.C28a))
			off += 28
			PackBits(buf[:], off, 1, uint64(v.P1))
			off += 1
			PackBits(buf[:], off, 28, uint64(v.C28b))
			off += 28
			PackBits(buf[:], off, 1, uint64(v.P2))
			off += 1
			PackBits(buf[:], off, 1, uint64(v.IR))
			off += 1
			PackBits(buf[:], off, 15, uint64(v.G15))
			off += 15
			PackBits(buf[:], off, 3, uint64(v.I3))

			got := hex.EncodeToString(buf[:])
			require.Equal(t, v.Hex, got)
		})
	}
}

// TestUnpackBits_Type1Vectors unpacks each test vector's hex payload using
// UnpackBits and verifies the extracted field values match.
func TestUnpackBits_Type1Vectors(t *testing.T) {
	vectors := loadType1Vectors(t)
	for _, v := range vectors {
		t.Run(v.Text, func(t *testing.T) {
			payload, err := hex.DecodeString(v.Hex)
			require.NoError(t, err)
			require.Len(t, payload, MsgBytes)

			off := 0
			c28a := uint32(UnpackBits(payload, off, 28))
			off += 28
			p1 := int(UnpackBits(payload, off, 1))
			off += 1
			c28b := uint32(UnpackBits(payload, off, 28))
			off += 28
			p2 := int(UnpackBits(payload, off, 1))
			off += 1
			ir := int(UnpackBits(payload, off, 1))
			off += 1
			g15 := uint16(UnpackBits(payload, off, 15))
			off += 15
			i3 := int(UnpackBits(payload, off, 3))

			require.Equal(t, v.C28a, c28a, "c28a")
			require.Equal(t, v.P1, p1, "p1")
			require.Equal(t, v.C28b, c28b, "c28b")
			require.Equal(t, v.P2, p2, "p2")
			require.Equal(t, v.IR, ir, "ir")
			require.Equal(t, v.G15, g15, "g15")
			require.Equal(t, v.I3, i3, "i3")
		})
	}
}

// TestUnpackBits_Type1Vectors_FieldDecode extends the unpack test by also
// passing the extracted field values through DecodeCallsign and DecodeGridField
// to verify the full decode chain produces the expected text.
func TestUnpackBits_Type1Vectors_FieldDecode(t *testing.T) {
	vectors := loadType1Vectors(t)
	for _, v := range vectors {
		t.Run(v.Text, func(t *testing.T) {
			payload, err := hex.DecodeString(v.Hex)
			require.NoError(t, err)

			c28a := uint32(UnpackBits(payload, 0, 28))
			c28b := uint32(UnpackBits(payload, 29, 28))
			ir := UnpackBits(payload, 58, 1) != 0
			g15 := uint16(UnpackBits(payload, 59, 15))

			call1, err := DecodeCallsign(c28a)
			require.NoError(t, err)

			call2, err := DecodeCallsign(c28b)
			require.NoError(t, err)

			grid, err := DecodeGridField(g15, ir)
			require.NoError(t, err)

			// Reconstruct the text: "CALL1 CALL2 GRID" (with empty grid omitted).
			var got string
			if grid == "" {
				got = fmt.Sprintf("%s %s", call1, call2)
			} else {
				got = fmt.Sprintf("%s %s %s", call1, call2, grid)
			}
			require.Equal(t, v.Text, got)
		})
	}
}
