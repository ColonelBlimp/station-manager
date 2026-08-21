# Session-history retirement record

**Status:** Point-in-time record
**Retired:** 2026-08-21
**Final tracked snapshot:** `d0391ed7`

Station Manager's rolling handoff accumulated implementation chronology from the early v2 daemon,
API, forwarding, CAT, operator-SPA, and FT8 work through the August 2026 lifecycle, logging, audit,
and dogfood arcs. The convenience archive reached 1.61 MB, while the recent handoff repeated state
already owned by code, canonical references, ADRs, reviews, and the ranked backlog.

The files were retired after the documentation library gained bounded replacements:

- [`current.md`](../current.md) — present goal, state, decisions, and next action;
- [`backlog.md`](../backlog.md) plus [`work/`](../work/) — ranked outcomes and selected-work evidence;
- [`README.md`](../README.md) — generated routing to current canonical references;
- [`decisions/`](../decisions/) — genuinely weighed alternatives and rulings;
- [`reviews/`](../reviews/) and [`reports/`](../reports/) — point-in-time evidence.

The historical text remains recoverable from Git and is deliberately not current truth:

```sh
git show d0391ed7:docs/session-handoff.md
git show d0391ed7:docs/session-handoff-archive.md
```

Use `git log -S'<distinct phrase>' -- docs/session-handoff.md
docs/session-handoff-archive.md` to locate an earlier version when the final snapshot is not enough.
Prefer the owning code, canonical reference, or ADR for any current claim. A small compatibility
stub remains at [`session-handoff.md`](../session-handoff.md) so older links route here instead of
becoming dead ends.
