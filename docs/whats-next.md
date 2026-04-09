# What's Next: `internal/ft8/dsp/` — FFT Pipeline & Soft Demodulation

Per the [implementation research](ft8-ft4-implementation-research.md), **item 6** is next:

> `internal/ft8/dsp/` — FFT pipeline, candidate detection, and soft demodulation

## Current State

Items 1–5 are **complete**:

| Item | Package | Status |
|---|---|---|
| 1. Audio I/O | `internal/audio/` | ✅ Complete |
| 2. WAV reader | `internal/audio/wav.go` | ✅ Complete |
| 3. Window timing | `internal/ft8/timing/` | ✅ Complete |
| 4. Message pack/unpack + CRC-14 | `internal/ft8/message/` | ✅ Complete |
| 5. LDPC codec (encoder + decoder) | `internal/ft8/codec/` | ✅ Complete |

The codec package provides:
- `Encode(info [12]byte) [22]byte` — systematic LDPC encoder (TX path)
- `Decode(llr [174]float32, maxIter int) (info [12]byte, ok bool)` — normalised
  min-sum belief-propagation decoder (RX path)
- `EncodeMessage(msg77 [10]byte) [22]byte` — convenience wrapper (pack → CRC → encode)
- `DecodeMessage(llr [174]float32, maxIter int) (msg77 [10]byte, ok bool)` —
  convenience wrapper (decode → CRC verify → extract message)

The full **TX encode chain** is now functional end-to-end at the bit level:
`message.Pack → message.Append91 → codec.Encode` (or simply `codec.EncodeMessage`).

What remains on the TX side (items 6–7) is symbol mapping, GFSK synthesis, and
audio output. On the RX side, the entire DSP front-end is needed to produce the
soft LLR inputs that `codec.Decode` consumes.

## Key Constants (FT8)

```
Sample rate:        12 000 Hz
Symbol period:      160 ms  (6.25 baud)
Tone spacing:       6.25 Hz (8-FSK)
FFT bin width:      6.25 Hz → FFT length 1920 samples
Symbols per message: 79  (21 sync + 58 data)
Costas sync positions: [0..6], [36..42], [72..78]
Window length:      15 s  → 180 000 samples
Frequency range:    ~200–3000 Hz audio passband
```

## What Needs to Be Built

### 1. Window function — `window.go`

A Hann (raised-cosine) window applied to the captured audio buffer before FFT
to reduce spectral leakage.

```go
// Hann applies an in-place Hann window to the samples.
func Hann(samples []float32)
```

- Simple `0.5 * (1 − cos(2πn/(N−1)))` formula.
- Operate in-place on the capture buffer slice.
- Include a unit test verifying known endpoint/midpoint values and energy
  normalisation factor.

### 2. FFT — `fft.go`

A real-input FFT producing complex frequency bins. FT8 needs a 1920-point FFT
per symbol period (160 ms × 12 000 Hz).

```go
// RealFFT computes the DFT of real-valued samples and returns complex bins.
// Only the non-negative frequency bins (N/2+1) are returned.
func RealFFT(samples []float32) []complex64
```

**Options:**
- **Pure Go**: implement a radix-2 Cooley-Tukey FFT. 1920 = 2⁷ × 3 × 5, so
  either zero-pad to 2048 or implement a mixed-radix FFT. Zero-padding to 2048
  is acceptable (interpolated bins still cover the 6.25 Hz resolution needed).
- **External library**: `github.com/mjibson/go-dsp/fft` provides a tested FFT,
  but operates on `[]complex128`. Evaluate whether the float64 overhead matters
  at 1920 points (likely negligible).
- **CGo wrapper**: `libfftw3` via CGo for maximum performance. Adds a build
  dependency. Probably overkill at this stage.

Recommendation: start with a pure-Go radix-2 FFT (zero-pad to 2048) to keep the
dependency footprint minimal. This can be swapped later if profiling shows it is
a bottleneck.

**Tests:**
- Known sinusoid at exact bin frequency → verify peak bin and magnitude.
- Parseval's theorem: energy in time domain ≈ energy in frequency domain.
- Power-of-two and non-power-of-two input lengths.

### 3. Power spectrum — `spectrum.go`

Compute the magnitude-squared (power) spectrum from FFT output, used for
candidate detection and waterfall display.

```go
// PowerSpectrum returns |X[k]|² for each frequency bin.
func PowerSpectrum(bins []complex64) []float32
```

### 4. Candidate detection — `candidates.go`

Scan the spectrogram (time × frequency) for FT8 signal candidates by
correlating against the known Costas sync pattern.

```go
type Candidate struct {
    Freq    float32 // audio frequency offset (Hz)
    TimeOff float32 // time offset within the window (seconds)
    Score   float32 // sync correlation strength
}

// FindCandidates searches the spectrogram for FT8 signals.
// Returns candidates sorted by descending score, up to maxCandidates.
func FindCandidates(spectrogram [][]float32, maxCandidates int) []Candidate
```

**Algorithm outline:**
1. Build a spectrogram: slide a 1920-sample FFT across the 180 000-sample
   buffer in steps of 1920 samples (one FFT per symbol period), producing a
   matrix of ~93 time steps × 961 frequency bins.
2. For each candidate (freq, time-offset) pair, compute the Costas sync
   correlation: sum the power at the 7 Costas tone positions in each of the
   three sync blocks (positions 0–6, 36–42, 72–78) relative to the 8-FSK
   tone grid anchored at that frequency.
3. Subtract the average power of non-sync symbol positions (noise floor) to
   normalise the score.
