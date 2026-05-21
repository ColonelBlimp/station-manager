// Package dsp holds FT8 signal-processing primitives — the
// audio-to-LLR pipeline that bridges raw WAV samples (or future
// live-audio capture) and the bit-level codec in internal/ft8/codec.
//
// The package is layered on top of internal/audio (generic
// audio-domain plumbing: WAV I/O, FFT). It contains FT8-specific
// algorithms: the protocol constants from QEX paper + WSJT-X's
// ft8_params.f90, the spectrogram and Costas-array sync detector,
// the baseband downsampler, and the soft demodulator that produces
// the 174 LLRs feeding LDPCDecode.
//
// All constants and algorithm shapes are derived from the QEX 2020
// paper plus the public-domain QEX ref [14] tarball, NOT from the
// GPL WSJT-X Fortran sources — per ADR 0021's licensing constraint.
package dsp

// FT8 protocol constants per QEX paper + ft8_params.f90.

const (
	// Fs is the FT8 audio sample rate in Hz. WSJT-X canonical;
	// captures from `jt9 -8` and ft8sim use this rate.
	Fs = 12000.0

	// NSPS is samples per FT8 symbol at Fs=12000.
	// 1920 samples / 12000 Hz = 160 ms per symbol → 6.25 baud.
	NSPS = 1920

	// NN is the total number of channel symbols per FT8
	// transmission: 7 Costas-array sync symbols × 3 (start, mid,
	// end of frame) + 58 data symbols = 79.
	NN = 79

	// NMAX is the number of audio samples in one FT8 slot
	// (15 s × Fs = 180000). Slot length matches the WSJT-X
	// 15-second TX/RX cadence.
	NMAX = 15 * 12000

	// NFFT1 is the spectrogram FFT size — 2 × NSPS = 3840. The 2×
	// oversampling in the frequency domain gives a bin spacing of
	// Fs/NFFT1 = 3.125 Hz, half the symbol-rate-natural 6.25 Hz.
	// The factor of 2 oversampling is what lets the sync detector
	// correlate Costas tones at sub-bin precision.
	NFFT1 = 2 * NSPS

	// NH1 is the number of positive-frequency bins in the
	// spectrogram FFT (NFFT1 / 2 = 1920). The DC bin and the
	// Nyquist bin are at indices 0 and NH1; the spectrogram
	// indexes bins [0, NH1).
	NH1 = NFFT1 / 2

	// NSTEP is the spectrogram time-step in samples — NSPS/4 = 480.
	// Each spectrogram column advances by NSTEP samples, giving
	// 4× temporal oversampling vs. the natural symbol boundary.
	// At Fs=12000 this is 40 ms per column.
	NSTEP = NSPS / 4

	// NHSYM is the number of spectrogram time columns covering a
	// full 15-second slot. The "−3" accounts for the windowed-
	// FFT tail (each column needs NSPS samples ahead of its
	// start, so the last 3 NSTEP positions overflow NMAX and
	// are dropped).
	NHSYM = NMAX/NSTEP - 3

	// NDOWN is the downsampler decimation factor: 12000 / 200 = 60.
	// The baseband path mixes signals to centre frequency and
	// decimates to a 200 Hz complex sample rate before symbol
	// demodulation.
	NDOWN = 60

	// NFFT2 is the FFT size used by the downsampler's inverse
	// transform (3200 = 2^7 · 5^2). The complex-output baseband
	// signal lives at 200 Hz over NP2 = NFFT2 - 388 samples for
	// the 15-second slot.
	NFFT2 = 3200

	// NFFT1DS is the FFT size used by the downsampler's forward
	// transform (192000 = 2^9 · 3 · 5^3) — the next 5-smooth size
	// at or above NMAX. The 12000 zero-pad samples are part of
	// the standard polyphase downsampling shape.
	NFFT1DS = 192000

	// Fs2 is the downsampled complex sample rate (200 Hz).
	Fs2 = Fs / NDOWN

	// Baud is the FT8 symbol rate (6.25 Hz).
	Baud = Fs / NSPS

	// SpectrogramScale is the empirical input-side scaling factor
	// applied to audio samples before the spectrogram FFT. The
	// WSJT-X Fortran reference uses 1/300 to keep float32 FFT
	// magnitudes in a comfortable working range. We carry the
	// same factor for parity-test purposes even though Go's
	// float64 FFT path doesn't need it for numerical reasons.
	SpectrogramScale = 1.0 / 300.0
)

// Icos7 is the 7-tone Costas synchronisation array per QEX paper
// §4: "For FT8 we use the seven-tone sequence 3, 1, 4, 0, 6, 5, 2
// inserted at the beginning, middle, and end of the transmitted
// waveform." The same sequence appears three times per slot:
//
//   - Symbols 0..6 (start sync)
//   - Symbols 36..42 (mid sync — between data symbols 28..35 and 43..50)
//   - Symbols 72..78 (end sync — after data symbols 50..71)
//
// Total 21 sync symbols + 58 data symbols = NN = 79.
var Icos7 = [7]uint8{3, 1, 4, 0, 6, 5, 2}

// GrayMap maps a 3-bit binary index to the 8-FSK tone number per
// QEX paper Table 3 ("Bi-directional Gray mapping between message
// bits and channel symbols"). The Gray code minimises bit errors
// for adjacent-tone misdecodes.
//
//	bits  tone
//	 000  → 0
//	 001  → 1
//	 010  → 3
//	 011  → 2
//	 100  → 5
//	 101  → 6
//	 110  → 4
//	 111  → 7
var GrayMap = [8]uint8{0, 1, 3, 2, 5, 6, 4, 7}

// GrayUnmap is the inverse of GrayMap — tone number → 3-bit binary
// value, per QEX paper Table 3 column 1 (Channel Symbol) ↔ column 2
// (FT8 Bits). Used by the soft demodulator to map per-tone
// correlation magnitudes back to bit positions in the codeword.
//
//	tone  bits
//	 0    → 000 (0)
//	 1    → 001 (1)
//	 2    → 011 (3)
//	 3    → 010 (2)
//	 4    → 110 (6)
//	 5    → 100 (4)
//	 6    → 101 (5)
//	 7    → 111 (7)
var GrayUnmap = [8]uint8{0, 1, 3, 2, 6, 4, 5, 7}
