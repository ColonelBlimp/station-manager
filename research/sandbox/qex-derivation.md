# QEX paper derivation pass

Clean-room derivation of what *The FT4 and FT8 Communication Protocols*
(Franke, Somerville, Taylor; QEX Jul/Aug 2020 —
`references/FT4_FT8_QEX.pdf`) motivates for the sandbox's symbol-detection
and LLR-generation paths, compared against what the sandbox does today.

The implementation source is the QEX paper plus the ref [14] public-domain
tarball (`references/ft4_ft8_public/`). The WSJT-X source is off-limits as
an implementation reference. Where the sandbox happens to match the
paper's prescription, the match is via independent derivation from the
paper; where it differs, the paper itself is the arbiter.

## 1. What the paper prescribes for symbol detection and decoding

Paragraph-by-paragraph from §6 *Symbol Detection and Decoding*:

1. **Candidate identification.** "A decoding pass starts by identifying
   all likely signals, or *candidates*, using spectral analysis." The
   paper is non-prescriptive on the exact detector form. The sandbox is
   free to choose its candidate scanner.

2. **Single-symbol (N=1) detection.** "WSJT-X calculates the 8
   correlations required for single-symbol (N=1) detection first, using
   a fast Fourier transform." For each FT8 symbol, the 32 complex
   baseband samples (at 200 Hz complex rate, 6.25 Hz tone spacing) are
   inner-producted against each of the 8 tone references. The result is
   eight complex values `C_m` per symbol. The within-symbol inner product
   IS coherent — phase stability is required *over* the 160 ms symbol.

3. **Block (N>1) detection — sensitivity-gaining, optional.**
   "Noncoherent block detection over sequences of two or more channel
   symbols improves sensitivity over single-symbol detection when the
   received signal maintains phase coherence over multiple symbols." For
   FT8 the paper uses N=1, 2, and 3. "The single-symbol complex results
   are then combined to produce the 64 correlations required for N=2 and
   the 512 correlations required for N=3 block detection." Block
   correlations are formed by *coherent within-block combining* of the
   N=1 complex outputs — i.e., phase IS used inside a block. The output
   of an N-symbol block is M^N complex correlation values.

4. **Magnitude-then-max-log demap.** *After* the within-block complex
   correlation, the paper takes magnitudes: "Only the magnitude of the
   complex-valued correlation is used, and detection is independent of
   phase differences between the received signal and locally generated
   waveforms." Bit soft metric:

   `L_j = K (max_{i: x_j=1} |C_i| − max_{i: x_j=0} |C_i|)`

   where `K` is "adjusted empirically to optimize performance over a
   range of SNRs and channel conditions."

5. **Precision of the word "noncoherent".** Phase continuity *between*
   block sequences is NOT assumed. Phase IS used inside a block; phase
   is NOT used across blocks. The paper explicitly forecloses a phase
   trajectory fit across all 79 symbols.

6. **Decode attempt cascade.** N=1 first, fed to BP. If BP fails (no
   valid CRC after a reasonable iteration budget), the soft decisions
   from N=2 are submitted; then N=3. OSD is optionally applied when BP
   fails.

7. **Multi-pass via channel-gain estimation.** Decoded message regenerates
   the transmitted waveform as reference `r(t)`. The channel response is
   estimated by `g(t) = LPF[s(t) · r*(t)]` — a single complex gain
   function smoothed by a low-pass filter. The reconstructed signal is
   subtracted as `s'(t) = s(t) − 2·Re[g(t)·r(t)]`. Successive decoding
   passes operate on the residual.

8. **Paper-measured sensitivity gains** (Table 5, AWGN):
   - N=1 BP only: −19.6 dB
   - N=1,2,3 BP: −20.3 dB (+0.7 dB)
   - N=1,2,3 BP+OSD: −20.8 dB (+1.2 dB vs N=1 BP)

   Mid-latitude disturbed: +0.5 dB from block detection. Diminishes on
   fast-fading channels where the block phase-stability assumption
   weakens.

## 2. What the sandbox does today, mapped onto the paper

| Stage | QEX §6 prescribes | Sandbox does | Status |
|---|---|---|---|
| Candidate spectral analysis | "spectral analysis"; form unspecified | `candidates.go::FindCandidates` — power-spectrogram, Costas-anchor sum across 21 anchor cells, local-noise denominator (column-median window, tone bins excluded), NMS, MaxResults=200 cap | Compatible. Paper is agnostic on form; sandbox's choice is defensible. |
| Within-symbol coherent matched filter (sync) | 32-sample complex inner product per symbol per tone, magnitude per symbol, sum across the 21 Costas anchors | `refine.go::costasBasebandScore` — exactly this shape, written "coherent within a symbol; incoherent across symbols" | Matches. |
| Within-symbol coherent matched filter (data symbols) | Same 32-sample complex inner product, eight tone outputs `C_m` per symbol | `symbols.go::ExtractSymbols` — 32-point complex FFT per symbol on the 200 Hz baseband, `Amps[s][m]` holds the complex `C_m`, `Tones[s][m] = |C_m|²` holds the power | Matches at the N=1 stage. |
| Bit LLR via max-log on magnitudes | `K (max\|C_i\|_{x=1} − max\|C_i\|_{x=0})` over the M^N=8 N=1 correlations | `metrics.go::SoftLLRs` — `max{power}_{x=0} − max{power}_{x=1}` (sign convention flipped vs paper, scale on `\|C\|²` not `\|C\|`) | Cosmetic difference. Power-vs-magnitude is a monotone transform; BP's noise-variance normalisation absorbs the constant-factor scale; sign convention is internal. Not a real gap. |
| **N=2 / N=3 block detection** | Combine pairs/triples of adjacent N=1 complex outputs into 64 (N=2) / 512 (N=3) longer-block complex correlations, magnitudes, max-log demap to a fresh LLR set | **Not implemented.** Sandbox only ever computes the N=1 LLR set. | **Headline gap.** Paper-measured ~+0.5–0.7 dB AWGN. |
| Decode attempt cascade | N=1 BP → N=2 BP → N=3 BP, each with optional OSD fall-back, CRC accept | Single N=1 LLR set fed to BP, then OSD on failure | Same gap as above. |
| Multi-pass subtract | `g(t) = LPF[s(t)·r*(t)]`, subtract `2·Re[g(t)·r(t)]` | `subtract.go` — per-symbol 2-parameter (a·cos + b·sin) LSQ fit in the audio domain, per-symbol subtraction | Different shape, same purpose. Sandbox's per-symbol fit is more local; paper's LPF-smoothed `g(t)` is a single message-wide channel response. Worth measuring eventually; not the headline. |

