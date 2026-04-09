package dsp

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecimator_NilInput(t *testing.T) {
	d := NewDecimator()
	assert.Nil(t, d.Decimate(nil))
}

func TestDecimator_EmptyInput(t *testing.T) {
	d := NewDecimator()
	assert.Nil(t, d.Decimate([]float32{}))
}

func TestDecimator_InputTooShort(t *testing.T) {
	d := NewDecimator()
	assert.Nil(t, d.Decimate([]float32{1, 2, 3})) // < DecimationFactor
}

func TestDecimator_OutputLength(t *testing.T) {
	d := NewDecimator()

	// 48 input samples → 12 output samples
	input := make([]float32, 48)
	out := d.Decimate(input)
	assert.Equal(t, 12, len(out))

	// Verify for a larger buffer typical of audio callbacks.
	d.Reset()
	input2 := make([]float32, 2048) // 512 frames × 4 samples
	out2 := d.Decimate(input2)
	assert.Equal(t, 512, len(out2))
}

func TestDecimator_SilenceProducesZero(t *testing.T) {
	d := NewDecimator()
	// Feed enough silence to flush the filter delay line.
	input := make([]float32, 48000) // 1 second at 48 kHz
	out := d.Decimate(input)
	require.Equal(t, 12000, len(out))
	for i, v := range out {
		assert.InDelta(t, 0.0, float64(v), 1e-10, "sample %d should be zero", i)
	}
}

func TestDecimator_DCPassthrough(t *testing.T) {
	// A constant (DC) input should pass through at approximately the same
	// level after the filter transient settles. The fil4 coefficients sum
	// to ~1.0 (they are normalised for unity gain at DC).
	d := NewDecimator()
	dcLevel := float32(0.5)
	input := make([]float32, 4800) // 100 ms at 48 kHz
	for i := range input {
		input[i] = dcLevel
	}
	out := d.Decimate(input)
	require.NotEmpty(t, out)

	// After the transient (first ~50 output samples = 200 input samples for
	// a 49-tap filter), output should be close to dcLevel. The fil4 filter
	// has 1 dB passband ripple by design, so the DC gain is ~1.056 (not
	// exactly 1.0). Allow 10% tolerance.
	for i := 50; i < len(out); i++ {
		assert.InDelta(t, float64(dcLevel), float64(out[i]), 0.06,
			"DC passthrough failed at output sample %d", i)
	}
}

func TestDecimator_LowFrequencyPassthrough(t *testing.T) {
	// A 1 kHz tone (well within the 4500 Hz passband) should pass through
	// the filter with minimal attenuation.
	d := NewDecimator()
	const freq = 1000.0
	const captureRate = 48000.0
	const nSamples = 48000 // 1 second

	input := make([]float32, nSamples)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / captureRate))
	}
	out := d.Decimate(input)
	require.Equal(t, 12000, len(out))

	// Measure the RMS of the output (skip the first 100 samples for transient).
	var sumSq float64
	for i := 100; i < len(out); i++ {
		sumSq += float64(out[i]) * float64(out[i])
	}
	rms := math.Sqrt(sumSq / float64(len(out)-100))

	// Input RMS of a unit sine is 1/√2 ≈ 0.7071. Output should be close.
	expectedRMS := 1.0 / math.Sqrt(2.0)
	assert.InDelta(t, expectedRMS, rms, 0.05,
		"1 kHz passband signal attenuated too much: RMS=%.4f, expected≈%.4f", rms, expectedRMS)
}

func TestDecimator_HighFrequencyRejection(t *testing.T) {
	// A 10 kHz tone (above the 6 kHz stopband) should be attenuated by at
	// least 30 dB (the filter spec is 40 dB stopband attenuation, but we're
	// lenient to account for edge effects).
	d := NewDecimator()
	const freq = 10000.0
	const captureRate = 48000.0
	const nSamples = 48000

	input := make([]float32, nSamples)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / captureRate))
	}
	out := d.Decimate(input)
	require.Equal(t, 12000, len(out))

	// Input RMS.
	var inSumSq float64
	for i := 200; i < len(input); i++ {
		inSumSq += float64(input[i]) * float64(input[i])
	}
	inRMS := math.Sqrt(inSumSq / float64(len(input)-200))

	// Output RMS (skip transient).
	var outSumSq float64
	for i := 100; i < len(out); i++ {
		outSumSq += float64(out[i]) * float64(out[i])
	}
	outRMS := math.Sqrt(outSumSq / float64(len(out)-100))

	// Attenuation in dB.
	if outRMS > 0 && inRMS > 0 {
		attenDB := 20 * math.Log10(outRMS/inRMS)
		assert.Less(t, attenDB, -30.0,
			"10 kHz should be attenuated >30 dB, got %.1f dB", attenDB)
	}
}

