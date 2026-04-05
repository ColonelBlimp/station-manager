# FT8/FT4 Pure Go Implementation Research

## Overview

This document captures research into implementing FT8 and FT4 digital modes natively in Go,
without relying on WSJT-X. It also identifies components that have immediate value to the
project independent of a full FT8/FT4 implementation (notably audio I/O for contest CQ playback).

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

### 1. Audio I/O
- No pure Go option for low-latency audio — requires CGo
- **Recommended library:** `github.com/gen2brain/malgo` (miniaudio wrapper)
  - Already used in the `cwdecoder` project at `/home/mveary/Development/cwdecoder`
  - The `internal/audio/capture.go` in that project is a well-structured, reusable `Capture`
    struct with atomic state, context cancellation, and a `Samples chan []float32` output
  - **This code should be ported/adapted into `internal/audio/` in this project**
- Sample rate: 12000 Hz (standard for WSJT modes)
- FT8: needs 180,000 samples buffered per 15s window
- FT4: needs 90,000 samples buffered per 7.5s window
- Playback (TX) also needed — `malgo` supports both capture and playback devices

#### Immediate Value: Contest CQ Playback
Audio I/O is useful **independently** of FT8/FT4:
- Play pre-recorded CQ calls (.wav files) during contests
- Route audio to the radio via soundcard interface
- Could be implemented as a standalone `internal/audio/` package with both
  `Capture` (RX) and `Playback` (TX) capabilities
- This is a natural first milestone before any DSP work begins

### 2. Precise Timing
- FT8: TX starts at T+1s after even 15s boundary (00:00, 00:15, 00:30...)
- FT4: TX starts at T+0.5s after each 7.5s boundary
- Go's `time.Now()` is sufficient if system clock is NTP-synced (within ±1s)
- Implementation: goroutine sleeping until next window start using `time.Until()`

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
| Audio I/O + buffering (port from cwdecoder) | Low–Medium | 1 week |
| WAV file playback for contest CQ | Low | 2–3 days |
| Precise timing | Low | 2–3 days |
| FFT + spectrum analysis | Medium | 1–2 weeks |
| Soft demodulation (LLRs) | High | 2–4 weeks |
| **LDPC decoder** | **Very High** | **4–8 weeks** |
| Message pack/unpack | Medium | 2–3 weeks |
| CRC-14 | Low | 1–2 days |
| LDPC encoder (TX) | Medium | 1–2 weeks |
| Audio synthesis + GFSK | Medium | 1–2 weeks |
| QSO state machine | Medium | 1–2 weeks |
| Testing + validation against real recordings | High | 4–8 weeks |
| **Total (full FT8/FT4)** | | **~6–12 months** |

---

## Recommended Implementation Order

1. **`internal/audio/`** — Port audio capture + add playback from `cwdecoder` project
   - Immediate contest CQ playback value
   - Foundation for all subsequent DSP work
   - Library: `github.com/gen2brain/malgo` (already proven in cwdecoder)

2. **`internal/audio/wav`** — WAV file reader/writer for recorded CQ files

3. **`internal/ft8/timing`** — TX/RX window timing

4. **`internal/ft8/message`** — 77-bit message pack/unpack + CRC-14

5. **`internal/ft8/codec`** — LDPC encoder first (TX path), then decoder (RX path)
   - Port from `ft8_lib` C source as reference

6. **`internal/ft8/dsp`** — FFT pipeline, soft demodulation

7. **`internal/ft8/service`** — Top-level `ft8.Service` with `Initialize()/Start()/Stop()`
   following the existing project service pattern

---

## Existing Project Assets Applicable to This Work

| Asset | Location | Relevance |
|---|---|---|
| Audio capture (malgo) | `cwdecoder/internal/audio/capture.go` | Port to `internal/audio/` |
| Goertzel DSP detector | `cwdecoder/internal/dsp/goertzel.go` | Reference for DSP patterns |
| Maidenhead grid encoding | `internal/maidenhead/` | Used in FT8 message packing |
| Service lifecycle pattern | `internal/cat/service.go` | Model for `ft8.Service` |
| Structured error handling | `internal/errors/` | Apply throughout FT8 package |
| Dependency injection | `internal/iocdi/` | Wire ft8.Service into apps |

---

## Key External References

- WSJT-X source: https://sourceforge.net/p/wsjt/wsjtx/ci/master/tree/
- ft8_lib (clean C reference): https://github.com/kgoba/ft8_lib
- Protocol specification: `wsjtx-doc.adoc` in WSJT-X source tree
- LDPC matrix: `lib/ft8/LDPC_174_91_3_generator.f90` in WSJT-X source
