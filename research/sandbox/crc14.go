package sandbox

// crc14PolyBits is the 15-bit MSB-first representation of the FT8
// CRC generator polynomial 0x6757 (per QEX paper §6 / ref [14]
// gen_crc14.f90). The polynomial is degree-14; the 15 bits include
// the implicit leading 1.
//
//	0x6757 = 0b110_0111_0101_0111
var crc14PolyBits = [15]uint8{1, 1, 0, 0, 1, 1, 1, 0, 1, 0, 1, 0, 1, 1, 1}

// computeCRC14 computes the 14-bit FT8 CRC over 77 message bits via
// the literal port of ref [14] gen_crc14.f90: the 77 bits are padded
// with 19 zeros to 96 total, a 15-bit shift register is loaded with
// the first 15 bits, and 82 iterations each (i) load the next message
// bit into the LSB of the register, (ii) XOR with the polynomial if
// the register MSB is 1, (iii) left-circular-shift by 1.
//
// The 19-zero pad (rather than the textbook 14 = CRC width) is part
// of the FT8 spec — the algorithm matches what gen_crc14.f90 does
// regardless of textbook convention, and that's the wire format.
//
// Returns the 14 CRC bits MSB-first.
func computeCRC14(msg77 []uint8) [LDPCCRCBits]uint8 {
	if len(msg77) != LDPCPayloadBits {
		panic("sandbox.computeCRC14: input must be 77 bits")
	}

	// Pad to 96 bits.
	var mc [96]uint8
	copy(mc[:], msg77)

	// Initialise the 15-bit shift register with the first 15 bits.
	var r [15]uint8
	copy(r[:], mc[0:15])

	// 82 iterations process mc[14..95] (Fortran's mc(15..96) 1-indexed).
	// Each iteration:
	//   r[14] ← next message bit (no-op on i=0 since mc[14] is already there)
	//   if r[0]==1: r ^= poly (this clears r[0] since poly[0]=1)
	//   left-circular-shift r by 1 (so r[14] ← old r[0], i.e. 0)
	for i := 0; i < 82; i++ {
		r[14] = mc[i+14]
		if r[0] == 1 {
			for j := 0; j < 15; j++ {
				r[j] ^= crc14PolyBits[j]
			}
		}
		first := r[0] // = 0 if XOR happened, = 0 if not (it was 0)
		for j := 0; j < 14; j++ {
			r[j] = r[j+1]
		}
		r[14] = first
	}

	// CRC is the trailing 14 register bits.
	var crc [LDPCCRCBits]uint8
	copy(crc[:], r[0:LDPCCRCBits])
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
