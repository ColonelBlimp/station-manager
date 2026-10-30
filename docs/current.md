# Current work

Updated: 2026-10-30

- **Goal:** W-0024 acceptance: Settings → Logbooks (Set as default, descriptions), step (4) of the data-model chain before contesting. [`backlog`](backlog.md) owns priority.
- **State:** **v2.0.0-alpha.5** (`0da2cd6a`) is dogfood-accepted and live. W-0024 **implementation complete**: slices 1–5c, Set as default **released** in `46a07d07`; CI green on `43d0ed98`. **Acceptance pending**, not deployed. W-0023 is design only (rulings A–D, `4919bffc`).
- **Open:** alpha.5 Findings #1 (W-0018), #2–#4 (W-0012), non-blocking. Reload every open tab after an upgrade (Finding #4).
- **Next:** W-0024 acceptance criteria and run sheet (candidate `43d0ed98`), for ruling; deployment not yet approved. Then the W-0023 follow-on ADR (design only).
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0024`](work/W-0024-settings-logbooks.md), [`W-0023`](work/W-0023-contest-logging.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
