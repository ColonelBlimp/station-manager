# Current work

Updated: 2026-09-11

- **Goal:** W-0019: FT4 on the station for the Africa FT4 DX Contest (Sat 2026-09-12 15:00–18:00Z); then close alpha.2 dogfood acceptance and ship the next candidate. [`backlog`](backlog.md) owns priority.
- **State:** station on `2.0.0-alpha.2-54-g94506823` (W-0019 slices 1–5 + 4 SPA fixes). On air 2026-09-11: G2 met with AC7 waived (1,574 FT4 decodes/30 min, DT median +0.1 s), G3 waived for the contest, G4 met (first run: 43 QSOs, all forwarded) — record 34–36.
- **Next:** the contest (one SPA tab only — a second tab starved requests, consistent with connection-budget exhaustion). Post-contest: G3 dummy-load measurement + AC7 debug window; inbox triage: map on the shell stream + enrichment concurrency cap; transient TX-alarm timing; Phone/CW default table (Finding #11); `smd` start-failure visibility; Excel export; mode selector clutter. Then the next candidate freeze. RF only by per-occasion agreement; FT8-10 BLOCKED.
- **Decisions not to revisit:** W-0004 named palettes DECLINED. PT-6 `fsOps` stays package-private. FT8 timing stays +0.500/+0.660 during W-0019.
- **Do not:** re-open a closed dossier (W-0001/W-0003/W-0004/W-0005); initiate RF/hardware without per-occasion agreement; amend or push without operator direction.
- **Relevant files:** [`W-0019`](work/W-0019-ft4-for-the-africa-ft4-dx-contest.md), [`ADR 0080`](decisions/0080-ft4-as-a-profile-of-the-ft8-subsystem.md), [`acceptance record`](reports/dogfood-acceptance-v2.0.0-alpha.2.md), [`dogfood gate`](dogfood-acceptance.md), [`W-0012`](work/W-0012-operator-experience-followups.md), [`W-0018`](work/W-0018-bring-the-embedded-manual-to-release-readiness.md).
- **Coordination:** the operator commits and pushes; non-Markdown commits draw a codex review to triage.
