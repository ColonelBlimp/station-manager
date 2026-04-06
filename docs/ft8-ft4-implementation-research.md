# FT8/FT4 Pure Go Implementation Research

## Overview

This document captures research into implementing FT8 and FT4 digital modes natively in Go,
without relying on WSJT-X. It also identifies components that have immediate value to the
project independent of a full FT8/FT4 implementation (notably audio I/O for contest CQ playback).

**Status:** Foundational layers (audio I/O, WAV support, window timing) and message
packing/unpacking (item 4) are complete. The next milestone is LDPC codec (item 5).

---

## Full Stack Required

```
┌─────────────────────────────────────────────┐
│              QSO State Machine               │
├─────────────────────────────────────────────┤
│         Message Packer / Unpacker            │
│     (77-bit callsign/grid compression)       │
├──────────────────┬──────────────────────────┤
│   TX Path        │   RX Path                │
│                  │                          │
│  LDPC Encoder    │   LDPC Decoder ← hardest │
│  CRC-14          │   CRC-14 verify          │
│  Symbol mapper   │   Soft demodulation      │
│  GFSK synthesis  │   Symbol timing sync     │
│  Audio playback  │   FFT / spectrum         │
│                  │   Audio capture          │
├──────────────────┴──────────────────────────┤
│         Audio I/O (malgo / miniaudio)        │
├─────────────────────────────────────────────┤
│         Precise Timing (NTP-synced clock)    │
└─────────────────────────────────────────────┘
```

---

## Component Breakdown

### 1. Audio I/O ✅
- No pure Go option for low-latency audio — requires CGo
- **Library:** `github.com/gen2brain/malgo` (miniaudio wrapper)
  - Originally ported from the `cwdecoder` project at `/home/mveary/Development/cwdecoder`
- **Implemented in `internal/audio/`:**
  - `Capture` — real-time audio capture with `Samples()` channel (64-frame buffer) and
    low-latency `SetCallback` for audio-thread-direct processing
  - `Playback` — WAV file playback via `PlayFile(ctx, path)` with context cancellation
  - `readWAV` — WAV reader supporting PCM 8/16-bit and IEEE float 32-bit
  - Device enumeration, integration tests, shared `Config` struct
  - See `internal/audio/README.md` for full API reference
- Sample rate: 12000 Hz (standard for WSJT modes)
- FT8: needs 180,000 samples buffered per 15s window
- FT4: needs 90,000 samples buffered per 7.5s window

#### Gap: `PlaySamples` method needed for TX
The current `Playback` only supports `PlayFile(ctx, path)` — reading a WAV from disk.
The FT8 TX pipeline will synthesise audio in memory (`[]float32`) and needs to play it
directly. A `PlaySamples(ctx, samples []float32, sampleRate uint32) error` method (or
similar) must be added to `Playback` before TX work begins (items 5–7).

#### Contest CQ Playback (complete)
Audio I/O is useful **independently** of FT8/FT4:
- Play pre-recorded CQ calls (.wav files) during contests
- Route audio to the radio via soundcard interface
- Compose with `internal/ptt/` for PTT assert/release around playback

### 2. Precise Timing ✅
- **Implemented in `internal/ft8/timing/`**
- FT8: TX starts at T+1s after even 15s boundary (00:00, 00:15, 00:30...)
- FT4: TX starts at T+0.5s after each 7.5s boundary
- Go's `time.Now()` is sufficient if system clock is NTP-synced (within ±1s)
- API: `CurrentWindowStart`, `NextWindowStart`, `SlotParity` (even/odd), `TimeUntilTX`,
  `WaitForNext(ctx, mode)` — all pure functions except `WaitForNext` which blocks
- Integer nanosecond arithmetic avoids floating-point drift in boundary calculations

### 3. Receive DSP Pipeline

```
Audio buffer (15s / 7.5s)
  → Hann window function applied
  → FFT (typically 1920-point at 12kHz)
  → Candidate signal detection (frequency + time offset search)
  → Doppler/drift estimation
  → 79-symbol soft demodulation (LLR values, not hard 0/1)
  → Feed soft symbols to LDPC decoder
```

Key DSP note: The **soft demodulation** step — computing log-likelihood ratios (LLRs) per
symbol — is critical for decoder performance. Poor LLRs cause decode failures even at
reasonable SNR.

The `cwdecoder` project at `/home/mveary/Development/cwdecoder/internal/dsp/goertzel.go`
implements a Goertzel detector. For FT8 a full FFT approach is needed (8 tones to
discriminate simultaneously per symbol period), but the DSP infrastructure pattern from
that project is directly applicable.

### 4. LDPC Decoder — The Hardest Component

FT8 uses a **(174, 91) LDPC code**:
- 77-bit message + 14-bit CRC → 91 information bits
- LDPC encodes to 174 bits (83 parity bits added)
- Decoder uses **belief propagation** (sum-product algorithm), typically ~50 iterations
- The parity check matrix H (83×174) is defined in the WSJT-X source:
  `lib/ft8/LDPC_174_91_3_generator.f90`
