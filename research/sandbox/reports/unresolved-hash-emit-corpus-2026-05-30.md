# Unresolved-hash emission — strict promotion, corpus 2026-05-30

Session-108. Promotes the unresolved-hash *emit* policy into strict
mode: a CRC-valid, gate-passing decode whose only defect is an
unresolvable hashed callsign is emitted with the canonical jt9 sentinel
`<...>` rather than discarded as `unpack_fail`.

## Origin — the trace that killed the N1Sep premise

Session-107 attributed 31 strict misses, leaving 18 "decoder-bound"
and inferred (from `had_crc_valid + unpack_fail`) that the bucket was
"BP converged to a nearby wrong LDPC codeword with coincidental CRC14."
The Session-108 **targeted committing-stage trace**
(`sandbox-miss-funnel -unpack-trace`, with `UnpackResult.Detail`
plumbed through the trace) overturned that inference:

```
unpack_fail truths traced: 16
  committing method: BP 15 / OSD-2 1     stage: N1/BP 13, N1Norm/BP 1, N2/BP 1, N2/OSD-2 1
```

Every one of the 16 was a **correct codeword** blocked solely on hash
resolution — `unresolved: call1ok=false call2ok=true gridok=true` (h22
hash miss) or `Type 4: h12=NNNN not in hash table` — with the `<...>`
placeholder position in the truth matching the unresolved-call position
in our decode. Not wrong codewords. The N1 reliability-calibration work
(N1Sep) was therefore aimed at a problem that mostly didn't exist; it is
parked clean and off-by-default (`SepKappa=0`).

## What changed

- `UnpackResult` gains `Unresolved bool` — structurally-valid-and-
  displayable but ≥1 hashed callsign unresolvable. Distinct from a
  genuinely undecodable payload (unsupported i3, reserved token, bad
  grid), where both `OK` and `Unresolved` are false.
- Unresolved hashed callsigns now render the **canonical `<...>`** (not
  the old numbered `<...12345>`), so display text compares equal to a
  jt9 truth that also failed to resolve. Raw hash is preserved in
  `Detail`. `NormalizeText` does NOT canonicalise numbered placeholders,
  so this had to be done at emit, not in the scorer.
- `MultiPassOptions.EmitUnresolvedHashes` (default **false**) lets the
  decode loop accept `Unresolved` decodes; the post-decode gate still
  runs. `DefaultMultiPassOptions()` is unchanged — this is a
  strict-profile promotion, not a library-default move.

## Result

| config | matched | extras |
|---|---|---|
| strict baseline (emit off) | 113 | 17 |
| **strict + emit-unresolved (new default)** | **128** | **19** |

**+15 matched / +2 extras.** `128/144 = 88.9%` — the largest single
decoder-side lift of the cluster. The +15 exceeds the 16 traced because
emitting these decodes registers their *resolvable* callsigns into the
hash table, resolving downstream references (beneficial cascade).

Funnel stage shift (strict config):

| stage | baseline | emit | Δ |
|---|---|---|---|
| matched | 113 | 128 | +15 |
| decoder-bound | 18 | 7 | −11 |
| gate-bound | 1 | 0 | −1 |
| refine-bound | 1 | 0 | −1 |
| stage2-bound | 2 | 1 | −1 |
| nms-bound | 7 | 6 | −1 |
| finder-miss | 2 | 2 | 0 |

## The +2 extras — characterized, not laundered

Three new extras appeared; one baseline extra resolved into a match
(net +2). The promotion risk was "unresolved-hash emit launders a
CRC-lottery codeword into plausible `<...>` text." It did NOT
materialize — all three sit at the *exact coordinate of a real truth
signal* (Δf < 1 Hz, Δdt ≈ 0):

| extra | coord | nearest truth | method | toneAgree | hardErr | grid | verdict |
|---|---|---|---|---|---|---|---|
| `TC19TC <...>` | 2592 Hz | `CQ TC19TC` | N1/BP | 77/79 | 2 | 18.9 | benign — Type-4 CQ render mismatch on the real TC19TC signal |
| `DG6JW/T SV0TPN +01` | 1995 Hz | `<DG6JW/T> SV0TPN +01` | N1/BP | 79/79 | 0 | 19.2 | perfect decode; bracket-convention mismatch (we resolved the hash, jt9 shows `<DG6JW/T>`) |
| `PE1NPS HG60IPA RR73` | 2252 Hz | `<...> HG60IPA RR73` | N2/BP | 63/79 | 25 | 0.35 | real signal at exact truth coord; cascade over-resolved the `<...>` to PE1NPS → text mismatch |

