package cmd

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// listDevices enumerates available audio capture devices and prints them to
// stdout. Used by the --list-devices flag.
func listDevices() error {
	capture := audio.New(audio.DefaultConfig())
	if err := capture.Init(); err != nil {
		return fmt.Errorf("audio init: %w", err)
	}
	defer func() { _ = capture.Close() }()

	devices, err := capture.ListDevices()
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}

	if len(devices) == 0 {
		fmt.Println("No audio capture devices found.")
		return nil
	}

	fmt.Printf("%-6s  %s\n", "INDEX", "DEVICE NAME")
	fmt.Println("──────  ──────────────────────────────────────────────")
	for i, d := range devices {
		fmt.Printf("%-6d  %s\n", i, d.Name())
	}
	fmt.Printf("\n%d capture device(s) found.\n", len(devices))
	fmt.Println("\nUsage: ft8 --device <INDEX>")

	return nil
}
