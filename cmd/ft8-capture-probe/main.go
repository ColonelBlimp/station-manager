// ft8-capture-probe is a standalone smoke utility for the
// internal/audio/capture package. It opens a configured audio device
// at 12 kHz mono, captures a fixed-duration window of samples, and
// (optionally) feeds the captured audio into the FT8 decoder to
// confirm end-to-end audio→messages on live hardware.
//
// Usage:
//
//	# List capture devices and exit
//	ft8-capture-probe -list
//
//	# Capture 15 s from the default device, decode FT8 messages
//	ft8-capture-probe
//
//	# Capture from a specific device for 30 s without decoding
//	ft8-capture-probe -device=2 -duration=30s -decode=false
//
// This binary exists to validate Phase A of M4.2 (audio capture
// wiring) before the slot scheduler is built. It is not part of the
// daemon and is safe to leave checked in as a developer probe.
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio/capture"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

func main() {
	var (
		listDevices = flag.Bool("list", false, "list capture devices and exit")
		deviceIndex = flag.Int("device", -1, "capture device index (-1 = default)")
		duration    = flag.Duration("duration", 15*time.Second, "capture duration")
		sampleRate  = flag.Uint("rate", 12000, "sample rate in Hz (FT8 = 12000)")
		decode      = flag.Bool("decode", true, "feed captured audio into ft8.Decode and print messages")
	)
	flag.Parse()

	cfg := capture.DefaultConfig()
	cfg.DeviceIndex = *deviceIndex
	cfg.SampleRate = uint32(*sampleRate)

	c := capture.New(cfg)
	if err := c.Init(); err != nil {
		fatal("capture init: %v", err)
	}
	defer func() { _ = c.Close() }()

	if *listDevices {
		devices, err := c.ListDevices()
		if err != nil {
			fatal("list devices: %v", err)
		}
		fmt.Printf("Found %d capture device(s):\n", len(devices))
		for i, d := range devices {
			fmt.Printf("  [%d] %s\n", i, d.Name())
		}
		return
	}

	fmt.Printf("Capturing %s from device=%d @ %d Hz mono float32\n",
		*duration, cfg.DeviceIndex, cfg.SampleRate)

	ctx, cancel := context.WithTimeout(context.Background(), *duration+5*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		fatal("capture start: %v", err)
	}

	collected := make([]float32, 0, int(*duration/time.Second)*int(cfg.SampleRate))
	deadline := time.After(*duration)

collect:
	for {
		select {
		case batch, ok := <-c.Samples():
			if !ok {
				break collect
			}
			collected = append(collected, batch...)
		case <-deadline:
			break collect
		case <-ctx.Done():
			break collect
		}
	}

	if err := c.Stop(); err != nil && err != capture.ErrNotRunning {
		fmt.Fprintf(os.Stderr, "warning: stop: %v\n", err)
	}

	peak := peakAmplitude(collected)
	dropped := c.DroppedChunks()

	fmt.Printf("\nCaptured %d samples (%.2f s of audio)\n",
		len(collected), float64(len(collected))/float64(cfg.SampleRate))
	fmt.Printf("Peak amplitude: %.4f (1.0 = full scale)\n", peak)
	fmt.Printf("Dropped chunks: %d\n", dropped)

	if dropped > 0 {
		fmt.Fprintln(os.Stderr, "WARNING: chunks were dropped — consumer was too slow")
	}
	if peak < 0.001 {
		fmt.Fprintln(os.Stderr, "WARNING: peak amplitude is very low — input may be muted or unconnected")
	}

	if !*decode {
		return
	}

	if cfg.SampleRate != 12000 {
		fmt.Fprintf(os.Stderr, "Skipping decode: ft8.Decode expects 12 kHz audio (got %d)\n",
			cfg.SampleRate)
		return
	}

	fmt.Println("\nDecoding FT8 messages…")
	t0 := time.Now()
	results := ft8.Decode(collected, ft8.DecodeOptions{})
	fmt.Printf("Decoded %d message(s) in %s\n", len(results), time.Since(t0).Round(time.Millisecond))
	for i, r := range results {
		fmt.Printf("  [%d] %7.2f Hz  %+.2f s  sync=%.2f  %q\n",
			i, r.Freq, r.DT, r.SyncPower, r.Text)
	}
}

func peakAmplitude(samples []float32) float64 {
	var peak float64
	for _, s := range samples {
		a := math.Abs(float64(s))
		if a > peak {
			peak = a
		}
	}
	return peak
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
