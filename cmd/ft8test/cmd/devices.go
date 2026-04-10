package cmd

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/gen2brain/malgo"
	"github.com/spf13/cobra"
)

var devicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List available audio capture and playback devices",
	RunE:  runDevices,
}

func init() {
	rootCmd.AddCommand(devicesCmd)
}

func runDevices(_ *cobra.Command, _ []string) error {
	capture := audio.New(audio.DefaultConfig())
	if err := capture.Init(); err != nil {
		return fmt.Errorf("audio init: %w", err)
	}
	defer func() { _ = capture.Close() }()

	devices, err := capture.ListDevices()
	if err != nil {
		return fmt.Errorf("list capture devices: %w", err)
	}

	fmt.Printf("CAPTURE DEVICES (%d)\n", len(devices))
	fmt.Printf("%-6s  %s\n", "INDEX", "DEVICE NAME")
	fmt.Println("──────  ──────────────────────────────────────────────")
	for i, d := range devices {
		fmt.Printf("%-6d  %s\n", i, d.Name())
	}
	fmt.Println()

	pbDevices, err := listPlaybackDevices()
	if err != nil {
		fmt.Printf("(could not list playback devices: %v)\n", err)
	} else {
		fmt.Printf("PLAYBACK DEVICES (%d)\n", len(pbDevices))
		fmt.Printf("%-6s  %s\n", "INDEX", "DEVICE NAME")
		fmt.Println("──────  ──────────────────────────────────────────────")
		for i, d := range pbDevices {
			fmt.Printf("%-6d  %s\n", i, d.Name())
		}
		fmt.Println()
	}

	fmt.Println("Usage: ft8test capture --device <INDEX>")

	return nil
}

func listPlaybackDevices() ([]malgo.DeviceInfo, error) {
	pb := audio.NewPlayback(audio.Config{DeviceIndex: -1})
	if err := pb.Init(); err != nil {
		return nil, err
	}
	defer func() { _ = pb.Close() }()
	return pb.ListDevices()
}
