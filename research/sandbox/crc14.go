package sandbox

// crc14PolyMask is the 15-bit MSB-first FT8 CRC generator polynomial
// 0x6757 (per QEX paper §6 / ref [14] gen_crc14.f90). The polynomial
// is degree-14; this constant carries the implicit leading 1, so the
// XOR clears bit 14 in the same step it modifies the lower bits.
//
//	0x6757 = 0b110_0111_0101_0111
const crc14PolyMask uint16 = 0x6757

// computeCRC14 computes the 14-bit FT8 CRC over 77 message bits.
//
// Mathematically equivalent to the literal port of ref [14]
// gen_crc14.f90: the 77 bits are padded with 19 zeros to 96 total, a
// 15-bit shift register is loaded with the first 15 bits, and 82
// iterations each (i) load the next message bit into the LSB of the
// register, (ii) XOR with the polynomial if the register MSB is 1,
// (iii) left-shift by 1. The 19-zero pad (rather than the textbook
// 14 = CRC width) is part of the FT8 spec — the algorithm matches
// gen_crc14.f90 regardless of textbook convention, and that's the
// wire format.
//
// Implementation: the 15-bit register is packed into the low 15 bits
// of a uint16 (msg77[0] at bit 14, msg77[14] at bit 0). Each iteration
// runs 4 integer ops; the original byte-array version ran ~30 byte-ops
// per iteration. Equivalence pinned by TestComputeCRC14_MatchesReference
// against the preserved byte-array reference.
//
// Returns the 14 CRC bits MSB-first.
func computeCRC14(msg77 []uint8) [LDPCCRCBits]uint8 {
	if len(msg77) != LDPCPayloadBits {
		panic("sandbox.computeCRC14: input must be 77 bits")
	}

	// Load first 15 bits: msg77[0] at bit 14, msg77[14] at bit 0.
	var r uint16
	for i := 0; i < 15; i++ {
		r = (r << 1) | uint16(msg77[i]&1)
	}

	// 82 iterations process bits 14..95 of the padded message. Bit 14
	// is already in the register from init; iterations 1..81 fold in
	// msg77[15..76] and 19 zero pad bits.
	for i := 0; i < 82; i++ {
		// Inject next input bit (or zero pad) at LSB. On i=0, mc[14] is
		// already at bit 0 from init — the OR is a no-op there.
		mcIdx := i + 14
		var newBit uint16
		if mcIdx < LDPCPayloadBits {
			newBit = uint16(msg77[mcIdx] & 1)
		}
		r = (r & 0xFFFE) | newBit

		// If MSB (bit 14) is set, XOR with poly. The poly has bit 14
		// set, so the XOR clears bit 14 and folds the polynomial taps
		// into the lower bits in the same operation.
		if r&0x4000 != 0 {
			r ^= crc14PolyMask
		}

		// Shift register left by 1; bit 14 (always 0 after XOR step)
		// falls off, LSB becomes 0. Mask retains 15-bit width.
		r = (r << 1) & 0x7FFE
	}

	// After 82 iterations, bit 0 = 0. The output CRC corresponds to the
	// top 14 bits of the original byte-array register (array indices
	// 0..13), which map to integer bits 14..1.
	var crc [LDPCCRCBits]uint8
	for i := 0; i < LDPCCRCBits; i++ {
		crc[i] = uint8((r >> (14 - i)) & 1)
	}
	return crc
}

// VerifyCRC14 checks that the trailing 14 bits of a 91-bit info word
// match the CRC computed over the leading 77 payload bits. This is
// the FT8 message-integrity gate that gates BP decode acceptance.
func VerifyCRC14(msg91 [LDPCInfoBits]uint8) bool {
	expected := computeCRC14(msg91[:LDPCPayloadBits])
	for i := 0; i < LDPCCRCBits; i++ {
		if expected[i] != msg91[LDPCPayloadBits+i] {
			return false
		}
	}
	return true
}
