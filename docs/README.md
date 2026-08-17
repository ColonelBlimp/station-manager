# Station Manager — documentation map

**Start here.** This is the authoritative index of the project's docs and which
are *live* (kept current, checked against the code) versus *historical* (a
frozen record — never edited to reflect current state). It exists to keep the
set of "sources of truth" small: when docs and code disagree, **the code wins**,
and only Tier 1 below is expected to track it.

`AGENTS.md` is the compact, tool-neutral instruction kernel and links here
rather than duplicating this map. `CLAUDE.md` imports that same file for Claude
Code compatibility; it is not a second handwritten instruction source.

---

## Tier 1 — live, authoritative, checked against the code

The small working set. Each is kept current; a drift between one of these and
the code is a bug to fix.

| Doc | Owns |
|---|---|
| [`../AGENTS.md`](../AGENTS.md) | Compact agent/contributor kernel: load-bearing **invariants**, safety rules, code/test conventions, and routing to this map. Codex reads it directly; [`CLAUDE.md`](../CLAUDE.md) imports it for Claude Code. |
| [`current.md`](current.md) | **Bounded current-work capsule.** The present goal, state, next action, settled constraints, relevant files, and coordination notes. Injected in full at session start; limited to 2 KB and never carries copied Git status or history. |
| [`backlog.md`](backlog.md) | **The definitive ranked worklist** — the one priority-ordered list of every triaged, not-yet-done cross-cutting item. Owns the ranking (Worklist index at the top). "What's next, and in what order" is answered here. Strike/remove when it ships. |
| [`reviews/ft8-logging-gaps.md`](reviews/ft8-logging-gaps.md) | **TRANSIENT — delete when shipped.** The 10-finding `internal/ft8` logging audit (2026-08-01), split out at the operator's request; `backlog.md` still owns the ranking and carries a pointer line to it. A satellite of the SHIP GATE log-coverage entry, not a second worklist. **Fold into `backlog-archive.md` and delete this file once the findings ship** — two open lists is exactly the drift this map exists to prevent. |
| [`reviews/bridge-logging-gaps.md`](reviews/bridge-logging-gaps.md) | **TRANSIENT — delete when shipped.** The 8-finding `internal/bridge` logging audit (2026-08-01); sibling of the ft8 one above, same axis, and its finding B2 is the SAME defect as that file's finding 1 (both hubs evict subscribers silently). Same rule: `backlog.md` owns the ranking, fold in and delete once shipped. |
| [`reviews/api-logging-gaps.md`](reviews/api-logging-gaps.md) | **TRANSIENT — delete when shipped.** The 6-finding `internal/api` logging audit (2026-08-01); third of the three siblings. Fewest findings because the access log already covers this package — its **NOT-gaps section is the load-bearing half** (why bodies and full URLs must never be logged). Same rule: `backlog.md` owns the ranking, fold in and delete once shipped. |
| [`reviews/qsoservice-logging-gaps.md`](reviews/qsoservice-logging-gaps.md) | **TRANSIENT — delete when shipped.** The `internal/qsoservice` logging audit (2026-08-01), fourth of the siblings. **Read its "READ FIRST" section even if you skip the findings** — it disproved SHIP GATE item (b) in `backlog.md` (already struck there) and corrected a claim in the api review. Same rule: `backlog.md` owns the ranking, fold in and delete once shipped. |
| [`reviews/forwarding-logging-gaps.md`](reviews/forwarding-logging-gaps.md) | **TRANSIENT — delete when shipped.** The `internal/forwarding` logging audit (2026-08-01), fifth and last of the siblings. The **inverted** one: not too quiet but 43.3% of `smd.log` by bytes, so its headline must be decided together with SHIP GATE item (d) (build-version stamping). Verifies there is **no credential leak** in the forwarding error lines. Same rule: `backlog.md` owns the ranking, fold in and delete once shipped. |
| [`dogfood-inbox.md`](dogfood-inbox.md) | **Raw capture only** (the `/log` in-tray). Un-triaged notes jotted mid-operation; NOT ranked or acted on here — each **graduates** to a fix, a `backlog.md` entry, or struck as a non-issue. |
| [`v2-design/api-endpoints.md`](v2-design/api-endpoints.md) | **Canonical, complete HTTP endpoint reference** — every route, full request/response/error/gating detail. Update in the same commit as any route change. |
| [`v2-design/config.md`](v2-design/config.md) | **Canonical config reference** — `config.json` shape, validation rules, safety ceilings. Decided + implemented. |
| [`ft8.md`](ft8.md) | FT8 operator & contributor guide — the single capture point for the FT8 picture (enabling/CGO, SPA panels + SSE wire, occupancy/TX). Keep current as FT8 evolves. |
| [`install.md`](install.md) | Operator install + first-run guide. Update when packaging, the unit file, the data-dir path, or first-run flow changes. |
| [`smcloud-deploy.md`](smcloud-deploy.md) | SM Cloud deployment runbook (ADR 0040 S6) — VPS + Postgres + TLS proxy + daemon wiring + verify/operations. Artifacts in `deploy/smcloud/`. Update with any smcloud config/route change. |
| [`../manual/`](../manual/) | **Operator manual source** — per-chapter markdown built by Hugo into a single self-contained zero-JS page (ADR 0036), embedded in the daemon and served at `/manual`, and shipped on disk in the RPM for `file://` reading. The built site is a shipped artifact; keep the manual's self-contained tables in step with the code (release-checklist re-sync). |
| [`keyboard-shortcuts.md`](keyboard-shortcuts.md) | Running inventory of every SPA keyboard shortcut. Update in the same commit as any binding change. |
| [`licensing.md`](licensing.md) | Current licensing rules (GPL-3.0-only; see ADR 0023). |

