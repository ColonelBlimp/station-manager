//go:build cgo

// ft8-tx-probe is a standalone smoke utility for the live FT8 transmit audio
// path (ADR 0029 step c): the ft8 encode→modulate chain (internal/ft8) feeding
// the internal/audio/playback output device. It is the tool for validating
// audio output before the PTT + slot-timing controller (step d) exists.
//
// It produces AUDIO ONLY — it does not key the rig. Played into a sound card
// (or the rig's audio input with PTT off) it is RF-safe; the tones can be
// captured and round-tripped through ft8-decode-file or jt9 to confirm the
// modulator and device agree end-to-end.
//
// Playback is CGO (miniaudio), so this binary only builds under CGO_ENABLED=1.
//
// Usage:
//
//	# List playback devices and their indices (use the index in config.json's
//	# ft8.tx.device, or the -device flag here).
//	ft8-tx-probe -list
//
//	# Encode and play a standard FT8 message at a chosen audio offset:
//	ft8-tx-probe -device=2 -msg="CQ G0ABC IO91" -offset=1500
//
//	# Capture the output (loopback / line-in) and verify it decodes back:
//	#   ft8-tx-probe -msg="CQ G0ABC IO91" -offset=1500 -wav=tx.wav
//	#   ft8-decode-file tx.wav   (or: jt9 -8 -d 3 tx.wav)
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/audio/playback"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

func main() {
	var (
		listDevices = flag.Bool("list", false, "list playback devices and exit")
		deviceIndex = flag.Int("device", -1, "playback device index (-1 = system default)")
		msg         = flag.String("msg", "CQ G0ABC IO91", "standard FT8 message to encode and play")
		offset      = flag.Float64("offset", 1500, "audio offset (Hz) of the base tone")
		dt          = flag.Float64("dt", 0.5, "start time within the 15 s slot (seconds); FT8 standard is +0.5 s")
		wavOut      = flag.String("wav", "", "also write the encoded slot to this WAV (16-bit PCM 12k mono) for an A/B decode")
	)
	flag.Parse()

	cfg := playback.DefaultConfig() // 12 kHz mono S16
	cfg.DeviceIndex = *deviceIndex

	p := playback.New(cfg)
	if err := p.Init(); err != nil {
		fatal("playback init: %v", err)
	}
	defer func() { _ = p.Close() }()

	if *listDevices {
		runListDevices(p)
		return
	}

	runPlay(p, cfg, *msg, *offset, *dt, *wavOut)
}

func runListDevices(p *playback.Player) {
	devices, err := p.ListDevices()
	if err != nil {
		fatal("list devices: %v", err)
	}
	fmt.Printf("Found %d playback device(s):\n", len(devices))
	for i, d := range devices {
		fmt.Printf("  [%d] %s\n", i, d.Name())
	}
	fmt.Println("\nSet the chosen index as ft8.tx.device in config.json (e.g. \"2\"), or pass -device=N here.")
}

func runPlay(p *playback.Player, cfg playback.Config, msg string, offset, dt float64, wavOut string) {
	slot, err := ft8.EncodeToSlot(msg, offset, dt)
	if err != nil {
		fatal("encode %q: %v", msg, err)
	}

	if wavOut != "" {
		if err := saveSlotWAV(wavOut, slot, cfg.SampleRate); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: write %s: %v\n", wavOut, err)
		} else {
			fmt.Printf("wrote %s\n", wavOut)
		}
	}

	fmt.Printf("Playing %q at %.0f Hz (dt=%.2fs) to device=%d — AUDIO ONLY, no PTT.\n",
		msg, offset, dt, cfg.DeviceIndex)

	done, err := p.Play(slot)
	if err != nil {
		fatal("play: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-done:
		// Natural end reached. Give the device buffer a moment to flush the
		// final period before we tear it down, so the tail isn't clipped.
		time.Sleep(100 * time.Millisecond)
		fmt.Println("waveform finished.")
	case <-sigCh:
		fmt.Fprintln(os.Stderr, "\nsignal received — stopping…")
	case <-time.After(ft8.SlotDuration + 5*time.Second):
		fmt.Fprintln(os.Stderr, "timeout — stopping…")
	}

	if err := p.Stop(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: stop: %v\n", err)
	}
}

// saveSlotWAV writes the slot's int16 samples as a 16-bit PCM mono WAV that
// ft8-decode-file / jt9 can read back for an A/B decode of the modulator.
func saveSlotWAV(path string, samples []int16, rate uint32) error {
	f := make([]float32, len(samples))
	for i, s := range samples {
		f[i] = float32(s) / 32767
	}
	return audio.WriteWAV(path, &audio.Data{SampleRate: rate, Channels: 1, Samples: f})
}

func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