## 3. Gap analysis

### 3.1 Already QEX-correct (no change needed)

- Within-symbol coherent matched filter for sync and for data-symbol
  extraction. The current `refine.go` and `symbols.go` shapes are exactly
  what §6 describes for the N=1 stage.
- Candidate scanner's local-noise denominator is a defensible
  implementation choice within the paper's "spectral analysis" latitude.
- Power-vs-magnitude difference in `SoftLLRs` is cosmetic; the
  paper-prescribed `K` factor is the same empirical scale BP's noise
  variance handles.

### 3.2 Headline gap: block detection (N=2, N=3) absent

The sandbox computes only the N=1 LLR set. The paper's Table 5 measures
the sensitivity loss of N=1-only at 0.7 dB on AWGN, 0.5 dB on
mid-latitude disturbed. The implementation freedom the paper grants
(latitude in the K scale, latitude in the LPF cutoff, latitude in the
candidate detector) does not extend to omitting block detection — that's
load-bearing for the published sensitivity numbers.

The single-symbol N=1 complex correlations are already preserved at
`SymbolGrid.Amps`. N=2 block detection is a function of `Amps` alone —
no new audio-domain processing required.

### 3.3 Multi-pass subtraction shape

Sandbox uses per-symbol 2-DOF LSQ in audio. Paper uses one
LPF-smoothed complex channel-gain function across the whole message.
Both estimate the channel response; the paper's approach naturally
accommodates slow phase/amplitude drift via the LPF cutoff, while the
sandbox's per-symbol approach is more local but can over-fit at low SNR
or under adjacent-channel interference.

This is a correct-shape-but-uncertain-magnitude difference. Worth
measuring after the headline gap is closed, not before.

## 4. What this rules out

Anything that would require fitting a phase trajectory *across* multiple
blocks — Costas-anchor-pilot phase tracking spanning the 79-symbol
frame, decision-directed cross-symbol phase estimation, "fully coherent"
demodulation in the sense that the post-block magnitude step is replaced
with a phase-coherent matched-filter likelihood across the message — is
foreclosed by the paper's §6 framing. The paper explicitly notes that
phase continuity between block sequences is not assumed, and only the
magnitude of the complex-valued within-block correlation is used. The
sensitivity numbers it reports were measured against that framing.

Implementations are free to depart, but anything that does so departs
from the paper's design and would need its own derivation and
sensitivity measurement. Default position: don't depart.

## 5. Revised queue, QEX-grounded

In priority order, each item with the paper section that motivates it:

1. **N=2 block detection LLR set** (paper §6, paragraphs 3–4 and 6).
   Combine the existing `SymbolGrid.Amps[s][m]` complex outputs into 64
   N=2 complex correlations per adjacent data-symbol pair, take
   magnitudes, max-log demap to a second 174-bit LLR set, feed BP+OSD as
   a fall-back attempt if N=1 fails. Largest paper-measured single
   lever (~+0.5–0.7 dB). Within-block phase stability over 0.32 s is
   the new assumption being introduced; usually satisfied on HF except
   in fast fading.

2. **LLR noise normalisation** (no specific paper prescription; lives
   inside the paper's "K is adjusted empirically" latitude). Port the
   Winsorized column-noise estimator from `research/demod` Session 95.
   Bounded lift, low risk.

3. **Channel-gain-LPF multi-pass subtraction** (paper §6, paragraph 7).
   Replace per-symbol LSQ with `g(t) = LPF[s(t)·r*(t)]` and subtract
   `2·Re[g(t)·r(t)]`. Correct-shape rewrite; measurable benefit at low
   SNR or slow fading.

4. **N=3 block detection** (paper §6, same as item 1 with N=3). Only if
   N=2 lifts and CPU budget permits. Adds 512 correlations per symbol
   triple; the paper's marginal sensitivity gain from N=3 over N=2 is
   modest.

## 6. What got ruled out from earlier conversation

The "full phase-coherent demod LLRs" direction I floated in conversation
prior to this derivation pass — fitting a phase trajectory across all
79 symbols using Costas anchors as pilots — is withdrawn. The paper's
§6 framing explicitly forecloses it. Phase is used inside a block; phase
is not used across blocks. The headline lever the paper backs is N=2/N=3
block detection, not cross-symbol phase coherence.
