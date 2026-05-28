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

## 7. SoftLLRsN3 — block detection at N=3

**Provenance.** N=2 and N=3 block detection are both prescribed at the
algorithm-family level by QEX § 6, paragraph 6 ("the noncoherent block
detector with block length N... we have used N=1, 2, and 3"). The
*specific* per-symbol coherent sum + max-log demap implementation that
follows mirrors what the N=2 derivation (§ 5 item 1, and the shipped
`SoftLLRsN2` code) already does — straightforward extension from N=2
to N=3, no new conceptual content. The pairing layout (how triples
divide the 29-data-symbol half-frames) is an implementation choice
NOT prescribed by the paper; it's an independent engineering decision
analogous to the N=2 pairing-layout choice, derived here from first
principles + the structural constraints of the data-symbol layout.

This section's job is to pin the block correlation math, the layout
choice, and the leftover-handling policy explicitly enough that the
implementation is a transcription, not an interpretation.

### 7.1 Block correlation math (extends N=2 to N=3)

For a 3-symbol block at data-symbol indices `(d1, d2, d3)` mapped to
audio-symbol indices `(s1, s2, s3)` via `dataSymbolIndices[]`, the N=3
block correlation under tone hypothesis `(m1, m2, m3) ∈ {0..7}³` is

```
C_{m1,m2,m3} = Amps[s1][m1] + Amps[s2][m2] + Amps[s3][m3]
```

— the coherent sum of three per-symbol complex correlations from
`SymbolGrid.Amps`. The block "power" used for demap is

```
P_{m1,m2,m3} = |C_{m1,m2,m3}|²
```

For each of the 9 codeword bits owned by the triple (3 bits per symbol,
selected by `inverseGrayMap[]`, identical to N=1 and N=2), the max-log
LLR is the difference of the max-power hypothesis with bit=0 and the
max-power hypothesis with bit=1:

```
LLR(bit_k) = max_{m1,m2,m3 : bit_k(m_owner)=0} P_{m1,m2,m3}
           − max_{m1,m2,m3 : bit_k(m_owner)=1} P_{m1,m2,m3}
```

where `m_owner` is the tone whose bit positions the LLR addresses
(m1 for d1's bits, m2 for d2's, m3 for d3's). Total operations per
triple: 512 complex sums for the C array, 512 magnitudes, 9 × 512
scans = ~5,120 floating-point ops. **Sign convention matches N=1
and N=2: positive LLR favours bit 0.** Power scale is in |C|² units,
same as N=2; BP's median-LLR normalisation absorbs the per-set scale
difference.

### 7.2 Phase-coherence assumption

The block detector requires phase stability within the block. N=2's
block length is 2 × 0.160 s = 0.32 s; N=3's is 3 × 0.160 s = 0.48 s.
The paper's framing is that block detection trades increasing within-
block phase-stability demands for AWGN sensitivity (each extra N reduces
the noncoherent-summing penalty). N=3 is strictly more demanding than
N=2: any channel disturbance that breaks phase coherence over 0.48 s
but not 0.32 s will cost N=3 decodes that N=2 still wins. On clean HF
this is usually satisfied; on fast-fading or polar paths it may not be.
That's the empirical question this work answers.

### 7.3 Pairing layout for 29 data symbols per half-frame

Data symbols are split by the middle Costas anchor block (positions
36–42) into two contiguous halves of 29 symbols each (data indices
0..28 = audio positions 7..35; data indices 29..57 = audio positions
43..71). Triples MUST NOT bridge the middle Costas gap (would require
1.6+ s phase coherence between data 28 and data 29; far beyond what
N=3 can claim). Each half-frame is divided independently.

29 doesn't divide evenly by 3: 29 = 9 × 3 + 2. The clean layouts per
half-frame are:

- **9 triples + 2 leftover symbols → N=1 fallback** (chosen). Mirrors
  the N=2 layout's "pairs + 1 N=1 leftover" pattern. Per half: 27
  symbols × 3 bits = 81 bits from N=3 + 2 × 3 = 6 bits from N=1 = 87
  bits. Total over both halves: 174 bits ✓.
- 8 triples + 2 pairs + 1 leftover → mixed N=3 + N=2 + N=1 (rejected).
  Adds plumbing complexity without a paper-backed justification; the
  6 leftover bits at N=1 are a small enough cost not to engineer
  around.

**Chosen layout: 9 non-overlapping triples + 2 trailing N=1 leftover
symbols per half-frame.** Triples cover data indices (0,1,2), (3,4,5),
…, (24,25,26) in the first half and (29,30,31), …, (53,54,55) in the
second. Leftover N=1 symbols: d=27, d=28 in the first half;
d=56, d=57 in the second. Each leftover symbol contributes 3 bits via
the existing `fillSymbolLLRsN1` helper (same code path the N=2 odd-
trailing-symbol falls back to).

### 7.4 What this rules out vs the cascade

This derivation covers **N=3 block-detection LLRs as a 174-bit output
vector**. It does NOT cover:

- **Bit-normalization** (the "nsym=1 bit-normalized" variant in the
  operator's proposal). Per-symbol noise estimation + LLR scaling —
  separate derivation pass when that step lands.
- **Per-bit best-of-N selection** (the "best-of nsym=1/2/3" variant).
  Selection algebra across multiple LLR vectors — separate derivation
  pass when that step lands.
- **Cascade ordering** (try N=1 → N=2 → N=3 → normalized → best-of, in
  that order). The cascade is the *use* of LLR variants, not the
  derivation of one. Each metric's standalone lift is measured first
  via per-metric attribution plumbing; the cascade order can then be
  argued empirically (best metric tried first) rather than from theory.

### 7.5 Sanity checks the implementation must pass

Three test conditions, mirroring how `SoftLLRsN2_test.go` pins N=2:

1. **Noiseless round-trip**: given a `SymbolGrid` constructed from a
   clean 174-bit codeword, the N=3 LLRs must be consistent with the
   original bits — every bit's LLR sign matches the encoded value
   (positive → bit 0, negative → bit 1).
2. **Sign convention**: confirm LLR > 0 ⇔ bit = 0 across multiple
   randomized noiseless codewords.
3. **Leftover-symbol fallback**: confirm the 12 bits owned by the 4
   N=1-fallback symbols (d=27, d=28, d=56, d=57) are byte-identical
   to the LLRs the same symbols would produce under `SoftLLRs` (N=1
   on the same grid).

### 7.6 Open implementation questions (none load-bearing)

- **Memory**: a `[8][8][8]float64` for the per-triple P array is 512
  doubles = 4 kB. Allocated as a local array (stack); no heap pressure.
- **Loop order**: outer `m1`, inner `m2`, innermost `m3`. Same nesting
  shape as N=2's `[8][8]`. Marginalization scans the entire 512-entry
  array once per of the 9 bits (4,608 scans per triple) — fine.
- **Code layout**: new `fillTripleLLRsN3(grid, llrs, d1, d2, d3)`
  helper, parallel to `fillPairLLRsN2`. Half-frame iterator
  `tripleHalfLLRs(grid, llrs, start, end)` parallel to
  `pairHalfLLRs`. Public entry point `SoftLLRsN3(grid) [174]float64`
  parallel to `SoftLLRsN2`. No changes to existing N=1 / N=2 code.

## 8. SoftLLRsN1BitNormalized — per-symbol noise-normalized N=1 LLRs

**Provenance.** Bit-normalization is **not** prescribed by the QEX
paper; the paper grants "K is adjusted empirically" latitude (§ 5
item 2 of this doc names this explicitly). The general principle —
that LDPC belief propagation works better when per-bit LLR magnitudes
are on a comparable confidence scale — is standard information-theory
and well-documented in textbooks on sum-product / belief propagation
decoders (Richardson & Urbanke, *Modern Coding Theory*, MIT 2008, ch.
4–5 covers the LLR-scale-invariance failure mode and the role of
per-channel-symbol noise normalization).

### 8.1 Why bit-normalization helps BP+OSD

Belief propagation on an LDPC factor graph operates by message-passing
real-valued LLRs. The decoder's intermediate decisions weight each
incoming LLR by its magnitude — large |LLR| messages dominate, small
|LLR| messages contribute little. This is the *correct* behaviour
when LLR magnitudes faithfully represent per-bit confidence on a
common scale.

When the channel SNR varies across the 79 symbols of an FT8 frame —
through fading, frequency-selective interference, or impulse noise —
some symbols' raw `|Amps|²` powers are 10× to 100× larger than
others' simply because their channel coefficient is larger. Without
normalization, the high-power symbols' LLRs swamp the low-power
symbols' contributions during BP iteration, even when the low-power
symbols carry equally reliable bit information *for their own SNR*.

The fix: estimate the per-symbol noise scale σ̂²_s and divide each
symbol's bit LLR contribution by it. After scaling, a unit |LLR|
means "1 standard deviation of separation between the two hypothesis
classes," regardless of which symbol the bit came from. BP then
weights bits by *normalized* confidence rather than raw power level.

This is a strictly more demanding channel model than N=1's "all
symbols have the same noise" assumption, but it's a model that
matches real HF channels better — and the cost is one extra estimator
per symbol (8 numbers in, 1 number out, very cheap).

### 8.2 Per-symbol noise estimator (the engineering choice)

For each data symbol s, we have 8 tone powers `p_m =
SymbolGrid.Tones[s][m]` for m=0..7. Exactly one tone is the signal;
the other 7 are noise + any adjacent-channel leakage. The signal
tone's power is much larger than noise on a good decode; on a marginal
decode the signal/noise gap shrinks.

Standard estimators and their tradeoffs:

- **Mean of all 8 powers**: contaminated by the signal tone (inflates
  by ~12% even at high SNR; much more at low). Rejected.
- **Mean of 7 non-winner tones**: identify the max-power tone, average
  the remaining 7. Robust against the signal but vulnerable to a
  strong adjacent-channel interferer that mimics signal-tone shape.
- **Mean of 6 lowest-power tones** (chosen): sort the 8, drop the top
  2 (the signal + the next-largest, which is most likely interferer-
  spillover or a high-frequency noise outlier), average the remaining
  6. Robust against one strong interferer + the signal itself. Per
  symbol cost: 8 powers → sort (8-comparison via partial-sort), sum
  the bottom 6, divide by 6. Negligible.
- **Median of 7 non-winner tones**: more robust still but discards more
  data per symbol. Reserved for a follow-up if 6-lowest-mean proves
  insufficient.

**Chosen estimator: σ̂²_s = mean(6 lowest of 8 tone powers at symbol
s)**. The decision rationale is in 8.4; the implementation form is in
8.5.

### 8.3 Normalization formula

Two equivalent forms — pick the one that maps cleanly onto the existing
N=1 code path:

**Form A (scale powers before max-log)**:

```
p'_m = p_m / σ̂²_s     for each m=0..7
max0' = max{p'_m : bit_k(m) = 0}
max1' = max{p'_m : bit_k(m) = 1}
LLR'(bit_k) = max0' − max1'
```

**Form B (scale LLR after max-log)**:

```
max0 = max{p_m : bit_k(m) = 0}
max1 = max{p_m : bit_k(m) = 1}
LLR(bit_k) = max0 − max1
LLR'(bit_k) = LLR(bit_k) / σ̂²_s
```

These are mathematically identical (max-log differences scale
linearly). **Form B is chosen** because it shares the per-tone max-log
computation byte-identical with `SoftLLRs`, then applies a single
per-symbol multiplicative scaling at the end. Easier to read, easier
to test (the unscaled max-log output of `SoftLLRsN1BitNormalized`
must match `SoftLLRs` byte-identical before scaling).

Sign convention matches `SoftLLRs` / `SoftLLRsN2` / `SoftLLRsN3`:
positive LLR favours bit 0. Scaling by a positive σ̂²_s preserves
sign.

### 8.4 Why "6 lowest of 8" rather than "7 non-winner"

Empirical risk: adjacent-channel signals (the diagnostic Session 84
identified as the dominant real-capture failure mode) often deposit
strong off-tone energy onto neighbouring frequencies. If the neighbour
falls within the candidate's admitted spectral slice and its main
lobe aliases to one of the 8 tone bins, that tone has *signal-
strength* power without being the demap winner.

"Mean of 7 non-winner" treats this neighbour-leakage tone as part of
the noise floor → inflates σ̂² → suppresses *all* of the symbol's
LLRs, including the legitimately-confident ones.

"Mean of 6 lowest" trims the top 2 — the actual signal + the worst
contaminant. The cost is sampling-variance increase (6 samples vs 7),
but real channel noise is well-estimated from 6 samples since FT8's
8-FSK tone bins are independent.

This is the channelizer-asymmetric-experiment story rerun at the
noise-estimator level: trimming more aggressively is more robust to
real-channel structure even when it sacrifices estimator efficiency
under the idealized model. Empirically measurable: if the
6-lowest-mean variant unlocks truths the 7-non-winner-mean wouldn't,
the asymmetric-channel adversarial structure is doing what we think
it is.

### 8.5 Implementation shape

```go
func SoftLLRsN1BitNormalized(grid *SymbolGrid) [FT8CodewordBits]float64 {
    var llrs [FT8CodewordBits]float64
    for d := 0; d < 58; d++ {
        sym := dataSymbolIndices[d]
        powers := grid.Tones[sym]
        // Step 1: max-log demap (identical to SoftLLRs).
        for bitPos := 2; bitPos >= 0; bitPos-- {
            max0, max1 := -math.MaxFloat64, -math.MaxFloat64
            for tone := 0; tone < 8; tone++ {
                if (inverseGrayMap[tone]>>bitPos)&1 == 0 {
                    if powers[tone] > max0 { max0 = powers[tone] }
                } else {
                    if powers[tone] > max1 { max1 = powers[tone] }
                }
            }
            cbi := 3*d + (2 - bitPos)
            llrs[cbi] = max0 - max1
        }
        // Step 2: per-symbol noise estimate = mean of 6 lowest tones.
        sigma2 := meanOfSixLowest(powers)
        // Step 3: scale the symbol's 3 LLRs by 1/sigma2.
        if sigma2 > 0 {
            inv := 1.0 / sigma2
            llrs[3*d] *= inv
            llrs[3*d+1] *= inv
            llrs[3*d+2] *= inv
        }
        // else: degenerate (all-zero tones); leave llrs as max-log diffs.
    }
    return llrs
}
```

The `meanOfSixLowest(powers [8]float64) float64` helper sorts a copy
(8-element partial sort) and averages the bottom 6. Implementation
detail; trivially testable.

### 8.6 Sanity checks the implementation must pass

1. **Noiseless round-trip**: clean grid → bit-normalized LLRs → hard
   decisions reconstruct the codeword exactly. Same shape as N=2 /
   N=3 round-trip tests.
2. **Sign convention**: positive LLR favours bit 0; tone-0 all-symbols
   grid yields all-positive LLRs.
3. **Scaling property**: for a clean symbol where σ̂²_s = K (constant
   non-winner power), the bit-normalized LLR equals N=1 LLR divided
   by K. The proportionality test pins Form-B's mathematical content.
4. **Robustness to one strong interferer**: synthetic test where 1 of
   the 8 tones at one symbol is artificially boosted to 5× the signal
   power. The bit-normalized estimator must NOT mistakenly count the
   interferer as signal — i.e., the resulting σ̂²_s must still reflect
   true noise level, and the bit LLR sign must remain correct on the
   actual signal tone.

### 8.7 Open question deferred to measurement

Whether bit-normalization should be a **5th cascade pass** (try after
N=3 fails) or a **modifier on N=1** (i.e., the cascade becomes "N=1 →
N=1-normalized → N=2 → N=3" or some reordering) is a question the
empirical measurement answers. The operator's proposal places it as
"pass 4" — after N=3, before best-of-N. We'll implement it that way
first and measure whether it recovers further truths from the N=3-
failed residue. If it instead shifts which truths land at N=1 (i.e.,
some N=2-recovered truths move down to N=1-normalized), the cascade
order needs reconsideration.

### 8.8 What this section does NOT cover

- **Best-of-N per-bit selection** — separate derivation (planned §9).
- **Cascade ordering rationale** — currently empirical, not theoretical.
- **N=1+bit-normalized as a substitute for N=1** — keeping N=1 raw in
  the cascade preserves the operator's "did adding the new metric earn
  its keep" measurability. Removing N=1 would conflate two changes.

## 9. SoftLLRsBestOfN — per-bit best-of-{N=1, N=2, N=3} selection

**Provenance.** Per-bit best-of-N selection is **not** prescribed by
QEX; the paper says nothing about combining multiple block-detection
metrics. The general principle — that when you have multiple
independent estimators of the same quantity, picking the one with the
highest confidence per data point can dominate any single estimator —
is again textbook information theory (Bayesian model selection with
log-likelihood as the confidence signal; equivalent to maximum a
posteriori source assignment under uniform priors).

The operator's directive explicitly scoped best-of to **{N=1, N=2,
N=3}** (not including N1Norm). § 9.6 records why this scope is
defended and why N1Norm is best left to the cascade rather than
merged into best-of.

### 9.1 Why per-bit best-of helps

The N=1, N=2, N=3 metrics differ in their phase-coherence assumption
(no coherence for N=1; 0.32 s for N=2; 0.48 s for N=3). On a real
HF channel some symbols will have within-block phase stability — so
N=2 and N=3 give well-determined LLRs there — while other symbols
sit at fading-null boundaries where N=2/N=3's coherence assumption
breaks but N=1's per-symbol estimate is still valid. *Within a
single 79-symbol frame*, different symbols can be better-served by
different metrics.

The cascade as built (§§ 7–8) tries each metric whole. Either N=1's
174 LLRs feed BP, or N=2's do, or N=3's, or N1Norm's. The cascade
doesn't mix-and-match per bit. Best-of-N is the per-bit version:
construct a 174-vector where each entry is taken from whichever
source has the highest |LLR| for that bit.

### 9.2 Selection rule

For each of the 174 codeword bits i:

```
source(i) = argmax_{k in {N=1, N=2, N=3}} |LLR_k[i]|
LLR_best[i] = LLR_source(i)[i]
```

Where ties in |LLR| are broken by preferring the lower-N variant
(N=1 > N=2 > N=3) — this is a defensive choice: lower N has weaker
phase-coherence assumptions and is therefore the "safer" default
when no metric clearly dominates.

Sign convention: positive LLR favours bit 0 across all three input
sources (pinned by sanity tests for each); the selection preserves
sign because it preserves the chosen-source's value as-is.

### 9.3 Scale-comparability concern (the open question)

The three input LLR sources are NOT on the same magnitude scale:

- N=1's |LLR| is bounded by max-difference of `|Amps|²` per symbol —
  roughly one tone's power.
- N=2's |LLR| is bounded by max-difference of `|sum of 2 tones|²` —
  roughly 4× a single tone's power when both tones add coherently.
- N=3's |LLR| is bounded by max-difference of `|sum of 3 tones|²` —
  roughly 9× a single tone's power.

So max-|LLR| selection systematically prefers higher-N sources
*even when the higher-N's coherence assumption fails*. This is the
opposite of what we want — we want to prefer the source that's most
*confident given its model*, not the source with the largest raw
magnitude.

Two ways to address this:

**(a) Pre-normalize each source's LLRs** to a common scale (e.g.,
divide each source's LLRs by its median |LLR|, then select). This
puts confidence on a rank-equivalent scale.

**(b) Defer the question** — implement raw max-|LLR| first, measure
whether the resulting decode quality is good enough, and if it
biases toward N=3-dominated selection (which we can measure
directly), then add normalization.

§ 9.7 records this as a deliberate engineering choice to start with
(b): the raw max-|LLR| variant is simpler, easier to test, and the
scale bias is *measurable* via per-source-selection-count instrumentation
that runs on the corpus. If best-of-N collapses to "always-N=3", we
know we need normalization. If it produces a per-bit-varying source
selection (some bits from N=1, some from N=2, some from N=3), the
raw selection is doing useful work despite the scale concern.

### 9.4 Implementation shape

```go
func SoftLLRsBestOfN(grid *SymbolGrid) [FT8CodewordBits]float64 {
    n1 := SoftLLRs(grid)
    n2 := SoftLLRsN2(grid)
    n3 := SoftLLRsN3(grid)

    var best [FT8CodewordBits]float64
    for i := 0; i < FT8CodewordBits; i++ {
        a1 := math.Abs(n1[i])
        a2 := math.Abs(n2[i])
        a3 := math.Abs(n3[i])
        // Lower-N tiebreak: N=1 > N=2 > N=3.
        switch {
        case a1 >= a2 && a1 >= a3:
            best[i] = n1[i]
        case a2 >= a3:
            best[i] = n2[i]
        default:
            best[i] = n3[i]
        }
    }
    return best
}
```

Per-call cost: 3× the per-metric cost (one N=1 + one N=2 + one N=3
generation) plus a 174-element O(1) selection loop. Since these are
called only when N=1+N=2+N=3+N1Norm have all failed, the cascade
position keeps the cost out of the hot path on most candidates.

### 9.5 Cascade position

Operator's proposal: pass 5, after N1Norm. The cascade becomes:

```
N=1 → N=2 → N=3 → N1Norm → BestOfN
```

Empirically defensible by elimination — if every individual metric
has failed BP+OSD, the per-bit best combination is the last
remaining LLR shape to try. Per-bit selection is genuinely different
from any single metric: the resulting LLR vector cannot be produced
by any single source, only by the combination.

This places best-of as the **highest-cost-per-call but least-frequently-
invoked** cascade entry. The cost (3 metric generations + selection)
is paid only on candidates where 4 prior metrics all failed — by
construction a small fraction of the candidate pool.

### 9.6 Why exclude N1Norm from best-of's source list

Three reasons:

1. **Scale mismatch is even worse.** N1Norm divides by σ̂²_s, which
   varies per-symbol. Its |LLR| scale is not stably comparable with
   raw N=1/N=2/N=3 |LLR|s — best-of-{N=1, N=2, N=3, N1Norm} would
   prefer N1Norm whenever σ̂² is small (low-power symbols), which
   isn't a confidence signal but a normalization artifact.
2. **Operator's directive is explicit:** "best of nsym=1/2/3" —
   3-way selection over the block-detection family.
3. **Measurability:** keeping N1Norm in the cascade lets us see
   what it uniquely recovers vs what best-of-{N=1, N=2, N=3}
   uniquely recovers. Merging them would conflate two signals.

If empirical evidence later shows best-of would benefit from
N1Norm-as-source, that's a separate change with its own derivation
(scale-normalization across all four sources). Out of scope here.

### 9.7 Sanity checks the implementation must pass

1. **Noiseless round-trip**: clean grid → best-of LLRs → hard
   decisions reconstruct the codeword exactly. Same shape as the
   other variants.
2. **Sign convention**: tone-0 grid → all-positive LLRs; tone-7
   grid → all-negative LLRs.
3. **Pure-N=1 input**: when N=2 and N=3 both produce zero LLRs
   (e.g., synthetic grid where Amps cancel in the block sum), the
   best-of output must equal N=1 byte-identical. Pins the
   tiebreak-to-lower-N rule.
4. **Source-attribution sanity**: on a randomized non-clean grid,
   the source-selection histogram should NOT collapse to "all N=3"
   — at least 5% of bits should come from N=1 and at least 5%
   from N=2 (loose bound; pins the raw-magnitude selector isn't
   degenerate). If this fails empirically on the corpus, §9.3's
   normalization fix is the response.

### 9.8 What this section does NOT cover

- **Source-attribution diagnostics on the live corpus** — the
  implementation will instrument source-selection counts during
  the corpus run so the bias question (§ 9.3) is empirically
  answerable. The diagnostic plumbing lives in the cmd tool, not
  in SoftLLRsBestOfN itself.
- **N1Norm-as-source** — out of scope; see § 9.6.
- **Weighted combination** instead of max-selection — rejected
  upstream: the scales are too different and weights would have
  to be tuned empirically with no theoretical basis.
