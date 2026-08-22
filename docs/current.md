# Current work

Updated: 2026-08-22

- **Goal:** W-0007 is closed; return work selection to the ranked backlog and take whatever it owns next.
- **State:** **W-0007 complete** — all four verified P1 correctness findings closed: PT-1 equal-version SM Cloud conflicts (`8bc0d9b3`); F-01 partial station baseline (`961a7055`); PT-2 concurrent QSO delete (`e4cdfbfe`, caller follow-up `0363ae0c`); F-02 re-enrichment generation race (`dc15e188`, codex follow-ups `88916515`/`11c4c94a`). Nothing pushed.
- **Next:** Select further work only from [`backlog.md`](backlog.md), which alone owns priority. The last P1 is cleared; ranked next are the P0 on-air validation gate (W-0002, operator-initiated RF) and the P2 release gates — W-0001 durable notifications, W-0002 FT8 type-4 on-air validation.
- **Decisions not to revisit:** each finding shipped as its own TDD slice/commit; `docs/backlog.md` alone owns priority; W-0007's rationale lives in its dossier and the persistence/frontend audits.
- **Do not:** re-rank priority in this file; initiate RF/hardware actions without per-occasion operator agreement; amend commits; push without operator direction.
- **Relevant files:** [`W-0007`](work/W-0007-close-verified-p1-correctness-findings.md), [`backlog`](backlog.md), [`persistence audit`](reviews/internal-persistence-transaction-audit.md), [`frontend review`](reviews/frontend-app-review.md).
- **Coordination:** Leave committing and pushing to the operator; SM Cloud integration tests need a disposable Postgres (`task db:pg:up`, `SMCLOUD_TEST_ALLOW_DEFAULT=1`).