4. Apply a coarse threshold and return the top candidates.

The Costas sync array for FT8 is: `{3, 1, 4, 0, 6, 5, 2}`.

**Tests:**
- Synthesise a clean FT8-like tone sequence at a known frequency, embed in
  silence → verify the detector finds it at the correct freq/time.
- Multiple signals at different frequencies → verify all are found.
- Noise-only input → verify no false positives above threshold.

### 5. Soft demodulation — `demod.go`

Extract 174 log-likelihood ratios from a detected candidate signal. This is the
**critical** bridge between the DSP front-end and `codec.Decode`.

```go
// Demodulate extracts 174 soft LLR values for the LDPC decoder from the
// spectrogram at the given candidate position.
func Demodulate(spectrogram [][]float32, cand Candidate) [174]float32
```

**Algorithm (per data symbol, 58 symbols):**
1. At the candidate's time offset + symbol index, extract the 8 power values
   corresponding to the 8 FSK tones at the candidate's frequency.
2. Convert to log-domain: `s[k] = log(power[k])` for k = 0..7.
3. For each of the 3 bits encoded in the symbol (MSB to LSB), compute the LLR
   as: `LLR = log(Σ exp(s[k]) for k where bit=0) − log(Σ exp(s[k]) for k where bit=1)`.
   Use the log-sum-exp trick for numerical stability.
4. The 58 data symbols × 3 bits = 174 LLRs, ready for `codec.Decode`.

**Tests:**
- Encode a known message → map to symbols → synthesise power values (no noise)
  → demodulate → verify all LLR signs match the original bits.
- Round-trip with noise: `message.Pack → codec.EncodeMessage → symbol map →
  add noise → Demodulate → codec.DecodeMessage → verify`.
- Edge case: signal at lowest/highest frequency bin.

### 6. Symbol mapping utilities — `symbols.go`

Shared constants and helpers for the 8-FSK symbol mapping used by both TX
(synthesis) and RX (demodulation).

```go
const (
    NumSymbols   = 79
    NumDataSyms  = 58
    NumSyncSyms  = 21
    SymbolPeriod = 0.160 // seconds (FT8)
    ToneSpacing  = 6.25  // Hz (FT8)
    NumTones     = 8
)

// CostasSync is the 7-symbol Costas synchronisation array for FT8.
var CostasSync = [7]uint8{3, 1, 4, 0, 6, 5, 2}

// BitsToSymbols maps 174 coded bits to 58 data symbols (3 bits → 0..7 each).
func BitsToSymbols(codeword [22]byte) [NumDataSyms]uint8

// InsertSync interleaves the 58 data symbols with 3 Costas sync blocks
// to produce the full 79-symbol sequence.
func InsertSync(data [NumDataSyms]uint8) [NumSymbols]uint8
```

**Tests:**
- Known codeword → symbols → verify against ft8_lib reference.
- Round-trip: `BitsToSymbols → InsertSync → extract data symbols → SymbolsToBits`
  recovers the original codeword.

### 7. Spectrogram builder — `spectrogram.go`

Ties together windowing, FFT, and power spectrum into a single function that
produces the time×frequency matrix consumed by candidate detection and
demodulation.

```go
// Spectrogram computes a time×frequency power matrix from a capture buffer.
// stepSamples is the FFT hop size (typically 1920 for FT8 = one symbol period).
// fftSize is the FFT length (typically 1920, or 2048 if zero-padded).
func Spectrogram(samples []float32, fftSize, stepSamples int) [][]float32
```

### 8. Package entry point — `dsp.go`

Top-level convenience that runs the full RX pipeline on a single window buffer:

```go
// ProcessWindow takes a captured audio buffer (one FT8 window, 180k samples
// at 12 kHz) and returns decoded messages.
func ProcessWindow(samples []float32, maxCandidates, maxIter int) []DecodedMessage

type DecodedMessage struct {
    Msg77   [10]byte
    Freq    float32 // audio frequency (Hz)
    TimeOff float32 // time offset within the window (s)
    SNR     float32 // estimated signal-to-noise ratio (dB)
}
```

This connects: `Spectrogram → FindCandidates → Demodulate → codec.DecodeMessage`
and filters results by CRC pass.

## Suggested Implementation Order

```
symbols.go ✅ → window.go ✅ → fft.go ✅ → spectrum.go ✅ → spectrogram.go ✅
  → candidates.go ✅ → demod.go ✅ → dsp.go ✅
```

`symbols.go` is **complete** — it provides `BitsToSymbols`, `SymbolsToBits`,
`InsertSync`, `ExtractData`, Gray code tables, and all FT8 protocol constants.
Full test coverage including round-trip tests against known encoder vectors.

`window.go` is **complete** — it provides `Hann` (in-place), `HannCoefficients`
(pre-computed table for reuse in the spectrogram builder), and `ApplyWindow`
(multiply by pre-computed coefficients). Full test coverage: endpoints, midpoint,
symmetry, coherent gain (≈0.5), normalised energy (≈0.375), known reference
values, edge cases, and FT8 frame-size verification.

`fft.go` is **complete** — pure-Go radix-2 Cooley-Tukey FFT with:
- `NextPow2(n)` — smallest power of 2 ≥ n (exported for spectrogram builder)
- `RealFFT(samples) []complex64` — real-input FFT returning N/2+1 non-negative
  frequency bins, zero-padded to next power of 2 (1920 → 2048 for FT8)
