package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/spf13/cobra"
)

var captureFlags struct {
	output   string
	channels int
}

var captureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Stage 1: Capture one 15 s FT8 window and save as WAV",
	Long: `Captures audio from the specified device at 48 kHz, extracts mono audio,
decimates to 12 kHz via the WSJT-X anti-aliasing FIR filter, reports
level statistics, and writes the result as a 16-bit PCM WAV file.

This validates the audio capture + decimation stage in isolation.
The output WAV can be fed into subsequent stages (spectrogram, candidates,
decode) for offline testing.`,
	Example: `  ft8test capture --device 3
  ft8test capture --device 3 --output my_capture.wav
  ft8test capture --device 3 --channels 1`,
	RunE: runCapture,
}

func init() {
	captureCmd.Flags().StringVar(&captureFlags.output, "output", "capture.wav",
		"output WAV file path")
	captureCmd.Flags().IntVar(&captureFlags.channels, "channels", 2,
		"capture channels (1=mono, 2=stereo; stereo extracts left channel)")
	rootCmd.AddCommand(captureCmd)
}

func runCapture(_ *cobra.Command, _ []string) error {
	dev := effectiveDevice()
	if dev < 0 {
		return fmt.Errorf("no device specified — use --device <INDEX> (see: ft8test devices)")
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Stage 1: Audio Capture + Decimation")
	fmt.Printf("  Device: %d  |  Channels: %d  |  48 kHz → 12 kHz\n", dev, captureFlags.channels)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("  Capturing one FT8 window (15 s)... press Ctrl+C to abort")

	samples, err := captureWindow(dev, captureFlags.channels)
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}

	fmt.Printf("  Captured %d samples at 12 kHz (expected %d)\n", len(samples), dsp.WindowSamples)
	fmt.Println()

	reportAudioStats(samples)

	if err := saveWAV(captureFlags.output, samples, dsp.SampleRate); err != nil {
		return fmt.Errorf("save WAV: %w", err)
	}
	fmt.Printf("  ✓ Saved: %s (%d samples, 12 kHz, 16-bit PCM mono)\n", captureFlags.output, len(samples))
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Printf("    ft8test spectrogram --input %s\n", captureFlags.output)
	fmt.Printf("    ft8test candidates  --input %s\n", captureFlags.output)
	fmt.Printf("    ft8test decode      --input %s\n", captureFlags.output)

	return nil
}

// captureWindow captures one FT8 window: record at 48 kHz, extract mono,
// decimate to 12 kHz. Returns exactly WindowSamples (180,000) at 12 kHz.
func captureWindow(deviceIndex, channels int) ([]float32, error) {
	cfg := audio.Config{
		DeviceIndex: deviceIndex,
		SampleRate:  dsp.CaptureSampleRate, // 48000
		Channels:    uint32(channels),
		BufferSize:  512,
	}

	capture := audio.New(cfg)
	if err := capture.Init(); err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}
	defer func() { _ = capture.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCtx, sigStop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer sigStop()

	if err := capture.Start(sigCtx); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	decimator := dsp.NewDecimator()
	buf := make([]float32, 0, dsp.WindowSamples+1024)
	samplesCh := capture.Samples()

	for len(buf) < dsp.WindowSamples {
		select {
		case <-sigCtx.Done():
			return buf, fmt.Errorf("interrupted after %d samples", len(buf))
		case chunk, ok := <-samplesCh:
			if !ok {
				return buf, fmt.Errorf("channel closed after %d samples", len(buf))
			}

			// Extract mono from interleaved multi-channel audio.
			var mono []float32
			if channels > 1 {
				mono = make([]float32, 0, len(chunk)/channels)
				for i := 0; i < len(chunk); i += channels {
					mono = append(mono, chunk[i])
				}
			} else {
				mono = chunk
			}

			decimated := decimator.Decimate(mono)
			if len(decimated) > 0 {
				buf = append(buf, decimated...)
			}
		}
	}

	_ = capture.Stop()
	return buf[:dsp.WindowSamples], nil
}

