// sandbox-make-overlap generates synthetic FT8 fixture WAVs with
// known overlapping signals + a matching truth manifest. Used to
// exercise the multi-pass subtraction/redecode loop against signals
// that single-pass cannot fully recover.
//
// Generates three fixtures by default:
//
//   - overlap-A: two signals at the same audio freq, dt offset 0.5 s
//     apart. Tests time-domain overlap with same-bin frequency.
//   - overlap-B: two signals 6.25 Hz apart (one FT8 tone-spacing) at
//     the same dt. Tests frequency-adjacent overlap.
//   - overlap-C: strong + weak. Signal A at 0 dB relative, signal B
//     at -12 dB relative, slight (50 ms) dt offset. Tests subtraction-
//     reveals-weaker-signal.
//
// Output: $outdir/{overlap-A,B,C}.wav + .truth.json for each.
//
// Usage:
//
//	go run ./research/cmd/sandbox-make-overlap -outdir research/captures/overlap
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/ColonelBlimp/station-manager/research/sandbox"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

const (
	audioRateHz   = 12000
	slotDurSec    = 15.0
	slotSamples   = audioRateHz * slotDurSec
	defaultSource = "synthetic"
)

// signalSpec defines one overlapping signal in a fixture.
type signalSpec struct {
	Call1, Call2 string
	Grid         string
	FreqHz       float64
	DtSec        float64
	// RelativeDB is the signal's amplitude in dB relative to the
	// strongest signal in this fixture (the strongest is at 0 dB).
	// Used to set the relative power level for "strong + weak"
	// fixtures.
	RelativeDB float64
}

// fixtureSpec defines one fixture's signal layout + AWGN level.
type fixtureSpec struct {
	Name      string
	Signals   []signalSpec
	NoiseDB   float64 // AWGN power in dB relative to the strongest signal; -math.Inf for clean
	NoiseSeed int64
}

func main() {
	outdir := flag.String("outdir", "research/captures/overlap", "output directory")
	flag.Parse()

	if err := os.MkdirAll(*outdir, 0o755); err != nil {
		log.Fatalf("mkdir %q: %v", *outdir, err)
	}

	fixtures := []fixtureSpec{
		{
			Name: "overlap-A",
			Signals: []signalSpec{
				{Call1: "CQ", Call2: "K1JT", Grid: "FN20", FreqHz: 1500.0, DtSec: 0.0, RelativeDB: 0},
				{Call1: "CQ", Call2: "W1AW", Grid: "FN31", FreqHz: 1500.0, DtSec: 0.5, RelativeDB: 0},
			},
			NoiseDB:   math.Inf(-1),
			NoiseSeed: 1,
		},
		{
			Name: "overlap-B",
			Signals: []signalSpec{
				{Call1: "CQ", Call2: "K1JT", Grid: "FN20", FreqHz: 1500.0, DtSec: 0.0, RelativeDB: 0},
				{Call1: "CQ", Call2: "W1AW", Grid: "FN31", FreqHz: 1506.25, DtSec: 0.0, RelativeDB: 0},
			},
			NoiseDB:   math.Inf(-1),
			NoiseSeed: 2,
		},
		{
			Name: "overlap-C",
			Signals: []signalSpec{
				{Call1: "CQ", Call2: "K1JT", Grid: "FN20", FreqHz: 1500.0, DtSec: 0.0, RelativeDB: 0},
				{Call1: "CQ", Call2: "W1AW", Grid: "FN31", FreqHz: 1500.0, DtSec: 0.05, RelativeDB: -12},
			},
			NoiseDB:   math.Inf(-1),
			NoiseSeed: 3,
		},
	}

	for _, f := range fixtures {
		if err := generateFixture(f, *outdir); err != nil {
			log.Fatalf("fixture %s: %v", f.Name, err)
		}
		fmt.Printf("wrote %s/%s.wav + %s.truth.json\n", *outdir, f.Name, f.Name)
	}
}