### How work flows through the docs

Each work-tracking doc has **one job**; work moves between them in one direction,
so nothing gets lost in a document and nothing is tracked in two places at once:

```
notice it → dogfood-inbox.md (raw capture)
          → triage → backlog.md (ranked — the definitive "what's next")
                   → pull top items → current.md (bounded: doing it now)
                                    → ship → MOVE detail to backlog-archive.md
                                           + add a handoff record only when useful
  durable fact (preference/invariant/state)? → memory files (~/.claude/.../memory/)
  decision with weighed alternatives?        → decisions/ (ADR)
  FT8-internal mechanics?                     → ft8.md
```

The discipline this enforces: a new idea goes to the **inbox**, not straight into
whatever's open; priority is decided **once**, in the backlog ranking; and the
active cycle stays small (1–3 items) so work gets *finished* rather than scattered.

## Tier 2 — historical / append-only (NOT current state)

A reasoning trail and frozen records. **Never edited to reflect current
behaviour** — when one of these describes the past, that's correct. Don't treat
them as references for "how it works now"; the code + Tier 1 are that.

| Doc / dir | What it is |
|---|---|
| [`decisions/`](decisions/) | **ADRs** — append-only decision log, one numbered file each, `status` field walks Proposed→Accepted→Superseded. The *why* behind choices that get revisited. Format in `decisions/README.md`. |
| [`session-handoff.md`](session-handoff.md) | Detailed recent session record retained for grep and selective reading; not current state and never automatic context. |
| [`v1-analysis/`](v1-analysis/) | Pre-v2 analysis baseline (`invariants.md`, `lessons-for-v2.md`, `design-decisions-log.md`, `bug-inventory.md`, `architecture-map.md`). Frozen — it fed the v2 rewrite decision. The invariants/lessons remain the rules to apply, but the docs themselves are not edited. |
| [`v2-design/`](v2-design/) *(except `api-endpoints.md` + `config.md`)* | **Pre/mid-build design briefs** — `structure.md`, `api.md`, `bridge.md`, `enrichment.md`, `forwarding.md`, `forwarding-implementation.md`, `frontend-spa.md`, `milestones.md`, `topology.md`, `ui-toolkit.md`, `rig-profiles.md`, `cat-serial-reuse.md`, `cat-performance.md`, `release-acceptance.md`, `sm-cloud-p1.md`. These describe *intent*; the **shipped code + ADRs are the current truth**. Each gets a one-line "historical design — current state is X" banner as it's touched (see Maintenance). |
| [`session-handoff-archive.md`](session-handoff-archive.md) | Rolled-off `### Session N` entries (grep-able convenience; git history is authoritative). |
| [`backlog-archive.md`](backlog-archive.md) | Shipped / resolved / ruled-out `backlog.md` items, **moved** here (not struck in place) so the live backlog stays lean. Not read at session start; open it for an item's history. |
| [`reviews/`](reviews/) + [`reviews/archive/`](reviews/archive/) | Point-in-time code-review and maintainability artifacts, including [`oss-maintainability-plan.md`](reviews/oss-maintainability-plan.md). They do not own priority: accepted work moves to `backlog.md`; artifacts are archived once actioned. |
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
