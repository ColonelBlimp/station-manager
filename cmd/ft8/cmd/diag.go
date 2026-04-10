package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
	"github.com/ColonelBlimp/station-manager/internal/ft8/message"
)

// runDiag captures audio from the specified device for a short duration and
// reports sample statistics. This bypasses the entire FT8 service to isolate
// audio capture problems.
//
// It tests two sample rates: the FT8-native 12 kHz and the common 48 kHz,
// so we can determine whether the device supports the rate miniaudio is
// requesting. Then it captures a full 15 s window and runs the DSP pipeline
// stage by stage, reporting what happens at each step.
func runDiag(deviceIndex int) error {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  FT8 Audio Diagnostics")
	fmt.Printf("  Device index: %d\n", deviceIndex)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	rates := []uint32{dsp.CaptureSampleRate, dsp.SampleRate}
	for _, rate := range rates {
		fmt.Printf("── Testing sample rate: %d Hz ─────────────────────────────\n", rate)
		if err := captureAndReport(deviceIndex, rate); err != nil {
			fmt.Printf("  ERROR: %v\n\n", err)
		}
	}

	// --- Full window capture + DSP pipeline test ---
	fmt.Println("── Full 15 s window capture + DSP pipeline ───────────────")
	fmt.Println("  Capturing one FT8 window (15 s at 48 kHz → decimate to 12 kHz)...")
	windowSamples, err := captureWindow(deviceIndex)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		return nil
	}
	fmt.Printf("  Captured %d samples (expected %d)\n", len(windowSamples), dsp.WindowSamples)

	// Save to WAV for offline analysis.
	wavPath := "ft8_diag_capture.wav"
	if err := saveDiagWAV(wavPath, windowSamples, dsp.SampleRate); err != nil {
		fmt.Printf("  WARNING: could not save WAV: %v\n", err)
	} else {
		fmt.Printf("  Saved capture to: %s\n", wavPath)
	}

	// Audio level stats for the window.
	var peakAbs float32
	var sumSq float64
	for _, s := range windowSamples {
		abs := s
		if abs < 0 {
			abs = -abs
		}
		if abs > peakAbs {
			peakAbs = abs
		}
		sumSq += float64(s) * float64(s)
	}
	rms := math.Sqrt(sumSq / float64(len(windowSamples)))
	fmt.Printf("  Peak |sample|: %.6f (%.1f dBFS)\n", peakAbs, 20*math.Log10(float64(peakAbs)))
	fmt.Printf("  RMS level    : %.6f (%.1f dBFS)\n", rms, 20*math.Log10(rms))
	fmt.Println()

	// Run DSP stages.
	runDSPPipeline(windowSamples)

	return nil
}

