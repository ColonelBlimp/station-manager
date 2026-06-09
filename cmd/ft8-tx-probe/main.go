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
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/audio/playback"
	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

func main() {
	var (
		listDevices = flag.Bool("list", false, "list playback devices and exit")
		deviceIndex = flag.Int("device", -1, "playback device index (-1 = system default)")
		msg         = flag.String("msg", "CQ G0ABC IO91", "standard FT8 message to encode and play")
		offset      = flag.Float64("offset", 1500, "audio offset (Hz) of the base tone")
		dt          = flag.Float64("dt", 0.5, "start time within the 15 s slot (seconds); FT8 standard is +0.5 s")
		wavOut      = flag.String("wav", "", "also write the encoded slot to this WAV (16-bit PCM 12k mono) for an A/B decode")
		key         = flag.Bool("key", false, "DANGER: key the rig (REAL RF) and transmit one slot via the FT8 TX controller (ADR 0030). Needs a connected rig; stop the daemon first to free the serial port.")
		configPath  = flag.String("config", "", "config.json path for -key (default: $SM_WORKING_DIR, else the XDG data dir ~/.local/share/station-manager)")
	)
	flag.Parse()

	// -key is the only RF path: it stands up a bridge connection and keys PTT.
	// It manages its own player + bridge, so dispatch before building the
	// audio-only player below.
	if *key {
		runKeyedTx(*configPath, *deviceIndex, *msg, *offset)
		return
	}

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

// runKeyedTx is the gated REAL-RF path (ADR 0030 step d): load config, stand up
// a bridge connection to the rig, wait for connect + identity, then transmit one
// FT8 slot via the daemon-owned TX controller — key PTT, play the waveform,
// unkey with the bridge's guaranteed stop. It owns its own bridge + player so no
// running daemon (or SPA) is involved; stop the systemd daemon first so this can
// own the serial port.
func runKeyedTx(configPath string, deviceIndex int, msg string, offset float64) {
	fmt.Fprintln(os.Stderr, "\n*** -key: THIS TRANSMITS REAL RF. Use a dummy load or a known-good antenna. ***")
	fmt.Fprintln(os.Stderr, "*** Stop the daemon first (smctl stop) so this probe can own the serial port. ***")

	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		fatal("load config %s: %v", configPath, err)
	}
	activeBridge := cfg.ActiveBridge()
	activeFt8 := cfg.ActiveFt8()

	mode := ""
	if activeFt8.TX != nil {
		mode = activeFt8.TX.Mode
	}

	// Playback device: the -device flag wins; otherwise the configured
	// ft8.tx.device. Empty/invalid → system default (-1).
	devIdx := deviceIndex
	if devIdx < 0 && activeFt8.TX != nil && activeFt8.TX.Device != "" {
		if n, perr := strconv.Atoi(activeFt8.TX.Device); perr == nil {
			devIdx = n
		}
	}
	pcfg := playback.DefaultConfig()
	pcfg.DeviceIndex = devIdx
	player := playback.New(pcfg)
	if err := player.Init(); err != nil {
		fatal("playback init: %v", err)
	}
	defer func() { _ = player.Close() }()

	// Bring up the bridge. An uninitialised &logging.Service{} is the
	// noop-logger pattern the bridge's own tests use.
	logger := &logging.Service{}
	br := bridge.New(activeBridge, logger)
	if err := br.Initialize(); err != nil {
		fatal("bridge init: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := br.Start(ctx); err != nil {
		fatal("bridge start: %v", err)
	}
	defer func() { _ = br.Stop() }()

	fmt.Fprintln(os.Stderr, "waiting for rig connection + identity…")
	if !waitTxReady(br, 30*time.Second) {
		fatal("rig not ready (connected + identity-confirmed) within 30s — is it powered on and is bridge.cat.driver correct?")
	}

	// Ctrl-C cancels the transmission; the controller's deferred unkey + the
	// bridge auto-off backstop guarantee PTT comes down.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nsignal received — aborting (PTT will drop)…")
		cancel()
	}()

	ctrl := ft8.NewTxController(ft8Keyer{br}, player, mode, logger)
	fmt.Fprintf(os.Stderr, "transmitting %q at %.0f Hz on the next UTC slot (mode=%q)…\n", msg, offset, mode)
	if err := ctrl.TransmitSlot(ctx, msg, offset); err != nil {
		fatal("transmit: %v", err)
	}
	fmt.Fprintln(os.Stderr, "transmission complete; PTT down.")
}

// waitTxReady polls the bridge until it can key TX (connected + identity
// confirmed) or the timeout elapses.
func waitTxReady(br *bridge.Service, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if br.TxReady() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return br.TxReady()
}

// ft8Keyer adapts the bridge's FT8 keying methods to the ft8.TxKeyer interface,
// keeping internal/ft8 free of any internal/bridge import (ADR 0030).
type ft8Keyer struct{ b *bridge.Service }

func (k ft8Keyer) KeyTx(ctx context.Context, mode string) error { return k.b.KeyFt8Tx(ctx, mode) }
func (k ft8Keyer) UnkeyTx(ctx context.Context) error            { return k.b.UnkeyFt8Tx(ctx) }

// resolveConfigPath finds config.json for the -key path: an explicit -config
// flag wins; otherwise $SM_WORKING_DIR (the systemd unit's value), else the XDG
// data dir. It deliberately does NOT use utils.WorkingDir's executable-dir
// fallback — under `go run` the binary lives in the go-build cache, so that
// would point at a cache dir with no config. The probe always targets the
// operator's real install, which lives at the XDG data dir.
func resolveConfigPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if wd := os.Getenv(utils.EnvSmWorkingDir); wd != "" {
		return filepath.Join(wd, "config.json")
	}
	return filepath.Join(utils.XDGDataDir(), "config.json")
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
