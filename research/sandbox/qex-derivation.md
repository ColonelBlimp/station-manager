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
| Bit LLR via max-log on magnitudes | `K (max\|C_i\|_{x=1} − max\|C_i\|_{x=0})` over the M^N=8 N=1 correlations | `metrics.go::SoftLLRs` — `max{power}_{x=0} − max{power}_{x=1}` (sign convention flipped vs paper, scale on `\|C\|²` not `\|C\|`) | **SPEC-DERIVATION ERROR** (refuted 2026-05-29; see § 3.1.1). Power vs magnitude is NOT a constant-factor transform on the demap *difference*. Sign convention is internal (fine). |
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

### 3.1.1 Spec-derivation error: power-vs-magnitude IS NOT cosmetic (refuted 2026-05-29)

An earlier version of this section claimed: *"Power-vs-magnitude
difference in SoftLLRs is cosmetic; the paper-prescribed K factor is
the same empirical scale BP's noise variance handles."*

That claim was wrong. The math:

Let `a = max_{x=1} |C_i|` and `b = max_{x=0} |C_i|` (correlation
magnitudes, the QEX domain). Then:

- **QEX magnitude LLR:** `L_mag = K · (a − b)`
- **Sandbox power LLR:** `L_pow = K' · (a² − b²) = K' · (a + b) · (a − b)`

The ratio `L_pow / L_mag = K'(a + b) / K`. The factor `(a + b)` is
**not** a global constant — it varies symbol-by-symbol with signal
strength, noise floor, and adjacent-channel interference. Strong
symbols (large a, b) have a larger `(a + b)` multiplier than weak
symbols; the per-symbol relative weighting inside the LLR vector is
different between the two domains.

BP's noise-variance normalisation absorbs **one global scale** per
LLR vector, not a per-bit varying scale. The argmax (which tone
wins for each bit) is unchanged between domains because `x²` is
monotone over [0, ∞), so hard decisions agree — but BP's
message-passing dynamics depend on the LLR *magnitudes*, not just
their signs. Per-bit varying multipliers reshape those dynamics in
a way that no global normalisation can undo.

This was discovered via independent external review pointing at
QEX § 6 (paragraph 5, demapper definition); the math was re-derived
from the paper directly. No WSJT-X or other implementation source
consulted. Clean-room provenance intact.

**Implication for measured corpus history:** the N1Norm cascade
pass (`SoftLLRsN1BitNormalized`) was an empirical attempt to fix
LLR dynamics by per-symbol normalisation — it may have been
partially compensating for exactly this domain error. Earlier
coherent-demod and refinement experiments that measured neutral
may have been doing so against a baseline whose LLR domain was
off-spec. Magnitude-domain A/B (see `MultiPassOptions.MagnitudeLLR`)
is the controlled test.

**Empirical confirmation (2026-05-29):** strict-mode corpus measurement
under `cmd/sandbox-asym-ab -strict` against power vs magnitude:

| Mode | Matched / Extras | N=1 | N=2 | N=3 | N1Norm |
|---|---|---|---|---|---|
| Power (legacy) | 111 / 23 | 98 | 7 | 2 | 4 |
| Magnitude (spec) | **113 / 23** | 103 | 6 | 0 | 4 |
| Δ | +2 / 0 | +5 | −1 | −2 | 0 |

Net +2 matched truths cracked, **zero extras inflation**. N=1
swallowed three cascade fallbacks (truths that previously needed
N=2 or N=3 under the off-spec scale now decode at N=1 with the
correct demap). N1Norm count unchanged — its per-symbol
normalisation was not primarily compensating for the domain
error on those 4 truths.

**Strict-mode default changed to magnitude-domain 2026-05-29.**
Operative baseline going forward: **113/144 matched / 23 extras**.
Legacy power-domain reachable via
`cmd/sandbox-asym-ab -strict -legacy-power-llr` for A/B comparison
against the historical 111/23.

