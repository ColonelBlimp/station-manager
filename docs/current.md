# Current work

Updated: 2026-08-22

- **Goal:** Close the [W-0007](work/W-0007-close-verified-p1-correctness-findings.md) verified P1 correctness findings in the fixed order, each as its own TDD slice and commit.
- **State:** W-0006 is done and handed off (unknown-key rejection; ADRs 0074/0075; `config.md`). W-0007 is selected and underway: **PT-1** (equal-version SM Cloud conflicts) is complete — an exact `(revision, modified_at)` tie is an idempotent no-op when the state matches and a `version_conflict` (409, whole-batch rollback, never a backup success) when payload/tombstone/logbook diverges (commit `8bc0d9b3`).
- **Next:** **F-01 — partial station baseline** (frontend): a strict `logging_station` decoder in `frontend/app/src/lib/api/config.ts`, verified in `station.svelte.ts`, so a malformed/partial GET can't become an authoritative baseline that erases sibling fields on Save. Then PT-2 (concurrent QSO delete), then F-02 (re-enrichment generation).
- **Decisions not to revisit:** each W-0007 finding is a separate slice/commit; SM Cloud equal-version divergence is a conflict, not arrival-order; `docs/backlog.md` alone owns priority.
- **Do not:** combine the four findings into one commit; weaken QSO/upload atomicity or the enrichment-never-blocks-logging rule; initiate RF/hardware actions; amend or push without operator direction.
- **Relevant files:** [`W-0007`](work/W-0007-close-verified-p1-correctness-findings.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md), [`frontend review`](reviews/frontend-app-review.md), [`backlog`](backlog.md).
- **Coordination:** SM Cloud integration tests need a disposable Postgres (`task db:pg:up`, `SMCLOUD_TEST_ALLOW_DEFAULT=1`); leave committing and pushing to the operator.
