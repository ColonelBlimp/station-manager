// ft8-gen10cq synthesises a 15-second FT8 slot containing 10 CQ
// signals at equally-spaced frequencies, then writes it to a 12 kHz
// mono 16-bit WAV file. The output is a deterministic, clean
// (noise-free) fixture for FT8 decoder + subtraction experiments.
//
// Usage:
//
//	go run ./cmd/ft8-gen10cq                 # writes research/10cq_clean.wav
//	go run ./cmd/ft8-gen10cq -out PATH.wav   # writes to PATH.wav
//
// Signals are placed at 500, 600, 700, ..., 1400 Hz (100 Hz spacing
// — comfortably non-overlapping; FT8 occupies 50 Hz per signal). All
// transmit at slot-start (dt = 0). All carry distinct callsigns
// calling CQ with a 4-character grid square.
//
// The mixed signal is rescaled to peak amplitude 0.7 so each individual
// signal contributes amplitude ~0.07-0.1 (comparable to typical real
// captures). No AWGN is added — the residual after subtracting any
// successfully-decoded signal should approach the floating-point
// noise floor.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"path/filepath"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/research/truth"
)

func main() {
	out := flag.String("out", "research/10cq_clean.wav", "output WAV path")
	flag.Parse()

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

	// Rescale so peak amplitude lands at 0.7 — leaves headroom and
	// keeps individual-signal amplitude in the range of real captures.
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
	if err := audio.WriteWAV(*out, data); err != nil {
		log.Fatalf("write %q: %v", *out, err)
	}

	// Emit the ground-truth manifest next to the WAV. Downstream
	// research tools (find-candidates, future decoders) score their
	// output against this without needing to actually decode the
	// audio — see research/truth for the format and rationale.
	manifest := &truth.Manifest{
		Wav:        filepath.Base(*out),
		SampleRate: uint32(dsp.Fs),
		Signals:    make([]truth.Signal, 0, len(signals)),
	}
	for _, s := range signals {
		manifest.Signals = append(manifest.Signals, truth.Signal{
			Text:   s.text,
			FreqHz: s.freq,
			DTSec:  0,
		})
	}
	truthPath := truth.PathFor(*out)
	if err := truth.Write(truthPath, manifest); err != nil {
		log.Fatalf("write manifest %q: %v", truthPath, err)
	}

	fmt.Printf("Wrote %d CQ signals to %s\n", len(signals), *out)
	fmt.Printf("Wrote truth manifest to %s\n", truthPath)
	fmt.Println("Frequency  Message")
	fmt.Println("---------- -----------------------")
	for _, s := range signals {
		fmt.Printf("%7.1f Hz  %s\n", s.freq, s.text)
	}
}
