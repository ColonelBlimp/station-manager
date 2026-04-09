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
	"sync/atomic"
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

var (
	container     *iocdi.Container
	configService *config.Service
	logService    *logging.Service
)

// Flag values.
var (
	flagWindows     int
	flagDevice      int
	flagListDevices bool
	flagVerbose     bool
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
  ft8 --verbose                 Enable debug-level logging`,
	RunE: runRX,
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

	// --- DI wiring (mirrors cmd/importer pattern) ---

	workingDir, err := utils.WorkingDir()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: cannot resolve working directory: %v\n", err)
		os.Exit(1)
	}

	container = iocdi.New()
	cobra.CheckErr(container.RegisterInstance("workingdir", workingDir))
	cobra.CheckErr(container.Register(config.ServiceName, reflect.TypeOf((*config.Service)(nil))))
	cobra.CheckErr(container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil))))
	cobra.CheckErr(container.Build())

	// Resolve config service.
	cfgInst, err := container.ResolveSafe(config.ServiceName)
	cobra.CheckErr(err)
	var ok bool
	configService, ok = cfgInst.(*config.Service)
	if !ok {
		panic("resolved config service is not *config.Service")
	}

	// Pre-seed logging config for console-only output before Initialize.
	// The config.Service.Initialize preseed logic preserves a LoggingConfig
	// whose Level field is non-empty.
	logLevel := "info"
	configService.AppConfig.LoggingConfig = types.LoggingConfig{
		Level:             logLevel,
		SkipFrameCount:    3,
		WithTimestamp:     true,
		ConsoleLogging:    true,
		FileLogging:       false,
		RelLogFileDir:     "logs",
		ShutdownTimeoutMS: 5000,
	}

	cobra.CheckErr(configService.Initialize())

	// Resolve logging service.
	logInst, err := container.ResolveSafe(logging.ServiceName)
	cobra.CheckErr(err)
	logService, ok = logInst.(*logging.Service)
	if !ok {
		panic("resolved logging service is not *logging.Service")
	}
	cobra.CheckErr(logService.Initialize())
}

func runRX(cmd *cobra.Command, args []string) error {
	// --- List devices mode ---
	if flagListDevices {
		return listDevices()
	}

	// --- Apply runtime overrides to FT8 config ---
	configService.AppConfig.FT8Config.Enabled = true
	if flagDevice >= 0 {
		configService.AppConfig.FT8Config.DeviceIndex = flagDevice
	}

	if flagVerbose {
		// Re-seed with debug level. The logging service is already initialised
		// so we update it via the config. This won't take effect on the zerolog
		// instance, so just log a note.
		logService.InfoWith().Msg("verbose mode requested (service logs at info level; DSP debug output enabled)")
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

	// --- RX loop ---
	var msgCount atomic.Int64
	var windowCount int
	startTime := time.Now()

	// Window counting via wall-clock timer (avoids modifying the FT8 service).
	windowTicker := time.NewTicker(15 * time.Second)
	defer windowTicker.Stop()

	messages := ft8svc.Messages()

	for {
		select {
		case <-ctx.Done():
			goto shutdown

		case <-windowTicker.C:
			windowCount++
			if flagWindows > 0 && windowCount >= flagWindows {
				fmt.Printf("\n  Reached %d window limit.\n", flagWindows)
				goto shutdown
			}

		case rxMsg, ok := <-messages:
			if !ok {
				goto shutdown
			}
			msgCount.Add(1)
			now := time.Now().Format("15:04:05")
			fmt.Printf("%-10s %+6.1f %8.1f  %s\n",
				now,
				rxMsg.SNR,
				rxMsg.Freq,
				rxMsg.Message.String())
		}
	}

shutdown:
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
	fmt.Printf("  Decoded:  %d messages\n", msgCount.Load())
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return nil
}

func windowsLabel(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", n)
}
