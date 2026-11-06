# Current work

Updated: 2026-11-06

- **Goal:** W-0024 acceptance: Settings → Logbooks (Set as default, descriptions). [`backlog`](backlog.md) owns priority.
- **State:** station runs candidate `b2a16f5a` (ADR 0099 W1: shell and rig streams on WebSocket), schema 19. Run sheet revision 5: R0–R6 passed, AC-1 to AC-11 passed on the station; R6 restored the pre-test data.
- **Open:** technical sign-off needs green CI on `4696e3c1` (shutdown-close test correction, entry 27); U1–U7 unexercised, not waived; operator sign-off. ADR 0099 still Proposed. W-0012 slice 3 = ADR 0098 (Proposed), on hold.
- **Next:** after sign-off, W-0023 follow-on ADR (design only).
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account. Shutdown cancels an in-flight stream write; 1001 is best effort.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0024 acceptance`](reports/acceptance-W-0024-set-as-default.md), [`ADR 0099`](decisions/0099-spa-event-stream-connection-budget.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
