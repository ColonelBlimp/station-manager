# Current work

Updated: 2026-09-19

- **Goal:** W-0019 (FT4) CLOSED 2026-09-19 and archived; clear the remaining findings, close alpha.2 dogfood acceptance, ship the next candidate. [`backlog`](backlog.md) owns priority.
- **State:** station on dev build `2.0.0-alpha.3-1-gbe613793` (2026-09-19, same source as the candidate; frozen RPM install deferred). W-0011 TX-alarm item CLOSED (entry 39); W-0020 Station Events live, page check PASSED.
- **Next:** on-screen tune check. W-0020 close-out awaits passive AC2 rows. Order (2026-09-19): alpha.3 acceptance → W-0010 outcome 9 → ADR 0071 archives (files first) → Settings → Logbooks → contesting; W-0012's 3 ruled slices independent. **alpha.3 FROZEN 2026-09-19** at `333427ea` (local tag); scope CC-5 + install/upgrade evidence + retire B1-01; [record](reports/dogfood-acceptance-v2.0.0-alpha.3.md) Gate A pending the operator. RF per-occasion only; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private. FT8 timing stays +0.500/+0.660; the W-0019 G3/AC7 closure waiver is never cited for a timing or admission-edge change. `txConfirmTimeout` change DEFERRED (on-air approval).
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005/W-0019); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0011`](work/W-0011-ft8-and-rig-refinements.md), [`alpha.3 record`](reports/dogfood-acceptance-v2.0.0-alpha.3.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0020`](work/W-0020-station-events.md), [`inbox`](dogfood-inbox.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review to triage.