None is a noise-launder. Two are textbook acceptable monitoring extras
(77–79/79 tone agreement, all-BP). The weak one (`PE1NPS`) is a
real-signal location where cascade registration *over-resolved* a hash
the oracle left `<...>` — ironically converting a clean recovery into a
mismatch. Net still +15/+2, so the policy wins.

## Two findings deferred to separate units

- **Bracket convention (separate A/B):** jt9 renders a hash-*resolved*
  call as `<CALL>` (brackets retained); we render bare `CALL`. The
  `DG6JW/T` extra is entirely this — emitting `<DG6JW/T>` would flip it
  extra→match. This changes display semantics for *resolved* hash-origin
  calls (not just unresolved emission) and may affect more than this one
  case, so it gets its own A/B with match/extras and a trace of all
  `<CALL>`-eligible outputs. NOT bundled here.
- **Over-resolution double-edge:** cascade registration can resolve a
  `<...>` the oracle left unresolved (`PE1NPS`), converting a recovery
  to a mismatch. Real interaction; net positive, noted for the bracket
  unit.

## Residual 7 decoder-bound (post-promotion) — confirmed non-hash

3 genuine real-signal BP failures (`CQ DX S56GD JN65` ×2, `CQ COTA
IK7NXU JN81`; grid 35–54), 2 noise-limited (`CQ PD5MVH JO22`,
`CQ YD2ADB OI52`; grid ~1.1–1.8), 1 Type-4 CQ form (`CQ TC19TC`), 1
jt9-resolved-bracket mismatch (`<DG6JW/T> SV0TPN +01`).

## Provenance / discipline

Alignment with oracle (jt9/WSJT-X) display behaviour — NOT a tuned
numeric threshold. The jt9-oracle truth manifests carry `<...>` for
exactly this case, so emitting `<...>` is parity-with-oracle. Per the
Session-108 decision rule, extras that are explainable real-signal
render mismatches do not require a separate holdout. `looksLikeCallsign`
rejects `<`-prefixed tokens, so canonical `<...>` never poisons the
hash table.

## Reproducibility

```bash
# new strict default (128/19):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict

# A/B off path (pre-promotion 113/17):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict -emit-unresolved=false

# committing-stage trace that drove the pivot:
go run ./research/cmd/sandbox-miss-funnel -dir captures \
    -stage2-mode filter -stage2-metric geo -stage2-threshold 0.70 \
    -osd-disable-n1 -osd-accept-ratio 0.045 -unpack-trace

# unresolved-emit extra audit (physical evidence per extra):
go run ./research/cmd/sandbox-miss-funnel -dir captures \
    -stage2-mode filter -stage2-metric geo -stage2-threshold 0.70 \
    -osd-disable-n1 -osd-accept-ratio 0.045 -emit-unresolved
```

## Files

- `research/sandbox/unpack.go` — `UnpackResult.Unresolved`; canonical
  `<...>` in `unpackCallsign28WithHashes` + `unpackType4`; displayable-
  vs-undecodable split in `unpackType12`/`unpackType4` with raw hash in
  `Detail`.
- `research/sandbox/multipass.go` — `MultiPassOptions.EmitUnresolvedHashes`;
  `DecodeRecord.Unresolved`; emit-or-drop branch in `MultiPassDecodeFull`;
  `accepted_unresolved` trace outcome. Also the parked N1Sep stage
  (`SepKappa`, `SoftLLRsN1SepWeighted`) — clean, off-by-default.
- `research/sandbox/trace.go` — trace evidence fields (`MeanAbsLLR`,
  `UnpackDetail`, sep summary, gate metrics) for the committing-stage +
  unresolved-extra audits.
- `research/sandbox/bp.go` — `BPResult.OSDSoftDist` / `OSDNormDist`.
- `research/cmd/sandbox-asym-ab/main.go` — strict applies
  `EmitUnresolvedHashes` unless `-emit-unresolved` is explicit.
- `research/cmd/sandbox-miss-funnel/main.go` — `-unpack-trace`,
  `-emit-unresolved`, the committing-stage trace + unresolved-extra
  audit.
