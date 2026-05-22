// ft8-capture-probe is a standalone smoke utility for the
// internal/audio/capture package and the internal/ft8 slot scheduler.
//
// In the default mode the probe captures a fixed-duration window of
// audio and (optionally) feeds it into ft8.Decode. In -scheduler mode
// the probe runs the live scheduler for N UTC-aligned slots, reporting
// per-slot offset, sample count, and decode results.
//
// Usage:
//
//	# List capture devices and exit
//	ft8-capture-probe -list
//
//	# Default mode: capture 15 s from the default device, decode FT8
//	ft8-capture-probe
//
//	# Capture 30 s from device 1 without decoding
//	ft8-capture-probe -device=1 -duration=30s -decode=false
//
//	# Scheduler mode: 4 slots (~60s after the next UTC :00/:15/:30/:45
//	# boundary), live FT8 decode for each
//	ft8-capture-probe -scheduler -slots=4 -device=1
//
//	# Capture 3 UTC-aligned slots to newcap_slot{1,2,3}.wav (16-bit PCM
//	# mono 12 kHz — same format as the testdata fixtures, decodable by
//	# jt9 and ft8.Decode alike) for offline refinement work
//	ft8-capture-probe -scheduler -slots=3 -out=newcap -device=1
//
// This binary validates M4.2 Phases A + B (capture + scheduler) before
// the daemon FT8 service is wired. It is not part of smd and is safe
// to leave in cmd/ as a developer probe.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/audio/capture"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

func main() {
	var (
		listDevices = flag.Bool("list", false, "list capture devices and exit")
		deviceIndex = flag.Int("device", -1, "capture device index (-1 = default)")
		duration    = flag.Duration("duration", 15*time.Second, "capture duration (default mode only)")
		sampleRate  = flag.Uint("rate", 12000, "sample rate in Hz (FT8 = 12000)")
		decode      = flag.Bool("decode", true, "feed captured audio into ft8.Decode and print messages")
		schedulerOn = flag.Bool("scheduler", false, "run the slot scheduler instead of single-shot capture")
		slotsToRun  = flag.Int("slots", 4, "number of slots to run in -scheduler mode")
		subPasses   = flag.Int("subtraction-passes", 0, "ft8.DecodeOptions.SubtractionPasses (0=fast, 1=sensitive)")
		outPath     = flag.String("out", "", "write captured audio to WAV (single-shot: exact path; scheduler: <out>_slotN.wav). Empty = no save.")
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
		runListDevices(c)
		return
	}

	if *schedulerOn {
		runSchedulerMode(c, cfg, *slotsToRun, *decode, *subPasses, *outPath)
		return
	}

	runSingleShot(c, cfg, *duration, *decode, *subPasses, *outPath)
}

func runListDevices(c *capture.Capture) {
	devices, err := c.ListDevices()
	if err != nil {
		fatal("list devices: %v", err)
	}
	fmt.Printf("Found %d capture device(s):\n", len(devices))
	for i, d := range devices {
		fmt.Printf("  [%d] %s\n", i, d.Name())
	}
}

func runSingleShot(c *capture.Capture, cfg capture.Config, duration time.Duration, decode bool, subPasses int, outPath string) {
	fmt.Printf("Capturing %s from device=%d @ %d Hz mono float32\n",
		duration, cfg.DeviceIndex, cfg.SampleRate)

	ctx, cancel := context.WithTimeout(context.Background(), duration+5*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		fatal("capture start: %v", err)
	}

	collected := make([]float32, 0, int(duration/time.Second)*int(cfg.SampleRate))
	deadline := time.After(duration)

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

	if err := c.Stop(); err != nil && !errors.Is(err, capture.ErrNotRunning) {
		_, _ = fmt.Fprintf(os.Stderr, "warning: stop: %v\n", err)
	}

	peak := peakAmplitude(collected)
	dropped := c.DroppedChunks()

	fmt.Printf("\nCaptured %d samples (%.2f s of audio)\n",
		len(collected), float64(len(collected))/float64(cfg.SampleRate))
	fmt.Printf("Peak amplitude: %.4f (1.0 = full scale)\n", peak)
	fmt.Printf("Dropped chunks: %d\n", dropped)

	if dropped > 0 {
		_, _ = fmt.Fprintln(os.Stderr, "WARNING: chunks were dropped — consumer was too slow")
	}
	if peak < 0.001 {
		_, _ = fmt.Fprintln(os.Stderr, "WARNING: peak amplitude is very low — input may be muted or unconnected")
	}

	if outPath != "" {
		saveWAV(outPath, collected, cfg)
	}

	if !decode {
		return
	}
	if cfg.SampleRate != 12000 {
		_, _ = fmt.Fprintf(os.Stderr, "Skipping decode: ft8.Decode expects 12 kHz audio (got %d)\n", cfg.SampleRate)
		return
	}

	fmt.Println("\nDecoding FT8 messages…")
	t0 := time.Now()
	results := ft8.Decode(collected, ft8.DecodeOptions{SubtractionPasses: subPasses})
	fmt.Printf("Decoded %d message(s) in %s\n", len(results), time.Since(t0).Round(time.Millisecond))
	printDecodes(results)
}

