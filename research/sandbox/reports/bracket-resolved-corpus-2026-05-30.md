# Hash-origin brackets on display — strict promotion, corpus 2026-05-30

Session-108 follow-up, run as a **separate unit** from the unresolved-
hash emission promotion (128/19). Promotes the hash-origin bracket
display convention into strict mode: a callsign that arrived as a HASH
(Type 1/2 h22, Type 4 h12) and was RESOLVED via the table renders as
`<CALL>` (jt9 convention, angle brackets retained to mark hash origin)
rather than bare `CALL`.

## Why separate from emit-unresolved

This changes display semantics for *resolved* hash-origin calls — a
different surface than emit-unresolved's `<...>` *placeholder* for
*unresolved* calls. It could in principle affect more than the one
obvious `DG6JW/T` case, so it got its own A/B + a trace of every
`<CALL>`-eligible output before promotion.

## Result

| config | matched | extras |
|---|---|---|
| strict (bracket off) | 128 | 19 |
| **strict + bracket-resolved (new default)** | **129** | **18** |
| strict −emit-unresolved + bracket | 113 | 17 (no change vs no-bracket) |

**+1 matched / −1 extra → 129/18.** Display-only — codeword, gate, and
hash registration are untouched (`<CALL>` is `<`-prefixed so
`registerCallsigns` skips it, and a resolved call is already in the
table, so a bracketed render changes no table state).

## Bracket-eligible output trace (the complete affected set)

`sandbox-miss-funnel -bracket-resolved` lists every accepted decode
rendering a bracketed resolved hash. On this corpus there are exactly
**2**:

```
20m_slot3.wav  1995.3Hz dt=+0.83  "<DG6JW/T> SV0TPN +01"        [accepted] → MATCH
live_slot3.wav 2252.5Hz dt=+0.10  "<PE1NPS>     HG60IPA RR73"   [accepted] → EXTRA
```

- `<DG6JW/T> SV0TPN +01` — the flip. jt9 emits this truth bracketed
  (the one bracketed-resolved truth in the corpus); our bare
  `DG6JW/T ...` was an extra under 128/19, now matches → +1 matched,
  −1 extra.
- `<PE1NPS> HG60IPA RR73` — the Session-108 over-resolution case
  (cascade resolved a `<...>` the oracle left unresolved). It was an
  extra as `PE1NPS ...` and stays an extra as `<PE1NPS> ...` —
  bracketing is **neutral** here. No match broken.

The "may affect more than the one obvious case" caution is resolved
empirically: 2 outputs affected, 1 helps, 1 neutral.

## Dependency — composes with emit-unresolved, no-op without it

bracket-resolved is a **no-op when emit-unresolved is off** (113/17
either way). The `DG6JW/T` resolution depends on DG6JW/T being in the
hash table, which only happens because emit-unresolved emitted the
Type-4 `<...> DG6JW/T` decode that registered it. So the +1 is
*composed* with emit-unresolved, not independently reachable. In the
strict profile (which has emit-unresolved) it composes to 129/18.

## Provenance / discipline

jt9/WSJT-X display convention — a hash-origin call keeps its angle
brackets. NOT a tuned threshold. The literal callsign paths (standard
c28, Type-4 c58 sender) and the `<...>` unresolved sentinel are
unaffected.

## Reproducibility

```bash
# new strict default (129/18):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict

# A/B off path (pre-bracket 128/19):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict -bracket-resolved=false

# bracket-eligible output trace:
go run ./research/cmd/sandbox-miss-funnel -dir captures \
    -stage2-mode filter -stage2-metric geo -stage2-threshold 0.70 \
    -osd-disable-n1 -osd-accept-ratio 0.045 -emit-unresolved -bracket-resolved
```

## Files

- `research/sandbox/unpack.go` — `Unpack77WithHashesOpts(payload, ht,
  bracketResolved)`; `bracketResolved` threaded through `unpackType12`,
  `unpackType4`, `unpackCallsign28WithHashes`; brackets the hash-
  resolved branch only. `Unpack77WithHashes` delegates with false
  (signature preserved for tests).
- `research/sandbox/multipass.go` — `MultiPassOptions.BracketResolvedHashes`;
  decode loop calls `Unpack77WithHashesOpts`.
- `research/cmd/sandbox-asym-ab/main.go` — `-bracket-resolved`; strict
  applies it unless explicit; banner line.
- `research/cmd/sandbox-miss-funnel/main.go` — `-bracket-resolved` +
  `printBracketEligible` output trace.