**Implication for parked experiments (revisit candidates):**
several earlier flat-result measurements were taken against the
off-spec baseline and deserve re-measurement against 113/23:
- `BestOfN` (tiebreak math operates on LLR magnitudes — per-symbol
  varying scales would have biased the per-bit max selection)
- Asymmetric channelizer (its +2 sym-vs-asym delta included the
  JY5IB false positive that is now excluded from strict scoring)
- Gate cube sweeps (per-symbol-varying LLR magnitudes change
  reliability thresholds)
- Coherent-demod / refinement experiments (the "no lift" verdicts
  used a baseline whose LLR scale was off)

Not all need re-running. Use the (forthcoming) per-truth funnel
diagnostic to pick the experiments where leverage genuinely
changed. Don't replay the full set blindly.

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

## 10. SoftLLRsAPCQ — a priori decoding for the CQ family (QEX § 7 AP2)

### 10.1 What QEX § 7 prescribes

The paper defines three families of a priori (AP) information that
can be exploited when channel LLRs are too weak for BP alone to
converge:

- **AP1** — hypothesise i3 (the 3-bit message-type field) is known
  in advance. Pins 3 of the 174 codeword bits.
- **AP2** — hypothesise the first callsign field (c28_1) carries a
  specific token, plus i3 and the auxiliary single-bit flags
  (p1, p2, r1) consistent with that hypothesis. The c28_1 token
  range carries DE / QRZ / CQ / "CQ nnn" / "CQ aaaa" — pinning
  any specific token candidate fixes 28 of 174 codeword bits
  (plus the auxiliaries: 33 total).
- **AP3** — hypothesise both callsign fields (c28_1 and c28_2) are
  known, e.g., a contest exchange between two pre-identified
  stations. Pins ~62 of 174 codeword bits. The strongest AP form
  but requires both call-side identities to be known a priori.

Each form is implemented as an LLR-injection: the prior is added
to the channel LLR at the known positions. BP iterates the
augmented LLR vector and recovers the unknown bits.

### 10.2 AP-CQ as the AP2 specialisation

`SoftLLRsAPCQ` implements AP2 with the c28_1 hypothesis being a
member of the **CQ family** of token values:

| Variant   | c28_1 value | Form encoded                  |
|-----------|-------------|-------------------------------|
| Bare CQ   | 2           | "CQ"                          |
| CQ DX     | 69279       | "CQ DXAA" (DX, A-padded)      |
| CQ COTA   | 46113       | "CQ COTA"                     |
| CQ POTA   | 274601      | "CQ POTA"                     |

The values derive from the FT8 token-range layout documented in
`unpack.go`:

- Values [0, 3) are the bare tokens "DE" / "QRZ" / "CQ".
- Values [3, 1003) carry "CQ nnn" (3-digit numeric).
- Values [1003, 1003 + 26⁴) carry "CQ aaaa" (4-letter), MSB-first
  base-26 encoded. Short modifiers (1-3 letters) are right-padded
  with 'A' (=0) and rendered without padding by jt9-style
  decoders. The sandbox's `decodeCQAbcd` matches this convention
  via right-trim of trailing 'A' chars.

For each variant, AP-CQ pins 34 of 174 codeword bits:

