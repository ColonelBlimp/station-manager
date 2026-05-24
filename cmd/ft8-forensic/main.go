// ft8-forensic: per-signal bit-level forensic comparison between our
// demod's LLR output and the expected 174-bit codeword for a known
// message.
//
// Background. Bulk statistics (LLR-magnitude distributions, BP
// syndrome-weight distributions) have hit their limits — distributions
// overlap heavily between decoded and failed candidates, and no clean
// gating threshold falls out. We're at ~45% match-to-oracle vs jt9 -8
// on the live corpus and miss strong-signal decodes (−1, −4, +2 dB
// SNR) that aren't sensitivity-bound, so there's likely a structural
// bug somewhere — Gray mapping, bit order, LDPC matrix, CRC bit-feed
// order, sample alignment, or symbol indexing.
//
// This tool isolates the question to ONE known signal at a time. For
// a missed jt9-decoded message, we:
//
//  1. Build the expected 174-bit codeword INDEPENDENTLY:
//     text → ParseMessage → 77-bit body → CRC14 → 91-bit info →
//     LDPCEncode → 174-bit codeword.
//     Our encoder and decoder share the codec package, so any error
//     in the bit-level conventions (Gray, bit-order, MSB/LSB)
//     would silently round-trip through encoder-then-decoder on
//     synthetic signals — which is why the existing tests pass while
//     real signals fail.
//
//  2. Extract the actual LLRs at the known candidate freq/dt from the
//     audio: ForwardSpectrum → DownsampleFromSpectrum → Demodulate.
//
//  3. Compare per-bit: codeword[i]=0 ↔ LLR[i] ≥ 0; codeword[i]=1 ↔
//     LLR[i] < 0 (per our "positive LLR = bit-0" convention).
//
//  4. Report the disagreement pattern. The PATTERN is the diagnostic:
//
//     - Random scatter, weak magnitudes at disagreements:
//     honest noise. No structural bug.
//     - Disagreements concentrated in CRC region (bits 77-90):
//     CRC14 bit-feed order or polynomial mismatch.
//     - Disagreements concentrated in parity region (bits 91-173):
//     LDPC generator matrix row ordering off.
//     - Disagreements at a specific j-position within each symbol
//     (e.g. every bit-1 or every bit-2): Gray mapping bug.
//     - Disagreements concentrated at low channel-symbol indices:
//     symbol-indexing off-by-one (we're reading a Costas symbol
//     where we should be reading a payload symbol).
//     - Confident-but-wrong magnitudes everywhere: top-level sign
//     convention flipped.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

