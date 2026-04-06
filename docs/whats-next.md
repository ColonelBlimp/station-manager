# What's Next: `internal/ft8/codec/` — LDPC (174,91) Encoder & Decoder

Per the [implementation research](docs/ft8-ft4-implementation-research.md), **item 5** is next:

> `internal/ft8/codec/` — LDPC encoder first (TX path), then decoder (RX path)

## Current State

Items 1–4 are **complete**:

| Item | Package | Status |
|---|---|---|
| 1. Audio I/O | `internal/audio/` | ✅ Complete |
| 2. WAV reader | `internal/audio/wav.go` | ✅ Complete |
| 3. Window timing | `internal/ft8/timing/` | ✅ Complete |
| 4. Message pack/unpack + CRC-14 | `internal/ft8/message/` | ✅ Complete |

The message package provides `Append91()` which takes a 77-bit packed message,
computes CRC-14, and returns a 91-bit payload in a `[12]byte` — the exact input
the LDPC encoder needs.

## Key Constants

```
N = 174   codeword length (total coded bits)
K =  91   information bits (77 message + 14 CRC)
M =  83   parity bits (N − K)
```

The code is an **irregular LDPC** with variable-node degree 3 (every coded bit
participates in exactly 3 parity checks).

## What Needs to Be Built

### 1. Constants & matrix data — `constants.go`

Define the package-level constants and the three matrix representations sourced
from ft8_lib `ldpc.c` (`kFT8_Nm`, `kFT8_Mn`, `kFT8_Num_rows`) and WSJT-X
`LDPC_174_91_3_generator.f90`:

| Array | Shape | Purpose |
|---|---|---|
| `Nm [N][3]uint8` | 174 × 3 | Each variable node's 3 check-node neighbours (1-indexed) |
| `Mn [M][7]uint8` | 83 × 7 | Each check node's up-to-7 variable-node neighbours (1-indexed) |
| `MnCount [M]uint8` | 83 | Degree (active column count) for each check-node row |
| `G [M][12]byte` | 83 × 91 bits | Generator matrix — 83 rows of 91 bits, bit-packed MSB-first |

`G` is only used by the encoder; `Nm`/`Mn`/`MnCount` are used by both encoder
(parity verification) and decoder (Tanner graph traversal).

### 2. LDPC Encoder — `encoder.go`

```go
func Encode(info [12]byte) [22]byte
```

Takes the 91-bit information payload (from `message.Append91`) and returns the
174-bit codeword packed MSB-first into 22 bytes.

**Algorithm (systematic encoding):**
1. Copy the 91 information bits into output positions 0–90.
2. For each parity bit `p` (0..82): compute `GF(2)` dot product of generator
   row `G[p]` with the 91 info bits — i.e. `popcount(G[p] AND info) mod 2`.
   Use `math/bits.OnesCount8` for the byte-wise inner product.
3. Write parity bit `p` into output position `91 + p`.

This produces the 174-bit codeword where bits 0–90 are information and bits
91–173 are parity.

### 3. Encoder tests — `encoder_test.go`

- **All-zero input** → all-zero codeword.
- **Known messages** from the message package test vectors → encode → verify
  all 83 parity checks pass (`H × codeword = 0` using the `Mn` table).
- **Single-row exercise**: for each of the 83 generator rows, set a single
  info bit and verify the corresponding parity pattern.
- **Reference vectors**: store a few ft8_lib-verified 174-bit codewords in
  `testdata/encoder_vectors.json` for regression.

### 4. LDPC Decoder — `decoder.go`

```go
func Decode(llr [N]float32, maxIter int) (info [12]byte, ok bool)
```

Takes 174 log-likelihood ratios (positive = bit more likely 0, negative = bit
more likely 1) and returns the decoded 91-bit information payload.

**Algorithm (normalised min-sum belief propagation):**

1. **Init**: variable-to-check messages `m_vc[i][j]` ← `llr[i]` for each edge
   in the Tanner graph (variable node `i`, check node `j`).