- bits 0..27: c28_1 = variant value (28 bits)
- bit 28: p1 = 0 (CQ-form messages don't carry rover-suffix flags)
- bit 57: p2 = 0
- bit 58: r1 = 0
- bits 74..76: i3 = 1 (Type 1)

The remaining 141 bits (43 unknown payload + 14 CRC + 83 parity)
carry no prior contribution — the decoder must recover them from
the channel alone.

### 10.3 Prior magnitude and mixing

Priors are **added** (not replaced) to the N=1 channel LLRs at the
33 known positions. With `apCQMagnitude = 10` and typical channel
`|LLR| ≤ 5` on the working fixtures, the prior dominates the
channel by ~2:1 at each pinned bit but doesn't completely override
it. Strong channel disagreement at a known position can still
flip the bit — that's the graceful-degradation property: if the
candidate isn't actually a CQ, BP fails (CRC mismatch) rather
than locking into a wrong codeword.

Magnitude is configurable via `MultiPassOptions.APCQMag` to allow
operator-driven sweeps without re-deriving the bit layout.

### 10.4 OSD mask gap (caught Session 104, CLOSED same session)

The priors flow into both BP iterations (via `channelLLRs[v] + Σ
extrinsic`) and OSD (the same `channelLLRs` argument is passed to
`runOSD`). With `|prior|=10` dominating typical channel `|LLR|<5`,
the 33 AP-pinned bits land at the top of OSD's reliability ranking
and are placed in the MRB. However:

**OSD-2 flips up to 2 MRB bits during its bit-flip search to find
a CRC-passing codeword.** Before Session 104's pin fix, OSD was
unaware that some MRB bits were load-bearing AP pins, not just
high-confidence channel decisions. A 1- or 2-bit flip that landed
on AP-pinned positions effectively undid the c28_1 hypothesis,
producing a CRC-passing codeword in the standard-callsign range
(c28_1 > 6_257_896) — not a CQ at all.

Empirical baseline before the pin landed: with 4 hypotheses (bare
CQ, DX, COTA, POTA) tried per failed candidate, AP-CQ produced 10
CRC-passing decodes; **all 10 had non-CQ text** like `OW6VHQ
HF0AB/P FP54` — OSD-2 had flipped its way out of the AP constraint
into a random standard-callsign codeword.

Two mitigations shipped:

1. **Post-decode text guard** in `multipass.go`: after `Unpack77`
   succeeds via the AP-CQ path, verify `Text` starts with "CQ";
   reject and continue otherwise. Catches the OSD-flipped failures
   cleanly; without the guard AP-CQ would have leaked standard-shape
   CRC-lottery extras the gate would then have to filter.
2. **OSD MRB pinning** in `osd.go` / `bp.go`: new `runOSDWithPin`
   takes an immutable `[174]bool` mask and skips any flip pattern
   that touches a pinned position. Re-projects natural-order pin
   onto post-Gauss-elimination MRB ordering via
   `pinnedMRB[i] = pinned[perm[i]]`. New `BPDecodeWithPin` threads
   the mask through BP's OSD fallback. Legacy `runOSD` /
   `BPDecode` are thin wrappers passing `nil` pin — zero overhead
   for non-AP callers. `apCQPinMask()` in `metrics.go` returns the
   33-position mask; the AP-CQ cascade path calls
   `BPDecodeWithPin(&pin)`.

After the pin landed: APCQ shadow-reject count dropped 10 → 0
(no more OSD-flipped CRC-lottery garbage in the audit). The pin
correctness is exercised by three unit tests
(`TestRunOSDWithPin_NilEqualsRunOSD`,
`_DoesNotFlipPinnedBit` with MRB-resident wrong-sign LLR setup,
`_NonPinnedBitsStillFlippable` no-op pin regression).

### 10.5 Why corpus impact is zero (revised after OSD pin)

Even with OSD pin enabled (no more garbage extras leaking),
AP-CQ recovered **0** of the 9 sym CQ-format misses on the working
corpus. Of the 9: 4 are "CQ <modifier>" forms (DX×2, COTA,
unknown short call), 5 are bare "CQ <call> <grid>". With c28_1
pinned to each hypothesis in turn and OSD's flip search
suppressed on the 34 pinned positions, neither BP nor OSD-2
returned a CRC-valid codeword for any of these candidates.

Two mechanisms explain the null result:

1. **Finder-bound misses** — Session 92-93 finder-recall
   measurements identified ~7 of 33 sym misses as candidate-
   scanner-bound (the signal never makes it to the LLR /
   decoder stage). AP-CQ can only help signals that surface as
   candidates; finder-bound truths are out of reach for any AP
   mechanism.

2. **Channel-noise-bound at the 43 unknown payload bits.** With
   c28_1 (28 bits) + p1/p2/r1/i3 (5 bits) pinned, BP/OSD must
   still recover 43 unknown payload bits (c28_2 + g15) + 14 CRC
   + 83 parity. On the working corpus the channel LLRs at the
   c28_2 / g15 positions are weak enough that 34 bits of prior
   leverage isn't sufficient — BP/OSD with pin returns
   `ok=false` (the pin correctly blocks the OSD-flipped garbage,
   but no legitimate CQ codeword is reachable either).

   This matches QEX § 7's expected behaviour for AP1/AP2: AP
   helps where channel SNR is just below BP threshold (the
   typical "marginal recovery" zone). On a corpus where misses
   sit *well* below threshold, AP2's narrow recovery band offers
   no win.

The Session 104 conclusion: **the priors-only AP-CQ approach is
empirically insufficient on this corpus, even with correct OSD
plumbing.** The OSD pin machinery itself is correct and
load-bearing infrastructure for any future AP work — but the
parity wall on this corpus needs more leverage than 34 bits can
provide.

### 10.6 AP3 as the natural follow-on

The strongest QEX § 7 AP form is **AP3**: pin both c28_1 AND
c28_2 from a known-caller hash table (`CallsignHashTable`). When
the caller has been seen in a prior decode, c28_2 = 28 known
bits; combined with the AP2 anchors (28 + 5 = 33), AP3 pins **61
of 174 codeword bits**, leaving only 15 g15 + 14 CRC + 83 parity
to recover. That's a doubling of leverage at the systematic-bits
layer.

The Session 104 OSD pin machinery (`runOSDWithPin`,
`BPDecodeWithPin`, the `[174]bool` mask format) carries forward
unchanged for AP3 — only the priors and pin mask change. AP3's
implementation work is:

1. Build an "AP3 hypothesis enumerator" — given the running
   `CallsignHashTable`, generate (c28_1 ∈ {CQ family, callsigns
   from hash}, c28_2 ∈ {callsigns from hash}) candidate pairs
   for each failed candidate.
2. For each pair, set priors at the 56 c28-bits + 5 auxiliaries
   and call `BPDecodeWithPin` with the 61-bit pin mask.
3. Text guard: verify the unpacked text matches the hypothesised
   caller / callee shape.

AP3 is parked for a future session — the OSD pin machinery
shipped today is the prerequisite that landed.

### 10.7 What this section does NOT cover

- **AP3 (full-call hash table)** — § 10.6 describes it but
  implementation is parked.
- **AP1 (i3-only)** — pins 3 bits. Far less leverage than AP-CQ;
  not implemented because the 33-bit AP-CQ already produced zero
  corpus impact, so the 3-bit version would too.
- **OSD-3** — the deeper bit-flip search (order 3 vs 2) is an
  alternative decoder-side lever to AP3, exploring ~125k
  candidates instead of ~4k. Compatible with the pin machinery
  shipped today (order-3 loops gate on `pinnedMRB[k]` already).
  Not measured against the corpus this session.

## 11. AP3 — both callsigns hypothesised from CallsignHashTable (QEX § 7 AP3)

### 11.1 What QEX § 7 prescribes

AP3 is the strongest of the three AP families described in QEX § 7.
Where AP-CQ / AP2 hypothesises only the first callsign field
(c28_1 = some token or known callsign), AP3 hypothesises **both**
callsign fields: c28_1 AND c28_2. The hypothesis source is a
running record of callsigns the decoder has previously seen — the
sandbox's `CallsignHashTable` is the natural feed, populated
incrementally as each successful decode adds caller and addressee.

The leverage of AP3 over AP2 is the doubling of pinned systematic
bits: AP-CQ pins 28 + 6 = 34 bits; AP3 pins 28 + 28 + 6 = 62 bits.
After AP3 the remaining unknown payload + parity bits drop from 43
+ 14 + 83 = 140 to 15 + 14 + 83 = 112 — a 20% reduction in the
channel-driven bit count. The QEX paper identifies AP3 as the
specific AP form responsible for the bulk of jt9's marginal
recovery in active QSO scenarios where the call-pair is known
from prior exchanges.

### 11.2 Pin layout

Bit positions traced to the PackType1 layout in `pack.go` (which
itself derives from QEX § 4):

```
bits  0..27   c28_1   (caller in non-CQ messages; CQ token for CQs)
bit  28       p1 = 0  (rover-suffix flag for call 1)
bits 29..56   c28_2   (callee/addressee)
bit  57       p2 = 0  (rover-suffix flag for call 2)
bit  58       r1 = 0  (roger flag)
bits 59..73   g15     (unknown — channel-driven)
bits 74..76   i3 = 1  (Type 1 message)
bits 77..90   CRC14   (depends on g15 + payload → unknown)
bits 91..173  parity  (depends on info + CRC → unknown)
```

Total pinned: 62 bits. `ap3PinMask()` in `metrics.go` materialises
this as a `[174]bool` for `BPDecodeWithPin`.

### 11.3 Hypothesis enumeration

The hypothesis space is the cross product (c28_1 candidates) ×
(c28_2 candidates), where:

- **c28_1 candidates**: bare-CQ token (numeric value 2) ∪ up to K
  callsigns from the hash table. CQ in c28_1 covers the case
  "this might be a CQ message we missed via the AP-CQ stage."
- **c28_2 candidates**: up to K callsigns from the hash table. CQ
  never appears in c28_2 (no station addresses "CQ").

K is `MultiPassOptions.AP3MaxCallsigns`, default 8. Self-pairs
(c1.callsign == c2.callsign) are filtered. Worst-case pair count
per failed candidate is `(1 + K) × K`; at K=8 with no callsign
filtering, that's 72 BP+OSD runs per candidate. Hash table
snapshots are unordered (Go map iteration); the K selected on a
given run is arbitrary. Future refinement could LRU-rank or score
by candidate freq proximity.

`enumerateAP3HypothesisPairs(ht, maxK)` returns the materialised
`[]ap3HypothesisPair{c28_1, c28_2, call1, call2}` slice. Empty
hash table returns nil — AP3 is a no-op on the first slot of a
session.

### 11.4 Cascade integration

AP3 is the **last cascade stage**, running after AP-CQ has failed.
`runCascade` orchestrates:

```
N=1 → N=2 → N=3 → N1Norm → BestOfN → AP-CQ → AP3
```

Each AP3 hypothesis builds `softLLRsAP3WithMag(grid, mag, c1, c2)`
and calls `BPDecodeWithPin(llrs, bpOpts, &ap3PinMask)`. First
BP-OK wins. The `cascadeOutcome` carries `TextGuard = "<call1>
<call2>"` for standard-call hypotheses, or `"CQ"` for CQ-family
hypotheses; the outer loop rejects post-Unpack77 if the text
doesn't start with the guard.

The cascade extract during Session 104 also fixed an inconsistency
the old inlined code had: the `EnableBestOfN`-then-APCQ branch
used `BPDecode` (no pin) while the `EnableBestOfN=false`-then-APCQ
branch used `BPDecodeWithPin`. The refactor unifies both paths
under the pinned variant.

### 11.5 Corpus measurement (Session 104)

Config: K=8, mag=apCQMagnitude (default 10).

| Mode  | Matched / Extras | Δ vs 111/23 baseline | AP3-attributed |
|-------|------------------|----------------------|----------------|
| Sym   | **111 / 23**     | unchanged            | 0 matched, 0 shadow |
| Asym  | **115 / 25**     | unchanged            | 0 matched, 0 shadow |

**AP3 recovered zero truths and produced zero shadow rejects**
on the working corpus. BP+OSD-with-pin returned `ok=false` on
every AP3 hypothesis attempted across every failed candidate
across all six captures.

**Runtime cost: ~3.5 s/slot baseline → ~38 s/slot (~11×).** Full
corpus run (6 captures × sym+asym = 12 slot-decodes) jumped from
~30 s to **7 m 38 s**. The cost is dominated by the (1+K)×K
inner-loop of BP+OSD-with-pin calls per failed candidate.

Saved audit artefact: `research/sandbox/reports/ap3-2026-05-29.txt`.

### 11.6 Why AP3 produced nothing on this corpus

Even with 62 of 174 codeword bits pinned via priors + OSD pin,
BP/OSD couldn't converge to a CRC-passing codeword for any
AP3-attempted candidate. Three converging factors explain the
null result:

1. **Channel-noise-bound at the unknown bits.** The g15 (15 bits)
   + CRC14 (14 bits) + parity (83 bits) = 112 channel-driven bits
   carry weak LLRs on the failed candidates. The 62 pinned bits
   give BP a strong anchor at the systematic layer but the parity
   constraints relate every codeword bit to several others — if
   the parity bits' LLRs are too weak/noisy, BP can't satisfy
   the parity equations even with perfect c28_1 + c28_2.

2. **The QEX § 7 narrow recovery band.** The paper notes AP forms
   are most effective when channel SNR is just below BP threshold
   — the marginal-recovery zone. On a corpus where pipeline
   misses sit well below threshold (which our shadow audit + the
   sandbox cascade's null results across all priors-only stages
   confirms is our regime), AP3's leverage isn't enough to bridge
   the gap.

3. **Hash sampling lottery.** With K=8 and unordered map iteration,
   the 8 callsigns drawn on any given AP3 invocation are an
   arbitrary subset of the live hash. If the *right* (c1, c2)
   pair for a given failed candidate isn't in the K=8 sample,
   AP3 has no chance regardless of how strong the priors are.
   This is fixable (LRU + freq-proximity ranking) but only
   matters if mechanism (1) doesn't dominate — and the empirical
   measurement says it does.

### 11.7 Why AP3 is parked

`EnableAP3` defaults false; the code stays in tree alongside
`EnableBestOfN` and `EnableAPCQ` as opt-in experimental. The
durable artefacts of the AP3 effort:

- The cascade refactor (`runCascade`) is cleaner than the
  pre-Session-104 nested if-else and supports further stages
  with minimal noise.
- `CallsignHashTable.Callsigns()` is reusable for any future
  consumer that needs an enumeration snapshot (logbook UI, audit
  tools, multi-pass restart loops).
- `applyAP3PriorsForC28s` + `ap3PinMask` + the AP3 LLR generator
  are reusable for AP3 variants (LRU-ranked hash, paired
  hypothesis injection from external context, etc.).
- `LLRMetricAP3` attribution flows through the existing audit
  pipeline (shadow rejects, per-metric matched tables).

The mechanism Session 104 surfaced — priors-only AP isn't enough
leverage on this corpus — is the load-bearing finding. Any future
revival needs either a fundamentally different signal stage (the
language conversation in the Session 104 wrap notes Go's lack of
SIMD-batched BP as the structural bottleneck) or a stronger
hypothesis source than systematic-bit pinning.

### 11.8 What this section does NOT cover

- **Multi-pass restart loop** — an explicit Pass-3 that re-tries
  pass-1 + pass-2 failures with the fully-populated hash table.
  Implementation-trivial extension of the current cascade; not
  shipped because the per-candidate AP3 already returned nothing,
  so multiplying its budget would just multiply its runtime.
- **LRU + freq-proximity hash ranking** — the optimisation that
  would improve hypothesis sampling. Not shipped for the same
  reason as above.
- **OSD-3 + AP3** — already compatible via the pin machinery, but
  not measured. Would be even slower than OSD-2 AP3 — Go runtime
  budget conversation has to happen first.
