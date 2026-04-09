// decimate.go — FIR anti-aliasing filter and 4× decimation for FT8 audio.
//
// FT8 audio processing operates at 12 kHz, but USB audio devices (such as the
// Texas Instruments PCM2903C used in Yaesu FTdx10 / FT-710) typically only
// support 48 kHz natively. Rather than relying on the audio framework's
// internal resampler (whose quality and filter characteristics are outside
// our control), we capture at the device's native 48 kHz and decimate to
// 12 kHz with a proper anti-aliasing lowpass filter.
//
// The filter coefficients are taken directly from WSJT-X's lib/fil4.f90:
//
//	fsample     = 48 000 Hz
//	Ntaps       = 49
//	fc          = 4 500 Hz (cutoff)
//	fstop       = 6 000 Hz (stopband edge)
//	Ripple      = 1 dB
//	Stop Atten  = 40 dB
//	fout        = 12 000 Hz
//	NDOWN       = 4 (decimation factor)
//
// This filter was designed with ScopeFIR and has been proven in millions of
// FT8 decodes worldwide via WSJT-X.

package dsp

const (
	// DecimationFactor is the ratio between the capture sample rate (48 kHz)
	// and the FT8 processing sample rate (12 kHz).
	DecimationFactor = 4

	// CaptureSampleRate is the native sample rate to request from the audio
	// device. Most USB audio codecs support 48 kHz.
	CaptureSampleRate = SampleRate * DecimationFactor // 48000

	// fil4Taps is the number of FIR filter coefficients.
	fil4Taps = 49
)

// fil4Coefficients are the 49-tap FIR lowpass filter coefficients from
// WSJT-X's lib/fil4.f90. The filter is symmetric about its centre tap
// (index 24). These coefficients are used to anti-alias the 48 kHz audio
// before 4× decimation to 12 kHz.
//
// The coefficients are float64 for precision during the accumulation, matching
// WSJT-X's Fortran REAL default precision.
var fil4Coefficients = [fil4Taps]float64{
	0.000861074040, 0.010051920210, 0.010161983649, 0.011363155076,
	0.008706594219, 0.002613872664, -0.005202883094, -0.011720748164,
	-0.013752163325, -0.009431602741, 0.000539063909, 0.012636767098,
	0.021494659597, 0.021951235065, 0.011564169382, -0.007656470131,
	-0.028965787341, -0.042637874109, -0.039203309748, -0.013153301537,
	0.034320769178, 0.094717832646, 0.154224604789, 0.197758325022,
	0.213715139513, // centre tap
	0.197758325022, 0.154224604789, 0.094717832646, 0.034320769178,
	-0.013153301537, -0.039203309748, -0.042637874109, -0.028965787341,
	-0.007656470131, 0.011564169382, 0.021951235065, 0.021494659597,
	0.012636767098, 0.000539063909, -0.009431602741, -0.013752163325,
	-0.011720748164, -0.005202883094, 0.002613872664, 0.008706594219,
	0.011363155076, 0.010161983649, 0.010051920210, 0.000861074040,
}

// Decimator performs 4× decimation of 48 kHz float32 audio to 12 kHz using
// the WSJT-X fil4 anti-aliasing FIR filter. It maintains filter state across
// calls so that audio delivered in chunks (from the audio callback) is
// filtered seamlessly.
//
// Not safe for concurrent use. Create one per capture stream.
type Decimator struct {
	// history holds the last (fil4Taps - 1) input samples to handle the
	// FIR overlap between successive calls.
	history [fil4Taps]float64
}

// NewDecimator creates a Decimator with zeroed filter state.
func NewDecimator() *Decimator {
	return &Decimator{}
}

// Decimate filters and decimates the input samples by a factor of 4.
//
// The input length must be a multiple of DecimationFactor (4). If it is
// not, trailing samples beyond the last complete group are silently dropped.
//
// The output slice has length len(input)/DecimationFactor. The caller may
// reuse the returned slice only until the next call to Decimate.
//
// This implementation mirrors WSJT-X's fil4.f90 subroutine:
//   - For each output sample, shift 4 new input samples into the tapped
//     delay line, then compute the dot product with the filter coefficients.
func (d *Decimator) Decimate(input []float32) []float32 {
	n := len(input)
	if n < DecimationFactor {
		return nil
	}

	nOut := n / DecimationFactor
	out := make([]float32, nOut)

	for i := 0; i < nOut; i++ {
		// Shift: move old samples down by DecimationFactor positions.
		// This is equivalent to: history[0 : taps-4] = history[4 : taps]
		copy(d.history[:fil4Taps-DecimationFactor], d.history[DecimationFactor:])

		// Insert DecimationFactor new samples at the end of the delay line.
		base := i * DecimationFactor
		for j := 0; j < DecimationFactor; j++ {
			d.history[fil4Taps-DecimationFactor+j] = float64(input[base+j])
		}

		// FIR dot product.
		var acc float64
		for k := 0; k < fil4Taps; k++ {
			acc += fil4Coefficients[k] * d.history[k]
		}
		out[i] = float32(acc)
	}

	return out
}

// Reset clears the filter state. Call this when starting a new capture
// session or if there is a discontinuity in the input audio.
func (d *Decimator) Reset() {
	d.history = [fil4Taps]float64{}
}