func TestDecimator_CoefficientSymmetry(t *testing.T) {
	// The filter should be symmetric (linear phase).
	for i := 0; i < fil4Taps/2; i++ {
		assert.InDelta(t, fil4Coefficients[i], fil4Coefficients[fil4Taps-1-i], 1e-12,
			"coefficient %d not symmetric with %d", i, fil4Taps-1-i)
	}
}

func TestDecimator_CoefficientSum(t *testing.T) {
	// The coefficients should sum to approximately 1.0 (unity DC gain).
	// The fil4 filter has 1 dB passband ripple, so the sum is ~1.056.
	var sum float64
	for _, c := range fil4Coefficients {
		sum += c
	}
	assert.InDelta(t, 1.0, sum, 0.1, "coefficient sum should be ~1.0, got %.6f", sum)
}

func TestDecimator_ChunkedEqualsMonolithic(t *testing.T) {
	// Feeding audio in small chunks (simulating audio callbacks) should
	// produce the same output as feeding the entire buffer at once.
	const nSamples = 48000
	input := make([]float32, nSamples)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * 800 * float64(i) / 48000))
	}

	// Monolithic.
	dMono := NewDecimator()
	monoOut := dMono.Decimate(input)

	// Chunked (512-sample chunks, typical audio callback size).
	dChunk := NewDecimator()
	var chunkOut []float32
	chunkSize := 512
	for off := 0; off < nSamples; off += chunkSize {
		end := off + chunkSize
		if end > nSamples {
			end = nSamples
		}
		partial := dChunk.Decimate(input[off:end])
		chunkOut = append(chunkOut, partial...)
	}

	require.Equal(t, len(monoOut), len(chunkOut), "output lengths should match")
	for i := range monoOut {
		assert.InDelta(t, float64(monoOut[i]), float64(chunkOut[i]), 1e-6,
			"mismatch at output sample %d", i)
	}
}

func TestDecimator_Reset(t *testing.T) {
	d := NewDecimator()
	// Feed some audio.
	input := make([]float32, 480)
	for i := range input {
		input[i] = 1.0
	}
	d.Decimate(input)

	// Reset.
	d.Reset()

	// After reset, silence should produce zeros (no residual state).
	silence := make([]float32, 480)
	out := d.Decimate(silence)
	for i, v := range out {
		assert.InDelta(t, 0.0, float64(v), 1e-10,
			"sample %d should be zero after reset", i)
	}
}

func TestDecimator_FT8TonePreservation(t *testing.T) {
	// Verify that a tone at a typical FT8 audio frequency (e.g., 1500 Hz)
	// passes through cleanly enough to be detected by a 12 kHz FFT at the
	// correct frequency bin.
	d := NewDecimator()
	const freq = 1500.0
	const captureRate = 48000.0
	const nSamples = 48000 * 2 // 2 seconds

	input := make([]float32, nSamples)
	for i := range input {
		input[i] = float32(0.8 * math.Sin(2*math.Pi*freq*float64(i)/captureRate))
	}
	out := d.Decimate(input)
	require.Equal(t, 24000, len(out))

	// Run a simple DFT at the expected frequency to check for a strong peak.
	// At 12 kHz, 1500 Hz falls in bin 1500*N/12000.
	N := len(out)
	binIdx := 1500.0 * float64(N) / 12000.0

	// Compute DFT magnitude at the target bin (skip transient: first 200 samples).
	steady := out[200:]
	Ns := len(steady)
	var re, im float64
	targetBin := 1500.0 * float64(Ns) / 12000.0
	for i, s := range steady {
		angle := 2 * math.Pi * targetBin * float64(i) / float64(Ns)
		re += float64(s) * math.Cos(angle)
		im += float64(s) * math.Sin(angle)
	}
	mag := math.Sqrt(re*re+im*im) / float64(Ns) * 2

	_ = binIdx
	// The magnitude should be close to the input amplitude (0.8).
	assert.InDelta(t, 0.8, mag, 0.1,
		"1500 Hz tone should pass through with ~0.8 amplitude, got %.4f", mag)
}

func TestDecimationFactor(t *testing.T) {
	assert.Equal(t, 4, DecimationFactor)
}

func TestCaptureSampleRate(t *testing.T) {
	assert.Equal(t, uint32(48000), uint32(CaptureSampleRate))
}
