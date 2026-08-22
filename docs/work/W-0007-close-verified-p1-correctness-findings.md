# W-0007 — Close the remaining verified P1 correctness findings

**Status:** In progress — PT-1 done; F-01 next
**Selected:** 2026-08-22
**Outcome:** Four independently observable data-loss or data-corruption paths identified by the
2026-08-20 audit reconciliation are closed in the fixed order below.

`W-0007` is an immutable identity. Priority lives only in
[`docs/backlog.md`](../backlog.md); the order below is part of that ranking, not an invitation to
combine the fixes into one commit.

## Ordered findings

1. **PT-1 — equal-version SM Cloud conflicts.** ✅ **DONE (commit `8bc0d9b3`, 2026-08-22).** An
   incoming row with the same `(uuid, modified_at, revision)` as the stored row must not overwrite
   divergent payload bytes invisibly. The routine reconcile path must either converge
   deterministically or surface the conflict. *Resolved:* the store's exact-version tie guard is
   now `>` with a JSONB/tombstone/logbook equality check — a matching tie is an idempotent no-op,
   a divergent tie is a `VersionConflictError` that rolls the whole batch back; the server returns
   `409 version_conflict` naming the UUID, and the forwarder classifies that as a terminal
   non-success, never an uploaded backup. Discovering *already-stored* divergence (a full-export
   diagnostic the manifest hash cannot do) is a deliberate non-goal of this slice.
2. **F-01 — partial station baseline.** A malformed or partial successful station-config response
   must not become an authoritative whole-block baseline that erases omitted sibling fields on
   Save.
3. **PT-2 — concurrent QSO delete.** Delete must not remove a newer concurrent revision or record
   the handler's stale snapshot as the append-only history preimage.
4. **F-02 — re-enrichment generation.** Enrichment for callsign A must never be persisted or
   forwarded onto callsign B after the editable callsign changes while the lookup is in flight.

## Verification boundary

Each finding is its own TDD slice. Tests must arrange the correct and nearest confusable outcomes
concurrently or with an observable barrier, assert at the public storage/API boundary, and include
a reversion proof. Fixes must preserve QSO/upload atomicity and the rule that enrichment failure
never blocks logging. No live credentials, network service, rig, audio device, or RF action is
part of acceptance.

## References

- [`internal-persistence-transaction-audit.md`](../reviews/internal-persistence-transaction-audit.md)
  — PT-1 and PT-2 evidence.
- [`frontend-app-review.md`](../reviews/frontend-app-review.md) — F-01 and F-02 evidence.
- The final pre-decomposition ranking and expanded notes are preserved at
  `d0391ed7:docs/backlog.md`.
