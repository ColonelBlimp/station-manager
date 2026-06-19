# `internal/ft8/codec` code review (session, 2026-05-19)

Scope: every Go file in `internal/ft8/codec/` at the current main tip
(post-Phase 2C/2D — Type 1 round-trip + Format/Parse text layer shipped).
~3,300 LOC non-test, ~3,400 LOC test (file-by-file count in §Inventory
below). Spec alignment checked against
[`/home/mveary/Downloads/FT4_FT8_QEX.pdf`](../../Downloads/FT4_FT8_QEX.pdf)
(Franke / Somerville / Taylor, QEX July/August 2020) — primarily §3
(error-correction layout), §6 (decoder architecture), §9 (licensing),
and Appendix A + Table 7 (c28 partitioning and per-tag field
semantics). The vendored public-domain reference programs in
`qexref14/` were spot-read against the Go implementations
(`std_call_to_c28.f90` in particular).

## Overall assessment

Very strong. The package is the most carefully-documented Go I've
read in this codebase — every primitive opens with a why-it-exists
paragraph, every magic number has either a QEX section reference or
a derivation, and the panic-vs-error split is consistent and
defensible (caller-bug → panic, wire-bug → error). The Layer 1
spec-vector tests pin against public-domain oracle output rather
than algorithm self-equality, and the LDPC matrix-property tests
(column weight 3, row weight 6/7, total 522) verify QEX §3 invariants
directly rather than trusting the embed.

Spec alignment is solid for the surface that's been implemented
(Type 1 / Table 1 layout, CRC14 / Table 2 c28 + g15 + g25 widths,
LDPC matrix dimensions, Token Table 7 boundaries). I found no
divergence from the spec text itself. The substantive findings are
about defence-in-depth (two paths that should agree but don't quite,
one tautological layout test, one parity-oracle gap that the doc
already acknowledges as Layer 5 work).

Findings are ordered roughly by impact. All are "the code is correct
today but the test or interaction shape lets a future change break
something silently."

---

## Findings

### 1. `EncodeMessage` accepts Type 1 tokens with rover bits set; `FormatMessage` rejects them (medium)

`encode.go:68-94` (`encodeStd`) validates Call1/Call2 via
`validateType1Call` (which accepts tokens) and the grid slot via
`validateG15Slot`, then unconditionally writes `boolBit(m.Rover1)`
and `boolBit(m.Rover2)` into the message body. There is no guard
matching `format.go:59-64`:

```go
if _, isTok := TokenToC28(m.Call1); isTok && m.Rover1 {
    return "", errors.New(op).WithMsgf("Call1 = %q is a token; Rover1 cannot be set on a non-callsign", m.Call1)
}
if _, isTok := TokenToC28(m.Call2); isTok && m.Rover2 {
    return "", errors.New(op).WithMsgf("Call2 = %q is a token; Rover2 cannot be set on a non-callsign", m.Call2)
}
```

Consequence: a caller can construct `Message{Call1: "CQ", Rover1:
true, ...}`, get a clean 77-bit body from `EncodeMessage`, then have
`FormatMessage` reject the same struct. The bit pattern is
spec-compliant (the rover bit is part of the wire format), but a
struct that encodes is supposed to also format — that's the
text-layer contract the round-trip tests in `parse_test.go:146-204`
exercise.

Today the round-trip tests don't catch this because their inputs
never set rover on a token. A future Phase 3 type that wires rover
semantics differently could land a test with `Call1="CQ"` and a
rover bit set, pass `EncodeMessage`, and only fail `FormatMessage` —
or vice versa, depending on which side the test hits first.

Two options:
- Hoist the token+rover guard into a shared `validateMessage(m)`
  helper used by both `encodeStd` and `formatStd`.
- Or document at `encodeStd` that EncodeMessage is the "wire-faithful"
  encoder and the higher-level `FormatMessage` is where semantic
  guards live — and add a one-liner test that asserts the asymmetry
  is intentional so a reader sees it pinned.

I'd lean (a). The cost is one helper function; the win is that
"`EncodeMessage` and `FormatMessage` reject the same things" becomes
a structural property.

### 2. The Type 1 layout test is tautological — it uses the SUT to derive its `want` (medium)

