# Current work

Updated: 2026-10-29

- **Goal:** W-0024: Settings → Logbooks slice 2 (Set as default, descriptions), step (4) of the data-model chain before contesting. [`backlog`](backlog.md) owns priority.
- **State:** **v2.0.0-alpha.5** (`0da2cd6a`) is dogfood-accepted and live. W-0024 slices 1–3b committed (`20c7f65f`, `9eebb500`, `76295c98`, `027d5845`); Set as default's routes stay **gated**. CI red on 3a/3b (race step over its 10m per-package budget); `e4c711af` raises it to 20m and measures uncached runtime, pending. W-0023 is design only; organiser email sent, awaiting reply.
- **Open:** alpha.5 Findings #1 (W-0018), #2–#4 (W-0012), non-blocking. Reload every open tab after an upgrade (Finding #4).
- **Next:** record the CI measurement and drop `-count=1`; then W-0024 slice 4 (stale-tab guard), then 5 (confirmation). **The routes are released only after both.**
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019/W-0022); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0024`](work/W-0024-settings-logbooks.md), [`W-0023`](work/W-0023-contest-logging.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
