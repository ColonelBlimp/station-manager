package ldpc

// CRC14 polynomial bits per QEX ref [14] gen_crc14.f90. The Fortran
// data statement reads
//
//	data p/1,1,0,0,1,1,1,0,1,0,1,0,1,1,1/
//
// which is the 15-bit MSB-first encoding of polynomial 0x6757
// (degree 14 with implicit leading x¹⁴ term included). The bit array
// is preserved verbatim here so the algorithm port can be a literal
// translation of the Fortran shift-register loop — no risk of
// mis-encoding the polynomial when converting between integer and
// bit-array forms.
var crc14PolyBits = [15]uint8{1, 1, 0, 0, 1, 1, 1, 0, 1, 0, 1, 0, 1, 1, 1}

// crc14Matches verifies the FT8 CRC14 over the 91-bit info word.
// The first 77 bits are the payload, the trailing 14 bits are the
// transmitted CRC. Returns true iff the computed CRC over the 77
// payload bits equals the transmitted CRC.
//
// Algorithm is a literal port of QEX ref [14] gen_crc14.f90:
// the 77 message bits are padded with 19 zeros to 96 bits, a 15-bit
// shift register is loaded with the first 15 bits, then 82
// iterations drive (load next bit) → (XOR polynomial if MSB set) →
// (left-circular-shift). The trailing 14 register bits are the CRC.
//
// The 19-zero pad (not the textbook 14 zeros of CRC width) is part
// of the FT8 spec — preserved as-is. Don't change without
// re-validating against the Fortran reference output.
func crc14Matches(info [infoBits]uint8) bool {
	var msg [payloadBits]uint8
	for i := 0; i < payloadBits; i++ {
		msg[i] = info[i]
	}
	computed := crc14(msg)

	// Recover the transmitted CRC from info bits 77..90, MSB first.
	var transmitted uint16
	for i := 0; i < crcBits; i++ {
		transmitted = (transmitted << 1) | uint16(info[payloadBits+i])
	}

	return computed == transmitted
}

// crc14 computes the 14-bit FT8 CRC over a 77-bit message. Output
// is in the low 14 bits of the return value, MSB-first ordering
// (bit 13 = r[0] from the Fortran reference, bit 0 = r[13]).
func crc14(msg [payloadBits]uint8) uint16 {
	// Padded 96-bit input: 77 message bits + 19 zero bits.
	var mc [96]uint8
	for i := 0; i < payloadBits; i++ {
		mc[i] = msg[i]
	}

	// 15-bit shift register, initialised with the first 15 message
	// bits (= mc[0..14]).
	var r [15]uint8
	for i := 0; i < 15; i++ {
		r[i] = mc[i]
	}

	// 82 iterations: load → XOR-if-MSB → left-circular-shift.
	// On iteration i, mc[i+14] is loaded into r[14] (Fortran
	// 1-indexed mc(i+15) → 0-indexed mc[i+14]; r(15) → r[14]).
	for i := 0; i <= 81; i++ {
		r[14] = mc[i+14]
		if r[0] == 1 {
			for k := 0; k < 15; k++ {
				r[k] ^= crc14PolyBits[k]
			}
		}
		// Left-circular shift by 1: new r[k] = old r[k+1], new r[14]
		// = old r[0]. (The new r[14] gets overwritten on the next
		// iteration's load, so its value here is meaningful only on
		// the final iteration.)
		r0 := r[0]
		for k := 0; k < 14; k++ {
			r[k] = r[k+1]
		}
		r[14] = r0
	}

	// CRC = r[0..13], MSB first.
	var crc uint16
	for k := 0; k < crcBits; k++ {
		crc = (crc << 1) | uint16(r[k])
	}
	return crc
}