func runSchedulerMode(c *capture.Capture, cfg capture.Config, slotsToRun int, decode bool, subPasses int, outPath string) {
	if cfg.SampleRate != 12000 {
		fatal("scheduler mode requires -rate=12000 (FT8 canonical), got %d", cfg.SampleRate)
	}
	if slotsToRun < 1 {
		fatal("-slots must be >= 1, got %d", slotsToRun)
	}

	// Generous deadline: cold-start can lose up to one slot before the
	// ring fills, then N slots × 15 s, plus 30 s of headroom.
	budget := time.Duration(slotsToRun+2) * ft8.SlotDuration
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	// SIGINT/SIGTERM → graceful cancel.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		_, _ = fmt.Fprintln(os.Stderr, "\nsignal received — shutting down…")
		cancel()
	}()

	if err := c.Start(ctx); err != nil {
		fatal("capture start: %v", err)
	}

	sch := ft8.NewScheduler(c.Samples(), nil)
	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	fmt.Printf("Scheduler running — will collect %d slot(s) (subtraction_passes=%d)\n",
		slotsToRun, subPasses)
	fmt.Println("Waiting for next UTC :00/:15/:30/:45 boundary…")

	collected := 0
	for slot := range sch.Slots() {
		collected++
		peak := peakAmplitude(slot.Samples)
		fmt.Printf("\nslot %d/%d  start=%s  offset=%dms  samples=%d  peak=%.4f\n",
			collected, slotsToRun,
			slot.StartUTC.Format("15:04:05"),
			slot.OffsetMs, len(slot.Samples), peak)

		if outPath != "" {
			saveWAV(fmt.Sprintf("%s_slot%d.wav", outPath, collected), slot.Samples, cfg)
		}

		if decode {
			t0 := time.Now()
			results := ft8.Decode(slot.Samples, ft8.DecodeOptions{SubtractionPasses: subPasses})
			fmt.Printf("  decoded %d msg(s) in %s\n", len(results), time.Since(t0).Round(time.Millisecond))
			printDecodes(results)
		}

		if collected >= slotsToRun {
			cancel()
			break
		}
	}

	// Wait for Run to clean up.
	<-runDone

	if err := c.Stop(); err != nil && !errors.Is(err, capture.ErrNotRunning) {
		_, _ = fmt.Fprintf(os.Stderr, "warning: stop: %v\n", err)
	}

	dropped := sch.Dropped()
	fmt.Printf("\nScheduler done: %d slot(s) processed, %d dropped, capture-drops=%d\n",
		collected, dropped, c.DroppedChunks())
	if dropped > 0 {
		_, _ = fmt.Fprintln(os.Stderr,
			"WARNING: slots were dropped — decode consumer is slower than the slot cadence")
	}
}

// saveWAV writes the captured buffer as a 16-bit PCM WAV matching the
// testdata fixture format. A failed write is a warning, not fatal — the
// probe's primary job (capture + decode reporting) still completed.
func saveWAV(path string, samples []float32, cfg capture.Config) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: mkdir %s: %v\n", dir, err)
			return
		}
	}
	d := &audio.Data{
		SampleRate: cfg.SampleRate,
		Channels:   uint16(cfg.Channels),
		Samples:    samples,
	}
	if err := audio.WriteWAV(path, d); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: write %s: %v\n", path, err)
		return
	}
	fmt.Printf("  wrote %s (%d samples, %.2f s)\n",
		path, len(samples), float64(len(samples))/float64(cfg.SampleRate))
}

func printDecodes(results []ft8.DecodedMessage) {
	for i, r := range results {
		fmt.Printf("    [%d] %7.2f Hz  %+.2f s  sync=%.2f  %q\n",
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
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
