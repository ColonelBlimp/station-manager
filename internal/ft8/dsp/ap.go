// ap.go — A priori (AP) decoding support for weak FT8 signals.
//
// WSJT-X uses additional decode passes with a priori information (known
// callsigns, CQ patterns, fixed message fragments) to decode signals that
// are too weak for standard LDPC convergence (-15 to -23 dB). AP works by
// substituting high-confidence LLR values for known message bits, effectively
// reducing the LDPC problem from 174→~100 unknown bits.
//
// This file provides:
//   - [APContext]: holds precomputed a priori symbol arrays for mycall/dxcall.
//   - AP type constants and pass tables matching WSJT-X ft8b.f90.
//   - Known message fragment arrays (CQ, RRR, 73, RR73).
//
// Reference: WSJT-X lib/ft8/ft8b.f90 lines 39–82, 254–401.

package dsp

import (
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// --- AP type constants -------------------------------------------------------

// APType identifies the kind of a priori information injected into an AP pass.
// These match WSJT-X ft8b.f90 iaptype values.
const (
	APTypeNone     = 0 // No AP (regular decode)
	APTypeCQ       = 1 // CQ + i3 bits (29+3=32 known bits)
	APTypeMyCall   = 2 // MyCall + i3 bits (29+3=32 known bits)
	APTypeMyDx     = 3 // MyCall + DxCall + i3 bits (58+3=61 known bits)
	APTypeMyDxRRR  = 4 // MyCall + DxCall + RRR (77 known bits)
	APTypeMyDx73   = 5 // MyCall + DxCall + 73 (77 known bits)
	APTypeMyDxRR73 = 6 // MyCall + DxCall + RR73 (77 known bits)
)

// --- AP pass tables ----------------------------------------------------------

// nappasses[qsoProgress] = number of AP decode passes for each QSO state.
// Matches WSJT-X ft8b.f90 nappasses(0:5).
var nappasses = [6]int{2, 2, 2, 4, 4, 3}

// naptypes[qsoProgress][passIdx] = AP type for each QSO progress state.
// passIdx is 0-based (corresponds to ft8b.f90's 1-based ipass-4).
// Matches WSJT-X ft8b.f90 naptypes(0:5,1:4).
var naptypes = [6][4]int{
	{APTypeCQ, APTypeMyCall, APTypeNone, APTypeNone},          // QSO progress 0: CQ listening
	{APTypeMyCall, APTypeMyDx, APTypeNone, APTypeNone},        // QSO progress 1: Tx1
	{APTypeMyCall, APTypeMyDx, APTypeNone, APTypeNone},        // QSO progress 2: Tx2
	{APTypeMyDx, APTypeMyDxRRR, APTypeMyDx73, APTypeMyDxRR73}, // QSO progress 3: Tx3
	{APTypeMyDx, APTypeMyDxRRR, APTypeMyDx73, APTypeMyDxRR73}, // QSO progress 4: Tx4
	{APTypeMyDx, APTypeCQ, APTypeMyCall, APTypeNone},          // QSO progress 5: Tx5
}

// --- Known message fragment arrays (bipolar: +1/-1) --------------------------
// These are the binary→bipolar (0→-1, 1→+1) encodings of known message
// fragments. Matches WSJT-X ft8b.f90 data statements lines 39–46 after
// the 2*mcq-1 conversion.

// mcq is the 29-bit CQ token pattern (bipolar).
// Bit pattern: 0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,1,0,0
var mcq = [29]int8{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, +1, -1, -1}

// mrrr is the 19-bit RRR pattern (bipolar, covers bits 59–77 of a Type 1 message).
// Bit pattern: 0,1,1,1,1,1,1,0,1,0,0,1,0,0,1,0,0,0,1
var mrrr = [19]int8{-1, +1, +1, +1, +1, +1, +1, -1, +1, -1, -1, +1, -1, -1, +1, -1, -1, -1, +1}

// m73 is the 19-bit 73 pattern (bipolar).
// Bit pattern: 0,1,1,1,1,1,1,0,1,0,0,1,0,1,0,0,0,0,1
var m73 = [19]int8{-1, +1, +1, +1, +1, +1, +1, -1, +1, -1, -1, +1, -1, +1, -1, -1, -1, -1, +1}

// mrr73 is the 19-bit RR73 pattern (bipolar).
// Bit pattern: 0,1,1,1,1,1,1,0,0,1,1,1,0,1,0,1,0,0,1
var mrr73 = [19]int8{-1, +1, +1, +1, +1, +1, +1, -1, -1, +1, +1, +1, -1, +1, -1, +1, -1, -1, +1}

// i3bits is the bipolar encoding of i3=001 (Type 1 message), bits 75–77 (0-indexed: 74–76).
// These are the last 3 bits of a 77-bit message: bit74=0, bit75=0, bit76=1.
var i3bits = [3]int8{-1, -1, +1}

// --- APContext ----------------------------------------------------------------

// APContext holds precomputed a priori information for AP decoding.
// It is constructed once from the operator's callsign and (optionally) the
// DX station's callsign, then reused across all candidates in a decode window.
type APContext struct {
	// MyCall is the operator's callsign (uppercase, trimmed).
	MyCall string

	// DxCall is the DX station's callsign (empty if unknown).
	DxCall string

	// QSOProgress tracks the current QSO state (0–5). Determines which
	// AP types are attempted. Default 0 = "CQ listening" mode.
	QSOProgress int

	// apsym holds the bipolar encoding of the first 58 message bits
	// (28-bit mycall + 1-bit p1 + 28-bit dxcall + 1-bit p2) for a
	// Type 1 message "mycall dxcall RRR". Values are +1/-1.
	// If dxcall is unknown, only the first 29 bits (mycall + p1) are valid.
	apsym [58]int8

	// hasMyCall is true if mycall was successfully encoded.
	hasMyCall bool

	// hasDxCall is true if dxcall was successfully encoded.
	hasDxCall bool
}

// NewAPContext creates an APContext from callsign strings.
//
// mycall is required. dxcall may be empty (only AP types 1 and 2 will be
// available). qsoProgress defaults to 0 if out of range.
//
// The function packs a dummy Type 1 message to extract the first 58 bits
// of the encoded callsigns, matching WSJT-X's ft8apset.f90 approach.
func NewAPContext(mycall, dxcall string, qsoProgress int) *APContext {
	ctx := &APContext{
		MyCall:      mycall,
		DxCall:      dxcall,
		QSOProgress: qsoProgress,
	}
	if qsoProgress < 0 || qsoProgress > 5 {
		ctx.QSOProgress = 0
	}

	// Encode mycall into apsym[0:29] (28-bit callsign + 1-bit p1=0).
	if mycall != "" {
		n28, err := encodeCallForAP(mycall)
		if err == nil {
			ctx.hasMyCall = true
			packN28Bipolar(ctx.apsym[:], 0, n28)
			ctx.apsym[28] = -1 // p1=0 → bipolar -1
		}
	}

	// Encode dxcall into apsym[29:58] (28-bit callsign + 1-bit p2=0).
	if dxcall != "" {
		n28, err := encodeCallForAP(dxcall)
		if err == nil {
			ctx.hasDxCall = true
			packN28Bipolar(ctx.apsym[:], 29, n28)
			ctx.apsym[57] = -1 // p2=0 → bipolar -1
		}
	}

	return ctx
}

// encodeCallForAP encodes a callsign (or CQ/DE/QRZ token) for AP use.
func encodeCallForAP(call string) (uint32, error) {
	switch call {
	case "CQ":
		return message.EncodeCQ(), nil
	case "DE":
		return message.EncodeDE(), nil
	case "QRZ":
		return message.EncodeQRZ(), nil
	}
	return message.EncodeCallsign(call)
}

// packN28Bipolar writes a 28-bit value into a bipolar slice at the given offset.
// Bits are packed MSB-first: 0→-1, 1→+1.
func packN28Bipolar(dst []int8, offset int, n28 uint32) {
	for i := 27; i >= 0; i-- {
		if n28&(1<<uint(i)) != 0 {
			dst[offset+27-i] = +1
		} else {
			dst[offset+27-i] = -1
		}
	}
}

// --- AP pass application -----------------------------------------------------

// applyAPPass modifies a copy of the base LLR array and constructs the apmask
// for a specific AP type. Returns the modified LLR array and apmask.
//
// Parameters:
//   - baseLLR: the nsym=1 LLR array (llra) to use as the starting point
//   - apType: the AP type to apply (APTypeCQ, APTypeMyCall, etc.)
//   - ctx: the APContext with precomputed callsign symbols
//
// Returns:
//   - llrz: modified LLR array with AP bits injected
//   - apmask: 174-element mask (1 = AP-fixed bit, 0 = free bit)
//   - ok: false if the AP type cannot be applied (missing callsign info)
func applyAPPass(baseLLR *[CodedBits]float32, apType int, ctx *APContext) (llrz [CodedBits]float32, apmask [codec.N]uint8, ok bool) {
	// Start with the base LLR values.
	copy(llrz[:], baseLLR[:])

	// Compute apmag = 1.01 × max(|llra|), matching ft8b.f90 line 241.
	apmag := float32(0)
	for _, v := range baseLLR {
		av := float32(math.Abs(float64(v)))
		if av > apmag {
			apmag = av
		}
	}
	apmag *= 1.01

	switch apType {
	case APTypeCQ:
		// CQ + i3 bits: inject 29 bits for CQ pattern + 3 bits for i3=001.
		// Positions 0–28 (1-indexed 1:29) and 74–76 (1-indexed 75:77).
		for i := 0; i < 29; i++ {
			apmask[i] = 1
			llrz[i] = -apmag * float32(mcq[i])
		}
		// i3 bits: positions 74, 75, 76 (0-indexed).
		apmask[74] = 1
		apmask[75] = 1
		apmask[76] = 1
		llrz[74] = -apmag * float32(i3bits[0])
		llrz[75] = -apmag * float32(i3bits[1])
		llrz[76] = -apmag * float32(i3bits[2])
		ok = true

	case APTypeMyCall:
		// MyCall + i3 bits: inject 29 bits for mycall + p1 + 3 bits for i3.
		if !ctx.hasMyCall {
			return
		}
		for i := 0; i < 29; i++ {
			apmask[i] = 1
			llrz[i] = -apmag * float32(ctx.apsym[i])
		}
		apmask[74] = 1
		apmask[75] = 1
		apmask[76] = 1
		llrz[74] = -apmag * float32(i3bits[0])
		llrz[75] = -apmag * float32(i3bits[1])
		llrz[76] = -apmag * float32(i3bits[2])
		ok = true

	case APTypeMyDx:
		// MyCall + DxCall + i3 bits: inject 58 bits + 3 bits for i3.
		if !ctx.hasMyCall || !ctx.hasDxCall {
			return
		}
		for i := 0; i < 58; i++ {
			apmask[i] = 1
			llrz[i] = -apmag * float32(ctx.apsym[i])
		}
		apmask[74] = 1
		apmask[75] = 1
		apmask[76] = 1
		llrz[74] = -apmag * float32(i3bits[0])
		llrz[75] = -apmag * float32(i3bits[1])
		llrz[76] = -apmag * float32(i3bits[2])
		ok = true

	case APTypeMyDxRRR:
		// Full message: MyCall + DxCall + RRR (all 77 bits known).
		if !ctx.hasMyCall || !ctx.hasDxCall {
			return
		}
		for i := 0; i < 58; i++ {
			apmask[i] = 1
			llrz[i] = -apmag * float32(ctx.apsym[i])
		}
		for i := 0; i < 19; i++ {
			apmask[58+i] = 1
			llrz[58+i] = -apmag * float32(mrrr[i])
		}
		ok = true

	case APTypeMyDx73:
		// Full message: MyCall + DxCall + 73.
		if !ctx.hasMyCall || !ctx.hasDxCall {
			return
		}
		for i := 0; i < 58; i++ {
			apmask[i] = 1
			llrz[i] = -apmag * float32(ctx.apsym[i])
		}
		for i := 0; i < 19; i++ {
			apmask[58+i] = 1
			llrz[58+i] = -apmag * float32(m73[i])
		}
		ok = true

	case APTypeMyDxRR73:
		// Full message: MyCall + DxCall + RR73.
		if !ctx.hasMyCall || !ctx.hasDxCall {
			return
		}
		for i := 0; i < 58; i++ {
			apmask[i] = 1
			llrz[i] = -apmag * float32(ctx.apsym[i])
		}
		for i := 0; i < 19; i++ {
			apmask[58+i] = 1
			llrz[58+i] = -apmag * float32(mrr73[i])
		}
		ok = true
	}

	return
}
