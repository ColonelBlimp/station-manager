package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

func main() {
	var (
		msgText  = flag.String("msg", "CQ K1JT FN20", "FT8 message to synthesise (parsed via codec.ParseMessage)")
		freq     = flag.Float64("freq", 1500, "carrier frequency in Hz")
		dt       = flag.Float64("dt", 0, "time offset in seconds")
		gain1    = flag.Float64("gain1", 1.0, "amplitude scale for the primary signal (1.0 = unit)")
		snr      = flag.Float64("snr", -15, "target SNR in dB (WSJT-X 2500 Hz convention); single-condition mode")
		sweep    = flag.String("sweep", "", "SNR sweep \"lo:hi:step\" in dB; overrides -snr and prints a per-stage table")
		trials   = flag.Int("trials", 1, "noise realisations per condition (averaged in sweep mode)")
		seed     = flag.Uint64("seed", 1, "base RNG seed for the noise (reproducible)")
		osdOrder = flag.Int("osd-order", 1, "OSD search order for the decode stage")
		interf   = flag.Float64("interferer", 0, "same-msg masker N Hz from f0 (legacy; ignored when -msg2 is set)")
		msg2Text = flag.String("msg2", "", "second FT8 message; non-empty enables two-signal subtraction-test mode")
		freq2    = flag.Float64("freq2", 0, "second-signal carrier frequency in Hz (only used when -msg2 is set)")
		gain2    = flag.Float64("gain2", 1.0, "amplitude scale for the second signal relative to primary")
		subtract = flag.Bool("subtract", false, "in two-signal mode, also run Decode with SubtractionPasses=1 and report")
		outWAV   = flag.String("out", "", "write the synthesised audio to this 12 kHz mono 16-bit WAV path (single condition only)")
	)
	flag.Parse()

	msgBits, codeword, formattedText := mustEncode(*msgText)

	cfg := condition{
		msgBits:    msgBits,
		codeword:   codeword,
		text:       formattedText,
		freq:       *freq,
		dt:         *dt,
		gain:       *gain1,
		osdOrder:   *osdOrder,
		interferer: *interf,
	}

	if *msg2Text != "" {
		msg2Bits, _, msg2Formatted := mustEncode(*msg2Text)
		cfg.msg2Bits = msg2Bits
		cfg.msg2Text = msg2Formatted
		cfg.freq2 = *freq2
		cfg.gain2 = *gain2
		cfg.testSubtract = *subtract
		if cfg.interferer != 0 {
			fmt.Fprintln(os.Stderr, "note: -interferer ignored because -msg2 is set")
			cfg.interferer = 0
		}
	}

	if *sweep == "" {
		if *outWAV != "" {
			if err := saveSynth(cfg, *snr, *seed, *outWAV); err != nil {
				fatal("write WAV: %v", err)
			}
			fmt.Printf("wrote %s  (msg=%q f=%.1f Hz, msg2=%q f2=%.1f Hz, snr=%.1f dB, seed=%d)\n",
				*outWAV, cfg.text, cfg.freq, cfg.msg2Text, cfg.freq2, *snr, *seed)
		}
		runSingle(cfg, *snr, *seed)
		return
	}
	if *outWAV != "" {
		fmt.Fprintln(os.Stderr, "note: -out ignored in sweep mode")
	}
	lo, hi, step, err := parseSweep(*sweep)
	if err != nil {
		fatal("%v", err)
	}
	if cfg.msg2Bits != nil {
		runSubtractionSweep(cfg, lo, hi, step, *trials, *seed)
		return
	}
	runSweep(cfg, lo, hi, step, *trials, *seed)
}

// saveSynth writes the synthesised audio for one (snr, seed) realisation
// to a 12 kHz mono 16-bit-PCM WAV. The audio is fully reproducible from
// (msg, freq, gain, msg2, freq2, gain2, snr, seed) so this WAV plus its
// parameters can be regenerated later if needed.
func saveSynth(c condition, snr float64, seed uint64, path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	samples := buildAudio(c, snr, seed)
	return audio.WriteWAV(path, &audio.Data{
		SampleRate: uint32(dsp.Fs),
		Channels:   1,
		Samples:    samples,
	})
}

