# W-0008 — Harden audited persistence, configuration, API, and frontend contracts

**Status:** Open — pick one numbered slice at a time
**Selected:** Not selected
**Outcome:** Verified P2 contract defects fail loudly or preserve the operator's intended state;
ambiguous outcomes are represented honestly instead of being reported as definite success or
failure.

`W-0008` is an immutable identity. Its internal order is the inherited order from the 2026-08-20
audit reconciliation; [`docs/backlog.md`](../backlog.md) alone ranks this dossier against others.

## Ordered slices

1. **Persistence:** PT-3 session-email stamps only the revision actually sent and reports the true
   emailed set; PT-4 partial logbook PATCH applies an atomic field-level partial update (writes only
   the present members; no revision-conflict response — the logbook row has no revision column); PT-5 import fallback does
   not proceed after an unverified rollback; PT-6/CC-5 config replacement gains unique temporary
   files and crash-durability, after the operator decides how to report a post-rename directory
   sync failure.
2. **Configuration:** CC-3 extends semantic validation to `datastore` and `logging`; CC-4 makes the
   persistence primitives enforce the intended normalize-then-validate contract. CC-2 belongs to
   [W-0006](../archive/work/W-0006-reject-unknown-config-keys.md), not here.
3. **API wire:** AW-2 requires presence-aware command booleans; AW-3 gives genuine no-op QSO PATCH
   an operator-decided outcome; AW-5/AW-6 bound and sanitize SM Cloud ingest; AW-4 makes `/v1/`
   404/405 responses JSON; AW-1 is the separately designed integer-ID to UUID compatibility
   migration.
4. **Frontend wire:** F-03 validates success/safety response shapes before use; F-04 represents an
   ambiguous transport timeout as outcome-unknown and provides a reconciliation path.

## Decisions required

- AW-3: `200` no-op or a client error.
- AW-6: maximum SM Cloud batch rows.
- AW-1: compatibility and removal release.
- PT-6: operator-visible treatment of a failure after rename but before directory sync completes.

## Verification boundary

One finding or deliberately coupled pair per behavior change, RED first and reversion-proved. Tests
must distinguish missing from explicit `false`, stale from current revision, committed from
outcome-unknown, and rollback-confirmed from rollback-unverified. Update the canonical API or config
reference in the same change as the behavior it describes.

## References

- [`internal-persistence-transaction-audit.md`](../reviews/internal-persistence-transaction-audit.md)
- [`internal-configuration-contract-audit.md`](../reviews/internal-configuration-contract-audit.md)
- [`internal-api-wire-contract-audit.md`](../reviews/internal-api-wire-contract-audit.md)
- [`frontend-app-review.md`](../reviews/frontend-app-review.md)
- Expanded pre-decomposition notes: `d0391ed7:docs/backlog.md`.
