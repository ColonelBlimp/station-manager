---
number: 0025
title: Feed FT8 AP-decode hints from the logbook via an injected provider seam
status: Proposed
date: 2026-06-02
---

# 0025 — Feed FT8 AP-decode hints from the logbook via an injected provider seam

## Context

The live FT8 path (ADR 0024) decodes via go-ft8. Enabling go-ft8's OSD-2/MRB
fallback (`ft8.enable_osd`, 2026-06-02) closed most of the weak-signal recall
gap a live A/B against jt9 `-d 3` had shown (go-ft8 baseline was 100% precision
/ ~87% recall; OSD recovered ~5 of 7 misses). The next lever beyond OSD is
**a-priori (AP) decoding**: when belief-propagation + OSD still miss, retry with
part of the message *assumed* known — "CQ \<call\>", "\<mycall\> \<dxcall\>",
"\<mycall\> \<dxcall\> \<report\>" — and accept on a CRC match. go-ft8 has a
broader AP catalogue in progress; AP's effectiveness depends on the **set of
candidate callsigns** it hypothesises against.

Station Manager holds the richest possible AP catalogue: the operator's
logbook (worked calls, by band/mode), plus live context (calls heard this
session, watchlists, award-needed lists). But the decode path must not couple
to storage. ADR 0013's narrow-daemon-scope invariant — reaffirmed by ADR 0024
("decode does not touch the log/forward subsystems; the invariant holds by the
import graph") — forbids `internal/ft8` from importing `internal/storage`. A
decode is not a QSO. So the question is *how* logbook-derived data reaches the
decoder without dragging a DB driver, credentials, schema knowledge, or query
latency into either the codec (go-ft8) or the FT8 wrapper (`internal/ft8`).

## Decision

Station Manager builds a **small, ranked, capped, deduped hint set** and passes
it into the decoder as a plain value; neither go-ft8 nor `internal/ft8` queries
the logbook. The hint set is produced by a storage-backed provider that lives
*outside* `internal/ft8` and is injected through an interface seam — the same
pattern as the `captureSource` audio seam:

```go
// in internal/ft8 — storage-free; just the contract.
type APCallHint struct {
    Call   string
    Weight float64
    Source string // "heard", "worked", "needed", "watchlist", …
}

type APHintProvider interface {
    // Ranked, capped, deduped hints for the current band/mode.
    Hints(band, mode string) []APCallHint
}
```

`ft8.Service` holds an `APHintProvider`, refreshes on its own cadence (every N
seconds / on band-mode change), and forwards the result into go-ft8's decode
options. The concrete provider lives in a new package (e.g. `internal/ft8hints`)
that *is* permitted to import `internal/storage` + `config`; it owns the DB
query, the ranked source mix, and the cap. `internal/ft8` stays storage-free.

(The go-ft8 API shape — `DecoderOptions.APCallHints` + `MaxAPCallHypotheses`, or
a stateful `decoder.SetAPCallHints(hints)` — is illustrative; it firms up when
go-ft8's AP catalogue API lands. The SM-side seam above is the decision here and
is independent of which go-ft8 surface ships.)

## Alternatives considered

### `internal/ft8` queries `internal/storage` directly

The obvious shortcut: have the FT8 service read worked calls itself. Rejected —
it violates the ADR 0013/0024 import-graph invariant (`internal/ft8` must not
import `internal/storage`); decode would become coupled to the logbook schema
and the DB lifecycle. The interface seam costs almost nothing and preserves the
boundary that the bridge/forwarder split already defends.

### go-ft8 opens the logbook itself

Push the DB path all the way down into the codec. Rejected outright: it would
put SQLite drivers, file paths, credentials, schema coupling, and query latency
inside a pure-DSP library that is shared, GPL, and has no business knowing what
a logbook is. The codec must receive data, never fetch it.

### Feed all worked calls, unranked

Hand go-ft8 the entire worked list and let it try everything. Rejected on
performance: AP hypothesis testing is per-candidate work, so an unbounded call
list multiplies decode cost without bound. SM must cap (≈50–200) and rank before
handing over; go-ft8 then budgets a strict top-K (≈2–4) AP hypotheses per FT8
candidate. Ownership of the cap is SM's because SM has the ranking signal.

### Operator's own callsign only (no logbook)

The minimal AP catalogue: just `MY_CALL` from config, no logbook query at all.
Viable as a *first* increment (it needs no provider, no new package) and worth
shipping first to de-risk the go-ft8 AP wiring. Rejected as the *end* state: it
leaves the logbook's recall value (worked-before, needed-for-award) on the table
— which is the whole reason SM is well-placed to do AP better than a bare
decoder.

## Consequences

- A new package (`internal/ft8hints` or similar) imports `internal/storage` +
  `config` and owns the ranked mix: in-subsystem heard-set + logbook
  (worked-on-band/mode, award-needed) + config watchlist, normalised, deduped,
  capped. The richest single source — "heard this session" — is the FT8
  subsystem's *own* running decode set and needs no storage at all.
- `internal/ft8` gains an `APHintProvider` interface and forwards hints into the
  decode call; it stays storage-free, so the import-graph invariant is intact
  and the existing boundary discipline is unchanged.
- The division of labour is explicit: **SM** owns query + rank + dedupe + cap;
  **go-ft8** owns cheap known-bit scoring, top-K AP hypotheses, BP-only-by-
  default, and per-source diagnostics.
- AP most likely **forces the move from the stateless `DecodeMessagesChecked` to
  go-ft8's stateful `Decoder`** (the live path currently decodes statelessly per
  slot; the docs already flag the stateful decoder as a later concern). A
  stateful decoder is also the natural home for `SetAPCallHints` refresh and for
  the persistent hash table already resolving hashed calls across slots. This is
  a larger change than the `enable_osd` knob and should be scoped on its own.
- AP decoding adds per-candidate cost on top of OSD; the strict top-K budget and
  the cap are what keep it inside the 15 s slot. The live A/B harness
  (`ft8-capture-probe -out` → jt9) is the tool to confirm AP earns its cost.

## Triggers to revisit

- **go-ft8's AP catalogue API lands.** Firm up this ADR Proposed → Accepted
  against the concrete surface (`DecoderOptions.APCallHints` vs stateful
  `SetAPCallHints`), and finalise `APCallHint`'s fields to match.
- If a live A/B shows AP hints do **not** measurably lift recall over OSD alone,
  the logbook-provider half may not be worth its complexity — ship own-call-only
  AP and stop there.
- If SM gains a **DX-cluster / spotting feed**, "spotted recently" becomes a new
  hint source and the provider's mix changes.
- If the stateful decoder proves too heavy for the slot budget on the operator's
  hardware, reconsider whether AP runs every slot or on a reduced cadence.
- If FT8 ever needs to write QSOs (not just decode), the `qsoservice` integration
  point — not a storage reach — is where logbook context would flow, per ADR 0024.

## References

- ADR 0013 — narrow-daemon-scope / import-graph discipline this preserves.
- ADR 0024 — FT8 external library + CGO-gated live pipeline (the `captureSource`
  seam this mirrors; the stateless-decode "later concern" note).
- ADR 0021 — FT8-as-subsystem (sibling-isolation rule, parked).
- go-ft8: `github.com/ColonelBlimp/go-ft8` (AP catalogue work in progress).
- `docs/session-handoff.md` Session 123 (the OSD A/B that motivates the next lever).
- memory `project_sm_ft8_integration`.
