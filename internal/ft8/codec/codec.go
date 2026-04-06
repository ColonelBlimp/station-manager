// codec.go — high-level convenience functions bridging the message package
// with the raw LDPC encoder and decoder.
//
// These provide clean entry points for the TX and RX pipelines:
//
//   - [EncodeMessage]: Pack77 → Append91 → Encode (TX path)
//   - [DecodeMessage]: Decode → CRC-14 verify → extract msg77 (RX path)

package codec

import "github.com/ColonelBlimp/station-manager/internal/ft8/message"

// EncodeMessage takes a 77-bit packed message, appends CRC-14, and
// LDPC-encodes to a 174-bit codeword.
//
// msg77 contains the 77-bit packed message in 10 bytes (as produced by
// [message.Pack]). Only the first 77 bits are used; any trailing bits in
// msg77[9] beyond bit 76 are ignored.
//
// Returns the 174-bit LDPC codeword packed MSB-first into 22 bytes.
func EncodeMessage(msg77 [10]byte) [NBytes]byte {
	info := message.Append91(msg77[:])
	return Encode(info)
}

// DecodeMessage takes 174 soft LLRs, LDPC-decodes, verifies CRC-14, and
// returns the 77-bit packed message.
//
// llr contains 174 log-likelihood ratios (positive = bit more likely 0,
// negative = bit more likely 1). maxIter is the maximum number of BP
// iterations (typically 25–50).
//
// Returns ok=false if the LDPC decode fails to converge or the CRC-14
// check does not pass.
func DecodeMessage(llr [N]float32, maxIter int) (msg77 [10]byte, ok bool) {
	info, decOK := Decode(llr, maxIter)
	if !decOK {
		return msg77, false
	}

	// Extract the 77-bit message from the 91-bit info payload.
	// Bytes 0–8 are fully message bits; byte 9 upper 5 bits (positions
	// 72–76) are message bits, lower 3 bits are the start of the CRC.
	copy(msg77[:], info[:10])
	msg77[9] &= 0xF8 // clear CRC bits that share byte 9

	// Recompute CRC-14 over the extracted message and compare to the
	// CRC embedded in the decoded info payload.
	wantCRC := message.CRC14(msg77[:])

	// Extract the 14-bit CRC from info bit positions 77–90.
	//   info[9] bits 2..0 → CRC bits 13..11
	//   info[10] bits 7..0 → CRC bits 10..3
	//   info[11] bits 7..5 → CRC bits 2..0
	gotCRC := uint16(info[9]&0x07) << 11
	gotCRC |= uint16(info[10]) << 3
	gotCRC |= uint16(info[11]>>5) & 0x07

	if gotCRC != wantCRC {
		return msg77, false
	}

	return msg77, true
}
