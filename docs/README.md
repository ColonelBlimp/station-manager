# Station Manager — documentation map

**Start here.** This is the authoritative index of the project's docs and which
are *live* (kept current, checked against the code) versus *historical* (a
frozen record — never edited to reflect current state). It exists to keep the
set of "sources of truth" small: when docs and code disagree, **the code wins**,
and only Tier 1 below is expected to track it.

`CLAUDE.md` (the Claude Code working doc — invariants, code style, project
idioms) links here rather than duplicating this map.

---

## Tier 1 — live, authoritative, checked against the code

The small working set. Each is kept current; a drift between one of these and
the code is a bug to fix.

| Doc | Owns |
|---|---|
| [`../CLAUDE.md`](../CLAUDE.md) | Agent working doc: load-bearing **invariants**, code style/idioms, project conventions, and the pointer to this map. Read by Claude Code every session; humans welcome. |
| [`session-handoff.md`](session-handoff.md) | Rolling cross-session state + **next steps**. Read at session start, update at session end. Keeps ~15 `### Session N` entries; older ones roll to the archive. |
| [`backlog.md`](backlog.md) | Cross-cutting **deferred work** — known bugs/enhancements that are real but deliberately not-now. Add when found-and-shelved; strike/remove when it ships. |
| [`dogfood-inbox.md`](dogfood-inbox.md) | Raw operating-time capture (the `/log` in-tray). **Capture-only**, not triaged here — items graduate to a fix or `backlog.md`. |
| [`v2-design/api-endpoints.md`](v2-design/api-endpoints.md) | **Canonical, complete HTTP endpoint reference** — every route, full request/response/error/gating detail. Update in the same commit as any route change. |
| [`v2-design/config.md`](v2-design/config.md) | **Canonical config reference** — `config.json` shape, validation rules, safety ceilings. Decided + implemented. |
| [`ft8.md`](ft8.md) | FT8 operator & contributor guide — the single capture point for the FT8 picture (enabling/CGO, SPA panels + SSE wire, occupancy/TX). Keep current as FT8 evolves. |
| [`install.md`](install.md) | Operator install + first-run guide. Update when packaging, the unit file, the data-dir path, or first-run flow changes. |
| [`../manual/`](../manual/) | **Operator manual source** — per-chapter markdown built by Hugo into a single self-contained zero-JS page (ADR 0036), embedded in the daemon and served at `/manual`, and shipped on disk in the RPM for `file://` reading. The built site is a shipped artifact; keep the manual's self-contained tables in step with the code (release-checklist re-sync). |
| [`keyboard-shortcuts.md`](keyboard-shortcuts.md) | Running inventory of every SPA keyboard shortcut. Update in the same commit as any binding change. |
| [`licensing.md`](licensing.md) | Current licensing rules (GPL-3.0-only; see ADR 0023). |

## Tier 2 — historical / append-only (NOT current state)

A reasoning trail and frozen records. **Never edited to reflect current
behaviour** — when one of these describes the past, that's correct. Don't treat
them as references for "how it works now"; the code + Tier 1 are that.

| Doc / dir | What it is |
|---|---|
| [`decisions/`](decisions/) | **ADRs** — append-only decision log, one numbered file each, `status` field walks Proposed→Accepted→Superseded. The *why* behind choices that get revisited. Format in `decisions/README.md`. |
| [`v1-analysis/`](v1-analysis/) | Pre-v2 analysis baseline (`invariants.md`, `lessons-for-v2.md`, `design-decisions-log.md`, `bug-inventory.md`, `architecture-map.md`). Frozen — it fed the v2 rewrite decision. The invariants/lessons remain the rules to apply, but the docs themselves are not edited. |
| [`v2-design/`](v2-design/) *(except `api-endpoints.md` + `config.md`)* | **Pre/mid-build design briefs** — `structure.md`, `api.md`, `bridge.md`, `enrichment.md`, `forwarding.md`, `forwarding-implementation.md`, `frontend-spa.md`, `milestones.md`, `topology.md`, `ui-toolkit.md`, `rig-profiles.md`, `cat-serial-reuse.md`, `cat-performance.md`, `release-acceptance.md`, `sm-cloud-p1.md`. These describe *intent*; the **shipped code + ADRs are the current truth**. Each gets a one-line "historical design — current state is X" banner as it's touched (see Maintenance). |
| [`session-handoff-archive.md`](session-handoff-archive.md) | Rolled-off `### Session N` entries (grep-able convenience; git history is authoritative). |
| [`reviews/`](reviews/) + [`reviews/archive/`](reviews/archive/) | Code-review artifacts, each with a point-in-time `## Resolution` section. Archived once actioned. |
| [`reports/`](reports/), [`research-pipeline.md`](research-pipeline.md) | Historical reports / notes. |

## Not in this repo: agent memory

The Claude Code memory files (`~/.claude/projects/.../memory/`, indexed by
`MEMORY.md`) are an agent-private overlay — durable facts/preferences, not
project docs. They're the same "too many sources" axis but governed separately;
prune/dedupe them on their own track, not here.

---

## How this is maintained (little-by-little)

- **Tier 1 is the contract.** Keep each Tier-1 doc current; fix drift against the
  code as it's found. Adding a route → update `api-endpoints.md` in the same
  commit; rebinding a key → `keyboard-shortcuts.md`; etc.
- **Tier 2 is never "freshened."** It's the record. If a Tier-2 doc is wrong
  about *today*, that's expected — point readers at the code/ADR instead.
- **Audit passes, one doc at a time.** The ongoing cleanup runs little-by-little,
  interleaved with code reviews: each pass takes ONE doc, checks it against the
  code, fixes Tier-1 drift, and — when a demoted `v2-design/` brief is touched —
  stamps the historical banner at its top. No big-bang rewrites.
- **New docs declare their tier here first.** Don't create a second index, and
  don't add a "live" doc whose job an existing Tier-1 doc already does.
