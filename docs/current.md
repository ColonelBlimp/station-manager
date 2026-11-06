# Current work

Updated: 2026-11-06

- **Goal:** W-0024 acceptance: Settings → Logbooks (Set as default, descriptions). [`backlog`](backlog.md) owns priority.
- **State:** station runs candidate `b2a16f5a` (ADR 0099 W1), schema 19. Revision 5: R0–R6 and AC-1 to AC-11 passed; CI green on `0c84d5f9` (shutdown-test fixes). Guest cases U2–U4, U6(a)–(c), U7 passed (entries 29–30).
- **Open:** operator decisions on U1 (unreachable in production; tests as evidence or waiver), U5 and keyed U6 (per-occasion RF agreement), contact retention, waivers; operator sign-off. Follow-ups O-a, O-b (entry 30). ADR 0099 still Proposed; W-0012 slice 3 = ADR 0098 (Proposed), on hold.
- **Next:** after sign-off, W-0023 follow-on ADR (design only).
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Shutdown cancels an in-flight stream write; 1001 is best effort.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0024 acceptance`](reports/acceptance-W-0024-set-as-default.md), [`ADR 0099`](decisions/0099-spa-event-stream-connection-budget.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