- `complex128` internally for twiddle-factor precision, `complex64` output
- Precomputed twiddle factors per stage (no accumulated multiplication error)
- Full test coverage: DC, impulse, single sample, cosine/sine at exact bins,
  Parseval's theorem, zero-padding equivalence, non-power-of-two input, FT8
  frame size, brute-force DFT cross-check, linearity, Hermitian symmetry.

`spectrum.go` is **complete** — power spectrum computation:
- `PowerSpectrum(bins) []float32` — magnitude-squared |X[k]|² = re² + im²
- `LogPowerSpectrum(bins, floorDB) []float32` — 10·log10(|X[k]|²) with
  configurable floor for zero-power bins (useful for waterfall display / SNR)
- Full test coverage: nil/empty, pure real/imaginary/complex, non-negativity,
  known dB values, floor clamping, consistency between PowerSpectrum and
  LogPowerSpectrum, Parseval energy check, cmplx.Abs² cross-check.

**Next up: `spectrogram.go`** — ties together windowing, FFT, and power spectrum
into the time × frequency matrix consumed by candidate detection and demodulation.

`spectrogram.go` is **complete** — spectrogram builder:
- `Spectrogram(samples, fftSize, stepSamples) [][]float32` — slides a Hann-
  windowed FFT across the capture buffer, returning [nFrames][nBins] power matrix
- Precomputes Hann coefficients once and reuses a frame buffer across all frames
- Only full frames are included; trailing samples are discarded
- For FT8: 180 000 samples → 93 frames × 1025 bins (~0.17s with race detector)
- Full test coverage: nil/invalid params, dimension checks (including FT8 93×1025),
  silence → zero output, sinusoid peak at correct bin, window-applied verification,
  exact match against manual HannCoefficients+ApplyWindow+RealFFT+PowerSpectrum
  pipeline, fftSize=1920 vs 2048 equivalence, non-negativity.

**Next up: `candidates.go`** — Costas sync correlation to detect FT8 signals in
the spectrogram. This is the first component that uses FT8-specific protocol
knowledge (sync block positions, tone grid) rather than generic DSP.

`candidates.go` is **complete** — FT8 signal candidate detection:
- `Candidate` struct with `Freq` (Hz), `TimeOff` (s), `Score`
- `FindCandidates(spectrogram, maxCandidates) []Candidate` — searches the
  spectrogram for FT8 signals via Costas sync correlation, returns candidates
  sorted by descending score, truncated to maxCandidates
- `syncScore(sg, timeOff, baseBin) float32` — computes mean sync power minus
  mean total power across 79×8 tone positions (unexported scoring function)
- Searches the ~200–3000 Hz audio passband, all valid time offsets
- Bin-to-tone mapping uses nearest-bin approximation (valid for all 8 FT8 tones
  at 2048-point FFT / 12 kHz; maximum error 0.47 bins at tone 7)
- Full test coverage: nil/empty/too-small inputs, silence (no false positives),
  single signal detection at correct (freq, time), full signal with data symbols,
  multiple signals at different frequencies, descending sort order, maxCandidates
  truncation, syncScore properties (positive for sync-only, zero for uniform,
  monotonic with signal strength), end-to-end integration from synthesised audio
  through Spectrogram → FindCandidates, minimum-size spectrogram (no panic).

**Next up: `demod.go`** — soft demodulation to extract 174 LLR values from a
detected candidate, bridging the DSP front-end to `codec.Decode`.

`demod.go` is **complete** — soft 8-FSK demodulation with Gray-code awareness:
- `Demodulate(spectrogram, cand) [174]float32` — extracts 174 soft LLR values
  from the spectrogram at a candidate's (freq, time) position, suitable for
  direct input to `codec.Decode` or `codec.DecodeMessage`
- Precomputed bit-group tables (`bit0Tones`, `bit1Tones`) derived from the Gray
  code mapping — partitions the 8 tones into 4+4 for each of the 3 bit positions
- `logSumExp4` — numerically stable log-sum-exp for 4 values
- `logFloor` (-30) prevents -Inf propagation from zero-power bins
- LLR sign convention: positive → bit 0, negative → bit 1 (matches `codec.Decode`)
- Bounds-checked: returns zero LLRs for out-of-range candidates
- Full test coverage: logSumExp4 properties (equal values, one dominant, known
  value, all negative), bit-group consistency with grayDecode, nil/out-of-bounds/
  zero-power edge cases, single-tone LLR sign correctness for all 8 tones,
  known codeword LLR sign verification against coded bits, **full round-trip**
  through `codec.EncodeMessage → BitsToSymbols → Demodulate → codec.DecodeMessage`
  recovering the original 77-bit message, LLR magnitude monotonicity with signal
  strength, uniform power → zero LLRs.

`dsp.go` is **complete** — top-level RX pipeline entry point:
- `DecodedMessage` struct with `Msg77`, `Freq`, `TimeOff`, `SNR`
- `ProcessWindow(samples, maxCandidates, maxIter) []DecodedMessage` — full RX
  chain: `Spectrogram → FindCandidates → Demodulate → codec.DecodeMessage`
- CRC-14 filtering (only successfully decoded messages are returned)
- Deduplication by 77-bit message content (first decode wins)
- `estimateNoiseFloor` — mean-power noise floor estimator
- `estimateSNR(score, noiseFloor)` — coarse dB-scale SNR from sync correlation
  score vs. noise floor (suitable for display/logging, not calibrated)
