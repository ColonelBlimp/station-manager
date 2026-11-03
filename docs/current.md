# Current work

Updated: 2026-11-03

- **Goal:** W-0024 acceptance: Settings → Logbooks (Set as default, descriptions). [`backlog`](backlog.md) owns priority.
- **State:** station runs `b766a145` (`-dirty`, docs only), schema 19: W-0024 with the F4/F5/F6 fixes, contest logging, W-0012 slices 1–2. W-0024 acceptance **incomplete**: run sheet revision 4 (installed artifact, fresh backup) **ratified** 2026-11-03.
- **Open:** F6 packaged rerun, F4 focus, F5 explicit target; U1–U7 unexercised. W-0012 slice 3 = ADR 0098 (Proposed), implementation and bench on hold.
- **Next:** revision 4 R0–R2 (preserve the installed RPM, `ci:local` at `b766a145`, guest checks F4–F6). R4–R6 on hold for the operator's outage window and discard consent. Then W-0023 follow-on ADR (design only).
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0024 acceptance`](reports/acceptance-W-0024-set-as-default.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
