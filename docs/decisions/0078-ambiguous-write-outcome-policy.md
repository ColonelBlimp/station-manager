---
number: 0078
title: Ambiguous write outcome policy — a timed-out write is outcome-unknown until proven
status: Accepted (operator-ratified 2026-08-31)
date: 2026-08-31
---

# 0078 — Ambiguous write outcome policy

## Context

W-0008 frontend-wire finding **F-04** (`docs/reviews/frontend-app-review.md`): most of the SPA's
state-mutating requests collapse an ambiguous transport outcome into a definite failure. `safeFetch`
already distinguishes the ambiguous case — a fired write timeout returns
`{kind:'network', timedOut:true}` (the request reached, or may have reached, the daemon and the
response was lost, so a write may already have committed), whereas a connection failure returns
`kind:'network'` without `timedOut` — but almost every write caller discards that distinction and
reports a generic failure (`_helpers.ts:47–61` documents the intended contract and records that it
went largely unhonoured; only the FT8/config family branched on `timedOut`).

The operator-observable harm: guidance can contradict daemon state. A confirm-by-push RF flow shows
an error toast while SSE then shows the requested state; a config save persists while the form still
says it failed; a QSO edit or an upload enqueue that actually committed invites a retry that repeats
non-idempotent work. The write surface is broad (~14 paths), so the fix is sliced; F-04a is the
first slice (QSO PATCH + upload enqueue) and F-04b the second (session email / export + restart).
This ADR records the cross-cutting policy the whole of F-04 follows.

## Decision

A write whose HTTP response was lost to a **fired timeout** is **outcome-unknown**, not failed,
until operation-specific evidence confirms it committed. The SPA never automatically retries an
ambiguous write; it tells the operator the outcome is unknown and how to check before they retry.

Each write belongs to one reconciliation class, defined by the evidence available to confirm it:

- **Re-readable** (config / setup blocks): re-read the authoritative block and compare the fields
  the operator edited. A full match confirms the write; a difference leaves it unknown. Evidence: a
  fresh authoritative GET of the same block.
- **Confirm-by-push** (rig / FT8 commands): the daemon's own SSE state is the truth. Defer the final
  claim to the pushed state; if none arrives, say "acknowledgement unknown," never "failed."
  Evidence: the subsequent authoritative SSE frame.
- **Re-queryable** (QSO PATCH, upload enqueue, restart): re-query observable state for proof.
  - QSO PATCH: re-read the QSO by uuid and compare the fields the operator attempted to change; a
    full match confirms success, anything else stays unknown (daemon-side normalisation that alters
    a field therefore degrades to unknown rather than a false success). Evidence: `GET /v1/qso/{uuid}`.
  - Upload enqueue: re-query by a stable QSO/destination identity. If the existing API cannot prove
    the matching queue entry, the outcome stays unknown — do **not** infer failure. (A multi-uuid
    batch drained in the background with no per-entry proof API is not reconcilable, so it resolves
    to the non-reconcilable treatment below.)
  - Restart: confirm only through lifecycle observation that proves a **new** daemon instance —
    a changed `/v1/version.instance` (`waitForDaemonBack`), keyed on the id captured before the
    POST so the still-shutting-down old daemon on a reused keep-alive is not mistaken for the
    replacement. Reconnection alone is insufficient. A new instance confirms the restart; none
    within the existing cap leaves it unknown; an explicit 409/503 is a definite failure with no
    reconciliation. Applied on the timed-out POST as well as the accepted 202, since the daemon may
    already be respawning when the response is lost.
- **Non-reconcilable** (session email / export): there is no evidence that separates committed from
  not, so report the outcome as unknown rather than failed. Email warns against a blind retry (a real
  message is the worst write to double-fire). Export is lighter: its only side effect is a
  best-effort server-side backup and the operator-visible download definitely did not arrive, so a
  re-export is acceptable and only a duplicate backup can result.

Every ambiguous outcome carries the same shared operator-facing lead, with operation-specific
recovery guidance appended:

> The request timed out before Station Manager confirmed the result, so the outcome is unknown.

- QSO: "Reload this QSO before trying again."
- Upload: "Check its upload status before trying again."
- Email: "The email may already have been sent; check before retrying."
- Export: "Export again if you still need the file; another backup may be archived."
- Restart: "Wait for Station Manager to reconnect or verify its status before trying again."

Ambiguity is represented by adding an optional `timedOut?: boolean` to each affected API outcome
type, incrementally, per sub-slice — not by migrating the whole write surface to a shared outcome
type. A **generic non-timeout transport error keeps its existing wording**: `timedOut` being absent
does not prove the request failed before reaching the daemon, so such an error is not newly
described as definitely rejected, and no "safe to retry" claim is added. Only an explicit server
rejection — or another transport state the code can actually prove — may be described as definite.

## Alternatives considered

### Keep reporting a generic failure

The status quo. Rejected: it is untruthful for the timed-out case — the write may have committed —
and it is exactly what drives the operator into a retry that repeats non-idempotent work or a
"failed" label the daemon contradicts.

### Automatically retry the ambiguous write

Rejected: the writes most likely to time out are the least safe to repeat — a re-sent QSO PATCH or
upload can double-apply, a re-sent email lands in a real inbox twice. The operator, not the client,
decides to retry, and only after checking.

### Migrate the whole write surface at once

Rejected: ~14 write paths with four different reconciliation stories is too large for one coherent,
reversion-provable change. Slicing (F-04a…) keeps each change small and lets each operation's
reconciliation be designed and tested on its own evidence.

### Introduce a shared `WriteOutcome` abstraction now

A single discriminated type (`rejected | aborted | transport-failed | outcome-unknown`) migrated
across every caller. Rejected for now: it is a large mechanical migration ahead of the consumers
that would justify it, and the incremental `timedOut?` field already matches the shape the FT8/config
family established. Revisit once several sub-slices share the pattern.

## Consequences

- No timed-out write is described as definitely rejected unless an HTTP response said so; the
  operator gets an honest "outcome unknown" plus how to check.
- Each state-mutating request in scope uses an intentional write-class timeout and a documented
  reconciliation behaviour, tested for the commit-then-timeout case.
- The policy is applied incrementally, so during F-04 some write paths still flatten the ambiguous
  case; the class list here is the map for finishing them.
- The `timedOut?` field spreads across several outcome types rather than being centralised, which is
  accepted duplication until a shared type earns its place.

## Triggers to revisit

- If three or more outcome types carry `timedOut?` and start sharing reconciliation logic, migrate to
  a shared `WriteOutcome` type (the deferred alternative above).
- If the daemon gains a per-request idempotency key or an authoritative "did this commit?" query, the
  re-queryable classes can confirm precisely instead of comparing fields, and upload enqueue becomes
  reconcilable rather than non-reconcilable.
- If a second operator or a non-local daemon topology appears, the "SPA talks only to the local
  daemon" assumption behind the timeout choices no longer holds.

## References

- `docs/reviews/frontend-app-review.md` — F-04.
- `docs/work/W-0008-harden-audited-contract-boundaries.md` — slice 4.
- `frontend/app/src/lib/api/_helpers.ts` — `safeFetch`, `timedOut`, the write timeouts.
- [`ADR 0077`](0077-spa-runtime-wire-decoders.md) — the sibling F-03 SPA-boundary decision.
