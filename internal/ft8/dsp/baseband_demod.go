// baseband_demod.go — complex-baseband demodulation and multi-pass LLR extraction.
//
// This is the Go port of the core decode logic from WSJT-X ft8b.f90 (lines
// 154–239) and sync8d.f90. After [DownsampleBaseband] produces 3200 complex
// samples at 200 Hz with the candidate at DC, this file:
//
//  1. Refines time/frequency using [Sync8d] — correlation of Costas sync
//     templates against the complex baseband signal.
//  2. Extracts per-symbol 32-point DFTs to get 8 complex tone values per
//     symbol: cs[tone][symbol].
//  3. Computes 4 different LLR sets using multi-symbol joint demodulation:
//     - bmeta (nsym=1): single-symbol, max-metric
//     - bmetb (nsym=2): joint 2-symbol
//     - bmetc (nsym=3): joint 3-symbol
//     - bmetd (nsym=1): bit-by-bit normalised
//  4. Normalises each LLR set via [NormalizeBmet] (unit standard deviation)
//     and scales by 2.83.
//
// Reference: WSJT-X lib/ft8/ft8b.f90, lib/ft8/sync8d.f90.

package dsp

import (
	"math"
	"math/cmplx"
)

// scaleFac is the post-normalization scale factor for LLRs, matching
// WSJT-X ft8b.f90 line 235.
const scaleFac = 2.83

// BasebandNP2 is the upper limit (exclusive) on usable baseband sample indices.
// Samples at indices 0..NP2-1 contain valid downsampled signal; indices
// NP2..NFFT2-1 are circular wrap-around from zero-padding and must not
// be accessed. Matches WSJT-X ft8b.f90 parameter(NP2=2812) and
// sync8d.f90 parameter(NP2=2812).
const BasebandNP2 = 2812

// minSyncForDecode is the minimum hard-sync count (out of 21) required
// to proceed with decode. Signals below this threshold are almost certainly
// noise. Matches WSJT-X ft8b.f90 line 177.
const minSyncForDecode = 6

// one is the precomputed bitmask table: one[i][j] = true if bit j of i is set.
// i ranges over 0..511 (9 bits), j ranges over 0..8 (bit positions).
// Matches WSJT-X ft8b.f90 lines 83–88.
var one [512][9]bool

func init() {
	for i := range 512 {
		for j := range 9 {
			if i&(1<<j) != 0 {
				one[i][j] = true
			}
		}
	}
}

// graymap maps 3-bit data value to FT8 tone index.
// Matches WSJT-X ft8b.f90 line 48.
var graymap = [8]int{0, 1, 3, 2, 5, 6, 4, 7}

// csyncCache holds precomputed Costas sync waveforms for sync8d.
// csync[tone][sample] = exp(j * 2π * icos7[tone] * sample / 32)
var csyncCache [7][BasebandSamplesPerSymbol]complex128

func init() {
	twopi := 2.0 * math.Pi
	for i := range 7 {
		dphi := twopi * float64(CostasSync[i]) / float64(BasebandSamplesPerSymbol)
		phi := 0.0
		for j := range BasebandSamplesPerSymbol {
			csyncCache[i][j] = complex(math.Cos(phi), math.Sin(phi))
			phi = math.Mod(phi+dphi, twopi)
		}
	}
}

// Sync8d computes the sync power of a complex baseband signal at a given
// time offset, optionally applying a frequency tweak.
//
// This is the Go port of WSJT-X sync8d.f90.
//
// Parameters:
//   - cd0: 3200 complex samples at 200 Hz (from [DownsampleBaseband])
//   - i0: starting sample index for the first symbol
//   - ctwk: 32-element frequency tweak phasor (nil or empty = no tweak)
//   - usetwk: if true, apply ctwk to the sync template
//
// Returns the total sync power across all 3 Costas arrays (21 symbols).
func Sync8d(cd0 []complex128, i0 int, ctwk []complex128, usetwk bool) float64 {
	sync := 0.0
	for i := range 7 {
		i1 := i0 + i*BasebandSamplesPerSymbol
		i2 := i1 + 36*BasebandSamplesPerSymbol
		i3 := i1 + 72*BasebandSamplesPerSymbol

		// Build the sync template (optionally tweaked).
		var csync2 [BasebandSamplesPerSymbol]complex128
		copy(csync2[:], csyncCache[i][:])
		if usetwk && len(ctwk) >= BasebandSamplesPerSymbol {
			for j := range BasebandSamplesPerSymbol {
				csync2[j] *= ctwk[j]
			}
		}

		// Correlate against three Costas positions.
		// Upper bound is NP2-1 (not len(cd0)-1), matching WSJT-X sync8d.f90.
		var z1, z2, z3 complex128
		if i1 >= 0 && i1+BasebandSamplesPerSymbol-1 <= BasebandNP2-1 {
			for j := range BasebandSamplesPerSymbol {
				z1 += cd0[i1+j] * complex(real(csync2[j]), -imag(csync2[j]))
			}
		}
		if i2 >= 0 && i2+BasebandSamplesPerSymbol-1 <= BasebandNP2-1 {
			for j := range BasebandSamplesPerSymbol {
				z2 += cd0[i2+j] * complex(real(csync2[j]), -imag(csync2[j]))
			}
		}
		if i3 >= 0 && i3+BasebandSamplesPerSymbol-1 <= BasebandNP2-1 {
			for j := range BasebandSamplesPerSymbol {
				z3 += cd0[i3+j] * complex(real(csync2[j]), -imag(csync2[j]))
			}
		}

		// Power = |z|^2
		sync += real(z1)*real(z1) + imag(z1)*imag(z1)
		sync += real(z2)*real(z2) + imag(z2)*imag(z2)
		sync += real(z3)*real(z3) + imag(z3)*imag(z3)
	}
	return sync
}