- Full test coverage: nil/empty/too-short/silence edge cases, maxCandidates=0
  and maxIter=0 guards, exact-79-frames boundary, **full end-to-end round-trip**
  (EncodeMessage → BitsToSymbols → InsertSync → tone synthesis → ProcessWindow
  → verify original message recovered), deduplication verification (same message
  at two frequencies decoded only once), multiple distinct messages at different
  frequencies both decoded, SNR ordering (stronger signal → higher SNR), SNR
  estimator unit tests (positive/negative/zero score, zero noise floor),
  noise floor estimator tests (nil, uniform, silence).

**This milestone is complete.** All 8 files in the `internal/ft8/dsp/` pipeline
are implemented and tested:
`symbols.go → window.go → fft.go → spectrum.go → spectrogram.go → candidates.go → demod.go → dsp.go`

## Design Notes

- **Pure Go FFT to start**: avoid external C dependencies. A radix-2 FFT with
  zero-padding to 2048 is sufficient. If profiling reveals a bottleneck, a
  mixed-radix or CGo FFTW wrapper can be substituted behind the same interface.

- **`[]float32` throughout**: the audio pipeline uses `float32` samples. Keep
  FFT and power spectrum in `float32`/`complex64` to match. Avoid unnecessary
  `float64` conversions.

- **No allocations in the hot path**: `ProcessWindow` will be called once per
  15-second window. The spectrogram and FFT buffers can be pre-allocated and
  reused across windows if a struct-based API is added later (e.g., `Processor`
  struct with `Reset()`/`Process()` methods).

- **FT4 forward-compatibility**: FT4 uses 4-FSK (not 8-FSK), different symbol
  rate (12.0 baud), and different Costas pattern. Parameterise where practical
  (constants in `symbols.go`), but FT8-only is fine for the initial
  implementation. Mark FT4 divergence points with `// TODO: FT4`.

- **SNR estimation**: a rough SNR estimate can be derived from the Costas sync
  correlation score vs. noise floor. This is useful for display and logging but
  does not need to be precise initially.

- **Candidate deduplication**: after decoding, multiple candidates may decode to
  the same message (slightly different freq/time estimates). Deduplicate by
  comparing the 77-bit message content.

## Test Strategy

- **Unit tests per file**: each `.go` file has a corresponding `_test.go` with
  focused tests (known-input/known-output, edge cases).
- **Integration test**: `TestProcessWindowRoundTrip` — synthesise a complete
  FT8 signal (known message → encode → symbol map → tone synthesis → add to
  buffer), then run `ProcessWindow` and verify the original message is recovered.
  This exercises the entire RX chain.
- **Testdata**: store reference spectrograms or signal vectors in `testdata/`
  for regression. Consider recording a real FT8 `.wav` file and including it
  (or generating a synthetic one programmatically).

## Next Milestone: TX Synthesis + PlaySamples

With the DSP layer complete, the next milestone delivers the **complete FT8 TX
audio path** — from channel symbols to speaker output. It comprises two tightly
coupled items:

1. **`internal/ft8/synth/`** — GFSK tone synthesis (Gaussian-filtered FSK)
2. **`Playback.PlaySamples`** — play in-memory `[]float32` via `internal/audio/`

The `internal/ft8/service/` integration layer is **deferred** to a subsequent
milestone — it depends on both TX and RX paths plus the QSO state machine and
is a larger orchestration concern.

### TX Pipeline (what's already done vs. what remains)

```
77-bit message
  → CRC-14 → 91 bits                          ✅ codec.EncodeMessage
  → LDPC encode → 174 bits                     ✅ codec.Encode
  → Map 3-bit groups → 8-tone symbols          ✅ dsp.BitsToSymbols
  → Insert 3 Costas sync arrays → 79 symbols   ✅ dsp.InsertSync
  → GFSK smoothing (Gaussian filter)            ✅ synth.SmoothedFrequency
  → Synthesise audio samples                    ✅ synth.Synthesize
  → Play at precise T+1s start time             ✅ audio.PlaySamples
```

### Key Constants (FT8 GFSK)

```
Gaussian BT product:  2.0 (bandwidth × symbol period)
Kernel truncation:    ±2 symbols (5-symbol span, 9600 taps at 1920 samples/sym)
Output length:        79 × 1920 = 151 680 samples (12.64 s at 12 kHz)
Audio frequency:      configurable, typically 1000–2000 Hz AF
Amplitude:            normalised to ≤ 0.95 (avoids clipping)
Phase precision:      float64 accumulation (max phase ≈ 238k radians, well within
                      float64's ~15 significant digits)
```

### What Needs to Be Built

#### 1. Gaussian filter — `gfsk.go`

Compute the Gaussian impulse response kernel used to smooth the step-wise
symbol-to-frequency mapping, producing continuous-phase GFSK.

```go
// GaussianFilter returns a normalised Gaussian impulse response kernel for
// GFSK smoothing. bt is the bandwidth-time product (2.0 for FT8), span is
// the truncation width in symbol periods (typically 5 = ±2 symbols), and
// symbolSamples is the number of audio samples per symbol period (1920).
//
// Returns a kernel of length span × symbolSamples, normalised to sum to 1.0.
func GaussianFilter(bt float64, span, symbolSamples int) []float64
```

- Standard Gaussian pulse shape: `h(t) = √(2π/ln2) · BT · exp(-2(π·BT·t)²/ln2)`.
- Sampled at `1/symbolSamples` intervals over `[-span/2, +span/2)` symbol
  periods, then normalised so the sum equals 1.0.
- The kernel is symmetric about its centre.

