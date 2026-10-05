# Current work

Updated: 2026-10-05

- **Goal:** W-0021: QSO archives; remove the saved-QSO recovery per [ADR 0087](decisions/0087-switch-archives-only-from-settings-and-state-the-loss.md) (supersedes 0085 / 0086). [`backlog`](backlog.md) owns priority.
- **State:** ADR 0087 removal BUILT test-first (uncommitted): Settings-only switch, Activate discard-and-switch gate, recovery + attribution endpoint + `config.updated` removed, `expect_*` → `409 reload_required`. Evidence in W-0021.
- **Next:** operator review and commit (code, then docs); push, deploy, then a passive check of the Activate prompt. **alpha.3 FROZEN** at `333427ea`; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 palettes DECLINED. PT-6 `fsOps` package-private. FT8 timing stays +0.500/+0.660. `txConfirmTimeout` DEFERRED. ClubLog is not a station account; its callsign default stays persisted at save.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0021`](work/W-0021-qso-archives.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review.