// BasebandDemodResult holds the 4 sets of LLRs produced by baseband demodulation.
type BasebandDemodResult struct {
	LLRa    [CodedBits]float32 // nsym=1, max-metric
	LLRb    [CodedBits]float32 // nsym=2, joint 2-symbol
	LLRc    [CodedBits]float32 // nsym=3, joint 3-symbol
	LLRd    [CodedBits]float32 // nsym=1, bit-by-bit normalised
	Nsync   int                // hard sync count (0–21)
	IBest   int                // refined starting sample index in baseband
	FreqAdj float64            // frequency adjustment (Hz) applied during refinement

	// Diagnostic fields (always populated, zero cost when unused).
	Is1       int     // sync hits in Costas block 1 (symbols 0–6)
	Is2       int     // sync hits in Costas block 2 (symbols 36–42)
	Is3       int     // sync hits in Costas block 3 (symbols 72–78)
	ValidSyms int     // number of symbols within NP2 bound (0–79)
	RawSigma  float64 // raw bmeta σ before normalization (0 if nsync ≤ 6)
}

// DemodulateBaseband performs complete baseband demodulation of a candidate
// signal, producing 4 LLR sets for multi-pass LDPC decoding.
//
// This is the Go port of WSJT-X ft8b.f90 lines 104–239.
//
// Parameters:
//   - samples: the full 15 s audio capture buffer at 12 kHz
//   - longFFT: precomputed frequency-domain data from [LongFFT]
//   - f0: candidate center frequency (Hz)
//   - xdt: candidate time offset (seconds)
//
// Returns the demodulation result with 4 LLR sets, or a result with Nsync ≤ 6
// if the signal quality is too poor for decoding.
func DemodulateBaseband(longFFT []complex128, f0, xdt float64) BasebandDemodResult {
	var result BasebandDemodResult

	fs2 := BasebandRate // 200.0
	dt2 := 1.0 / fs2

	// Step 1: Downsample to complex baseband.
	cd0 := DownsampleBaseband(longFFT, f0)

	// Step 2: Fine time sync — search ±10 samples around initial guess.
	// Note: xdt is the absolute time offset in seconds from the start of the
	// audio buffer (our convention), NOT the WSJT-X convention where xdt=0
	// means 0.5 seconds into the capture. We convert directly to baseband
	// sample index without the +0.5 offset used in ft8b.f90.
	i0 := int(math.Round(xdt * fs2))
	smax := 0.0
	ibest := i0
	for idt := i0 - 10; idt <= i0+10; idt++ {
		sync := Sync8d(cd0, idt, nil, false)
		if sync > smax {
			smax = sync
			ibest = idt
		}
	}

	// Step 3: Fine frequency sync — search ±2.5 Hz in 0.5 Hz steps.
	twopi := 2.0 * math.Pi
	smax = 0.0
	delfbest := 0.0
	for ifr := -5; ifr <= 5; ifr++ {
		delf := float64(ifr) * 0.5
		dphi := twopi * delf * dt2
		ctwk := make([]complex128, BasebandSamplesPerSymbol)
		phi := 0.0
		for i := range BasebandSamplesPerSymbol {
			ctwk[i] = complex(math.Cos(phi), math.Sin(phi))
			phi = math.Mod(phi+dphi, twopi)
		}
		sync := Sync8d(cd0, ibest, ctwk, true)
		if sync > smax {
			smax = sync
			delfbest = delf
		}
	}

	// Apply frequency correction and re-downsample.
	f1 := f0 + delfbest
	cd0 = DownsampleBaseband(longFFT, f1)

	// Step 4: Final fine time sync — search ±4 samples.
	var ss [9]float64
	for idt := -4; idt <= 4; idt++ {
		ss[idt+4] = Sync8d(cd0, ibest+idt, nil, false)
	}
	smax = ss[0]
	imax := 0
	for i := 1; i < 9; i++ {
		if ss[i] > smax {
			smax = ss[i]
			imax = i
		}
	}
	ibest = imax - 4 + ibest

	result.IBest = ibest
	result.FreqAdj = delfbest

	// Count how many symbols are within the NP2 bound.
	validSyms := 0
	for k := 0; k < NumSymbols; k++ {
		i1 := ibest + k*BasebandSamplesPerSymbol
		if i1 >= 0 && i1+BasebandSamplesPerSymbol-1 <= BasebandNP2-1 {
			validSyms++
		}
	}
	result.ValidSyms = validSyms

	// Step 5: Extract per-symbol 32-point DFTs.
	// cs[tone][symbol] = complex tone value, s8[tone][symbol] = magnitude.
	var cs [NumTones][NumSymbols]complex128
	var s8 [NumTones][NumSymbols]float64

	for k := 0; k < NumSymbols; k++ {
		i1 := ibest + k*BasebandSamplesPerSymbol
		var csymb [BasebandSamplesPerSymbol]complex128
		// Upper bound is NP2-1 (not len(cd0)-1), matching WSJT-X ft8b.f90 line 157.
		if i1 >= 0 && i1+BasebandSamplesPerSymbol-1 <= BasebandNP2-1 {
			copy(csymb[:], cd0[i1:i1+BasebandSamplesPerSymbol])
		}

		// 32-point FFT.
		fftBuf := csymb[:]
		complexFFTForward(fftBuf)

		// Extract tones 0–7 from bins 0–7 of the FFT output.
		// Bin 0 = DC (tone 0), bin k = k*6.25 Hz (tone k).
		// In Fortran (1-indexed): cs(0:7,k) = csymb(1:8)/1e3
		// In Go (0-indexed): cs[tone][k] = csymb[tone]/1e3
		// Scale by 1e-3 to match WSJT-X.
		for tone := range NumTones {
			cs[tone][k] = csymb[tone] / 1e3
			s8[tone][k] = cmplx.Abs(csymb[tone])
		}
	}

	// Step 6: Hard sync quality check — per Costas block.
	is1, is2, is3 := 0, 0, 0
	for k := 0; k < 7; k++ {
		// First Costas block (symbols 0–6).
		peakTone := 0
		for t := 1; t < NumTones; t++ {
			if s8[t][k] > s8[peakTone][k] {
				peakTone = t
			}
		}
		if peakTone == int(CostasSync[k]) {
			is1++
		}

		// Second Costas block (symbols 36–42).
		peakTone = 0
		for t := 1; t < NumTones; t++ {
			if s8[t][k+36] > s8[peakTone][k+36] {
				peakTone = t
			}
		}
		if peakTone == int(CostasSync[k]) {
			is2++
		}

		// Third Costas block (symbols 72–78).
		peakTone = 0
		for t := 1; t < NumTones; t++ {
			if s8[t][k+72] > s8[peakTone][k+72] {
				peakTone = t
			}
		}
		if peakTone == int(CostasSync[k]) {
			is3++
		}
	}
	nsync := is1 + is2 + is3
	result.Nsync = nsync
	result.Is1 = is1
	result.Is2 = is2
	result.Is3 = is3

	if nsync <= minSyncForDecode {
		return result
	}

	// Step 7: Multi-symbol joint LLR extraction.
	// Matches ft8b.f90 lines 182–229.
	var bmeta, bmetb, bmetc, bmetd [CodedBits]float32

	for nsym := 1; nsym <= 3; nsym++ {
		nt := 1
		for range 3 * nsym {
			nt *= 2
		}
		// nt = 2^(3*nsym): 8 for nsym=1, 64 for nsym=2, 512 for nsym=3

		for ihalf := 1; ihalf <= 2; ihalf++ {
			for k := 1; k <= 29; k += nsym {
				var ks int
				if ihalf == 1 {
					ks = k + 7
				} else {
					ks = k + 43
				}

				// Compute s2[i] = magnitude of sum of complex tone vectors.
				s2 := make([]float64, nt)
				for i := 0; i < nt; i++ {
					i3 := i & 7
					i2 := (i & 63) >> 3
					i1 := i >> 6

					var val complex128
					switch nsym {
					case 1:
						val = cs[graymap[i3]][ks-1] // Fortran 1-indexed → Go 0-indexed
					case 2:
						val = cs[graymap[i2]][ks-1] + cs[graymap[i3]][ks]
					case 3:
						val = cs[graymap[i1]][ks-1] + cs[graymap[i2]][ks] + cs[graymap[i3]][ks+1]
					}
					s2[i] = cmplx.Abs(val)
				}

				// LLR bit index: 1-indexed in Fortran → 0-indexed in Go.
				i32 := (k-1)*3 + (ihalf-1)*87

				var ibmax int
				switch nsym {
				case 1:
					ibmax = 2
				case 2:
					ibmax = 5
				case 3:
					ibmax = 8
				}

				for ib := 0; ib <= ibmax; ib++ {
					bitIdx := i32 + ib
					if bitIdx >= CodedBits {
						continue
					}

					// bm = max(s2 where bit NOT set) - max(s2 where bit set)
					// This gives positive bm when bit is likely 0, matching
					// our LLR convention (positive → bit more likely 0).
					// WSJT-X uses the opposite convention internally and its
					// LDPC decoder expects positive → bit likely 1.
					maskBit := ibmax - ib
					max1 := -math.MaxFloat64 // max where bit is set
					max0 := -math.MaxFloat64 // max where bit is not set
					for i := 0; i < nt; i++ {
						if one[i][maskBit] {
							if s2[i] > max1 {
								max1 = s2[i]
							}
						} else {
							if s2[i] > max0 {
								max0 = s2[i]
							}
						}
					}
					bm := float32(max0 - max1)

					switch nsym {
					case 1:
						bmeta[bitIdx] = bm
						// Bit-by-bit normalised: bm/den
						den := max1
						if max0 > den {
							den = max0
						}
						if den > 0 {
							bmetd[bitIdx] = bm / float32(den)
						} else {
							bmetd[bitIdx] = 0
						}
					case 2:
						bmetb[bitIdx] = bm
					case 3:
						bmetc[bitIdx] = bm
					}
				}
			}
		}
	}

	// Step 8: Compute raw sigma before normalization (for diagnostics).
	{
		n := float64(CodedBits)
		var sum, sum2 float64
		for i := range CodedBits {
			v := float64(bmeta[i])
			sum += v
			sum2 += v * v
		}
		bmetav := sum / n
		bmet2av := sum2 / n
		vari := bmet2av - bmetav*bmetav
		if vari > 0 {
			result.RawSigma = math.Sqrt(vari)
		} else {
			result.RawSigma = math.Sqrt(bmet2av)
		}
	}

	// Step 9: Normalize and scale.
	NormalizeBmet(&bmeta)
	NormalizeBmet(&bmetb)
	NormalizeBmet(&bmetc)
	NormalizeBmet(&bmetd)

	for i := range CodedBits {
		result.LLRa[i] = scaleFac * bmeta[i]
		result.LLRb[i] = scaleFac * bmetb[i]
		result.LLRc[i] = scaleFac * bmetc[i]
		result.LLRd[i] = scaleFac * bmetd[i]
	}

	return result
}