func main() {
	var (
		wavPath = flag.String("wav", "", "input 12 kHz mono 16-bit WAV file")
		msgText = flag.String("msg", "", "expected message text (e.g. \"OH6IH IU7KEG JN81\")")
		freq    = flag.Float64("freq", 0, "candidate centre frequency in Hz")
		dt      = flag.Float64("dt", 0, "candidate time offset in seconds from nominal TX start")
		verbose = flag.Bool("v", false, "dump every bit position (174 lines) — not just summary")

		// Optional: subtract a known neighbour signal from the audio
		// before extracting LLRs. The neighbour is encoded
		// independently (same encoder path as the target), then
		// dsp.SubtractSignal estimates its amplitude+phase via matched
		// filter and removes it from a copy of the audio. The forensic
		// then proceeds on the residual.
		subMsg  = flag.String("subtract-msg", "", "neighbour message text to subtract first")
		subFreq = flag.Float64("subtract-freq", 0, "neighbour centre frequency in Hz")
		subDT   = flag.Float64("subtract-dt", 0, "neighbour time offset in seconds")
	)
	flag.Parse()

	if *wavPath == "" || *msgText == "" || *freq == 0 {
		fmt.Fprintln(os.Stderr, "usage: ft8-forensic -wav PATH -msg \"TEXT\" -freq HZ [-dt SEC] [-v]")
		fmt.Fprintln(os.Stderr, "       [-subtract-msg \"NEIGHBOUR\" -subtract-freq HZ -subtract-dt SEC]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	// Step 1 — build expected codeword from the message text.
	expected, err := buildExpectedCodeword(*msgText)
	if err != nil {
		fatalf("expected-codeword: %v", err)
	}

	// Step 2 — load audio. Optionally subtract a neighbour first.
	samples, err := loadAudio(*wavPath)
	if err != nil {
		fatalf("load audio: %v", err)
	}
	if *subMsg != "" {
		amp, err := subtractNeighbour(samples, *subMsg, *subFreq, *subDT)
		if err != nil {
			fatalf("subtract neighbour: %v", err)
		}
		fmt.Printf("Subtracted neighbour: %q at freq=%g dt=%g  (estimated amplitude=%.4f)\n\n",
			*subMsg, *subFreq, *subDT, amp)
	}

	// Step 3 — extract LLRs at the target candidate from the (possibly residual) audio.
	llrs, err := extractLLRsFromSamples(samples, *freq, *dt)
	if err != nil {
		fatalf("extract LLRs: %v", err)
	}

	// Step 4 — compare and report.
	report(expected, llrs, *verbose)
}

// loadAudio reads a 12 kHz mono 16-bit WAV file.
func loadAudio(wavPath string) ([]float32, error) {
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		return nil, err
	}
	if float64(data.SampleRate) != dsp.Fs {
		return nil, fmt.Errorf("sample rate = %d, want %g", data.SampleRate, dsp.Fs)
	}
	if data.Channels != 1 {
		return nil, fmt.Errorf("channels = %d, want 1 (mono)", data.Channels)
	}
	// Return a copy — subtract mutates in place, and we don't want
	// the next iteration of the experiment to be operating on
	// already-subtracted samples.
	out := make([]float32, len(data.Samples))
	copy(out, data.Samples)
	return out, nil
}

// subtractNeighbour synthesises the neighbour message's audio
// template (sin + cos) via the verified codec → LDPCEncode →
// channel-symbol → GFSK pipeline, then uses dsp.SubtractSignal's
// matched-filter projection to estimate the neighbour's actual
// amplitude+phase in the audio and remove it in place. Returns the
// estimated amplitude.
func subtractNeighbour(samples []float32, text string, freq, dt float64) (float64, error) {
	body, err := buildExpectedBody(text)
	if err != nil {
		return 0, fmt.Errorf("build neighbour body: %w", err)
	}
	amp := dsp.SubtractSignal(samples, body, freq, dt)
	if amp == 0 {
		return 0, fmt.Errorf("dsp.SubtractSignal returned 0 (bad input or degenerate template)")
	}
	return amp, nil
}

// buildExpectedBody turns a message text into the 77-bit message body
// (the input to CRC + LDPCEncode, and also the input dsp.SubtractSignal
// / dsp.Synthesize expect).
func buildExpectedBody(text string) ([]byte, error) {
	msg, err := codec.ParseMessage(text)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	body, err := codec.EncodeMessage(msg)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	if len(body) != codec.MessageBits {
		return nil, fmt.Errorf("encoded body is %d bits, want %d", len(body), codec.MessageBits)
	}
	return body, nil
}

// buildExpectedCodeword turns a message text into the exact 174-bit
// codeword that the FT8 transmitter would emit (77-bit body + 14-bit
// CRC + 83 LDPC parity bits).
func buildExpectedCodeword(text string) ([]byte, error) {
	body, err := buildExpectedBody(text)
	if err != nil {
		return nil, err
	}

	// Append CRC14 (14 bits MSB-first into positions 77-90).
	crc := codec.CRC14(body)
	info := make([]byte, codec.InfoBits)
	copy(info[:codec.MessageBits], body)
	for i := 0; i < 14; i++ {
		info[codec.MessageBits+i] = byte((crc >> (13 - i)) & 1)
	}

	// LDPCEncode appends 83 parity bits to make 174.
	codeword := codec.LDPCEncode(info)
	if len(codeword) != codec.CodewordBits {
		return nil, fmt.Errorf("codeword is %d bits, want %d", len(codeword), codec.CodewordBits)
	}
	return codeword, nil
}

// extractLLRsFromSamples runs the given audio samples through the
// same Spectrogram → ForwardSpectrum → Downsample → Demodulate
// pipeline that internal/ft8.Decode uses, at the supplied (freq, dt).
// Bypasses Sync — caller picks the candidate position directly.
func extractLLRsFromSamples(samples []float32, freq, dt float64) ([]float64, error) {
	// Forward FFT over the whole slot at the FT8 baseband sample rate
	// — matches how Decode prepares the spectrum once per slot.
	forwardPlan := audio.NewPlan(dsp.NFFT1DS)
	spectrum := dsp.ForwardSpectrumWithPlan(samples, forwardPlan)

	inversePlan := audio.NewPlan(dsp.NFFT2)
	baseband := dsp.DownsampleFromSpectrumWithPlan(spectrum, freq, inversePlan)
	if baseband == nil {
		return nil, fmt.Errorf("downsample returned nil at freq %g (out of band?)", freq)
	}

	symPlan := audio.NewPlan(dsp.SymbolFFTSize)
	llrs := dsp.DemodulateWithPlan(baseband, dt, symPlan, dsp.DefaultLLRScale)
	if llrs == nil {
		return nil, fmt.Errorf("demod returned nil at dt %g (too short or out of bounds)", dt)
	}
	if len(llrs) != codec.CodewordBits {
		return nil, fmt.Errorf("LLR count = %d, want %d", len(llrs), codec.CodewordBits)
	}
	return llrs, nil
}

// report prints the structured comparison.
func report(expected []byte, llrs []float64, verbose bool) {
	type tally struct {
		agree, disagree   int
		agreeMagSum       float64
		disagreeMagSum    float64
		disagreeMaxMag    float64
		disagreePositions []int
	}

	// Sign convention: positive LLR ⟹ bit-0; negative ⟹ bit-1.
	bitFromLLR := func(l float64) byte {
		if l < 0 {
			return 1
		}
		return 0
	}

	all := tally{}
	body := tally{}         // bits 0..76
	crc := tally{}          // bits 77..90
	parity := tally{}       // bits 91..173
	bySymbol := [58]tally{} // per channel symbol (29 + 29)
	byJ := [3]tally{}       // per bit-position within symbol (0=MSB, 1, 2=LSB)

	for i := range expected {
		want := expected[i]
		got := bitFromLLR(llrs[i])
		mag := math.Abs(llrs[i])
		symIdx := i / 3
		j := i % 3

		addOne := func(t *tally, agree bool, mag float64) {
			if agree {
				t.agree++
				t.agreeMagSum += mag
			} else {
				t.disagree++
				t.disagreeMagSum += mag
				if mag > t.disagreeMaxMag {
					t.disagreeMaxMag = mag
				}
				t.disagreePositions = append(t.disagreePositions, i)
			}
		}

		agree := want == got
		addOne(&all, agree, mag)
		switch {
		case i < codec.MessageBits:
			addOne(&body, agree, mag)
		case i < codec.InfoBits:
			addOne(&crc, agree, mag)
		default:
			addOne(&parity, agree, mag)
		}
		addOne(&bySymbol[symIdx], agree, mag)
		addOne(&byJ[j], agree, mag)
	}

	pct := func(num, denom int) float64 {
		if denom == 0 {
			return 0
		}
		return 100.0 * float64(num) / float64(denom)
	}
	avgMag := func(sum float64, n int) float64 {
		if n == 0 {
			return 0
		}
		return sum / float64(n)
	}

	fmt.Println("=== Per-region agreement ===")
	fmt.Printf("%-22s %5s %5s %7s   %-12s %-12s\n", "region", "ok", "bad", "agree%", "avg|LLR|/ok", "avg|LLR|/bad")
	fmt.Println("---------------------------------------------------------------------------")
	printRow := func(label string, t tally) {
		fmt.Printf("%-22s %5d %5d %6.1f%%   %-12.3f %-12.3f\n",
			label, t.agree, t.disagree, pct(t.agree, t.agree+t.disagree),
			avgMag(t.agreeMagSum, t.agree), avgMag(t.disagreeMagSum, t.disagree))
	}
	printRow("body (0..76)", body)
	printRow("CRC14 (77..90)", crc)
	printRow("parity (91..173)", parity)
	fmt.Println("---------------------------------------------------------------------------")
	printRow("ALL (0..173)", all)

	fmt.Println()
	fmt.Println("=== Per bit-position within each 3-bit symbol ===")
	fmt.Printf("%-22s %5s %5s %7s   %-12s %-12s\n", "position", "ok", "bad", "agree%", "avg|LLR|/ok", "avg|LLR|/bad")
	fmt.Println("---------------------------------------------------------------------------")
	printRow("j=0 (MSB, mask=4)", byJ[0])
	printRow("j=1 (mid, mask=2)", byJ[1])
	printRow("j=2 (LSB, mask=1)", byJ[2])

	fmt.Println()
	fmt.Println("=== Disagreement positions (channel-symbol index, bit-position-within-symbol) ===")
	disagreements := all.disagreePositions
	if len(disagreements) == 0 {
		fmt.Println("(none — every bit's LLR sign matches the expected codeword)")
	} else {
		fmt.Printf("%d / %d disagreements:\n", len(disagreements), codec.CodewordBits)
		fmt.Println("LLRidx  chanSym  j  expected  got  LLR")
		for _, idx := range disagreements {
			cs := llrIdxToChannelSym(idx)
			j := idx % 3
			fmt.Printf("%6d  %7d  %d  %8d  %3d  %+.3f\n", idx, cs, j, expected[idx], bitFromLLR(llrs[idx]), llrs[idx])
		}
	}

	if verbose {
		fmt.Println()
		fmt.Println("=== Full per-bit dump ===")
		fmt.Println("LLRidx  chanSym  j  region   expected  got  agree?  LLR")
		for i := range expected {
			cs := llrIdxToChannelSym(i)
			j := i % 3
			var region string
			switch {
			case i < codec.MessageBits:
				region = "body"
			case i < codec.InfoBits:
				region = "crc"
			default:
				region = "parity"
			}
			agree := expected[i] == bitFromLLR(llrs[i])
			mark := "ok"
			if !agree {
				mark = "**MISMATCH**"
			}
			fmt.Printf("%6d  %7d  %d  %-6s   %8d  %3d  %-12s  %+.3f\n",
				i, cs, j, region, expected[i], bitFromLLR(llrs[i]), mark, llrs[i])
		}
	}
}

// llrIdxToChannelSym maps a position in the 174-LLR vector (in demod
// output order) to its FT8 channel-symbol index (0..78). The demod
// emits 87 LLRs from symbols 7..35 (first data block), then 87 LLRs
// from symbols 43..71 (second data block) — Costas symbols at 0..6,
// 36..42, 72..78 are skipped.
func llrIdxToChannelSym(llrIdx int) int {
	symInDataOrder := llrIdx / 3
	if symInDataOrder < 29 {
		return dsp.SymbolDataStart1 + symInDataOrder
	}
	return dsp.SymbolDataStart2 + (symInDataOrder - 29)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ft8-forensic: "+format+"\n", args...)
	os.Exit(1)
}
