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
  → candidates.go ✅ → demod.go → dsp.go
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

## After This Milestone

With the DSP layer complete, the remaining items are:

- **TX synthesis** (`internal/ft8/synth/` or similar): `BitsToSymbols →
  InsertSync → GFSK tone synthesis → audio samples`. This is item 7 in the
  research doc and is relatively straightforward once symbol mapping exists.
- **`Playback.PlaySamples`**: needed to route synthesised TX audio to the
  soundcard (gap noted in the research doc).
- **`internal/ft8/service/`** (item 7 in research doc): top-level service
  with `Initialize()/Start()/Stop()` wiring audio capture, DSP processing,
  and the QSO state machine together.