**Tests:**
- Kernel sums to 1.0 (normalisation).
- Kernel is symmetric.
- Peak is at the centre index.
- Length equals `span × symbolSamples`.
- BT=2.0 / span=5 / 1920 samples: verify shape against a known reference value
  at the midpoint.

#### 2. Smoothed frequency trajectory — `gfsk.go`

```go
// SmoothedFrequency convolves the step-wise symbol-to-frequency mapping with
// the Gaussian kernel to produce a smooth per-sample frequency trajectory.
//
// symbols is the 79-symbol channel sequence (from dsp.InsertSync).
// baseFreqHz is the audio offset frequency for tone 0.
// kernel is the Gaussian filter from GaussianFilter.
//
// Returns a frequency trajectory of length NumSymbols × symbolSamples.
func SmoothedFrequency(symbols [dsp.NumSymbols]uint8, baseFreqHz float64,
    kernel []float64, symbolSamples int) []float64
```

- Raw frequency at sample n: `baseFreqHz + float64(symbols[n/symbolSamples]) × ToneSpacing`.
- Convolve with the Gaussian kernel. Use a direct linear convolution (the
  kernel is ~9600 taps, but only needs to be applied once per TX).
- Output length: exactly `79 × 1920 = 151 680` samples.

**Tests:**
- Constant-symbol input → mid-symbol frequency equals `baseFreq + tone × ToneSpacing`.
- Maximum adjacent-sample frequency delta is bounded (no discontinuities).
- Output length matches expectation.

#### 3. Audio synthesis — `synth.go`

```go
// Synthesize generates FT8 audio samples from the 79-symbol channel sequence.
//
// symbols is the full channel sequence from dsp.InsertSync (sync + data).
// baseFreqHz is the audio offset frequency for tone 0 (typically 1000–2000 Hz).
//
// Returns 151 680 float32 samples at 12 kHz, amplitude-normalised to ≈0.95.
func Synthesize(symbols [dsp.NumSymbols]uint8, baseFreqHz float64) []float32

// SynthesizeWithAmplitude is like Synthesize but with configurable peak amplitude.
func SynthesizeWithAmplitude(symbols [dsp.NumSymbols]uint8,
    baseFreqHz, amplitude float64) []float32
```

- Build Gaussian kernel (BT=2.0, span=5, 1920 samples/symbol).
- Compute smoothed frequency trajectory.
- Phase-integrate: `φ[n+1] = φ[n] + 2π × freq[n] / SampleRate`, with
  `φ = math.Mod(φ, 2π)` every symbol period for insurance.
- Output: `sample[n] = float32(amplitude × math.Sin(φ[n]))`.
- Uses `float64` internally for phase accumulation; downcast to `float32` output.

**Tests:**
- Output length = 151 680.
- All samples within `[-amplitude, +amplitude]`.
- Constant-symbol input → FFT peak at the expected frequency bin.
- Phase starts near 0 (first sample ≈ 0, since sin(0) = 0).
- **Full TX→RX round-trip**: `codec.EncodeMessage → dsp.BitsToSymbols →
  dsp.InsertSync → synth.Synthesize → dsp.ProcessWindow → verify decoded
  message matches input`. This is the capstone test validating that GFSK
  synthesis produces audio that the RX pipeline can decode.

#### 4. PlaySamples — `playback.go`

```go
// PlaySamples plays in-memory float32 audio samples to the playback device,
// blocking until all samples have been played, the context is cancelled, or
// Stop()/Close() is called.
//
// sampleRate is the playback sample rate (e.g., 12000 for FT8).
// channels is the number of audio channels (1 for mono FT8 audio).
//
// Returns ErrPlaybackEmptySamples if samples is nil or empty.
// Other error semantics match PlayFile.
func (p *Playback) PlaySamples(ctx context.Context, samples []float32,
    sampleRate uint32, channels uint32) error
```

- Follows the same `playing` CAS, `cancelPlay`, `wg`, and `closed` guards as
  `PlayFile` — extract the shared setup into a helper if the duplication is
  excessive.
- `onSendFrames` callback copies from the `samples` slice using a position
  cursor (identical pattern to `PlayFile`).
- 500 ms drain wait after final sample, same as `PlayFile`.
- New sentinel: `ErrPlaybackEmptySamples`.
- Update `internal/audio/README.md` with API reference and usage example.

**Tests (unit — no hardware):**
- nil/empty samples → `ErrPlaybackEmptySamples`.
- Not initialised → `ErrPlaybackNotInitialized`.
- Already playing → `ErrPlaybackAlreadyPlaying`.
- Closed → `ErrPlaybackClosed`.

**Tests (integration — real hardware, `//go:build integration`):**
- Synthesise a 1-second 440 Hz sine wave as `[]float32`, play via
  `PlaySamples`, assert it blocks for >200 ms.

### Suggested Implementation Order

```
gfsk.go ✅ → synth.go ✅ → synth_test.go ✅ → PlaySamples (playback.go) ✅ → playback_test.go ✅ → docs ✅
```

`gfsk.go` is **complete** — Gaussian filter and smoothed frequency trajectory:
- `GaussianFilter(bt, span, symbolSamples) []float64` — normalised Gaussian
  impulse response kernel, parameterised by BT product and truncation span.
  h(t) = √(2π/ln2)·BT·exp(−2(π·BT·t)²/ln2), normalised to sum=1.0.
- `SmoothedFrequency(symbols, baseFreqHz, kernel, symbolSamples) []float64` —
  convolves step-wise frequency trajectory with the kernel, producing 151 680
  smooth per-sample frequency values. Uses segment decomposition with kernel
  prefix sums for O(outLen × numSegments) performance (~100× faster than naive
  convolution). Edge padding matches WSJT-X/ft8_lib dummy-symbol convention.