// NormalizeBmet normalises a 174-element LLR array to unit standard deviation.
// This matches WSJT-X's normalizebmet subroutine (ft8b.f90 lines 466–479).
//
// The normalisation divides all elements by the standard deviation (σ),
// computed as sqrt(var) where var = mean(bmet²) - mean(bmet)².
// If the variance is non-positive, σ = sqrt(mean(bmet²)) is used as fallback.
func NormalizeBmet(bmet *[CodedBits]float32) {
	n := float64(CodedBits)
	var sum, sum2 float64
	for i := range CodedBits {
		v := float64(bmet[i])
		sum += v
		sum2 += v * v
	}

	bmetav := sum / n
	bmet2av := sum2 / n
	vari := bmet2av - bmetav*bmetav

	var sigma float64
	if vari > 0 {
		sigma = math.Sqrt(vari)
	} else {
		sigma = math.Sqrt(bmet2av)
	}

	if sigma <= 0 {
		return
	}

	invSigma := float32(1.0 / sigma)
	for i := range CodedBits {
		bmet[i] *= invSigma
	}
}

// complexFFTForward computes an in-place forward FFT on a complex slice.
// Auto-dispatches: radix-2, mixed-radix (5-smooth), or Bluestein.
func complexFFTForward(x []complex128) {
	generalDFT(x)
}