// mustEncode parses, encodes, and round-trip-formats an FT8 message.
// Exits on failure — bad input here is a CLI-usage error, not a
// runtime condition worth recovering from.
func mustEncode(text string) (msgBits, codeword []byte, formatted string) {
	m, err := codec.ParseMessage(text)
	if err != nil {
		fatal("parse %q: %v", text, err)
	}
	msgBits, err = codec.EncodeMessage(m)
	if err != nil {
		fatal("encode %q: %v", text, err)
	}
	formatted, err = codec.FormatMessage(m)
	if err != nil {
		fatal("format %q: %v", text, err)
	}
	return msgBits, knownCodeword(msgBits), formatted
}

// condition is everything fixed across noise realisations / SNRs.
type condition struct {
	msgBits    []byte
	codeword   []byte // known 174-bit codeword (ground truth for demod)
	text       string
	freq       float64
	dt         float64
	gain       float64 // amplitude scale for the primary signal
	osdOrder   int
	interferer float64 // Hz offset of an added strong same-msg masker; 0 = none (ignored when msg2 set)

	// Two-signal subtraction mode (msg2 != nil). msg2 carries a
	// genuinely different message so that subtraction's correctness
	// can be scored from which messages survive.
	msg2Bits     []byte
	msg2Text     string
	freq2        float64
	gain2        float64
	testSubtract bool // run a second Decode pass with SubtractionPasses=1
}

// stageResult is the per-stage outcome of one decode attempt.
type stageResult struct {
	syncFound    bool
	syncPower    float64
	syncRank     int
	syncTotal    int
	demodErrTrue int // bit errors at the TRUE f0/dt (ideal alignment)
	demodErrCand int // bit errors at the sync candidate's quantised f0
	bpOK         bool
	osdOK        bool
	decoded      bool // recovered message == input
	decodedText  string
}

func runSingle(c condition, snr float64, seed uint64) {
	audio := buildAudio(c, snr, seed)
	// Working buffer for Decode() — ft8.Decode is allowed to mutate
	// via subtraction passes, and evalStages also depends on a stable
	// view of audio. Use audio for stage analysis; pass a copy to
	// Decode runs.
	r := evalStages(c, audio)

	fmt.Printf("msg: %q  f0=%.1f Hz  dt=%.2f s  gain=%.2f  SNR=%.0f dB  seed=%d", c.text, c.freq, c.dt, c.gain, snr, seed)
	if c.msg2Bits != nil {
		fmt.Printf("\nmsg2:%q  f0=%.1f Hz  gain=%.2f  (Δf=%+.1f Hz)", c.msg2Text, c.freq2, c.gain2, c.freq2-c.freq)
	} else if c.interferer != 0 {
		fmt.Printf("  interferer=%+.0f Hz", c.interferer)
	}
	fmt.Println()

	if r.syncFound {
		fmt.Printf("  sync:        candidate near %.1f Hz FOUND (power=%.2f, rank %d/%d)\n",
			c.freq, r.syncPower, r.syncRank, r.syncTotal)
	} else {
		fmt.Printf("  sync:        NO candidate near %.1f Hz (%d candidates total)\n", c.freq, r.syncTotal)
	}
	fmt.Printf("  demod@true:  %d/%d bit errors  (LLR hard-decisions vs known codeword)\n", r.demodErrTrue, codec.CodewordBits)
	fmt.Printf("  demod@cand:  %d/%d bit errors  (at the sync candidate's quantised freq)\n", r.demodErrCand, codec.CodewordBits)
	fmt.Printf("  BP:          decoded=%s\n", yn(r.bpOK))
	fmt.Printf("  OSD-%d:       decoded=%s\n", c.osdOrder, yn(r.osdOK))
	fmt.Printf("  result:      %s", yn(r.decoded))
	if r.decoded {
		fmt.Printf(" → %q", r.decodedText)
	} else if r.decodedText != "" {
		fmt.Printf(" → got %q (WRONG)", r.decodedText)
	}
	fmt.Println()

	if c.msg2Bits != nil {
		printSubtractionRun(c, audio)
	}
}

