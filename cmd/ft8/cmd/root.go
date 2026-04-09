// Package cmd provides the Cobra commands for the FT8 RX test harness.
//
// This is a minimal CLI tool for integration-testing the FT8 receive pipeline
// against real transceiver audio. It wires the FT8 service in RX-only mode,
// prints decoded messages to stdout, and exits on SIGINT/SIGTERM.
//
// Usage:
//
//	ft8 [flags]
//	ft8 --list-devices
//	ft8 --device 3 --windows 10
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	service "github.com/ColonelBlimp/station-manager/internal/ft8/service"
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

// Flag values.
var (
	flagWindows     int
	flagDevice      int
	flagListDevices bool
	flagVerbose     bool
	flagDiag        bool
)

var rootCmd = &cobra.Command{
	Use:   "ft8",
	Short: "FT8 RX test harness",
	Long: `Minimal CLI tool for integration-testing the FT8 receive pipeline
against a real transceiver. Captures audio, runs the full DSP decode
pipeline, and prints decoded messages to stdout.

Requires a config.json in the working directory (or SM_WORKING_DIR).
The ft8_config.enabled field is forced to true at runtime.

Examples:
  ft8 --list-devices            List available audio capture devices
  ft8 --device 3                Decode using capture device 3
  ft8 --device 3 --windows 4   Decode 4 windows (60 s) and exit
  ft8 --device 1 --diag         Audio capture diagnostics (no decode)
  ft8 --verbose                 Enable debug-level logging`,
	PersistentPreRunE: setup,
	RunE:              runRX,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().IntVar(&flagWindows, "windows", 0,
		"number of 15 s FT8 windows to decode (0 = unlimited)")
	rootCmd.Flags().IntVar(&flagDevice, "device", -1,
		"override audio capture device index (-1 = use config default)")
	rootCmd.Flags().BoolVar(&flagListDevices, "list-devices", false,
		"list audio capture devices and exit")
	rootCmd.Flags().BoolVar(&flagVerbose, "verbose", false,
		"enable debug-level logging")
	rootCmd.Flags().BoolVar(&flagDiag, "diag", false,
		"run audio capture diagnostics (no FT8 decode)")
}

// setup performs DI wiring, config loading, and logging initialisation.
// Running in PersistentPreRunE ensures errors flow through Cobra's normal
// error path instead of calling os.Exit in init(), and deferred cleanup
// runs correctly.
func setup(_ *cobra.Command, _ []string) error {
	workingDir, err := utils.WorkingDir()
	if err != nil {
		return fmt.Errorf("cannot resolve working directory: %w", err)
	}

	container := iocdi.New()
	if err := container.RegisterInstance("workingdir", workingDir); err != nil {
		return fmt.Errorf("DI register workingdir: %w", err)
	}
	if err := container.Register(config.ServiceName, reflect.TypeOf((*config.Service)(nil))); err != nil {
		return fmt.Errorf("DI register config: %w", err)
	}
	if err := container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil))); err != nil {
		return fmt.Errorf("DI register logging: %w", err)
	}
	if err := container.Build(); err != nil {
		return fmt.Errorf("DI build: %w", err)
	}

	// Resolve config service.
	cfgInst, err := container.ResolveSafe(config.ServiceName)
	if err != nil {
		return fmt.Errorf("DI resolve config: %w", err)
	}
	var ok bool
	configService, ok = cfgInst.(*config.Service)
	if !ok {
		return fmt.Errorf("resolved config service has unexpected type %T", cfgInst)
	}

	// Pre-seed logging config for console-only output before Initialize.
	// The config.Service.Initialize preseed logic preserves a LoggingConfig
	// whose Level field is non-empty.
	logLevel := "info"
	if flagVerbose {
		logLevel = "debug"
	}
	configService.AppConfig.LoggingConfig = types.LoggingConfig{
		Level:             logLevel,
		SkipFrameCount:    3,
		WithTimestamp:     true,
		ConsoleLogging:    true,
		FileLogging:       false,
		RelLogFileDir:     "logs",
		ShutdownTimeoutMS: 5000,
	}

	if err := configService.Initialize(); err != nil {
		return fmt.Errorf("config init: %w", err)
	}

	// Resolve logging service.
	logInst, err := container.ResolveSafe(logging.ServiceName)
	if err != nil {
		return fmt.Errorf("DI resolve logging: %w", err)
	}
	logService, ok = logInst.(*logging.Service)
	if !ok {
		return fmt.Errorf("resolved logging service has unexpected type %T", logInst)
	}
	if err := logService.Initialize(); err != nil {
		return fmt.Errorf("logging init: %w", err)
	}

	return nil
}

