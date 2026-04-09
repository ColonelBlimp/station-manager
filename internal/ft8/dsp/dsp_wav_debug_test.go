// dsp_wav_debug_test.go — diagnostic test to investigate WAV decode failures.

package dsp

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// dbgLDPCInstrumentation runs the instrumented LDPC decoder and logs the
// full per-iteration convergence trace. This is the primary diagnostic tool
// for understanding why BP fails to converge on real-world signals.
func dbgLDPCInstrumentation(t *testing.T, label string, llr [CodedBits]float32) bool {
	t.Helper()

	// Convert DSP-sized LLR array to codec-sized array.
	var codecLLR [codec.N]float32
	copy(codecLLR[:], llr[:])

	result := codec.DecodeDebug(codecLLR, 100)
	t.Logf("  --- %s LDPC instrumentation ---", label)
	t.Logf("%s", result.Summary())

	// Highlight convergence patterns.
	if len(result.Stats) >= 2 {
		first := result.Stats[0]
		last := result.Stats[len(result.Stats)-1]

		// Check if syndrome weight is decreasing, oscillating, or stuck.
		minSW := first.SyndromeWeight
		maxSW := first.SyndromeWeight
		increasing := 0
		decreasing := 0
		for i := 1; i < len(result.Stats); i++ {
			sw := result.Stats[i].SyndromeWeight
			prevSW := result.Stats[i-1].SyndromeWeight
			if sw > prevSW {
				increasing++
			} else if sw < prevSW {
				decreasing++
			}
			if sw < minSW {
				minSW = sw
			}
			if sw > maxSW {
				maxSW = sw
			}
		}

		t.Logf("  Syndrome range: %d–%d (↑%d ↓%d flat=%d)",
			minSW, maxSW, increasing, decreasing,
			len(result.Stats)-1-increasing-decreasing)

		if last.SyndromeWeight > 0 && last.BitFlips > 0 && last.BitFlips < 10 {
			t.Logf("  ⚠ Low bit-flips (%d) with non-zero syndrome — BP may be oscillating", last.BitFlips)
		}
		if last.SyndromeWeight > 0 && last.BitFlips == 0 {
			t.Logf("  ⚠ Zero bit-flips with syndrome=%d — BP has converged to wrong codeword", last.SyndromeWeight)
		}
		if first.SyndromeWeight > 60 {
			t.Logf("  ⚠ Very high initial syndrome (%d/83) — input LLRs may be severely corrupted", first.SyndromeWeight)
		}
		if result.InputMeanAbs < 0.5 {
			t.Logf("  ⚠ Very low mean|LLR| (%.3f) — weak signal or demod issue", result.InputMeanAbs)
		}
		if result.InputMeanAbs > 4.0 {
			t.Logf("  ⚠ High mean|LLR| (%.3f) — LLRs may be over-confident", result.InputMeanAbs)
		}
	}

	return result.OK
}

func TestWAVDebugDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WAV debug diagnostics (slow under -race)")
	}
	dir := testdataDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no testdata dir")
	}

	hannWin := HannCoefficients(SamplesPerSymbol)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".wav") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			wav, err := audio.ReadWAV(path)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}
			if wav.SampleRate != SampleRate || wav.Channels != 1 {
				t.Skipf("wrong format (rate=%d ch=%d)", wav.SampleRate, wav.Channels)
			}
			t.Logf("Samples: %d (%.2f s)", len(wav.Samples), float64(len(wav.Samples))/float64(SampleRate))

			sg := Spectrogram(wav.Samples, SamplesPerSymbol, SamplesPerSymbol)
			candidates := FindCandidates(sg, 100, 1)
			t.Logf("Candidates: %d", len(candidates))

			limit := min(len(candidates), 5)

			for i := 0; i < limit; i++ {
				c := candidates[i]
				t.Logf("\n=== Candidate [%d] freq=%.1f Hz time=%.3f s score=%.1f ===",
					i, c.Freq, c.TimeOff, c.Score)

				// 1. Refine via Goertzel sync correlation.
				refined := RefineCandidateAudio(wav.Samples, hannWin, c)
				t.Logf("  Refined: freq=%.2f Hz time=%.4f s score=%.1f",
					refined.Freq, refined.TimeOff, refined.Score)

				// 2. Demodulate with production DemodulateAudio (LLR clamped).
				llr := DemodulateAudio(wav.Samples, hannWin, refined)
				dbgLogLLRStats(t, "DemodulateAudio", llr)

				msg77, ok := codec.DecodeMessage(llr, 100)
				if ok {
					dbgReportDecode(t, "DemodulateAudio", msg77)
					continue
				}
				t.Logf("  DemodulateAudio: decode FAILED")

				// 2a. LDPC decoder instrumentation — shows per-iteration
				// convergence metrics to diagnose why BP isn't converging.
				dbgLDPCInstrumentation(t, "DemodulateAudio", llr)

				// 3. Try unclamped Goertzel demod.
				llrRaw := dbgGoertzelDemod(wav.Samples, hannWin, refined)
				dbgLogLLRStats(t, "Goertzel-raw", llrRaw)
				msg77, ok = codec.DecodeMessage(llrRaw, 100)
				if ok {
					dbgReportDecode(t, "Goertzel-raw", msg77)
					continue
				}
				dbgLDPCInstrumentation(t, "Goertzel-raw", llrRaw)

				// 4. Try various LLR scale factors.
				for _, scale := range []float32{0.3, 0.5, 0.7, 1.5, 2.0} {
					var scaled [CodedBits]float32
					for j := range CodedBits {
						scaled[j] = llrRaw[j] * scale
					}
					msg77, ok = codec.DecodeMessage(scaled, 100)
					if ok {
						dbgReportDecode(t, fmt.Sprintf("scaled*%.1f", scale), msg77)
						break
					}
				}

				// 5. Try max-log LLR.
				llrMaxLog := dbgGoertzelDemodMaxLog(wav.Samples, hannWin, refined)
				dbgLogLLRStats(t, "max-log", llrMaxLog)
				msg77, ok = codec.DecodeMessage(llrMaxLog, 100)
				if ok {
					dbgReportDecode(t, "max-log", msg77)
					continue
				}
				dbgLDPCInstrumentation(t, "max-log", llrMaxLog)

				// 6. Try scaled max-log.
				for _, scale := range []float32{0.3, 0.5, 0.7, 1.5, 2.0} {
					var scaled [CodedBits]float32
					for j := range CodedBits {
						scaled[j] = llrMaxLog[j] * scale
					}
					msg77, ok = codec.DecodeMessage(scaled, 100)
					if ok {
						dbgReportDecode(t, fmt.Sprintf("max-log*%.1f", scale), msg77)
						break
					}
				}
			}
		})
	}
}