// printSubtractionRun runs the full ft8.Decode pipeline on the
// two-signal audio with subtraction OFF, and (when c.testSubtract is
// set) with SubtractionPasses=1 — and reports which of the two known
// messages were found in each pass, plus the count of any extra
// (unexpected) decodes. Each Decode call gets its own audio copy
// because Decode may mutate its working buffer.
func printSubtractionRun(c condition, audio []float32) {
	fmt.Println()
	fmt.Println("subtraction test (ft8.Decode):")
	fmt.Printf("  targets: A=%q @ %.1f Hz  B=%q @ %.1f Hz\n", c.text, c.freq, c.msg2Text, c.freq2)

	base := decodeAndScore(c, audio, 0)
	fmt.Printf("  passes=0: A=%s  B=%s  extras=%d  total=%d\n",
		yn(base.foundA), yn(base.foundB), base.extras, base.total)
	for _, x := range base.extraTexts {
		fmt.Printf("            extra: %q\n", x)
	}

	if c.testSubtract {
		sub := decodeAndScore(c, audio, 1)
		fmt.Printf("  passes=1: A=%s  B=%s  extras=%d  total=%d", yn(sub.foundA), yn(sub.foundB), sub.extras, sub.total)
		// Δ helps the reader judge whether subtraction actually helped.
		dB := boolToInt(sub.foundB) - boolToInt(base.foundB)
		dA := boolToInt(sub.foundA) - boolToInt(base.foundA)
		dE := sub.extras - base.extras
		fmt.Printf("   Δ(A,B,extras)=(%+d,%+d,%+d)\n", dA, dB, dE)
		for _, x := range sub.extraTexts {
			fmt.Printf("            extra: %q\n", x)
		}
	}
}

// decodeScore is the per-Decode-run outcome in two-signal mode.
type decodeScore struct {
	foundA     bool
	foundB     bool
	total      int
	extras     int
	extraTexts []string
}

// decodeAndScore runs ft8.Decode on a copy of audio and tallies which
// of the two known target messages were recovered, plus any extra
// (unexpected) decodes — those are the "false positives" the
// subtraction loop is meant to avoid producing.
func decodeAndScore(c condition, audio []float32, subPasses int) decodeScore {
	working := make([]float32, len(audio))
	copy(working, audio)
	out := ft8.Decode(working, ft8.DecodeOptions{SubtractionPasses: subPasses})

	score := decodeScore{total: len(out)}
	for _, d := range out {
		switch d.Text {
		case c.text:
			score.foundA = true
		case c.msg2Text:
			score.foundB = true
		default:
			score.extras++
			score.extraTexts = append(score.extraTexts, d.Text)
		}
	}
	return score
}

