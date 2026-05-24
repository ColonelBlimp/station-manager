// ft8-gen10cq synthesises a 15-second FT8 slot containing 10 CQ
// signals at equally-spaced frequencies, optionally adds AWGN at a
// configured per-signal SNR, and writes the result to a 12 kHz mono
// 16-bit WAV file. The output is a deterministic fixture for FT8
// decoder + subtraction experiments and verifier calibration.
//
// Usage:
//
//	go run ./cmd/ft8-gen10cq                           # research/10cq_clean.wav
//	go run ./cmd/ft8-gen10cq -snr -16                  # research/10cq_snr-16dB.wav
//	go run ./cmd/ft8-gen10cq -out PATH.wav             # writes to PATH.wav
//	go run ./cmd/ft8-gen10cq -snr -20 -seed 42 -out X  # explicit everything
//
// Signals are placed at 500, 600, 700, ..., 1400 Hz (100 Hz spacing
// — comfortably non-overlapping; FT8 occupies 50 Hz per signal). All
// transmit at slot-start (dt = 0). All carry distinct callsigns
// calling CQ with a 4-character grid square.
//
// When -snr is finite, AWGN is added at the requested per-signal
// SNR (WSJT-X convention: SNR in dB relative to a 2,500 Hz noise
// bandwidth). The RNG seed defaults to 1 for byte-identical
// regeneration; override with -seed.
//
// After AWGN (if any), the mixed buffer is rescaled to peak
// amplitude 0.7 — signal and noise scale together so the configured
// SNR is preserved. This keeps the WAV away from int16 clipping
// while leaving the SNR baked in.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"path/filepath"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

func main() {
	out := flag.String("out", "", "output WAV path (default: research/10cq_clean.wav or research/10cq_snr<N>dB.wav)")
	snrDB := flag.Float64("snr", math.Inf(1), "per-signal SNR in dB vs 2,500 Hz noise BW; Inf (default) = clean")
	seed := flag.Uint64("seed", 1, "RNG seed for AWGN — produces byte-identical output for a given (snr, seed)")
	flag.Parse()

	cleanFixture := math.IsInf(*snrDB, 1)
	outPath := *out
	if outPath == "" {
		if cleanFixture {
			outPath = "research/10cq_clean.wav"
		} else {
			outPath = fmt.Sprintf("research/10cq_snr%+gdB.wav", *snrDB)
		}
	}

	callsigns := []struct{ call, grid string }{
		{"K1JT", "FN20"},
		{"W1AW", "FN31"},
		{"VE3KI", "FN03"},
		{"G3XTT", "IO91"},
		{"DL9HCG", "JO64"},
		{"JA1NUT", "PM95"},
		{"VK3OT", "QF22"},
		{"ZL2IFB", "RE78"},
		{"7Q5MLV", "KH71"},
		{"OH8X", "KP43"},
	}

	const (
		freqStart = 500.0
		freqStep  = 100.0
	)

	mixed := make([]float32, dsp.NMAX)
	type signalInfo struct {
		text string
		freq float64
	}
	signals := make([]signalInfo, 0, len(callsigns))

	for i, cs := range callsigns {
		text := fmt.Sprintf("CQ %s %s", cs.call, cs.grid)
		msg, err := codec.ParseMessage(text)
		if err != nil {
			log.Fatalf("parse %q: %v", text, err)
		}
		body, err := codec.EncodeMessage(msg)
		if err != nil {
			log.Fatalf("encode %q: %v", text, err)
		}
		f := freqStart + float64(i)*freqStep
		signal := dsp.Synthesize(body, f, 0)
		if signal == nil {
			log.Fatalf("synth nil for %q at %g Hz", text, f)
		}
		for j := range mixed {
			mixed[j] += signal[j]
		}
		signals = append(signals, signalInfo{text: text, freq: f})
	}

	// Add AWGN at the configured per-signal SNR.
	//
	// Each signal is dsp.Synthesize output (unit-peak sine, 12.64 s
	// active in a 15 s slot). Per-sample mean-square inside the
	// active window is 0.5 for a unit-peak sine, so P_sig = 0.5
	// in the unit-peak amplitude domain. WSJT-X SNR convention:
	//
	//	SNR_dB = 10·log10(P_sig / P_noise_in_2500Hz)
	//
	// White noise with per-sample variance σ² at sample rate Fs
	// spreads uniformly across [0, Fs/2] (one-sided), so its power
	// in 2,500 Hz of bandwidth is:
	//
	//	P_noise_in_2500Hz = σ² · 2500 / (Fs/2)
	//	                  = σ² · 2500 / 6000
	//	                  = σ² / 2.4
	//
	// Solving for σ²:
	//
	//	σ² = 2.4 · P_sig · 10^(-SNR/10)
	//	   = 2.4 · 0.5 · 10^(-SNR/10)
	//	   = 1.2 · 10^(-SNR/10)
	if !cleanFixture {
		sigma := math.Sqrt(1.2 * math.Pow(10, -*snrDB/10))
		// nosem: gosec G404 — RNG is deterministic by design, not
		// security-sensitive. Same (snr, seed) must reproduce the
		// fixture byte-identically.
		rng := rand.New(rand.NewSource(int64(*seed))) // #nosec G404
		for i := range mixed {
			mixed[i] += float32(rng.NormFloat64() * sigma)
		}
	}

	// Rescale so peak amplitude lands at 0.7 — leaves headroom and
	// keeps individual-signal amplitude in the range of real captures.
	// When AWGN is present, signal and noise scale together so the
	// SNR is preserved.
	var peak float32
	for _, s := range mixed {
		a := float32(math.Abs(float64(s)))
		if a > peak {
			peak = a
		}
	}
	if peak > 0 {
		scale := float32(0.7) / peak
		for j := range mixed {
			mixed[j] *= scale
		}
	}

	data := &audio.Data{
		SampleRate: uint32(dsp.Fs),
		Channels:   1,
		Samples:    mixed,
	}
	if err := audio.WriteWAV(outPath, data); err != nil {
		log.Fatalf("write %q: %v", outPath, err)
	}

	// Emit the ground-truth manifest next to the WAV. SNR and seed
	// are recorded when AWGN was added so calibration tooling can
	// group fixtures by signal-to-noise condition.
	manifest := &truth.Manifest{
		Wav:        filepath.Base(outPath),
		SampleRate: uint32(dsp.Fs),
		Signals:    make([]truth.Signal, 0, len(signals)),
	}
	if !cleanFixture {
		snr := *snrDB
		sd := *seed
		manifest.SNRDB = &snr
		manifest.Seed = &sd
	}
	for _, s := range signals {
		manifest.Signals = append(manifest.Signals, truth.Signal{
			Text:   s.text,
			FreqHz: s.freq,
			DTSec:  0,
		})
	}
	truthPath := truth.PathFor(outPath)
	if err := truth.Write(truthPath, manifest); err != nil {
		log.Fatalf("write manifest %q: %v", truthPath, err)
	}

	fmt.Printf("Wrote %d CQ signals to %s\n", len(signals), outPath)
	if cleanFixture {
		fmt.Println("  no AWGN (clean fixture)")
	} else {
		fmt.Printf("  AWGN at %.1f dB per-signal SNR (2,500 Hz BW reference), seed %d\n", *snrDB, *seed)
	}
	fmt.Printf("Wrote truth manifest to %s\n", truthPath)
	fmt.Println("Frequency  Message")
	fmt.Println("---------- -----------------------")
	for _, s := range signals {
		fmt.Printf("%7.1f Hz  %s\n", s.freq, s.text)
	}
}