func dbgLogLLRStats(t *testing.T, label string, llr [CodedBits]float32) {
	t.Helper()
	var absSum, maxAbs float64
	var posCount, negCount, zeroCount int
	for _, v := range llr {
		a := math.Abs(float64(v))
		absSum += a
		if a > maxAbs {
			maxAbs = a
		}
		if v > 0 {
			posCount++
		} else if v < 0 {
			negCount++
		} else {
			zeroCount++
		}
	}
	t.Logf("  %s LLR: mean|LLR|=%.3f max|LLR|=%.3f pos=%d neg=%d zero=%d",
		label, absSum/float64(CodedBits), maxAbs, posCount, negCount, zeroCount)
}

func dbgReportDecode(t *testing.T, label string, msg77 [10]byte) {
	t.Helper()
	t.Logf("  *** %s DECODED: %x", label, msg77)
	if m, uerr := message.Unpack(msg77); uerr == nil {
		t.Logf("      -> %s", formatMessage(m))
	}
}

// dbgGoertzelDemod — unclamped Goertzel demod using log-sum-exp.
func dbgGoertzelDemod(samples []float32, hann []float32, cand Candidate) [CodedBits]float32 {
	var llr [CodedBits]float32
	startSample := int(math.Round(float64(cand.TimeOff) * SampleRate))
	baseFreq := float64(cand.Freq)
	idx := 0

	demodSym := func(pos int) {
		symStart := startSample + pos*SamplesPerSymbol
		if symStart < 0 || symStart+SamplesPerSymbol > len(samples) {
			idx += BitsPerSymbol
			return
		}
		frame := samples[symStart : symStart+SamplesPerSymbol]
		powers, _ := GoertzelTones(frame, hann, baseFreq)

		var s [NumTones]float64
		for k := range NumTones {
			if powers[k] > 0 {
				s[k] = math.Log(powers[k])
			} else {
				s[k] = logFloor
			}
		}
		for b := range BitsPerSymbol {
			g0, g1 := bit0Tones[b], bit1Tones[b]
			llr[idx] = float32(max4(s[g0[0]], s[g0[1]], s[g0[2]], s[g0[3]]) -
				max4(s[g1[0]], s[g1[1]], s[g1[2]], s[g1[3]]))
			idx++
		}
	}

	for pos := Sync1Start + SyncLen; pos < Sync2Start; pos++ {
		demodSym(pos)
	}
	for pos := Sync2Start + SyncLen; pos < Sync3Start; pos++ {
		demodSym(pos)
	}
	return llr
}

// dbgGoertzelDemodMaxLog — max-log approximation for LLR.
func dbgGoertzelDemodMaxLog(samples []float32, hann []float32, cand Candidate) [CodedBits]float32 {
	var llr [CodedBits]float32
	startSample := int(math.Round(float64(cand.TimeOff) * SampleRate))
	baseFreq := float64(cand.Freq)
	idx := 0

	demodSym := func(pos int) {
		symStart := startSample + pos*SamplesPerSymbol
		if symStart < 0 || symStart+SamplesPerSymbol > len(samples) {
			idx += BitsPerSymbol
			return
		}
		frame := samples[symStart : symStart+SamplesPerSymbol]
		powers, _ := GoertzelTones(frame, hann, baseFreq)

		sorted := make([]float64, NumTones)
		copy(sorted, powers[:])
		slices.Sort(sorted)
		noise := (sorted[3] + sorted[4]) / 2
		if noise <= 0 {
			noise = 1e-20
		}
		logNoise := math.Log(noise)

		var s [NumTones]float64
		for k := range NumTones {
			if powers[k] > 0 {
				s[k] = math.Log(powers[k]) - logNoise
			} else {
				s[k] = logFloor
			}
		}

		for b := range BitsPerSymbol {
			g0, g1 := bit0Tones[b], bit1Tones[b]
			max0 := max(s[g0[0]], s[g0[1]], s[g0[2]], s[g0[3]])
			max1 := max(s[g1[0]], s[g1[1]], s[g1[2]], s[g1[3]])
			llr[idx] = float32(max0 - max1)
			idx++
		}
	}

	for pos := Sync1Start + SyncLen; pos < Sync2Start; pos++ {
		demodSym(pos)
	}
	for pos := Sync2Start + SyncLen; pos < Sync3Start; pos++ {
		demodSym(pos)
	}
	return llr
}
