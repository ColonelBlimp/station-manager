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
// The decode chain is: belief-propagation first; if BP fails to converge,
// ordered-statistics decoding (OSD order-1) is attempted as a fallback.
// Returns ok=false if both decoders fail or the CRC-14 check does not pass.
func DecodeMessage(llr [N]float32, maxIter int) (msg77 [10]byte, ok bool) {
	info, decOK := Decode(llr, maxIter)
	if !decOK {
		// BP failed to converge — try OSD as fallback.
		info, decOK = DecodeOSD(llr, 1)
		if !decOK {
			return msg77, false
		}
	}
	return verifyAndExtract(info)
}

// DecodeMessageAP takes 174 soft LLRs with an AP mask, LDPC-decodes using
// AP-aware BP and OSD, verifies CRC-14, and returns the 77-bit packed message.
//
// This function is the AP counterpart of [DecodeMessage]. For bits where
// apmask[i]==1, the decoder holds those bits at their injected LLR value
// during BP iterations and skips flipping them during OSD search.
//
// The decode chain matches WSJT-X decode174_91.f90 with maxosd=2:
//   - BP with apmask (30 iterations)
//   - Up to 2 OSD calls fed by cumulative zsum snapshots from BP
//
// Returns ok=false if all decoders fail or the CRC-14 check does not pass.
func DecodeMessageAP(llr [N]float32, apmask [N]uint8, maxIter int) (msg77 [10]byte, ok bool) {
	info, zsave, decOK := DecodeAP(llr, apmask, maxIter)
	if decOK {
		return verifyAndExtract(info)
	}

	// BP failed — try OSD with zsave snapshots (maxosd=2).
	// zsave[0] = channel LLRs (iteration 0 is not saved, but zsave[0] is
	// from iteration 1 zsum). We try up to 2 OSD calls matching WSJT-X.
	maxOSD := 2
	for i := range maxOSD {
		info, decOK = DecodeOSDAP(zsave[i], apmask, 1)
		if decOK {
			return verifyAndExtract(info)
		}
	}

	return msg77, false
}

// verifyAndExtract checks CRC-14 and extracts the 77-bit message from a
// decoded 91-bit info payload. Shared by [DecodeMessage] and [DecodeMessageAP].
func verifyAndExtract(info [KBytes]byte) (msg77 [10]byte, ok bool) {
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
