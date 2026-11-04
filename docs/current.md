# Current work

Updated: 2026-11-04

- **Goal:** W-0024 acceptance: Settings → Logbooks (Set as default, descriptions). [`backlog`](backlog.md) owns priority.
- **State:** station runs `b766a145` (`-dirty`, docs only), schema 19: W-0024 with the F4/F5/F6 fixes, contest logging, W-0012 slices 1–2. W-0024 acceptance **incomplete** (run sheet revision 4): R0–R2 complete; F4–F6 passed in the guest (agent-driven browser evidence, corroborated from its logs and database); guest disposed of.
- **Open:** R3–R6 station checks; U1–U7 unexercised, not waived; operator sign-off. W-0012 slice 3 = ADR 0098 (Proposed), implementation and bench on hold.
- **Next:** R3 passive station readings, then R4–R6 once the operator gives the outage window and consent to discard intervening writes. Then W-0023 follow-on ADR (design only).
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0024 acceptance`](reports/acceptance-W-0024-set-as-default.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
