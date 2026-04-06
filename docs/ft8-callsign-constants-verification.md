# ft8_lib Constant Verification Report

**Verified against:** ft8_lib HEAD `9fec6ca` (master) — `kgoba/ft8_lib`  
**Source files checked:** `ft8/message.c`, `ft8/text.c`, `ft8/text.h`, `ft8/message.h`  
**Go file:** `internal/ft8/message/callsign.go`  
**Date:** 2026-04-06

## Result: ✅ All constants match exactly

---

### 1. Core Constants

| Constant | Go value | ft8_lib value | Source | Match |
|---|---|---|---|---|
| `NBase` | 262,177,560 | 37×36×10×27×27×27 = 262,177,560 | Implicit in `pack_basecall()` | ✅ |
| `Max22` | 4,194,304 | `#define MAX22 ((uint32_t)4194304ul)` | `message.c:9` | ✅ |
| `NTokens` | 2,063,592 | `#define NTOKENS ((uint32_t)2063592ul)` | `message.c:10` | ✅ |

### 2. Sentinel Tokens

| Token | Go value | ft8_lib (`pack28()`) | Match |
|---|---|---|---|
| `TokenDE` | 0 | `equals(callsign, "DE") → return 0` | ✅ |
| `TokenQRZ` | 1 | `equals(callsign, "QRZ") → return 1` | ✅ |
| `TokenCQ` | 2 | `equals(callsign, "CQ") → return 2` | ✅ |

### 3. CQ Sub-Ranges

| Range | Go value | ft8_lib derivation | Match |
|---|---|---|---|
| `tokenCQNumBase` | 3 | `pack28(): return 3 + v` where `parse_cq_modifier()` returns `atoi(freq)` (0–999) | ✅ |
| `tokenCQNumMax` | 1,002 | 3 + 999 = 1,002 | ✅ |
| `tokenCQSufBase` | 1,003 | `pack28(): return 3 + v` where `parse_cq_modifier()` returns `1000 + m` | ✅ |
| `tokenCQSufMax` | 532,443 | 1,003 + 27⁴ − 1 = 1,003 + 531,440 = 532,443. Confirmed by `unpack28(): if (n28 <= 532443ul)` | ✅ |

### 4. Field Regions

| Region | Go value | ft8_lib equivalent | Match |
|---|---|---|---|
| `hashBase` | `NTokens` = 2,063,592 | `pack28(): return NTOKENS + n22` | ✅ |
| `callBase` | `NTokens + Max22` = 6,257,896 | `pack28(): return NTOKENS + MAX22 + n28` | ✅ |

### 5. 28-Bit Field Layout

```
NTokens + Max22 + NBase − 1 = 2,063,592 + 4,194,304 + 262,177,560 − 1 = 268,435,455 = 2²⁸ − 1  ✅
```

The three regions exactly fill the 28-bit field with no gaps or overlaps.

### 6. Mixed-Radix Encoding (pack_basecall / EncodeCallsign)

ft8_lib `pack_basecall()` uses `nchar()` with these character tables:

| Position | ft8_lib table | Size | Go mapping | Match |
|---|---|---|---|---|
| c[0] | `FT8_CHAR_TABLE_ALPHANUM_SPACE` | 37 | `charsetIndex()` → 0–36 | ✅ |
| c[1] | `FT8_CHAR_TABLE_ALPHANUM` | 36 | `charsetIndex() − 1` → 0–35 | ✅ |
| c[2] | `FT8_CHAR_TABLE_NUMERIC` | 10 | `charsetIndex() − 1` → 0–9 | ✅ |
| c[3–5] | `FT8_CHAR_TABLE_LETTERS_SPACE` | 27 | `letterSpaceMR()` → 0–26 | ✅ |

Encoding formula matches: `n = i0; n = n*36+i1; n = n*10+i2; n = n*27+i3; n = n*27+i4; n = n*27+i5;`

### 7. Basecall Values Cross-Checked via Compiled ft8_lib

| Callsign | ft8_lib `pack_basecall()` | Go test expects | n28 (ft8_lib) | n28 (Go) | Match |
|---|---|---|---|---|---|
| W1AW | 6,319,593 | 6,319,593 | 12,577,489 | 12,577,489 | ✅ |
| K1ABC | 3,957,069 | 3,957,069 | 10,214,965 | 10,214,965 | ✅ |
| VK2XYZ | 230,742,323 | 230,742,323 | 237,000,219 | 237,000,219 | ✅ |
| 9A1A | 72,847,512 | 72,847,512 | 79,105,408 | 79,105,408 | ✅ |
| ZZ9ZZZ | 262,177,559 | NBase−1 | 268,435,455 | 268,435,455 | ✅ |

### 8. Decode Verification (unpack28)

The `unpack28()` boundaries match exactly:
- `n28 <= 2u` → DE/QRZ/CQ
- `n28 <= 1002u` → CQ nnn (subtract 3)
- `n28 <= 532443ul` → CQ suffix (subtract 1003)
- `n28 < NTOKENS` but > 532443 → reserved (error)
- `NTOKENS ≤ n28 < NTOKENS + MAX22` → 22-bit hash
- `≥ NTOKENS + MAX22` → standard callsign (subtract NTOKENS + MAX22)

### 9. Special Prefix Workarounds ✅

ft8_lib's `pack_basecall()` and `unpack28()` contain special-case handling for:
- **Swaziland (3DA0):** `3DA0XYZ → 3D0XYZ` (pack) / `3D0XYZ → 3DA0XYZ` (unpack)
- **Guinea (3X):** `3XA0XYZ → QA0XYZ` (pack) / `QA... → 3XA...` (unpack)

These workarounds are implemented in `callsign.go` as `packCallWorkaround()` (encode-time
remapping) and `unpackCallWorkaround()` (decode-time reversal), with full test coverage in
`callsign_workaround_test.go` including round-trip, Pack/Unpack integration, and edge cases.

---

**Conclusion:** All constants, sentinel values, CQ sub-range offsets, mixed-radix encoding/decoding, and special prefix workarounds in `callsign.go` are verified correct against ft8_lib HEAD (`9fec6ca`). No changes needed.