func generateFixture(f fixtureSpec, outdir string) error {
	audio := make([]float32, slotSamples)
	manifest := truth.Manifest{
		Wav:        f.Name + ".wav",
		SampleRate: audioRateHz,
		Source:     stringPtr(defaultSource),
	}

	// Synthesize each signal at the right amplitude.
	for _, sig := range f.Signals {
		payload, err := sandbox.PackType1(sig.Call1, sig.Call2, sig.Grid)
		if err != nil {
			return fmt.Errorf("pack %s/%s/%s: %w", sig.Call1, sig.Call2, sig.Grid, err)
		}
		info := sandbox.PayloadToInfo91(payload)
		cw := sandbox.EncodeLDPC(info)
		tones := sandbox.CodewordToTones(cw)

		cosSynth, _, _, _ := sandbox.SynthesizeAudio(
			tones, sig.FreqHz, sig.DtSec, audioRateHz, slotSamples,
		)

		// Apply relative amplitude. Reference signal at 0 dB → amplitude 0.3
		// (well below clip). -12 dB → amplitude 0.075.
		amp := 0.3 * math.Pow(10, sig.RelativeDB/20)
		for n := range audio {
			audio[n] += float32(amp) * cosSynth[n]
		}

		text := sig.Call1 + " " + sig.Call2 + " " + sig.Grid
		manifest.Signals = append(manifest.Signals, truth.Signal{
			Text:   text,
			FreqHz: sig.FreqHz,
			DTSec:  sig.DtSec,
		})
	}

	// Optional AWGN.
	if !math.IsInf(f.NoiseDB, -1) {
		rng := rand.New(rand.NewSource(f.NoiseSeed))
		// Noise amplitude in linear units = reference_amp * 10^(noiseDB/20)
		noiseAmp := 0.3 * math.Pow(10, f.NoiseDB/20)
		for n := range audio {
			audio[n] += float32(noiseAmp * rng.NormFloat64())
		}
		manifest.SNRDB = &f.NoiseDB
		manifest.Seed = uint64Ptr(uint64(f.NoiseSeed))
	}

	// Write WAV.
	wavPath := filepath.Join(outdir, f.Name+".wav")
	if err := writeWAV(wavPath, audio, audioRateHz); err != nil {
		return fmt.Errorf("write wav: %w", err)
	}

	// Write truth manifest.
	truthPath := filepath.Join(outdir, f.Name+".truth.json")
	tf, err := os.Create(truthPath)
	if err != nil {
		return fmt.Errorf("create truth: %w", err)
	}
	defer tf.Close()
	enc := json.NewEncoder(tf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&manifest); err != nil {
		return fmt.Errorf("encode truth: %w", err)
	}
	return nil
}

// writeWAV emits a PCM16 mono WAV at sampleRate Hz with the float32
// samples clamped to [-1, 1] and scaled to int16.
func writeWAV(path string, samples []float32, sampleRate uint32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	const (
		channels      uint16 = 1
		bitsPerSample uint16 = 16
		byteRate             = uint32(audioRateHz) * uint32(channels) * uint32(bitsPerSample) / 8
		blockAlign    uint16 = channels * bitsPerSample / 8
	)
	dataSize := uint32(len(samples)) * uint32(blockAlign)
	totalSize := 36 + dataSize

	// RIFF/WAVE header
	if _, err := f.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, totalSize); err != nil {
		return err
	}
	if _, err := f.Write([]byte("WAVE")); err != nil {
		return err
	}
	// fmt chunk
	if _, err := f.Write([]byte("fmt ")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(1)); err != nil {
		return err
	} // PCM
	if err := binary.Write(f, binary.LittleEndian, channels); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, sampleRate); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, blockAlign); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, bitsPerSample); err != nil {
		return err
	}
	// data chunk
	if _, err := f.Write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, dataSize); err != nil {
		return err
	}
	for _, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		v := int16(s * 32767)
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	return nil
}

func stringPtr(s string) *string { return &s }
func uint64Ptr(u uint64) *uint64 { return &u }