- `GaussianBT = 2.0`, `KernelSpan = 5` — FT8 defaults with `// TODO: FT4`.
- Full test coverage: nil/invalid params, kernel length, normalisation (sum=1.0
  within 1e-12), symmetry, peak at centre, non-negativity, centre value cross-
  check against formula, BT comparison (higher BT → narrower peak), constant-
  symbol passthrough, mid-symbol frequency dominance, no discontinuities (max
  adjacent-sample Δf < 1 Hz), frequency bounds, monotone transition, and
  **cross-validation against the erf-difference overlap-add** (WSJT-X/ft8_lib
  algorithm) — max difference < 0.01 Hz.

`synth.go` is **complete** — GFSK audio waveform synthesis:
- `Synthesize(symbols, baseFreqHz) []float32` — convenience wrapper with
  DefaultAmplitude (0.95). Returns 151 680 float32 samples at 12 kHz.
- `SynthesizeWithAmplitude(symbols, baseFreqHz, amplitude) []float32` — full
  pipeline: GaussianFilter → SmoothedFrequency → phase integration (float64
  precision, modulo 2π wrap every symbol period) → sin(φ) output → raised-
  cosine envelope shaping on first/last 240 samples (matching WSJT-X
  gen_ft8wave.f90 and ft8_lib gen_ft8.c).
- `OutputSamples = 151 680`, `DefaultAmplitude = 0.95`, `rampSamples = 240`.
- Full test coverage: nil/invalid amplitude, amplitude clamping, output length,
  amplitude bounds, phase starts at zero, envelope shaping (edge=0, body≈full),
  constant-tone FFT frequency verification, amplitude scaling correctness,
  **cross-validation against ft8_lib's synth_gfsk** (max body difference < 0.02),
  **full TX→RX round-trip** (EncodeMessage → BitsToSymbols → InsertSync →
  Synthesize → ProcessWindow → verify original message recovered), and
  **multi-message round-trip** (two messages at different frequencies, both decoded).

`PlaySamples` is independent of synth but completes the TX audio path.

`PlaySamples` is **complete** — in-memory audio playback on `Playback` struct:
- `PlaySamples(ctx, samples, sampleRate, channels) error` — plays `[]float32`
  samples directly to the audio device, blocking until completion/cancellation.
  Follows the same CAS/cancel/wg guard pattern as `PlayFile`.
- `ErrPlaybackEmptySamples` — returned when samples is nil or empty.
- Refactored `PlayFile` and `PlaySamples` to share `acquirePlay()` (preamble),
  `releasePlay()` (cleanup), and `playBuffer()` (device lifecycle + drain) —
  eliminating ~100 lines of duplication.
- Full unit test coverage: nil samples, empty samples, not initialised, after
  close, already playing, mutual exclusion with PlayFile, empty-before-acquire
  ordering.
- Full integration test coverage: 1-second 440 Hz sine wave playback (elapsed
  > 200 ms), stop mid-play, context cancellation, close during play.
- `internal/audio/README.md` updated with PlaySamples usage example, API
  reference, and FT8 TX path documentation.

**This milestone is complete.** All TX audio path components are implemented:
`gfsk.go → synth.go → PlaySamples (playback.go)`

### Dependencies on Existing Code

| Dependency | Used by | How |
|---|---|---|
| `dsp.NumSymbols`, `dsp.SamplesPerSymbol`, `dsp.ToneSpacing`, `dsp.SampleRate` | `synth` | Protocol constants (import) |
| `dsp.BitsToSymbols`, `dsp.InsertSync` | `synth_test.go` | Build channel symbols for round-trip tests |
| `dsp.ProcessWindow` | `synth_test.go` | RX-side verification of synthesised TX audio |
| `codec.EncodeMessage` | `synth_test.go` | Encode test messages for round-trip |
| `Playback` struct, `malgo` | `PlaySamples` | Reuse existing device init, CAS, cancel, wg patterns |
| `errors.Op`, `errors.New` | Both packages | Structured error wrapping |

### Design Notes

- **Gaussian kernel truncation span:** hardcode 5 (±2 symbols) per WSJT-X
  convention. Add a `// TODO: FT4` comment — FT4 may need a different BT
  product or span.

- **Phase wrap:** include `φ = math.Mod(φ, 2π)` every symbol period as cheap
  insurance, even though `float64` precision is sufficient for 151k samples.

- **`PlaySamples` channel count:** accept a `channels` parameter for generality
  beyond FT8 (e.g., stereo contest CQ playback). FT8 TX will always pass 1.

- **No new external dependencies.** The synth package is pure Go math. The
  `PlaySamples` addition reuses the existing `malgo` binding.

### After This Milestone

With TX synthesis and PlaySamples complete, the remaining items are:

- **`internal/ft8/service/`** — top-level service with `Initialize()/Start()/
  Stop()` wiring audio capture → `dsp.ProcessWindow` (RX), and `timing.WaitForNext`
  → `synth.Synthesize` → `audio.PlaySamples` (TX), plus PTT control via
  `internal/ptt/`.
- **QSO state machine** — slot selection (even/odd), timeout handling, duplicate
  suppression, RRR/RR73 handling.
- **Testing against real FT8 recordings** — validate decode rate on actual
  on-air signals.

## Milestone: TX Orchestration in `internal/ft8/service/`