// reportAudioStats prints peak and RMS level statistics.
func reportAudioStats(samples []float32) {
	var peakAbs float32
	var sumSq float64
	for _, s := range samples {
		abs := s
		if abs < 0 {
			abs = -abs
		}
		if abs > peakAbs {
			peakAbs = abs
		}
		sumSq += float64(s) * float64(s)
	}
	rms := math.Sqrt(sumSq / float64(len(samples)))

	fmt.Printf("  Peak |sample|: %.6f", peakAbs)
	if peakAbs > 0 {
		fmt.Printf(" (%.1f dBFS)", 20*math.Log10(float64(peakAbs)))
	}
	fmt.Println()
	fmt.Printf("  RMS level    : %.6f", rms)
	if rms > 0 {
		fmt.Printf(" (%.1f dBFS)", 20*math.Log10(rms))
	}
	fmt.Println()

	if peakAbs < 0.001 {
		fmt.Println("  ⚠  SILENCE — samples flowing but signal is near zero")
	} else if peakAbs < 0.01 {
		fmt.Println("  ⚠  VERY LOW LEVEL — increase transceiver AF gain")
	} else if peakAbs > 0.95 {
		fmt.Println("  ⚠  POSSIBLE CLIPPING — reduce transceiver AF gain")
	} else {
		fmt.Println("  ✓  Audio levels look healthy")
	}
	fmt.Println()
}

// saveWAV writes float32 samples as a 16-bit PCM mono WAV file.
func saveWAV(path string, samples []float32, sampleRate uint32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	numSamples := uint32(len(samples))
	dataSize16 := numSamples * 2

	write := func(b []byte) { _, _ = f.Write(b) }
	writeU32LE := func(v uint32) {
		write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	}
	writeU16LE := func(v uint16) {
		write([]byte{byte(v), byte(v >> 8)})
	}

	write([]byte("RIFF"))
	writeU32LE(36 + dataSize16)
	write([]byte("WAVE"))

	write([]byte("fmt "))
	writeU32LE(16)
	writeU16LE(1) // PCM
	writeU16LE(1) // mono
	writeU32LE(sampleRate)
	writeU32LE(sampleRate * 2) // byte rate
	writeU16LE(2)              // block align
	writeU16LE(16)             // bits per sample

	write([]byte("data"))
	writeU32LE(dataSize16)

	buf := make([]byte, 2)
	for _, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		v := int16(s * 32767)
		buf[0] = byte(v)
		buf[1] = byte(v >> 8)
		write(buf)
	}

	return nil
}

// readWAV reads a 16-bit PCM WAV file and returns the samples as float32
// values in [-1, 1] along with the sample rate. Multi-channel files are
// downmixed to mono (first channel only).
func readWAV(path string) (samples []float32, sampleRate uint32, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	if len(data) < 44 {
		return nil, 0, fmt.Errorf("file too small to be a WAV (%d bytes)", len(data))
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("not a WAV file")
	}

	if string(data[12:16]) != "fmt " {
		return nil, 0, fmt.Errorf("expected fmt chunk at offset 12")
	}
	format := u16LE(data[20:22])
	if format != 1 {
		return nil, 0, fmt.Errorf("unsupported WAV format %d (expected 1=PCM)", format)
	}
	numChannels := u16LE(data[22:24])
	sampleRate = u32LE(data[24:28])
	bitsPerSample := u16LE(data[34:36])

	if bitsPerSample != 16 {
		return nil, 0, fmt.Errorf("unsupported bits per sample %d (expected 16)", bitsPerSample)
	}

	// Find the data chunk (may not be at a fixed offset).
	dataOffset := 36
	for dataOffset+8 <= len(data) {
		chunkID := string(data[dataOffset : dataOffset+4])
		chunkSize := int(u32LE(data[dataOffset+4 : dataOffset+8]))
		if chunkID == "data" {
			pcmData := data[dataOffset+8 : dataOffset+8+chunkSize]
			bytesPerFrame := int(numChannels) * int(bitsPerSample) / 8
			numFrames := len(pcmData) / bytesPerFrame
			samples = make([]float32, numFrames)
			for i := 0; i < numFrames; i++ {
				off := i * bytesPerFrame
				v := int16(uint16(pcmData[off]) | uint16(pcmData[off+1])<<8)
				samples[i] = float32(v) / 32768.0
			}
			return samples, sampleRate, nil
		}
		dataOffset += 8 + chunkSize
		if chunkSize%2 != 0 {
			dataOffset++
		}
	}

	return nil, 0, fmt.Errorf("no data chunk found in WAV file")
}

func u16LE(b []byte) uint16 {
	return uint16(b[0]) | uint16(b[1])<<8
}

func u32LE(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