func runRX(_ *cobra.Command, _ []string) error {
	// --- List devices mode ---
	if flagListDevices {
		return listDevices()
	}

	// --- Audio diagnostics mode ---
	if flagDiag {
		dev := flagDevice
		if dev < 0 {
			dev = configService.AppConfig.FT8Config.DeviceIndex
		}
		return runDiag(dev)
	}

	// --- Apply runtime overrides to FT8 config ---
	configService.AppConfig.FT8Config.Enabled = true
	if flagDevice >= 0 {
		configService.AppConfig.FT8Config.DeviceIndex = flagDevice
	}

	ft8Cfg := configService.AppConfig.FT8Config
	logService.InfoWith().
		Int("device_index", ft8Cfg.DeviceIndex).
		Uint32("buffer_size", ft8Cfg.BufferSize).
		Int("max_candidates", ft8Cfg.MaxCandidates).
		Int("max_iterations", ft8Cfg.MaxIterations).
		Msg("FT8 RX configuration")

	// --- Create and initialise the FT8 service ---
	ft8svc := &service.Service{
		ConfigService: configService,
		Logger:        logService,
	}

	if err := ft8svc.Initialize(); err != nil {
		return fmt.Errorf("FT8 service init: %w", err)
	}

	// --- Signal handling ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ft8svc.Start(ctx); err != nil {
		return fmt.Errorf("FT8 service start: %w", err)
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  FT8 RX — listening for signals")
	fmt.Printf("  Device: %d  |  Windows: %s  |  Ctrl+C to stop\n",
		ft8Cfg.DeviceIndex, windowsLabel(flagWindows))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Printf("%-10s %6s %9s  %s\n", "TIME", "SNR", "FREQ", "MESSAGE")
	fmt.Println("────────── ────── ─────────  ──────────────────────────────────")

	startTime := time.Now()
	msgCount, windowCount := rxLoop(ctx, ft8svc, flagWindows)

	// --- Shutdown ---
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if err := ft8svc.Stop(); err != nil {
		logService.WarnWith().Err(err).Msg("error stopping FT8 service")
	}
	if err := ft8svc.Close(); err != nil {
		logService.WarnWith().Err(err).Msg("error closing FT8 service")
	}

	elapsed := time.Since(startTime).Round(time.Second)
	fmt.Printf("  Elapsed:  %s\n", elapsed)
	fmt.Printf("  Windows:  %d\n", windowCount)
	fmt.Printf("  Decoded:  %d messages\n", msgCount)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return nil
}

// rxLoop runs the main receive loop, printing decoded messages and counting
// windows. It returns when the context is cancelled, the messages channel
// closes, or the window limit is reached.
func rxLoop(ctx context.Context, ft8svc *service.Service, maxWindows int) (msgCount int64, windowCount int) {
	messages := ft8svc.Messages()
	windowDone := ft8svc.WindowDone()

	for {
		select {
		case <-ctx.Done():
			return

		case _, ok := <-windowDone:
			if !ok {
				return
			}
			windowCount++
			if maxWindows > 0 && windowCount >= maxWindows {
				fmt.Printf("\n  Reached %d window limit.\n", maxWindows)
				return
			}

		case rxMsg, ok := <-messages:
			if !ok {
				return
			}
			msgCount++
			now := time.Now().Format("15:04:05")
			fmt.Printf("%-10s %+6.1f %8.1f  %s\n",
				now,
				rxMsg.SNR,
				rxMsg.Freq,
				rxMsg.Message.String())
		}
	}
}

func windowsLabel(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", n)
}