TX orchestration is **complete**. The FT8 service now supports both RX and TX
paths running concurrently on separate audio devices.

### TX Pipeline

```
TXRequest → timing.WaitForNext (parity-aligned) → TX offset wait (1 s) →
  PTT assert → synth.Synthesize → audio.PlaySamples → PTT release
```

### Files created/modified:
1. **`internal/types/ft8.go`** — extended `FT8Config` with TX fields:
   `TXEnabled`, `TXDeviceIndex`, `TXBufferSize`, `TXBaseFreqHz`,
   `PTTPortName`, `PTTLine`, `TXParity`.
2. **`internal/ft8/service/tx.go`** (new, ~290 lines) — `TXRequest`, `Transmit()`,
   `CancelTX()`, `IsTXActive()`, `txLoop`, `executeTX`, `parseParity`, plus
   `txPlayer` and `pttController` interfaces for testability.
3. **`internal/ft8/service/service.go`** — wired TX into lifecycle:
   - `Service` struct gains `playback`, `pttCtl`, `txQueue`, `txActive`,
     `txMu`, `txPlayCancel`, `txCancel`, `txWg` fields.
   - `Initialize` creates `Playback`, opens PTT (optional), allocates `txQueue`.
   - `Start` launches `txLoop` goroutine alongside `rxLoop`.
   - `Stop` cancels and waits for both loops.
   - `Close` releases playback and PTT resources.
4. **`internal/ft8/service/tx_test.go`** (new) — 17 tests covering all TX
   guard conditions, queue behaviour, cancellation, loop lifecycle, and the
   encode/synth pipeline.

### Test results: all 38 pass (21 RX + 17 TX)
- Transmit guards (nil receiver, not initialized, TX disabled, not running,
  nil message, queue full)
- Transmit success, queue delivery
- CancelTX drains queue, nil-safe
- IsTXActive nil receiver, default false
- txLoop context cancel, channel closed
- executeTX context timeout during window wait, nil message guard, pack failure
- parseParity even/odd/default

### Key design decisions:
- **`txPlayer` / `pttController` interfaces**: narrow interfaces allow mock
  injection for unit tests without real audio hardware or serial ports.
- **Capacity-1 `txQueue`**: only one TX request at a time; `Transmit` returns
  `errMsgTXQueueFull` on overflow. Future QSO state machine can manage
  sequencing externally.
- **PTT is optional**: nil `pttCtl` is handled gracefully (VOX mode).
- **RX and TX are independent**: separate audio devices, no pause needed
  during TX.
- **Parity-aligned TX windows**: `executeTX` loops past wrong-parity windows
  until the matching slot arrives.

### After This Milestone

The remaining items are:

- **QSO state machine** — slot selection (even/odd auto-toggle), timeout
  handling, duplicate suppression, RRR/RR73 handling, auto-reply sequencing.
- **Testing against real FT8 recordings** — validate decode rate on actual
  on-air signals.

## Milestone: 48 kHz Capture + FIR Decimation (Live RX Decode)

This milestone is **complete**. The FT8 RX pipeline now decodes live signals
from the FTdx10 transceiver on 28.074 MHz.

### Problem

The FT8 service was requesting 12 kHz audio directly from the FTdx10's USB
audio codec (TI PCM2903C), which only natively supports 48 kHz. This forced
miniaudio's internal resampler to downsample with unknown filter quality,
introducing spectral artifacts that corrupted the narrow FT8 tones (6.25 Hz
spacing, requiring clean spectral representation for LDPC decode).

WSJT-X handles this by capturing at 48 kHz and decimating to 12 kHz with a
proven 49-tap FIR anti-aliasing filter (`lib/fil4.f90`), designed with ScopeFIR:
- Cutoff: 4500 Hz
- Stopband edge: 6000 Hz
- Passband ripple: 1 dB
- Stopband attenuation: 40 dB

### Solution

1. **`internal/ft8/dsp/decimate.go`** (new) — `Decimator` struct using exact
   WSJT-X `fil4` coefficients. `Decimate(input []float32) []float32` maintains
   FIR filter state across chunked audio callbacks. Constants:
   `DecimationFactor=4`, `CaptureSampleRate=48000`.

2. **`internal/ft8/service/service.go`** — capture at 48 kHz instead of 12 kHz;
   decimation step in `rxLoop` after stereo channel extraction; config defaults
   for `MaxCandidates=50`, `MaxIterations=25`, `BufferSize=512`.

3. **`cmd/ft8/cmd/diag.go`** — `captureWindow()` captures at 48 kHz stereo,
   extracts left channel, decimates to 12 kHz.

4. **`build/config.json`** — added `capture_channels` (default 2) and
   `capture_channel` (default "left") fields.

### RX Pipeline (updated)

```
audio.Capture (48 kHz stereo) → channel extraction (left/right) →
  Decimate4×  (12 kHz mono, fil4 FIR) → sample accumulation →
  dsp.ProcessWindow → message.Unpack → output channel
```

### Test results

- 14 new decimator unit tests (all pass with `-race`)
- 43 existing service tests (all pass)
- Full DSP suite (all pass)
- Live decode: 7 messages in 3 windows on 28.074 MHz

### After This Milestone

The remaining items are:

- **Window alignment to 15 s wall-clock boundaries** — WSJT-X's Detector.cpp
  resets the accumulation buffer at T%15==0. The current implementation
  accumulates continuously. This may affect decode rate slightly (secondary).
