// Package message
// Bit-level packing and unpacking utilities for FT8/FT4 77-bit messages.
//
// FT8 messages are represented as byte slices in big-endian (MSB-first) bit
// order: bit 0 is the MSB of byte 0. PackBits and UnpackBits read and write
// arbitrary-width fields at arbitrary bit offsets within such slices, enabling
// the Type 1/Type 0/etc. message packers to compose field encoders (callsign,
// grid, report) into the 77-bit payload without manual shift-and-mask logic.
//
// Reference: ft8_lib src/ft8/encode.c pack_bits()/unpack_bits().
package message

// PackBits writes the low `width` bits of value into dst starting at bit
// position `offset`, MSB-first. Bits already present in dst outside the
// written range are preserved.
//
// PackBits panics if width is negative or exceeds 64, or if the write would
// extend past the end of dst.
//
// Example: PackBits(buf, 0, 28, 0x0000002) writes the 28-bit CQ token into
// the first 28 bits of buf.
func PackBits(dst []byte, offset int, width int, value uint64) {
	if width < 0 || width > 64 {
		panic("message: PackBits width must be in [0, 64]")
	}
	if width == 0 {
		return
	}
	endBit := offset + width
	if endBit > len(dst)*8 {
		panic("message: PackBits would write past end of dst")
	}

	for i := 0; i < width; i++ {
		bitPos := offset + i
		byteIdx := bitPos / 8
		bitIdx := uint(7 - bitPos%8)

		// Extract source bit: MSB of the value field is written first.
		srcBit := (value >> uint(width-1-i)) & 1

		if srcBit != 0 {
			dst[byteIdx] |= 1 << bitIdx
		} else {
			dst[byteIdx] &^= 1 << bitIdx
		}
	}
}

// UnpackBits reads `width` bits from src starting at bit position `offset`,
// MSB-first, and returns them as a uint64.
//
// UnpackBits panics if width is negative or exceeds 64, or if the read would
// extend past the end of src.
//
// Example: UnpackBits(buf, 0, 28) reads the first 28 bits of buf as a uint64.
func UnpackBits(src []byte, offset int, width int) uint64 {
	if width < 0 || width > 64 {
		panic("message: UnpackBits width must be in [0, 64]")
	}
	if width == 0 {
		return 0
	}
	endBit := offset + width
	if endBit > len(src)*8 {
		panic("message: UnpackBits would read past end of src")
	}

	var value uint64
	for i := 0; i < width; i++ {
		bitPos := offset + i
		byteIdx := bitPos / 8
		bitIdx := uint(7 - bitPos%8)

		bit := uint64((src[byteIdx] >> bitIdx) & 1)
		value = (value << 1) | bit
	}

	return value
}