`encode_test.go:158-179`:

```go
want := make([]byte, 0, MessageBits)
want = append(want, bitsOfValue(uint64(stdCallToC28(tc.msg.Call1)), CallsignBits)...)
want = append(want, boolBitByte(tc.msg.Rover1))
want = append(want, bitsOfValue(uint64(stdCallToC28(tc.msg.Call2)), CallsignBits)...)
... etc ...
if !slices.Equal(got, want) { ... }
```

The doc comment acknowledges this ("Layer 1's tests own each
primitive's value correctness") — but as written, a regression in
`stdCallToC28` that flipped the c28 result would propagate to BOTH
`got` and `want`, and `TestEncodeMessage_Type1_LayoutMatchesQEXTable1`
would still pass. The test only catches layout reorderings, not
value corruption.

Similarly, `TestStdCallToC28_K1JTUsesActualHash` (decode_test.go:234)
pins `stdCallToC28("K1JT") == 4159881`, but 4159881 is the output of
`HashedCallC28("K1JT")` itself — so the test pins *internal*
consistency, not what WSJT-X would actually transmit for K1JT.

The honest answer is that no parity oracle for the full message
exists yet — `jt9` is documented as the eventual Layer 5 check.
For now, two cheap improvements:

- Pin **one** canonical case to a hex-encoded literal expected
  output for the entire 77-bit body. The QEX paper's example QSO
  ("Tx6: CQ K1JT FN20" on p.12) is the natural choice, and once
  Layer 5 `jt9` integration arrives you can pin a second one as a
  cross-validation. A single literal-bits assertion catches any
  drift in `stdCallToC28`'s output without waiting for Layer 5.
- Add a one-line `t.Log` or doc-comment note inside this test that
  it's structurally tautological pending Layer 5, so a reader doesn't
  mistake green CI for spec verification.

### 3. `stdCallToC28` short-call routing has no external parity oracle (medium, related to #2)

`encode.go:130-150` routes 3-4 char calls and 5-char-2-prefix calls
through `HashedCallC28` rather than `CallsignC28`. This is
*provably* correct per QEX Table 7 (p.16) — `std_call_to_c28.f90`
produces 6,040,944 for K1JT, which lands in the 22-bit hash range
[2063592, 6257895] rather than the std-call range starting at
6,257,896, so the receiver can only recover K1JT if the sender used
the actual 22-bit hash (4159881 = HashedCallC28("K1JT")) on the wire.

But the spec doesn't say this in so many words — Table 7's boundary
implies it, and `pack77.f90` (WSJT-X's higher-level packer that
makes the routing decision) is GPL and off-limits per ADR 0021.
SM's choice is the only routing that makes sense given Table 7, but
nothing in the codec confirms WSJT-X's actual wire choice matches
SM's. The `TestStdCallToC28_RoutingBasedOnShape` test (decode_test.go:181)
re-uses the SUT's `HashedCallC28` to derive `want` — same
tautology pattern as #2.

Suggested action: when Layer 5 lands (jt9 parity oracle in
`cmd/ft8-corpus-prep`), add a vector pinning the bit pattern WSJT-X
emits for "CQ K1JT FN20" against SM's output. Until then, the
codec's behaviour is internally consistent and Table-7-consistent,
which is the strongest claim available — but the test comments
should say so explicitly so future readers don't mistake the
green test bar for ground-truth confirmation.

### 4. `decodeStd` reads rover bits unconditionally — token+rover wire pattern decodes into an unformattable Message (small)

`decode.go:88-124`:

```go
c28First := uint32(readBitsUint64(bits, 0, CallsignBits))
rover1 := bits[28] == 1
...
```

If a 77-bit body arrives with c28First in the token partition (e.g.
CQ) AND bits[28]==1, `DecodeMessage` returns
`Message{Call1: "CQ", Rover1: true, ...}` cleanly. That struct, fed
back to `FormatMessage`, would error out per `format.go:59-64`.

This is the receive-side mirror of finding #1. The encoder never
emits this combination if the encoder gains the guard from #1, but
a remote encoder (other WSJT-X variants, malformed corpus, fuzz
input, post-LDPC-but-pre-validation corruption) could. The codec
currently has no policy: does the wire bit-pattern win (decode silently,
let the caller filter), or does the semantic guard win (return
`ErrTokenWithRover` or similar)?

I'd lean toward "wire bit-pattern wins, but document the asymmetry
with `FormatMessage`" — the codec layer's job is bit-faithful
decode; rejecting a wire-valid-but-spec-violating combination is the
service layer's call. Pin this with a test ("token c28 with rover
bit set decodes; FormatMessage on the result errors") so the
intentional asymmetry is recorded.

### 5. `EncodeMessage` returns `BitBuilder.Bits()` directly — relies on per-call allocation for independence (small)

`encode.go:93` returns `b.Bits()`. The BitBuilder doc
(bitbuilder.go:68-71) is explicit:

> The returned slice aliases the builder's internal storage —
> callers that mutate it (or that keep using the builder afterwards)
> will corrupt subsequent appends.

`TestEncodeMessage_Type1_ResultIsIndependent` (encode_test.go:353)
passes today because every `encodeStd` constructs a fresh BitBuilder
on the stack — no aliasing across calls. But Phase 4's belief-
propagation decoder will likely pool BitBuilders (or, equivalently,
the encode hot path during contest TX queues). A `sync.Pool`-backed
BitBuilder reuse would silently break `Result independence`.

Two ways out:
- Have `EncodeMessage` detach with `slices.Clone(b.Bits())` at the
  return — costs one 77-byte copy per encode, which is invisible
  next to the LDPC encode that follows.
- Document at `EncodeMessage` that "the returned slice is owned by
  the caller and safe to mutate; any future pooling of BitBuilder
  internals must respect this boundary." Then keep the existing
  test as the structural pin.

(a) is cheaper to reason about and matches what callers
intuitively expect from a top-level codec function. I'd take it.

### 6. `isLongFormatStdCallsign` is project-coined terminology that may confuse future readers (small)

`encode.go:140-150` defines:

```go
func isLongFormatStdCallsign(s string) bool {
    switch len(s) {
    case 5:
        // 1-char prefix: [letter][digit][letter]{3}
        return isLetter(s[0]) && isDigit(s[1]) && allLetters(s[2:])
    case 6:
        // 2-char prefix: [alnum]{2} ≥1-letter + [digit] + [letter]{3}
        return allAlnum(s[:2]) && hasLetter(s[:2]) && isDigit(s[2]) && allLetters(s[3:])
    }
    return false
}
```

The QEX paper's std-callsign definition (§A, p.15) is "a one- or
two-character prefix, at least one of which must be a letter,
followed by a decimal digit and a suffix of up to three letters" —
which includes 3-char (M1A), 4-char (K1JT), and 5-char-2-prefix
(VK7MO) calls. SM's "long-format" is a separate concept: "shapes
whose `CallsignC28` output lands at or above `stdCallOffset`."

The name conflates two ideas. A reader trying to map this back to
the spec will fail until they read the doc paragraph. Rename
candidates: `callsignC28LandsInStdRange`, `directlyEncodableViaC28`,
`fitsStdCallC28Partition`. None is pretty, but each makes the
algorithmic property explicit instead of inventing a category
("long-format") that has no spec basis.

### 7. `formatSignedReport` has an unreachable-by-Grid4ToG15 branch (small)

`grid.go:209-224`:

```go
func formatSignedReport(n int) string {
    sign := byte('+')
    mag := n
    if n < 0 {
        sign = '-'
        mag = -n
    }
    if mag < 10 {
        return string([]byte{sign, '0', byte('0' + mag)})
    }
    if mag < 100 {
        return string([]byte{sign, byte('0' + mag/10), byte('0' + mag%10)})
    }
    // Out-of-protocol range (3+ digits magnitude). Spell out fully.
    return string(sign) + strconv.Itoa(mag)
}
```

The third branch is reachable only if `n` exceeds ±99. `Grid4ToG15`
caps reports at the g15 range, which by the partition math caps
magnitude at maxG15-maxGrid4-bias = 32767-32400-35 = 332 in
principle — but `G15ToGrid4` arithmetic only reaches the 3-digit
branch for corrupted wire input lying outside the FT8 protocol
band. No test covers this. Either:
- Add a test for the 3-digit branch with a corrupted-wire g15
  value (so the branch is pinned), or
- Drop it and panic instead (matches the codec's "internal bug →
  panic" convention; the branch only fires when wire data violates
  the protocol band, which by the package's split should surface as
  an error, not a silent rendering).

I'd take the test addition — it's cheap and exercises the
out-of-band-decode case directly, which is exactly the kind of
codepoint a corpus fuzz would generate.

### 8. `validateStdCallsign` double-checks the alphabet (very small)

`encode.go:200-209`:

```go
for i := range len(call) {
    c := call[i]
    if !(c >= '0' && c <= '9') && !(c >= 'A' && c <= 'Z') {
        return errors.New(op).WithMsgf("...invalid character %q at index %d...", ...)
    }
}
if !isStdCallsignShape(call) { ... }
```

`isStdCallsignShape` already enforces `allAlnum(prefix)`,
`isDigit(digit)`, `allLetters(suffix)` — the per-char loop above
is redundant. Removing it changes the error message (the shape
gate's diagnostic is less specific about which character is bad),
which is the actual value of the explicit loop — better
per-character diagnostics. Acceptable as-is; flagging only because
the code looks redundant on first read.

### 9. `HashCodes` returns three values when most callers want one (very small, design)

`hashcodes.go:79`:

```go
func HashCodes(callsign string) (h10, h12, h22 uint32) {
```

Phase 2C's only caller is `HashedCallC28` which discards h10 and
h12. Phase 4 will add h10/h12 consumers in EU VHF and DXpedition
packers. The current shape — "always compute all three, return
all three" — is fine for now (the cost is three shifts vs one).
Once Phase 4 lands, a `HashCodeN(callsign, nbits)` selector with
the three-output as a backward-compat wrapper might be cleaner,
but I'd defer that until both consumers are wired and the actual
call shape can be designed against real usage.

### 10. CRC14 inner loop's redundant first iteration is preserved by design — worth a one-line test (very small)

`crc14.go:90-114` documents that the first loop iteration redundantly
reloads `r[crc14RegBits-1]` for Fortran-reference parity. The
explanation is convincing; no behavioural concern. But the
`TestCRC14_SpecVectors` table doesn't include the specific "first
14 bits + zeros" case that would catch a regression from removing
the redundant reload. The `first14_set` vector at crc14_test.go:56
gets close but uses 14 1s, not "the message that exercises the
fold-back path." Not blocking; flagging because the doc comment
goes to length explaining a subtle property that no test pins.

---

## Spec alignment notes

Cross-checked against the QEX paper:

- **§3, FEC layout**: 77-bit message → +14 CRC → 91 → +83 LDPC parity
  → 174 codeword. SM constants `MessageBits=77`, `CRCBits=14`,
  `InfoBits=91`, `ParityBits=83`, `CodewordBits=174` all match.
  LDPC matrix dims (83 × 91 generator, 83 × 174 parity) match.
  Column weight 3, row weight 6 or 7 — verified by
  `TestLDPCParity_ColumnsHaveExactly3Ones` and
  `TestLDPCParity_RowWeightsAre6or7`. ✓
- **§A, c28 width**: 28 bits per Table 7. SM `CallsignBits=28`. The
  Table 7 partition boundaries (DE=0, QRZ=1, CQ=2, "CQ 000-999"
  = 3-1002, "CQ A-ZZZZ" rows = 1004-532443, hash @ nTokens=2063592,
  std-call offset = 6257896 = nTokens+max22, max c28 = 2^28-1) are
  all pinned by `token_test.go` and `callsign_test.go` against the
  paper. ✓
- **§A, g15**: 15 bits, 4-char Maidenhead → 18×18×10×10 = 32400,
  plus 5 reserved + signed-report band. SM `G15Bits=15`,
  `maxGrid4=32400`, reserved slots at maxGrid4+1..+4. Report bias
  35 matches `n stored as maxGrid4 + n + 35`. ✓
- **§A, g25**: 6-char Maidenhead via 18·18·10·10·24·24 = 18,662,400
  < 2^25. SM `G25Bits=25`, `Grid6ToG25` formula matches. ✓
- **§A, f71**: 13 chars × 42 alphabet = 42^13 ≈ 9.27×10^20 < 2^71.
  SM `F71Bits=71`, `f71MessageLen=13`, alphabet matches
  `' 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ+-./?'` verbatim. The
  two-word (hi, lo) accumulator is the right shape for the
  64-bit-overflow case. ✓
- **§A, c58**: 11 chars × 38 alphabet = 38^11 ≈ 2.39×10^17 < 2^58.
  SM `C58Bits=58`, `nonstdCallLen=11`, alphabet shared with
  `hashAlphabet`. ✓
- **Table 1, Type 1 layout**: `c28 | r1 | c28 | r1 | R1 | g15 | i3`,
  widths 28+1+28+1+1+15+3 = 77. SM `encodeStd` writes exactly this
  order; `decodeStd` reads exactly this order; bit positions
  verified by `TestEncodeMessage_Type1_BitPositions`. ✓
- **Table 1, i3=1**: SM constant `i3Std=1`. ✓
- **§3, CRC polynomial 0x6757**: `crc14Poly` is
  `0b110_0111_0101_0111` = 0x6757. ✓
- **§9, licensing**: reference [14] is public domain; SM vendors
  generator.dat, parity.dat, arrl_rac_sections.txt,
  states_provinces.txt with provenance documented in
  `qexref14/README.md`. WSJT-X main source not consulted. ✓

I didn't find a single spec divergence in the implemented surface.

## Inventory

| File | Code LOC | Test LOC | Notes |
|---|---:|---:|---|
| `bitbuilder.go` | 79 | 298 | MSB-first bit composer, output-range guards |
| `bits.go` | 68 | 252 | Pack/Unpack with fuzz target |
| `callsign.go` | 298 | 260 | CallsignC28 + C28ToCallsign inverse |
| `crc14.go` | 122 | 121 | 14-bit shift-register, gen_crc14 parity |
| `decode.go` | 177 | 509 | Type 1 decode + Phase 2D token+gap paths |
| `doc.go` | 91 | – | Package-level architecture doc |
| `encode.go` | 319 | 417 | Type 1 encode + validators |
| `format.go` | 109 | 178 | Message → text (Phase 2D) |
| `freetext.go` | 121 | 168 | f71 base-42 packer (Phase 3 caller TBD) |
| `grid.go` | 310 | 339 | g15 + g25 packers + g15 inverse |
| `hashcodes.go` | 120 | 200 | h10/h12/h22 + HashedCallC28 |
| `ldpc.go` | 224 | 493 | matrix embed/parse + LDPCEncode |
| `message.go` | 108 | – | Message struct + MessageType enum |
| `nonstdcall.go` | 96 | 166 | CallsignC58 (Phase 3 caller TBD) |
| `parse.go` | 300 | 204 | text → Message (Phase 2D) |
| `token.go` | 205 | 319 | Token Table 7 boundaries + gaps |

`bits.go` + `bitbuilder.go` are the foundational layer; the rest
sits above them. The split between `encode.go`/`decode.go` (bit
codec) and `format.go`/`parse.go` (text codec) is a clean two-tier
arrangement and one of the package's strongest design choices.

## Suggested follow-up order

If you have an hour to spend on this package:

1. **Finding #1** (token+rover guard) — 15 min. Move the
   `format.go:59-64` guard into a shared helper, call it from both
   sides. Add one test pinning the symmetry.
2. **Finding #5** (slices.Clone on EncodeMessage return) — 5 min.
   One-line change, preserves the existing test, removes a future
   footgun.
3. **Finding #4** (decode-side token+rover policy decision) — 10 min.
   Just a test + doc comment recording the chosen policy; no code
   change needed if you decide "wire wins."
4. **Finding #2** (one literal-bits test for Type 1 layout) — 20 min.
   Pin "CQ K1JT FN20" or similar to a hex-encoded expected body.
   Layer 5 `jt9` integration can later cross-validate the literal.

Findings #3, #6, #7, #8, #9, #10 are nice-to-haves; pick them up
opportunistically as you touch the surrounding code.
