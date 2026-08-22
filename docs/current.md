# Current work

Updated: 2026-08-22

- **Goal:** Close the [W-0007](work/W-0007-close-verified-p1-correctness-findings.md) verified P1 correctness findings in the fixed order, each as its own TDD slice and commit.
- **State:** W-0006 done. W-0007 nearly closed, three of four findings done: **PT-1** equal-version SM Cloud conflicts → idempotent no-op or `version_conflict` (`8bc0d9b3`); **F-01** partial station baseline → strict `logging_station` decoder (`961a7055`); **PT-2** concurrent QSO delete → revision-guarded delete with an in-tx authoritative preimage, a stale delete racing a concurrent edit returns `409 delete_conflict` (404 stays for missing/tombstoned) (`e4cdfbfe`).
- **Next:** **F-02 — re-enrichment generation** (frontend, final W-0007 slice): give `EditQsoModal.svelte`'s lookup the generation + AbortController discipline of `operate/enrich.svelte.ts`, so a slow lookup for callsign A can never modify a form (or its save patch / hidden dxcc/zones) after it changes to B; check both generation and normalized callsign before applying, and tag `enrichExtras` with the callsign that produced them.
- **Decisions not to revisit:** each W-0007 finding is a separate slice/commit; SM Cloud equal-version divergence is a conflict, not arrival-order; `docs/backlog.md` alone owns priority.
- **Do not:** combine the four findings into one commit; weaken QSO/upload atomicity or the enrichment-never-blocks-logging rule; initiate RF/hardware actions; amend or push without operator direction.
- **Relevant files:** [`W-0007`](work/W-0007-close-verified-p1-correctness-findings.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md), [`frontend review`](reviews/frontend-app-review.md), [`backlog`](backlog.md).
- **Coordination:** SM Cloud integration tests need a disposable Postgres (`task db:pg:up`, `SMCLOUD_TEST_ALLOW_DEFAULT=1`); leave committing and pushing to the operator.
