//go:build cgo

// ft8-capture-probe is a standalone smoke utility for the live FT8 path:
// the internal/audio/capture device layer + the internal/ft8 slot scheduler
// and decoder. It is the tool for validating capture before (or instead of)
// running the full daemon with ft8.enabled=true.
//
// Capture is CGO (miniaudio), so this binary only builds under CGO_ENABLED=1.
//
// Usage:
//
//	# List capture devices and their indices (use the index in config.json's
//	# ft8.device, or the -device flag here).
//	ft8-capture-probe -list
//
//	# Audio sanity check: capture 10 s from the default device and report
//	# sample count + peak amplitude (is audio actually arriving?).
//	ft8-capture-probe -device=2 -duration=10s
//
//	# The real decode smoke: run the UTC-aligned slot scheduler for N slots
//	# and decode each one, printing the heard messages.
//	ft8-capture-probe -scheduler -slots=4 -device=2
//
// A non-aligned single-shot capture rarely decodes real signals (FT8 needs
// the 15 s window aligned to the UTC :00/:15/:30/:45 boundary) — use
// -scheduler for a decode smoke; single-shot is for the audio-level check.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio/capture"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func main() {
	var (
		listDevices = flag.Bool("list", false, "list capture devices and exit")
		deviceIndex = flag.Int("device", -1, "capture device index (-1 = system default)")
		duration    = flag.Duration("duration", 10*time.Second, "capture duration (single-shot mode)")
		schedulerOn = flag.Bool("scheduler", false, "run the UTC-aligned slot scheduler + decode instead of single-shot")
		slotsToRun  = flag.Int("slots", 4, "number of slots to decode in -scheduler mode")
	)
	flag.Parse()

	cfg := capture.DefaultConfig() // 12 kHz mono float32
	cfg.DeviceIndex = *deviceIndex

	c := capture.New(cfg)
	if err := c.Init(); err != nil {
		fatal("capture init: %v", err)
	}
	defer func() { _ = c.Close() }()

	switch {
	case *listDevices:
		runListDevices(c)
	case *schedulerOn:
		runSchedulerMode(c, *slotsToRun)
	default:
		runSingleShot(c, cfg, *duration)
	}
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
	fmt.Println("\nSet the chosen index as ft8.device in config.json (e.g. \"2\"), or pass -device=N here.")
}

func runSingleShot(c *capture.Capture, cfg capture.Config, duration time.Duration) {
	fmt.Printf("Capturing %s from device=%d @ %d Hz mono\n", duration, cfg.DeviceIndex, cfg.SampleRate)

	ctx, cancel := context.WithTimeout(context.Background(), duration+5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		fatal("capture start: %v", err)
	}

	collected := 0
	var peak float64
	deadline := time.After(duration)
collect:
	for {
		select {
		case batch, ok := <-c.Samples():
			if !ok {
				break collect
			}
			collected += len(batch)
			if p := peakAmplitude(batch); p > peak {
				peak = p
			}
		case <-deadline:
			break collect
		case <-ctx.Done():
			break collect
		}
	}

	if err := c.Stop(); err != nil && !errors.Is(err, capture.ErrNotRunning) {
		_, _ = fmt.Fprintf(os.Stderr, "warning: stop: %v\n", err)
	}

	fmt.Printf("\nCaptured %d samples (%.2f s)\n", collected, float64(collected)/float64(cfg.SampleRate))
	fmt.Printf("Peak amplitude: %.4f (1.0 = full scale)\n", peak)
	fmt.Printf("Dropped chunks: %d\n", c.DroppedChunks())
	if peak < 0.001 {
		_, _ = fmt.Fprintln(os.Stderr, "WARNING: peak is near zero — input may be muted, wrong device, or unconnected")
	}
	fmt.Println("\nFor a decode smoke, re-run with -scheduler (FT8 needs UTC-aligned slots).")
}

func runSchedulerMode(c *capture.Capture, slotsToRun int) {
	if slotsToRun < 1 {
		fatal("-slots must be >= 1, got %d", slotsToRun)
	}

	// Cold start can lose up to one slot before the ring fills, then N slots
	// of 15 s, plus headroom.
	budget := time.Duration(slotsToRun+2) * ft8.SlotDuration
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

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

	// Convert the float32 capture stream to the int16 the scheduler consumes
	// — the same seam the daemon's capture source bridges internally.
	int16ch := make(chan []int16, capture.SampleChannelBufferSize)
	go func() {
		defer close(int16ch)
		for batch := range c.Samples() {
			out := make([]int16, len(batch))
			for i, f := range batch {
				out[i] = floatToInt16(f)
			}
			select {
			case int16ch <- out:
			case <-ctx.Done():
				return
			}
		}
	}()

	sch := ft8.NewScheduler(int16ch, nil)
	runDone := make(chan error, 1)
	go func() { runDone <- sch.Run(ctx) }()

	fmt.Printf("Scheduler running — collecting %d slot(s).\n", slotsToRun)
	fmt.Println("Waiting for the next UTC :00/:15/:30/:45 boundary…")

	collected := 0
	for slot := range sch.Slots() {
		collected++
		fmt.Printf("\nslot %d/%d  start=%s  offset=%dms  samples=%d  peak=%.4f\n",
			collected, slotsToRun, slot.StartUTC.Format("15:04:05"),
			slot.OffsetMs, len(slot.Samples), peakAmplitudeInt16(slot.Samples))

		t0 := time.Now()
		msgs := ft8.DecodeSlot(slot.Samples, logging.Noop())
		fmt.Printf("  decoded %d msg(s) in %s\n", len(msgs), time.Since(t0).Round(time.Millisecond))
		for i, m := range msgs {
			fmt.Printf("    [%d] %8.2f Hz  %+.2f s  sync=%.2f  %q\n", i, m.FreqHz, m.DTSec, m.Sync, m.Text)
		}

		if collected >= slotsToRun {
			cancel()
			break
		}
	}
	<-runDone

	if err := c.Stop(); err != nil && !errors.Is(err, capture.ErrNotRunning) {
		_, _ = fmt.Fprintf(os.Stderr, "warning: stop: %v\n", err)
	}
	fmt.Printf("\nDone: %d slot(s) processed, slot-drops=%d, capture-drops=%d\n",
		collected, sch.Dropped(), c.DroppedChunks())
}

// floatToInt16 mirrors the daemon's capture-source conversion (clamp + scale).
func floatToInt16(f float32) int16 {
	v := int32(f * 32767)
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

func peakAmplitude(samples []float32) float64 {
	var peak float64
	for _, s := range samples {
		if a := math.Abs(float64(s)); a > peak {
			peak = a
		}
	}
	return peak
}

func peakAmplitudeInt16(samples []int16) float64 {
	var peak float64
	for _, s := range samples {
		if a := math.Abs(float64(s)) / 32768; a > peak {
			peak = a
		}
	}
	return peak
}

func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
