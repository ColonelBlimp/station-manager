# W-0007 — Close the remaining verified P1 correctness findings

**Status:** COMPLETE — all four findings closed 2026-08-22
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
2. **F-01 — partial station baseline.** ✅ **DONE (commit `961a7055`, 2026-08-22).** A malformed or
   partial successful station-config response must not become an authoritative whole-block baseline
   that erases omitted sibling fields on Save. *Resolved:* the `api/config.ts` decoder now rejects a
   missing/null/array/non-string `logging_station` (and requires a non-empty `station_callsign` once
   `setup_complete`), so a semantically-invalid GET stays a load error and the section unloaded — the
   existing `!loaded` save guard then makes a blanking PUT unreachable, and a malformed 2xx save
   response leaves the form, shared context, and baseline untouched.
3. **PT-2 — concurrent QSO delete.** ✅ **DONE (commit `e4cdfbfe`, 2026-08-22).** Delete must not
   remove a newer concurrent revision or record the handler's stale snapshot as the append-only
   history preimage. *Resolved:* `DeleteQsoByIDTx` is revision-guarded (write-first soft-delete at
   the expected revision, mirroring the edit path's CAS) and returns the authoritative pre-delete
   image read inside the transaction; a still-live revision mismatch is `409 delete_conflict` (404
   stays for missing/tombstoned), and the audit `before_image` is the true last-live state.
4. **F-02 — re-enrichment generation.** ✅ **DONE (commit `dc15e188`, codex follow-ups `88916515`
   + `11c4c94a`, 2026-08-22).** Enrichment for callsign A must never be persisted or forwarded onto
   callsign B after the editable callsign changes while the lookup is in flight. *Resolved:*
   `EditQsoModal.svelte`'s lookup now mirrors `operate/enrich.svelte.ts` — normalized identities, a
   monotonic generation plus `AbortController`, independent generation-and-callsign checks before
   applying, `enrichExtras` tagged with its callsign and re-checked in `buildPatch`, per-field write
   provenance for retract/restore, and abort/invalidate on callsign change and unmount; a partial
   same-callsign re-enrich keeps a prior lookup's visible and hidden values.

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