- **Extended live testing** — compare decode rate against WSJT-X.
- **QSO state machine** — slot selection, timeout handling, duplicate
  suppression, RRR/RR73 handling, auto-reply sequencing.

## Milestone: FT8 CLI Hardening (Config Validation + Console Logging Fix)

This milestone is **complete**. The FT8 CLI tool (`cmd/ft8`) now validates its
config at startup and correctly outputs structured log messages to the console.

### 1. FT8 Config Validation

Added `validate` struct tags to `FT8Config` (in `internal/types/ft8.go`) and a
`validateConfig()` function in `internal/ft8/service/`, following the same
pattern used by the database and CAT services.

**`internal/types/ft8.go`** — struct tags added:
```
DeviceIndex    validate:"min=-1"
BufferSize     validate:"min=0,max=8192"
MaxCandidates  validate:"min=0,max=200"
MaxIterations  validate:"min=0,max=100"
CaptureChannels validate:"min=0,max=2"
CaptureChannel  validate:"omitempty,oneof=left right"
TXDeviceIndex  validate:"min=-1"
TXBufferSize   validate:"min=0,max=8192"
TXBaseFreqHz   validate:"min=0,max=5000"
PTTLine        validate:"omitempty,oneof=RTS DTR"
TXParity       validate:"omitempty,oneof=even odd"
```

**Files created:**
- **`internal/ft8/service/validation.go`** — `validateConfig(*FT8Config) error`
  using `go-playground/validator/v10`, `sync.Once` for validator instance.
- **`internal/ft8/service/validation_test.go`** — 18 tests covering all
  validated fields: NilConfig, ValidMinimal, ValidDefaults, DeviceIndexTooLow,
  BufferSizeTooLarge, MaxCandidatesTooLarge, MaxIterationsTooLarge,
  CaptureChannelsTooLarge, CaptureChannelInvalid, CaptureChannelEmpty,
  TXDeviceIndexTooLow, TXBufferSizeTooLarge, TXBaseFreqHzTooHigh,
  PTTLineInvalid, PTTLineValid, TXParityInvalid, TXParityValid, FullTXConfig.

**`internal/ft8/service/service.go`** — wired `validateConfig(&cfg)` into
`Initialize()` immediately after loading the raw config from `config.json`,
before applying defaults.

### 2. Console Logging Fix (IOCDI Preseed Timing)

**Problem:** The FT8 CLI's `--verbose` flag had no effect — all log output went
to a file instead of stderr. The on-disk `config.json` has
`console_logging: false, file_logging: true` (correct for Wails desktop apps),
and the CLI's preseed that overrides this to `console_logging: true` was applied
**after** `container.Build()`.

**Root cause:** The IOCDI container's `Build()` method auto-calls `Initialize()`
on all beans in topological dependency order (lines 258–268 of `container.go`).
In the original `setup()`:
1. `container.Build()` at line 106 — auto-initialises `configService` (reads
   `config.json` with `console_logging: false`) and `logService` (reads the
   config and sets up file-only writers).
2. Preseed at line 130 — sets `ConsoleLogging: true`, but both services are
   already initialised. `Initialize()` is guarded by `sync.Once`, so subsequent
   calls are no-ops.

**Fix:** Create the `configService` manually, set the preseed on it, then
register it as an instance via `RegisterInstance` before calling `Build()`:

```go
configService = &config.Service{WorkingDir: workingDir}
configService.AppConfig.LoggingConfig = types.LoggingConfig{
    Level:          logLevel,
    ConsoleLogging: true,
    FileLogging:    false,
    // ...
}
container.RegisterInstance(config.ServiceName, configService)
// Build() now auto-calls configService.Initialize(), which sees the preseed
// (Level != "") and preserves it after loading config.json from disk.
```

The `config.Service.Initialize()` preseed logic (lines 57–67 of
`config/service.go`) saves `AppConfig.LoggingConfig` before loading the disk
file and restores it afterwards if `Level != ""`. By setting the preseed before
`Build()`, the logging service initialises with the correct console-only config.

**Files modified:**
- **`cmd/ft8/cmd/root.go`** — restructured `setup()`: config service created
  and preseeded before `Build()`; removed post-Build `Initialize()` calls;
  removed all diagnostic code and unused imports (`io`, `zerolog`).

### 3. Build Task

Added `task ft8` to `Taskfile.yml` for building the FT8 CLI binary with
incremental source tracking:

```yaml
ft8:
  desc: "Build the FT8 RX/TX CLI tool → build/bin/ft8"
  cmds:
    - mkdir -p build/bin
    - cd cmd/ft8 && go build -o ../../build/bin/ft8 .
  sources:
    - cmd/ft8/**/*.go
    - internal/**/*.go
```

Updated `AGENTS.md` and `DEVELOPING.md` with the new task.

### Test Results

- 18 new validation tests (all pass)
- 43 existing service tests (all pass)
- 14 decimation tests (all pass)
- Full DSP suite (all pass)
- `go vet ./cmd/ft8/...` — clean
- Live smoke test: `ft8 --device 1 --windows 1 --verbose` — structured log
  output appears on stderr via zerolog ConsoleWriter ✅

### After This Milestone

The remaining items are:

- **Window alignment to 15 s wall-clock boundaries** — WSJT-X's Detector.cpp
  resets the accumulation buffer at T%15==0. The current implementation
  accumulates continuously. This may affect decode rate slightly (secondary).
- **Extended live testing** — compare decode rate against WSJT-X.
- **QSO state machine** — slot selection, timeout handling, duplicate
  suppression, RRR/RR73 handling, auto-reply sequencing.

