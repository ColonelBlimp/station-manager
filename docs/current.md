# Current work

Updated: 2026-08-22

- **Goal:** Close the [W-0007](work/W-0007-close-verified-p1-correctness-findings.md) verified P1 correctness findings in the fixed order, each as its own TDD slice and commit.
- **State:** W-0006 done. W-0007 underway, two of four findings closed: **PT-1** (equal-version SM Cloud conflicts) — an exact `(revision, modified_at)` tie is an idempotent no-op or a `version_conflict` (409, whole-batch rollback, never a backup success) when payload/tombstone/logbook diverges (`8bc0d9b3`); **F-01** (partial station baseline) — a strict `logging_station` decoder rejects a malformed/partial GET so a whole-block PUT can't save blanks (`961a7055`).
- **Next:** **PT-2 — concurrent QSO delete**: make deletion revision-guarded and obtain the authoritative pre-delete image inside the transaction, so a stale-snapshot delete racing a concurrent edit returns `409 delete_conflict` (404 stays for missing/tombstoned) and history records the true last-live image. Then F-02 (re-enrichment generation).
- **Decisions not to revisit:** each W-0007 finding is a separate slice/commit; SM Cloud equal-version divergence is a conflict, not arrival-order; `docs/backlog.md` alone owns priority.
- **Do not:** combine the four findings into one commit; weaken QSO/upload atomicity or the enrichment-never-blocks-logging rule; initiate RF/hardware actions; amend or push without operator direction.
- **Relevant files:** [`W-0007`](work/W-0007-close-verified-p1-correctness-findings.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md), [`frontend review`](reviews/frontend-app-review.md), [`backlog`](backlog.md).
- **Coordination:** SM Cloud integration tests need a disposable Postgres (`task db:pg:up`, `SMCLOUD_TEST_ALLOW_DEFAULT=1`); leave committing and pushing to the operator.
