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
// The decode chain matches WSJT-X decode174_91.f90 with maxosd=2:
//   - Sum-product belief-propagation with convergence checking
//   - On BP failure, up to 2 OSD calls fed by cumulative zsum snapshots
//     (zsave) from the BP iterations
//
// Returns ok=false if all decoders fail or the CRC-14 check does not pass.
func DecodeMessage(llr [N]float32, maxIter int) (msg77 [10]byte, ok bool) {
	info, zsave, decOK := DecodeWithZsave(llr, maxIter)
	if decOK {
		return verifyAndExtract(info)
	}

	// BP failed — try OSD with zsave snapshots (maxosd=2).
	// zsave[0] = cumulative zsum after iteration 1
	// zsave[1] = cumulative zsum after iteration 2
	// These carry BP-refined posterior information that can decode
	// weaker signals where BP partially converged.
	// ndeep=2 matches WSJT-X norder=2 (order-2 pair-flip search).
	maxOSD := 2
	for i := range maxOSD {
		info, decOK = DecodeOSD(zsave[i], 2)
		if decOK {
			return verifyAndExtract(info)
		}
	}

	return msg77, false
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
	return DecodeMessageAPWithDepth(llr, apmask, maxIter, 2)
}

// DecodeMessageShallow is like [DecodeMessage] but uses OSD order-1 instead
// of order-2. This reduces false alarm probability by 45× (91 single-bit
// flips vs 91+4095 pair flips) at the cost of slightly reduced sensitivity
// for marginal signals.
//
// Use this for secondary LLR passes (bmetb, bmetc) where the primary pass
// (bmeta) already has full OSD order-2 coverage.
func DecodeMessageShallow(llr [N]float32, maxIter int) (msg77 [10]byte, ok bool) {
	info, zsave, decOK := DecodeWithZsave(llr, maxIter)
	if decOK {
		return verifyAndExtract(info)
	}

	maxOSD := 2
	for i := range maxOSD {
		info, decOK = DecodeOSD(zsave[i], 1) // order-1 only
		if decOK {
			return verifyAndExtract(info)
		}
	}

	return msg77, false
}

// DecodeMessageAPWithDepth is like [DecodeMessageAP] but accepts an explicit
// OSD search depth (ndeep). Use ndeep=0 for AP types with few constrained
// bits (e.g., AP CQ type 1 constrains only 32 of 77 message bits) to reduce
// false alarm probability. Use ndeep=2 for AP types that constrain most
// bits (types 3–6).
//
// ndeep controls:
//   - 0: order-0 OSD only (hard decisions, no flip search)
//   - 1: order-0 + order-1 (91 single-bit flips)
//   - 2: order-0 + order-1 + order-2 (91 + 4095 pair flips)
//   - <0: BP only, no OSD fallback
func DecodeMessageAPWithDepth(llr [N]float32, apmask [N]uint8, maxIter, ndeep int) (msg77 [10]byte, ok bool) {
	info, zsave, decOK := DecodeAP(llr, apmask, maxIter)
	if decOK {
		return verifyAndExtract(info)
	}

	// BP failed — try OSD with zsave snapshots.
	if ndeep >= 0 {
		maxOSD := 2
		for i := range maxOSD {
			info, decOK = DecodeOSDAP(zsave[i], apmask, ndeep)
			if decOK {
				return verifyAndExtract(info)
			}
		}
	}

	return msg77, false
}

// verifyAndExtract checks CRC-14 and extracts the 77-bit message from a
// decoded 91-bit info payload. Shared by [DecodeMessage] and [DecodeMessageAP].
//
// Also rejects the all-zero codeword (WSJT-X ft8b.f90 line 423) which is a
// trivial fixed point of LDPC decoding.
func verifyAndExtract(info [KBytes]byte) (msg77 [10]byte, ok bool) {
	// Reject all-zero info payload. The all-zero codeword is a valid LDPC
	// codeword (zero syndrome) and passes CRC-14 trivially, but it encodes
	// no useful message. This matches WSJT-X ft8b.f90 line 423:
	//   if(count(cw.eq.0).eq.174) cycle
	allZero := true
	for _, b := range info {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
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

// ValidateMsg77 checks that a CRC-verified 77-bit message has valid (i3,n3)
// type fields, can be unpacked, and contains plausible callsigns. This
// filters false decodes that pass CRC by chance but contain structurally
// invalid or implausible message payloads.
//
// Call this after [DecodeMessage] or [DecodeMessageAP] returns ok=true to
// apply WSJT-X-style post-decode validation plus callsign plausibility.
//
// Checks (matching WSJT-X ft8b.f90 lines 425–430 + additional):
//  1. i3 must be in [0,5]
//  2. When i3=0, n3 must be in [0,6] excluding 2
//  3. For supported types, message.Unpack must succeed
//  4. Decoded callsigns must be structurally plausible (letter+digit rule)
func ValidateMsg77(msg77 [10]byte) bool {
	i3 := int(message.UnpackBits(msg77[:], 74, 3))
	n3 := int(message.UnpackBits(msg77[:], 71, 3))

	// i3 range check: i3 must be 0–5.
	if i3 > 5 {
		return false
	}

	// When i3=0, n3 must be 0–6, excluding n3=2 (reserved/invalid).
	if i3 == 0 && n3 > 6 {
		return false
	}
	if i3 == 0 && n3 == 2 {
		return false
	}

	// Try to unpack — reject if the supported unpackers fail.
	// For unsupported (i3,n3) combinations, we pass through: they are
	// valid FT8 messages, just not types we can decode text for.
	_, err := message.Unpack(msg77)
	if err != nil {
		// Distinguish "unsupported type" (pass through) from "invalid payload"
		// (reject). For types we claim to support, unpack failure means
		// corrupted payload fields — reject as false decode.
		switch {
		case i3 == 1: // Type 1 standard — we support this, so failure = bad
			return false
		case i3 == 0 && n3 == 0: // Type 0 free text — we support this
			return false
		case i3 == 4: // Type 4 non-standard — we support this
			return false
		}
		// Other types: pass through (unsupported but structurally valid).
	}

	// Callsign plausibility check: reject messages with structurally
	// implausible callsigns (e.g., all-digits or all-letters).
	if !message.PlausibleMessage(msg77) {
		return false
	}

	return true
}
