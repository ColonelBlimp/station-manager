package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

func main() {
	var (
		msgText  = flag.String("msg", "CQ K1JT FN20", "FT8 message to synthesise (parsed via codec.ParseMessage)")
		freq     = flag.Float64("freq", 1500, "carrier frequency in Hz")
		dt       = flag.Float64("dt", 0, "time offset in seconds")
		snr      = flag.Float64("snr", -15, "target SNR in dB (WSJT-X 2500 Hz convention); single-condition mode")
		sweep    = flag.String("sweep", "", "SNR sweep \"lo:hi:step\" in dB; overrides -snr and prints a per-stage table")
		trials   = flag.Int("trials", 1, "noise realisations per condition (averaged in sweep mode)")
		seed     = flag.Uint64("seed", 1, "base RNG seed for the noise (reproducible)")
		osdOrder = flag.Int("osd-order", 1, "OSD search order for the decode stage")
		interf   = flag.Float64("interferer", 0, "if non-zero, add a strong second signal this many Hz from f0")
	)
	flag.Parse()

	m, err := codec.ParseMessage(*msgText)
	if err != nil {
		fatal("parse %q: %v", *msgText, err)
	}
	msgBits, err := codec.EncodeMessage(m)
	if err != nil {
		fatal("encode %q: %v", *msgText, err)
	}
	codeword := knownCodeword(msgBits)

	cfg := condition{
		msgBits:    msgBits,
		codeword:   codeword,
		text:       *msgText,
		freq:       *freq,
		dt:         *dt,
		osdOrder:   *osdOrder,
		interferer: *interf,
	}

	if *sweep == "" {
		runSingle(cfg, *snr, *seed)
		return
	}
	lo, hi, step, err := parseSweep(*sweep)
	if err != nil {
		fatal("%v", err)
	}
	runSweep(cfg, lo, hi, step, *trials, *seed)
}

// condition is everything fixed across noise realisations / SNRs.
type condition struct {
	msgBits    []byte
	codeword   []byte // known 174-bit codeword (ground truth for demod)
	text       string
	freq       float64
	dt         float64
	osdOrder   int
	interferer float64 // Hz offset of an added strong second signal; 0 = none
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
	r := evalStages(c, audio)

	fmt.Printf("msg: %q  f0=%.1f Hz  dt=%.2f s  SNR=%.0f dB  seed=%d", c.text, c.freq, c.dt, snr, seed)
	if c.interferer != 0 {
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

// buildAudio synthesises the known message at (freq, dt), optionally adds
// a strong interferer, and adds calibrated AWGN for the target SNR.
func buildAudio(c condition, snr float64, seed uint64) []float32 {
	sig := dsp.Synthesize(c.msgBits, c.freq, c.dt)
	if sig == nil {
		fatal("Synthesize returned nil (bad msgBits length?)")
	}
	if c.interferer != 0 {
		// A second, ~6 dB stronger signal carrying a different message,
		// to test demod robustness against an adjacent strong neighbour.
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