2. **Iterate** (up to `maxIter`, typically 50):

   a. **Check-to-variable update** (for each check node `c`, for each connected
      variable node `v`):
      - Product of signs of all other incoming `m_vc` messages (excluding `v`)
      - Minimum magnitude of all other incoming `m_vc` messages (excluding `v`)
      - Scale by `β = 0.8` (normalised min-sum approximation)
      - `m_cv[c][v] = sign × β × min_mag`

   b. **Variable-to-check update** (for each variable node `v`, for each
      connected check node `c`):
      - `m_vc[v][c] = llr[v] + Σ(m_cv[c'][v])` for all check nodes `c' ≠ c`

   c. **Hard decision**: `a_posteriori[v] = llr[v] + Σ(m_cv[c][v])` for all
      check nodes `c` connected to `v`. Hard bit = `a_posteriori < 0 ? 1 : 0`.

   d. **Syndrome check**: compute `H × hard_bits`. If all 83 checks pass,
      extract bits 0–90 → `info`, return `ok=true`.

3. If `maxIter` iterations complete without convergence → return `ok=false`.

### 5. Decoder tests — `decoder_test.go`

- **Perfect LLRs**: encode a known message → convert hard bits to `±6.0` LLR
  values → decode → verify exact match. (Should converge in 1 iteration.)
- **Round-trip with noise**: `Pack → Append91 → Encode → add Gaussian noise
  (seeded PRNG) → soft LLR input → Decode → CRC14 verify`.
- **Realistic SNR (~−21 dB)**: several seeded noisy trials at FT8 threshold to
  verify the decoder handles realistic conditions.
- **Failure case**: uniformly random LLRs → should return `ok=false`.
- **Max iterations respected**: verify convergence in ≤ expected iteration count
  for clean inputs.

### 6. High-level convenience — `codec.go`

```go
// EncodeMessage takes a 77-bit packed message, appends CRC-14, and LDPC-encodes
// to a 174-bit codeword.
func EncodeMessage(msg77 [10]byte) [22]byte

// DecodeMessage takes 174 soft LLRs, LDPC-decodes, verifies CRC-14, and returns
// the 77-bit message. Returns ok=false if decode or CRC fails.
func DecodeMessage(llr [174]float32, maxIter int) (msg77 [10]byte, ok bool)
```

These bridge `message.Append91` / `message.CRC14` with the raw LDPC functions,
providing clean entry points for the TX and RX pipelines.

## Suggested Implementation Order

```
constants.go → encoder.go + encoder_test.go → decoder.go + decoder_test.go → codec.go
```

Start with the encoder — it is straightforward matrix multiplication and
immediately testable. The decoder is the hardest component in the entire FT8
stack (estimated 4–8 weeks) and benefits from having the encoder available to
generate test inputs.

## Matrix Data Provenance

- **Generator matrix `G`**: transcribe from WSJT-X `lib/ft8/LDPC_174_91_3_generator.f90`.
  This is a 83×91 binary matrix in Fortran, row-major. Convert to `[83][12]byte`
  bit-packed MSB-first (pad each 91-bit row to 96 bits in 12 bytes, unused high
  bits zero).

- **Tanner graph arrays `Nm`/`Mn`/`MnCount`**: transcribe from ft8_lib `src/ft8/ldpc.c`
  arrays `kFT8_Nm[174][3]`, `kFT8_Mn[83][7]`, `kFT8_Num_rows[83]`. Values are
  1-indexed; adjust to 0-indexed in the encoder/decoder loop code (not in the
  stored arrays, to keep them auditable against the reference).

## Design Notes

- **Min-sum scaling factor**: use `β = 0.8` (matching ft8_lib). Define as an
  unexported constant. If future tuning is needed, promote to a `Decoder` struct
  field.

- **FT4 forward-compatibility**: FT4 uses the same (174,91) code structure but
  may have different matrices (TBC). For now, hardcode FT8 tables only and mark
  with `// TODO: FT4 matrices` where applicable.

- **No allocations in the hot path**: the decoder inner loop should use
  fixed-size arrays, not slices, to avoid GC pressure during real-time decode.

## After This Milestone

The next item (6) is **`internal/ft8/dsp/`** — FFT pipeline and soft
demodulation. With the codec complete, the full encode chain is:
`message.Pack → message.Append91 → codec.Encode → symbol mapping → GFSK synthesis`.