// runSubtractionSweep is the two-signal counterpart to runSweep. At
// each SNR it averages decodeAndScore over `trials` noise realisations
// and prints A%/B%/extras for passes=0 and (if requested) passes=1.
func runSubtractionSweep(c condition, lo, hi, step float64, trials int, seed uint64) {
	fmt.Printf("msg A: %q @ %.1f Hz  (gain %.2f)\n", c.text, c.freq, c.gain)
	fmt.Printf("msg B: %q @ %.1f Hz  (gain %.2f, Δf=%+.1f Hz)\n", c.msg2Text, c.freq2, c.gain2, c.freq2-c.freq)
	fmt.Printf("trials=%d  subtract=%v\n", trials, c.testSubtract)
	header := "  SNR   A0%%   B0%%  extra0"
	if c.testSubtract {
		header += "   A1%%   B1%%  extra1"
	}
	fmt.Println(header)
	fmt.Println(strings.Repeat("-", len(header)+2))

	for snr := lo; snr <= hi+1e-9; snr += step {
		var a0, b0, a1, b1 int
		var sumExtra0, sumExtra1 float64
		for t := 0; t < trials; t++ {
			audio := buildAudio(c, snr, seed+uint64(t)*1009)
			s0 := decodeAndScore(c, audio, 0)
			if s0.foundA {
				a0++
			}
			if s0.foundB {
				b0++
			}
			sumExtra0 += float64(s0.extras)
			if c.testSubtract {
				s1 := decodeAndScore(c, audio, 1)
				if s1.foundA {
					a1++
				}
				if s1.foundB {
					b1++
				}
				sumExtra1 += float64(s1.extras)
			}
		}
		tf := float64(trials)
		line := fmt.Sprintf("%5.0f  %4.0f%%  %4.0f%%  %5.2f",
			snr, pct(a0, trials), pct(b0, trials), sumExtra0/tf)
		if c.testSubtract {
			line += fmt.Sprintf("   %4.0f%%  %4.0f%%  %5.2f",
				pct(a1, trials), pct(b1, trials), sumExtra1/tf)
		}
		fmt.Println(line)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func runSweep(c condition, lo, hi, step float64, trials int, seed uint64) {
	fmt.Printf("msg: %q  f0=%.1f Hz  dt=%.2f s  trials=%d", c.text, c.freq, c.dt, trials)
	if c.interferer != 0 {
		fmt.Printf("  interferer=%+.0f Hz", c.interferer)
	}
	fmt.Println()
	fmt.Printf("%5s %7s %12s %12s %7s %7s %9s\n",
		"SNR", "sync%", "demodErr@tru", "demodErr@cnd", "BP%", "OSD%", "decoded%")
	fmt.Println(strings.Repeat("-", 66))

	for snr := lo; snr <= hi+1e-9; snr += step {
		var syncN, bpN, osdN, decN int
		var sumErrTrue, sumErrCand float64
		for t := 0; t < trials; t++ {
			audio := buildAudio(c, snr, seed+uint64(t)*1009)
			r := evalStages(c, audio)
			if r.syncFound {
				syncN++
			}
			sumErrTrue += float64(r.demodErrTrue)
			sumErrCand += float64(r.demodErrCand)
			if r.bpOK {
				bpN++
			}
			if r.osdOK {
				osdN++
			}
			if r.decoded {
				decN++
			}
		}
		tf := float64(trials)
		fmt.Printf("%5.0f %6.0f%% %12.1f %12.1f %6.0f%% %6.0f%% %8.0f%%\n",
			snr, pct(syncN, trials), sumErrTrue/tf, sumErrCand/tf,
			pct(bpN, trials), pct(osdN, trials), pct(decN, trials))
	}
}

// buildAudio synthesises the known message at (freq, dt), optionally
// adds a second signal (same-msg masker via -interferer OR distinct
// msg2 via two-signal mode), and adds calibrated AWGN for the target
// SNR. SNR is referenced to the PRIMARY signal at gain=1.0; gain1 != 1
// shifts the effective SNR of the primary by 20·log10(gain1).
func buildAudio(c condition, snr float64, seed uint64) []float32 {
	gain1 := c.gain
	if gain1 == 0 {
		gain1 = 1.0
	}
	sig := dsp.Synthesize(c.msgBits, c.freq, c.dt)
	if sig == nil {
		fatal("Synthesize returned nil (bad msgBits length?)")
	}
	if gain1 != 1.0 {
		for i := range sig {
			sig[i] *= float32(gain1)
		}
	}
	switch {
	case c.msg2Bits != nil:
		// Two-signal mode: a distinct message at (freq2, dt) with its
		// own amplitude. Both signals share the slot timing.
		other := dsp.Synthesize(c.msg2Bits, c.freq2, c.dt)
		if other == nil {
			fatal("Synthesize msg2 returned nil")
		}
		for i := range sig {
			sig[i] += float32(c.gain2) * other[i]
		}
	case c.interferer != 0:
		// Legacy mode: a same-msg masker ~6 dB stronger; useful for
		// adjacent-signal-interference demod stress tests but NOT
		// suitable for scoring subtraction (identical msg bits).
		other := dsp.Synthesize(c.msgBits, c.freq+c.interferer, c.dt)
		const interfererGain = 2.0 // ~+6 dB
		for i := range sig {
			sig[i] += float32(interfererGain) * other[i]
		}
	}
	addNoise(sig, snr, seed)
	return sig
}

// addNoise adds zero-mean Gaussian noise for the target SNR in dB,
// using the WSJT-X 2500 Hz reference bandwidth convention.
//
// The GFSK signal is unit-amplitude → mean power Ps = 0.5. Real samples
// at Fs spread noise variance σ² over the 0..Fs/2 Nyquist band, so the
// noise power in 2500 Hz is σ²·2500/(Fs/2). Setting Ps/Pn = 10^(SNR/10)
// gives σ² = Ps·(Fs/2)/(2500·10^(SNR/10)).
func addNoise(sig []float32, snrDB float64, seed uint64) {
	const signalPower = 0.5
	refBW := 2500.0
	nyquist := dsp.Fs / 2.0
	noiseVar := signalPower * nyquist / (refBW * math.Pow(10, snrDB/10))
	sigma := math.Sqrt(noiseVar)

	r := rand.New(rand.NewPCG(seed, seed*2+1))
	for i := range sig {
		sig[i] += float32(sigma * r.NormFloat64())
	}
}

// evalStages runs the pipeline and scores each stage against ground truth.
func evalStages(c condition, audio []float32) stageResult {
	var r stageResult

	// --- Sync ---
	spec := dsp.Spectrogram(audio)
	cands := dsp.Sync(spec, dsp.SyncOptions{})
	r.syncTotal = len(cands)
	const freqTol = 4.0 // Hz; sync quantises to 3.125 Hz bins
	bestCandFreq := c.freq
	for i, cand := range cands {
		if math.Abs(cand.Freq-c.freq) <= freqTol {
			r.syncFound = true
			r.syncPower = cand.SyncPower
			r.syncRank = i + 1
			bestCandFreq = cand.Freq
			break
		}
	}

	// --- Demod at the TRUE freq/dt (isolates demod quality from sync) ---
	r.demodErrTrue = demodBitErrors(audio, c.freq, c.dt, c.codeword)
	// --- Demod at the sync candidate's quantised freq (realistic path) ---
	r.demodErrCand = demodBitErrors(audio, bestCandFreq, c.dt, c.codeword)

	// --- Decode (BP, then OSD) from the realistic-path LLRs ---
	baseband := dsp.Downsample(audio, bestCandFreq)
	llrs := dsp.Demodulate(baseband, c.dt, dsp.DefaultLLRScale)
	if llrs != nil {
		if _, ok := codec.LDPCDecode(llrs, 0); ok {
			r.bpOK = true
		}
		if msg, ok := codec.LDPCDecodeWithOSD(llrs, 0, c.osdOrder, 0); ok {
			r.osdOK = true
			if text, err := codec.FormatMessage(mustDecode(msg)); err == nil {
				r.decodedText = text
				r.decoded = equalBits(msg, c.msgBits)
			}
		}
	}
	return r
}

// demodBitErrors downsamples at f0, demodulates, and counts how many of
// the 174 LLR hard-decisions disagree with the known codeword. Positive
// LLR ⟹ bit 0 (LDPC convention).
func demodBitErrors(audio []float32, f0, dt float64, codeword []byte) int {
	baseband := dsp.Downsample(audio, f0)
	llrs := dsp.Demodulate(baseband, dt, dsp.DefaultLLRScale)
	if llrs == nil || len(llrs) != len(codeword) {
		return len(codeword) // total failure
	}
	errs := 0
	for i, l := range llrs {
		hard := byte(0)
		if l < 0 {
			hard = 1
		}
		if hard != codeword[i] {
			errs++
		}
	}
	return errs
}

// knownCodeword reproduces Synthesize's encode chain (77-bit message +
// 14-bit CRC → LDPCEncode) to get the transmitted 174-bit codeword.
func knownCodeword(msgBits []byte) []byte {
	info := make([]byte, codec.InfoBits)
	copy(info[:codec.MessageBits], msgBits)
	crc := codec.CRC14(msgBits)
	for i := 0; i < codec.CRCBits; i++ {
		info[codec.MessageBits+i] = byte((crc >> (codec.CRCBits - 1 - i)) & 1)
	}
	return codec.LDPCEncode(info)
}

func mustDecode(msgBits []byte) codec.Message {
	m, _ := codec.DecodeMessage(msgBits)
	return m
}

func equalBits(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parseSweep(s string) (lo, hi, step float64, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("sweep must be \"lo:hi:step\", got %q", s)
	}
	if lo, err = strconv.ParseFloat(parts[0], 64); err != nil {
		return
	}
	if hi, err = strconv.ParseFloat(parts[1], 64); err != nil {
		return
	}
	if step, err = strconv.ParseFloat(parts[2], 64); err != nil {
		return
	}
	if step <= 0 || hi < lo {
		return 0, 0, 0, fmt.Errorf("sweep needs step>0 and hi>=lo")
	}
	return
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

func yn(b bool) string {
	if b {
		return "YES"
	}
	return "no"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