// captureWindow captures one FT8 window by recording at 48 kHz stereo,
// extracting the left channel, and decimating to 12 kHz via the WSJT-X
// antialiasing FIR filter. Returns exactly WindowSamples (180,000) at
// 12 kHz.
func captureWindow(deviceIndex int) ([]float32, error) {
	cfg := audio.Config{
		DeviceIndex: deviceIndex,
		SampleRate:  dsp.CaptureSampleRate, // 48000
		Channels:    2,                     // stereo (USB audio codecs)
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
	samples := capture.Samples()

	for len(buf) < dsp.WindowSamples {
		select {
		case <-sigCtx.Done():
			return buf, fmt.Errorf("interrupted after %d samples", len(buf))
		case chunk, ok := <-samples:
			if !ok {
				return buf, fmt.Errorf("channel closed after %d samples", len(buf))
			}

			// Extract left channel from interleaved stereo.
			mono := make([]float32, 0, len(chunk)/2)
			for i := 0; i < len(chunk); i += 2 {
				mono = append(mono, chunk[i])
			}

			// Decimate from 48 kHz to 12 kHz.
			decimated := decimator.Decimate(mono)
			if len(decimated) > 0 {
				buf = append(buf, decimated...)
			}
		}
	}

	_ = capture.Stop()

	// Truncate to exactly WindowSamples.
	return buf[:dsp.WindowSamples], nil
}

// runDSPPipeline runs each stage of the FT8 DSP pipeline on a captured
// window and reports detailed diagnostics at each step.
func runDSPPipeline(samples []float32) {
	const maxCandidates = 120
	const maxIter = 40

	fmt.Println("  ── Stage 1: Spectrogram ──")
	sg := dsp.SpectrogramFT8(samples)
	if sg == nil {
		fmt.Println("  SpectrogramFT8 returned nil — buffer too short?")
		return
	}
	fmt.Printf("  Spectrogram: %d frames × %d bins\n", len(sg), len(sg[0]))

	// Check spectrogram power range.
	var sgMin, sgMax float32
	sgMin = sg[0][0]
	sgMax = sg[0][0]
	for _, row := range sg {
		for _, v := range row {
			if v < sgMin {
				sgMin = v
			}
			if v > sgMax {
				sgMax = v
			}
		}
	}
	fmt.Printf("  Log2-power range: [%.2f, %.2f]\n", sgMin, sgMax)
	fmt.Println()

	fmt.Println("  ── Stage 2: Candidate detection (Costas sync) ──")
	const stepsPerSymbol = 4
	candidates := dsp.FindCandidates(sg, maxCandidates, stepsPerSymbol)
	fmt.Printf("  Candidates found: %d (max %d)\n", len(candidates), maxCandidates)

	if len(candidates) == 0 {
		fmt.Println()
		fmt.Println("  ❌ No sync candidates found. Possible causes:")
		fmt.Println("     - Transceiver not tuned to an FT8 frequency")
		fmt.Println("     - Audio level too low or too high (clipping)")
		fmt.Println("     - No FT8 signals present on the band")
		fmt.Println("     - Wrong audio device (monitor vs. capture)")
		return
	}

	// Print top candidates.
	limit := len(candidates)
	if limit > 10 {
		limit = 10
	}
	fmt.Printf("  Top %d candidates:\n", limit)
	fmt.Printf("    %-8s %-10s %-10s\n", "SCORE", "FREQ (Hz)", "TIME (s)")
	for i := 0; i < limit; i++ {
		c := candidates[i]
		fmt.Printf("    %-8.2f %-10.1f %-10.3f\n", c.Score, c.Freq, c.TimeOff)
	}
	fmt.Println()

	fmt.Println("  ── Stage 3: Demodulate + LDPC decode ──")
	hann := dsp.HannCoefficients(dsp.SamplesPerSymbol)
	var decoded, crcFail int

	for i := range candidates {
		cand := &candidates[i]
		refined := dsp.RefineCandidateAudio(samples, hann, *cand)
		llr := dsp.DemodulateAudio(samples, hann, refined)
		dsp.NormalizeLLR(&llr)

		_, ok := codec.DecodeMessage(llr, maxIter)
		if ok {
			decoded++
		} else {
			crcFail++
		}
	}

	fmt.Printf("  Candidates processed : %d\n", len(candidates))
	fmt.Printf("  LDPC+CRC pass        : %d\n", decoded)
	fmt.Printf("  LDPC+CRC fail        : %d\n", crcFail)
	fmt.Println()

	if decoded == 0 {
		fmt.Println("  ❌ No messages decoded. Sync candidates were found but")
		fmt.Println("     LDPC decode failed on all of them. Possible causes:")
		fmt.Println("     - SNR too low (weak signals)")
		fmt.Println("     - Sample rate or timing mismatch")
		fmt.Println("     - DSP pipeline bug (try the saved WAV in WSJT-X)")
		fmt.Println()
		fmt.Printf("  The captured audio was saved to %s.\n", "ft8_diag_capture.wav")
		fmt.Println("  Open it in WSJT-X or Audacity to verify signals are present.")
	} else {
		fmt.Printf("  ✓ %d message(s) decoded successfully!\n", decoded)
		fmt.Println()
		fmt.Println("  Now run the full decode with message unpacking:")
		results := dsp.ProcessWindow(samples, maxCandidates, maxIter)
		for _, dm := range results {
			msg, err := message.Unpack(dm.Msg77)
			if err != nil {
				fmt.Printf("    %.1f Hz  SNR %+.0f  (unpack error: %v)\n", dm.Freq, dm.SNR, err)
			} else {
				fmt.Printf("    %.1f Hz  SNR %+.0f  %s\n", dm.Freq, dm.SNR, msg.String())
			}
		}
	}
	fmt.Println()
}

func captureAndReport(deviceIndex int, sampleRate uint32) error {
	cfg := audio.Config{
		DeviceIndex: deviceIndex,
		SampleRate:  sampleRate,
		Channels:    1,
		BufferSize:  512,
	}

	capture := audio.New(cfg)
	if err := capture.Init(); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	defer func() { _ = capture.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Also listen for Ctrl+C during diagnostics.
	sigCtx, sigStop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer sigStop()

	if err := capture.Start(sigCtx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	samples := capture.Samples()

	const captureDuration = 5 * time.Second
	fmt.Printf("  Capturing for %s...\n", captureDuration)

	deadline := time.After(captureDuration)

	var (
		totalSamples int64
		totalChunks  int64
		peakAbs      float32
		sumSquared   float64
		firstChunk   int // sample count of first chunk (for size info)
	)

	for {
		select {
		case <-sigCtx.Done():
			fmt.Println("  Interrupted.")
			reportStats(sampleRate, totalSamples, totalChunks, firstChunk, peakAbs, sumSquared, captureDuration)
			return nil

		case <-deadline:
			_ = capture.Stop() // Best-effort stop; drain remaining samples.
			reportStats(sampleRate, totalSamples, totalChunks, firstChunk, peakAbs, sumSquared, captureDuration)
			return nil

		case chunk, ok := <-samples:
			if !ok {
				reportStats(sampleRate, totalSamples, totalChunks, firstChunk, peakAbs, sumSquared, captureDuration)
				return nil
			}
			totalChunks++
			if firstChunk == 0 && len(chunk) > 0 {
				firstChunk = len(chunk)
			}
			for _, s := range chunk {
				totalSamples++
				abs := s
				if abs < 0 {
					abs = -abs
				}
				if abs > peakAbs {
					peakAbs = abs
				}
				sumSquared += float64(s) * float64(s)
			}
		}
	}
}

func reportStats(sampleRate uint32, totalSamples, totalChunks int64, firstChunkSize int, peakAbs float32, sumSquared float64, duration time.Duration) {
	fmt.Println()
	fmt.Printf("  Sample rate requested : %d Hz\n", sampleRate)
	fmt.Printf("  Chunks received       : %d\n", totalChunks)
	fmt.Printf("  First chunk size      : %d samples\n", firstChunkSize)
	fmt.Printf("  Total samples         : %d\n", totalSamples)

	effectiveDuration := duration.Seconds()
	if effectiveDuration > 0 && totalSamples > 0 {
		effectiveRate := float64(totalSamples) / effectiveDuration
		fmt.Printf("  Effective sample rate : %.0f Hz\n", effectiveRate)

		expectedSamples := float64(sampleRate) * effectiveDuration
		ratio := float64(totalSamples) / expectedSamples
		fmt.Printf("  Rate ratio (act/req)  : %.3f", ratio)
		if ratio < 0.9 || ratio > 1.1 {
			fmt.Print("  ⚠ MISMATCH")
		} else {
			fmt.Print("  ✓ OK")
		}
		fmt.Println()
	}

	fmt.Printf("  Peak |sample|         : %.6f\n", peakAbs)
	if totalSamples > 0 {
		rms := math.Sqrt(sumSquared / float64(totalSamples))
		fmt.Printf("  RMS level             : %.6f\n", rms)

		if peakAbs > 0 {
			peakDB := 20 * math.Log10(float64(peakAbs))
			rmsDB := 20 * math.Log10(rms)
			fmt.Printf("  Peak dBFS             : %.1f\n", peakDB)
			fmt.Printf("  RMS dBFS              : %.1f\n", rmsDB)
		}
	}

	// Verdict.
	fmt.Println()
	if totalSamples == 0 {
		fmt.Println("  ❌ NO SAMPLES RECEIVED — device may not support this rate,")
		fmt.Println("     or audio routing from the transceiver is not active.")
	} else if peakAbs < 0.001 {
		fmt.Println("  ⚠  SILENCE — samples are flowing but the signal level is")
		fmt.Println("     effectively zero. Check transceiver AF output level and")
		fmt.Println("     audio routing (USB vs. line-in, DATA mode vs. SSB).")
	} else if peakAbs < 0.01 {
		fmt.Println("  ⚠  VERY LOW LEVEL — signal present but extremely weak.")
		fmt.Println("     Increase transceiver AF gain or USB audio level.")
	} else {
		fmt.Println("  ✓  Audio capture looks healthy.")
	}
	fmt.Println()

	// Write a small raw sample dump for eyeball inspection.
	if totalSamples > 0 && totalChunks > 0 {
		fmt.Println("  To save captured audio for offline analysis, use:")
		fmt.Printf("    ft8 --diag --device %d  (and pipe stderr to a file)\n", 0)
	}
}

// saveDiagWAV writes captured samples as a 12 kHz mono WAV file for offline
// analysis (e.g. loading into Audacity or feeding to the DSP test suite).
func saveDiagWAV(path string, samples []float32, sampleRate uint32) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	numSamples := uint32(len(samples))
	dataSize := numSamples * 4 // float32 = 4 bytes, but WAV PCM uses int16
	// Write 16-bit PCM WAV for maximum compatibility.
	dataSize16 := numSamples * 2

	// RIFF header
	write := func(b []byte) { _, _ = f.Write(b) }
	writeU32LE := func(v uint32) {
		write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	}
	writeU16LE := func(v uint16) {
		write([]byte{byte(v), byte(v >> 8)})
	}
	_ = dataSize // suppress unused

	write([]byte("RIFF"))
	writeU32LE(36 + dataSize16) // file size - 8
	write([]byte("WAVE"))

	// fmt chunk
	write([]byte("fmt "))
	writeU32LE(16)             // chunk size
	writeU16LE(1)              // PCM format
	writeU16LE(1)              // mono
	writeU32LE(sampleRate)     // sample rate
	writeU32LE(sampleRate * 2) // byte rate (sampleRate * channels * bitsPerSample/8)
	writeU16LE(2)              // block align (channels * bitsPerSample/8)
	writeU16LE(16)             // bits per sample

	// data chunk
	write([]byte("data"))
	writeU32LE(dataSize16)

	// Convert float32 → int16 and write.
	buf := make([]byte, 2)
	for _, s := range samples {
		// Clamp to [-1, 1].
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