- Best Go implementation path: port from [`ft8_lib`](https://github.com/kgoba/ft8_lib)
  (clean C reference implementation with well-commented LDPC code)

FT4 uses a **(152, 76) LDPC code** with similar structure.

### 5. Message Packing (77 bits)

→ *Implementation order item 4: `internal/ft8/message/` (includes CRC-14 from §6 below)*

Well-specified but intricate. Key elements:
- Standard callsigns: 28-bit encoding (base-37 charset: `0-9A-Z `, up to 6 chars)
- Grid square: 15 bits (4-char Maidenhead — project already has `internal/maidenhead/`)
- Signal report: 7 bits (-30 to +30 dB in 1 dB steps)
- Message type tag: 3 bits
- Special formats: CQ with band, directed CQ, contest exchanges, free text

Full specification in WSJT-X source: `lib/ft8/pack77.f90` and `wsjtx-doc.adoc`.

### 6. CRC-14
- Generator polynomial: `0x2757`
- Computed over 77 message bits + 3-bit mode indicator = 80 bits
- 14-bit result appended → 91 bits total before LDPC encoding
- Straightforward to implement in Go

### 7. Transmit DSP Pipeline (simpler than receive)

```
77-bit message
  → CRC-14 → 91 bits
  → LDPC encode → 174 bits
  → Map 3-bit groups → 8-tone symbols (58 data symbols)
  → Insert 3 Costas sync arrays (7 symbols each = 21 sync symbols)
  → Total: 79 symbols at 6.25 baud (FT8) or 4-FSK at 12.0 baud (FT4)
  → GFSK smoothing (Gaussian filter on frequency transitions)
  → Synthesize audio at target frequency offset (typically 1000–2000 Hz AF)
  → Play at precise T+1s start time
```

Symbol details (FT8):
- Symbol period: 160ms (6.25 Hz baud rate)
- 8 tones spaced 6.25 Hz apart
- Costas arrays at symbol positions 0, 36, 72

### 8. QSO State Machine

Standard FT8 exchange sequence:
```
CQ DE W1AW FN31
W1AW VK2XYZ -12
VK2XYZ W1AW R-08
W1AW VK2XYZ RR73
```

Needs: slot selection (even/odd 15s cycle), timeout handling, duplicate suppression,
RRR/RR73 handling, optional simultaneous multi-QSO support.

---

## Effort Estimate

| Component | Complexity | Rough Effort |
|---|---|---|
| ~~Audio I/O + buffering~~ | ~~Low–Medium~~ | ✅ Complete |
| ~~WAV file playback for contest CQ~~ | ~~Low~~ | ✅ Complete (reader; writer not needed for pipeline) |
| ~~Precise timing~~ | ~~Low~~ | ✅ Complete |
| `Playback.PlaySamples` for TX | Low | 2–3 days |
| FFT + spectrum analysis | Medium | 1–2 weeks |
| Soft demodulation (LLRs) | High | 2–4 weeks |
| **LDPC decoder** | **Very High** | **4–8 weeks** |
| Message pack/unpack + CRC-14 | ~~Medium~~ | ✅ Complete |
| LDPC encoder (TX) | Medium | 1–2 weeks |
| Audio synthesis + GFSK | Medium | 1–2 weeks |
| QSO state machine | Medium | 1–2 weeks |
| Testing + validation against real recordings | High | 4–8 weeks |
| **Remaining (full FT8/FT4)** | | **~5–11 months** |

---

## Recommended Implementation Order

1. ~~**`internal/audio/`** — Audio capture + playback~~ ✅
   - `Capture`, `Playback`, `SetCallback`, device enumeration
   - Library: `github.com/gen2brain/malgo`

2. ~~**`internal/audio/wav.go`** — WAV file reader for recorded CQ files~~ ✅
   - Reader only (PCM 8/16-bit, IEEE float 32-bit). Writer not needed for the FT8 pipeline.

3. ~~**`internal/ft8/timing/`** — TX/RX window timing~~ ✅
   - `Mode`, `Parity`, `CurrentWindowStart`, `NextWindowStart`, `SlotParity`,
     `TimeUntilTX`, `WaitForNext`

4. **`internal/ft8/message/`** — 77-bit message pack/unpack + CRC-14 ✅
   - `Pack`/`Unpack` dispatching to Type 1 (standard) and Type 0 (free text)
   - `Append91` (77→91 bits with CRC-14)
   - 3DA0/3X callsign workarounds
   - Full test coverage with ft8_lib cross-checked vectors

5. **`internal/ft8/codec/`** — LDPC encoder first (TX path), then decoder (RX path) ← **next**
   - Port from `ft8_lib` C source as reference

6. **`internal/ft8/dsp/`** — FFT pipeline, soft demodulation

7. **`internal/ft8/service/`** — Top-level `ft8.Service` with `Initialize()/Start()/Stop()`
   following the existing project service pattern

---

## Existing Project Assets Applicable to This Work

| Asset | Location | Relevance |
|---|---|---|
| Audio I/O (capture + playback) | `internal/audio/` | ✅ Foundation for all DSP work |
| WAV reader | `internal/audio/wav.go` | ✅ Reads PCM/float WAV files for playback |
| FT8/FT4 window timing | `internal/ft8/timing/` | ✅ Window boundaries, slot parity, TX offset, wait-for-next |
| PTT control (serial RTS/DTR) | `internal/ptt/` | Assert/Release TX during FT8/FT4 transmit |
| Maidenhead grid encoding | `internal/maidenhead/` | Used in FT8 message packing |
| Service lifecycle pattern | `internal/cat/service.go` | Model for `ft8.Service` |
| Structured error handling | `internal/errors/` | Apply throughout FT8 package |
| Dependency injection | `internal/iocdi/` | Wire ft8.Service into apps |
| Goertzel DSP detector | `cwdecoder/internal/dsp/goertzel.go` | Reference for DSP patterns |
| Original audio capture | `cwdecoder/internal/audio/capture.go` | Historical — ported to `internal/audio/` |

---

## Key External References

- WSJT-X source: https://sourceforge.net/p/wsjt/wsjtx/ci/master/tree/
- ft8_lib (clean C reference): https://github.com/kgoba/ft8_lib
- Protocol specification: `wsjtx-doc.adoc` in WSJT-X source tree
- LDPC matrix: `lib/ft8/LDPC_174_91_3_generator.f90` in WSJT-X source
