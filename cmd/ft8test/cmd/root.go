// Package cmd provides the Cobra commands for the FT8 stage-by-stage
// integration test CLI.
//
// Each pipeline stage is exposed as a separate subcommand so it can be
// developed, tested, and debugged in isolation with live or recorded audio.
//
// Subcommands:
//
//	devices     – list available audio capture/playback devices
//	capture     – capture one 15 s window → WAV file (audio + decimation)
//	spectrogram – compute spectrogram from WAV → diagnostics
//	candidates  – detect Costas sync candidates from WAV → table
//	decode      – full pipeline: WAV or live → decoded messages
//
// Usage:
//
//	ft8test devices
//	ft8test capture  --device 3 --output capture.wav
//	ft8test spectrogram --input capture.wav
//	ft8test candidates  --input capture.wav
//	ft8test decode      --input capture.wav
//	ft8test decode      --device 3 --windows 4
package cmd

import (
	"fmt"
	"os"
	"reflect"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/spf13/cobra"
)

// Services resolved during setup (PersistentPreRunE).
var (
	configService *config.Service
	logService    *logging.Service
)

// Persistent flags (inherited by all subcommands).
var (
	flagVerbose bool
	flagDevice  int
)

var rootCmd = &cobra.Command{
	Use:   "ft8test",
	Short: "FT8 stage-by-stage integration test CLI",
	Long: `Step-by-step integration test harness for the FT8 DSP pipeline.

Each pipeline stage is exposed as a separate subcommand so it can be
tested in isolation with live audio or recorded WAV files. Stages
produce file-based intermediate outputs that feed into subsequent stages.

Pipeline stages (in order):
  1. capture     – audio capture + 48→12 kHz decimation → WAV
  2. spectrogram – FFT spectrogram from audio → diagnostics
  3. candidates  – Costas sync candidate detection → table
  4. decode      – full pipeline: demod + LDPC + CRC → messages

Requires a config.json in the working directory (or SM_WORKING_DIR).

Examples:
  ft8test devices
  ft8test capture --device 3
  ft8test capture --device 3 --output my_capture.wav
  ft8test spectrogram --input capture.wav
  ft8test candidates --input capture.wav --max-candidates 200
  ft8test decode --input capture.wav
  ft8test decode --device 3 --windows 2`,
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false,
		"enable debug-level logging")
	rootCmd.PersistentFlags().IntVar(&flagDevice, "device", -1,
		"audio capture device index (-1 = use config default)")

	rootCmd.PersistentPreRunE = setup
}

// setup performs DI wiring, config loading, and logging initialisation.
// Runs as PersistentPreRunE so all subcommands inherit it.
func setup(_ *cobra.Command, _ []string) error {
	workingDir, err := utils.WorkingDir()
	if err != nil {
		return fmt.Errorf("cannot resolve working directory: %w", err)
	}

	logLevel := "info"
	if flagVerbose {
		logLevel = "debug"
	}
	configService = &config.Service{WorkingDir: workingDir}
	configService.AppConfig.LoggingConfig = types.LoggingConfig{
		Level:             logLevel,
		SkipFrameCount:    3,
		WithTimestamp:     true,
		ConsoleLogging:    true,
		FileLogging:       false,
		RelLogFileDir:     "logs",
		ShutdownTimeoutMS: 5000,
	}

	container := iocdi.New()
	if err := container.RegisterInstance("workingdir", workingDir); err != nil {
		return fmt.Errorf("DI register workingdir: %w", err)
	}
	if err := container.RegisterInstance(config.ServiceName, configService); err != nil {
		return fmt.Errorf("DI register config: %w", err)
	}
	if err := container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil))); err != nil {
		return fmt.Errorf("DI register logging: %w", err)
	}

	if err := container.Build(); err != nil {
		return fmt.Errorf("DI build: %w", err)
	}

	logInst, err := container.ResolveSafe(logging.ServiceName)
	if err != nil {
		return fmt.Errorf("DI resolve logging: %w", err)
	}
	var ok bool
	logService, ok = logInst.(*logging.Service)
	if !ok {
		return fmt.Errorf("resolved logging service has unexpected type %T", logInst)
	}

	return nil
}

// effectiveDevice returns the device index to use, preferring the CLI flag
// over the config file value.
func effectiveDevice() int {
	if flagDevice >= 0 {
		return flagDevice
	}
	return configService.AppConfig.FT8Config.DeviceIndex
}
