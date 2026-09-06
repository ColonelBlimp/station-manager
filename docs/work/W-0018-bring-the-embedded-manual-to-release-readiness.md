# W-0018 — Bring the embedded manual to release readiness

**Status:** Open — a page-by-page standing pass during dogfooding; a release gate before a public release
**Selected:** 2026-09-06
**Outcome:** Every chapter of the embedded manual is accurate for the shipped behaviour it describes, an
operator can complete first run, import, update and uninstall from the manual alone, and the DOCS-01 and
DOCS-02 acceptance rows pass on a fresh deployment.

`W-0018` is an immutable identity. Its status may change, while priority and ranked position live only in
[`docs/backlog.md`](../backlog.md).

## Why this item exists

The alpha.2 fresh deployment on 2026-09-06 (acceptance record, Findings #9, #10, #13, #14, #15) showed that
the manual cannot yet carry a new operator through the documented journeys: the no-rig band confirmation
that blocks the first QSO is undocumented, the CAT chapter opens with an unreadable heading, there is no
update or uninstall guidance at all and the install guide that holds it is not shipped, and the install
guide's rig section is stale. The operator's assessment is that the whole manual must be worked through and
amended as dogfooding proceeds; it is not near a releasable state. This item is not a blocker for the next
internal candidate.

## Inventory

- Finding #9 — document the no-rig band confirmation and the Rig panel's **confirm** control in the
  first-run and logging chapters; mirror the note in the install guide §4.
- Finding #10 — install guide §4: remove "there is no rig editor in the SPA yet"; point at Settings → Rigs.
- Finding #13 — CAT chapter: simplify the heading "Important: keep data-mode PTT off the control lines" to
  "Important" and let the body carry the instruction.
- Finding #15 — add update and uninstall guidance to the embedded manual.
- Standing pass — each dogfood session amends the chapters for the surfaces it touched; chapters touched so
  far: first-run, CAT, importing, logging. Chapters not yet walked: forwarding, FT8, my-station,
  session-log, troubleshooting, tuning, the appendices.

## Verification boundary

- Documentation changes only; `task docs:check` and a Hugo build of `manual/` (part of the RPM build).
- Acceptance is the DOCS-01/DOCS-02 rows on the next fresh deployment, walked from the manual alone.
- No hardware, rig command or RF is part of this item.

## References

- [`reports/dogfood-acceptance-v2.0.0-alpha.2.md`](../reports/dogfood-acceptance-v2.0.0-alpha.2.md) —
  Findings #9, #10, #13, #14, #15 and the A2-06/A2-07 rows.
- [`install.md`](../install.md) §4, §7, §10.
- `manual/content/chapters/` — the chapters this item owns.
