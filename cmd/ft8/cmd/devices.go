package cmd

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/gen2brain/malgo"
)

// listDevices enumerates available audio capture and playback devices and
// prints them to stdout. Used by the --list-devices flag.
func listDevices() error {
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

	// Also list playback devices — on PipeWire/PulseAudio, the rig's received
	// audio may appear as a playback sink that needs a loopback or monitor source.
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

	fmt.Println("NOTE: On Yaesu FTdx10/FT-710, the rig's received audio appears")
	fmt.Println("as a USB audio playback stream. If the capture device shows only")
	fmt.Println("noise, use a 'Monitor of ...' capture source or set up a PipeWire")
	fmt.Println("loopback to route the rig's playback to a capture device.")
	fmt.Println()
	fmt.Println("Usage: ft8 --device <CAPTURE_INDEX>")

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
