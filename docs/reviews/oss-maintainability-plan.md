# Project tidy-up and OSS maintainability plan

**Status:** Accepted — Phase 1 in progress (tickets 1–3 delivered; ticket 4 next) as of 2026-08-20\
**Date:** 2026-08-17  
**Documentation tier:** Tier 2 — point-in-time review and proposed plan

This document records a bounded plan for addressing weaknesses identified while comparing Station
Manager with other maintainer-led OSS applications of a similar size. The overall body of work is
called a **project tidy-up**, not a refactor: most of it is information architecture, context
routing, test ergonomics, and contributor access. A few later, individually bounded code changes
are refactors, but they do not define the programme.

This is **not** a second worklist: [`docs/backlog.md`](../backlog.md) remains the only authority for
priority and scheduling. If this plan is accepted, its concrete tasks move into that backlog in the
order the operator chooses; this file then remains the rationale and acceptance reference.

## Objective

Make Station Manager easier for a second maintainer to understand, test, and change without
weakening its unusually strong correctness, RF-safety, and release disciplines.

The programme targets four related weaknesses:

1. documentation volume, navigation, duplication, and agent-context cost;
2. contributor entry cost and bus factor;
3. current-state architectural discoverability;
4. slow or overly implementation-specific test feedback;
5. known complexity concentrations in daemon composition and large handlers.

## Baseline

Approximate repository counts measured on 2026-08-17:

- 80,286 lines of production Go and 44,806 lines of production Svelte/TypeScript/JavaScript;
- 102,740 lines of Go tests and 30,845 lines of frontend tests;
- 559 test files, 74 Go packages, and 74,055 lines of Markdown;
- 73 ADR files and 2,287 commits, attributed to one author.

The documentation tree itself is approximately 5.8 MB. Its largest file,
`docs/session-handoff-archive.md`, is 1.61 MB. The more immediate cost is the hot agent context:

- root `CLAUDE.md` is 37.7 KB;
- the injected `## Now` section is 3.5 KB and 42 lines despite its stated 25-line limit;
- entering `internal/ft8` can add another 37.8 KB of scoped instructions;
- an FT8-oriented session can therefore consume roughly 79 KB of project prose before reading the
  task's code or design.

These are rough repository counts and include some generated, retained, or historical material.
They nevertheless establish that Station Manager is a mid-sized product rather than a small
application.

Its architecture, tests, CI, and decision trace are stronger than typical for a maintainer-led OSS
application. Its main risks are the reverse side of those strengths: a large private vocabulary,
long current/historical trails, brittle source-shape tests in a few areas, significant complexity
outliers, and no proven path for a second person to contribute independently.

## Constraints

- Do not interrupt or mix unrelated cleanup into the active ADR 0070 lifecycle work.
- Do not create another source of truth for priority; accepted work belongs in `docs/backlog.md`.
- Prefer small, behavior-preserving slices over a broad architecture rewrite.
- Treat context as a budgeted runtime resource: automatic guidance contains rules and routing, not
  history or task dossiers.
- Preserve the existing RF/TX safety boundaries. No live transmission or hardware-dependent action
  is part of this programme.
- Reuse `DEVELOPING.md`, `docs/README.md`, the Taskfile, and existing CI rather than duplicating
  them in new manuals or frameworks.
- Measure improvement at contributor-visible boundaries, not by line count or coverage percentage.

## Phase 0 — finish the lifecycle slice cleanly

**Timing:** complete the active ADR 0070 phase-1 work first.

Do not mix onboarding or structural cleanup into Shutdown C or its phase-1 acceptance test. Before
ADR 0070 reaches production migration, resolve the logging/report sequencing question explicitly:
`Orchestrator.Shutdown` returns a logging-free report for `cmd/smd` to format, so the production
design must say when the logging service closes and how it remains usable while that report is
formatted.

Exit criteria:

- Shutdown C and the design §5 acceptance test are complete and reviewed.
- The lifecycle design states exactly where logging closes relative to report formatting.
- The lifecycle commit series contains no unrelated maintainability refactor.

## Phase 1 — build the documentation library and reduce agent context

**Estimated effort:** 4–7 focused days, delivered as several independently reviewable slices.

This phase precedes new contributor documentation. Adding another guide before fixing the library
would make the problem larger.

### 1.1 Give every document one role

Use five implementation-document classes, plus the separate operator manual:

| Class | Purpose | Agent-loading policy |
|---|---|---|
| Kernel | Global safety rules and project conventions | Automatic |
| Current | Present goal, state, decisions, and next action | Automatic |
| Canonical | Current reference for one subject | On demand |
| Work item | Evidence and criteria for one active backlog item | Only when selected |
| Record | ADRs, reviews, session history, and superseded designs | Never automatic |

The normal agent path becomes: load Kernel + Current, consult the catalog, then read only the
relevant Canonical document and selected Work item. Operator documentation is not implementation
context unless the task concerns operator behavior.

### 1.2 Make `AGENTS.md` the generic instruction kernel

Create a tool-neutral root `AGENTS.md`, distilled to no more than 8 KB. Keep only:

- safety and RF prohibitions;
- load-bearing architectural invariants;
- unusual code, testing, worktree, and generated-file rules;
- documentation routing;
- the commands needed to verify a change.

Move chronology, cautionary tales, current work, release history, detailed ATDD essays, and
subsystem explanations into routed canonical or historical documents.

Codex natively discovers hierarchical `AGENTS.md` files and has a 32 KiB default combined project
instruction limit, so renaming the current 37.7 KB `CLAUDE.md` unchanged would already overflow the
default before nested guidance was added. See the
[official Codex `AGENTS.md` documentation](https://learn.chatgpt.com/docs/agent-configuration/agents-md).

Preserve Claude Code compatibility without maintaining a second handwritten copy. On this
Linux-focused repository, first verify whether committed symlinks work correctly for both tools:

```text
CLAUDE.md -> AGENTS.md
internal/bridge/CLAUDE.md -> AGENTS.md
internal/ft8/CLAUDE.md -> AGENTS.md
```

If either tool does not follow that arrangement, use the tool's verified import mechanism or
generate both filenames from one canonical source and enforce equality in CI.

Create scoped `AGENTS.md` files only where local rules earn their context cost. The bridge's current
6.8 KB guidance is already near the desired shape. Distil FT8's 37.8 KB guidance to approximately
10 KB, retaining the TX/attribution invariants and coordinated-edit checklist and routing the
explanations to `docs/ft8.md`.

### 1.3 Replace the rolling handoff with a session capsule

Create a tracked `docs/current.md`, limited to 2 KB and approximately 12–15 lines, with a fixed
schema:

```text
Goal · State · Next · Decisions not to revisit · Do not · Relevant files · Coordination
```

The SessionStart hook should inject that entire bounded file. It should derive branch, dirty-tree,
push, and recent-commit facts from Git rather than copying commit history into prose. Detailed
session notes go directly to cold history; one file no longer serves as both orientation and a
rolling historical record.

### 1.4 Turn living work documents into indexes

Reduce `docs/backlog.md` to a ranked index, preferably under 10 KB. Each row carries only a stable
ID, priority, one-line outcome, status, and link. Put evidence, alternatives, acceptance criteria,
and history in one dossier per open item under `docs/work/`; move closed dossiers under
`docs/archive/work/`.

Apply the same rule to `docs/dogfood-inbox.md`: unresolved captures remain in the inbox; resolved or
triaged entries move immediately to their destination. An inbox must not double as a 193 KB archive.

### 1.5 Add a small generated catalog

Maintain a small manifest for live documents only: stable ID, path, class, topics, applicable code
scope, and a one-sentence summary. Directory conventions classify decisions, reviews, and archives
as records without listing every historical file by hand.

Generate a compact `docs/README.md` view by audience, subsystem/topic, and class. Add a simple
`task docs:find QUERY=<term>` that returns paths and summaries rather than document contents. Do not
build a vector database for a 5.8 MB local corpus.

### 1.6 Split and retire selectively

Split only live canonical references that remain too large to use on demand—initial candidates are
the config, API endpoint, FT8, and FTdx10 CAT references. Use a small index and a handful of stable
topic files, leaving temporary compatibility stubs at old paths. Do not fragment historical design
documents merely because they are large; keep them out of automatic context instead.

Replace the 1.61 MB session archive with short periodic summaries after preserving its final state
in Git history or a named tag. Retain ADRs: they are naturally scoped records and supply valuable
decision rationale. Archive completed reviews once their durable outcome exists in code, an ADR, or
a canonical reference.

### 1.7 Prevent regrowth

Add CI checks for:

- root kernel ≤ 8 KB;
- current capsule ≤ 2 KB;
- ordinary automatic context ≤ 10 KB and scoped context ≤ 20 KB;
- compatibility links or generated provider files are valid and equal;
- unique canonical owner per live topic;
- catalog paths and internal Markdown links resolve;
- backlog/inbox size budgets;
- no record-class document is marked for automatic loading.

Exit criteria:

- A normal session automatically receives no more than 10 KB of repository guidance; a complex
  scoped session receives no more than 20 KB.
- `AGENTS.md` is the canonical generic instruction kernel, with tested Claude Code compatibility.
- The SessionStart payload is a complete ≤2 KB capsule, not a slice of a growing historical file.
- A topic or code path resolves through the catalog to one canonical reference and, when active, one
  work dossier.
- `docs/backlog.md` and `docs/dogfood-inbox.md` are usable indexes/inboxes rather than archives.
- Documentation budgets, catalog integrity, and internal links are CI-enforced.

## Phase 2 — create a thin contributor front door

**Estimated effort:** 1–2 focused days.

Add a concise root `CONTRIBUTING.md` that points to the existing detailed material instead of
copying it. It should answer:

- what to read first;
- how to make and test a small change;
- which areas can touch credentials, real hardware, or RF;
- what the project expects from acceptance tests and reversion proofs;
- which generated and historical files must not be edited;
- what a useful pull request contains.

Add a read-only `task doctor` that reports the installed Go, Node, npm, Hugo, Task, and C-toolchain
versions; identifies missing required tools and artifacts; distinguishes optional SM Cloud tooling;
and prints corrective commands. It must not install packages, alter configuration, start services,
or touch operator data.

Add a minimal `SECURITY.md` covering private vulnerability reporting, supported versions, the
loopback-only default, credential handling, and the treatment of RF-safety reports.

Exit criteria:

- On a clean supported workstation, a contributor can clone the repository and run one focused
  test within 15 minutes.
- That path requires no rig, audio device, credentials, systemd service, or dogfood data.
- `CONTRIBUTING.md` remains a gateway rather than another manual, preferably under 200 lines.
- `task doctor` is covered by tests for missing required and optional tools.

## Phase 3 — provide one current architecture map

**Estimated effort:** 2–3 focused days.

Create one compact Tier-1 `docs/architecture.md` and register it in `docs/README.md`. It should
describe current code only and contain:

- process and package boundaries;
- ownership of configuration, SQLite data, hub events, lifecycle state, and RF control;
- end-to-end paths for QSO submission, forwarding, rig commands, and shutdown;
- “change this here” pointers for common contributor tasks;
- explicit network, credential, hardware, and RF boundaries.

Do not turn it into another design history. ADRs retain the reasoning trail; this document provides
the current map.

In the same phase, reconcile Tier-1 documentation and the live backlog: archive closed or stale
entries, remove contradictory current-state claims, and leave historical documents historical. Add
an automated check for internal Markdown links and documentation-map targets.

Exit criteria:

- A contributor can trace each of the four principal flows without reading the ADR archive.
- Every Tier-1 document has one distinct responsibility.
- Internal links and documentation-map entries are checked in CI.
- No second active-cycle or ranked-work document is introduced.

## Phase 4 — establish explicit test lanes

**Estimated effort:** 3–5 focused days.

Expose four clear test levels through the Taskfile and document when each is expected:

1. **Focused/package** — seconds; the normal edit loop.
2. **Fast repository** — no hardware, live credentials, external network, expensive decodes, or
   destructive database setup.
3. **Race/full** — the existing comprehensive local and hosted releasability gates.
4. **Hardware/manual** — explicitly opt-in and never reached by an ordinary contributor command.

Audit AST and source-shape guards. Retain guards that protect genuine TX, lock-order, ownership, or
publication invariants. Every retained guard must explain why a behavioral test would not defend
the invariant. Convert incidental implementation-shape assertions to behavioral tests, and replace
timing sleeps with observable barriers or fake clocks where practical.

Exit criteria:

- The fast repository lane completes in approximately 90 seconds or less on the reference machine.
- Focused package work has a documented sub-15-second feedback loop.
- Every retained source-shape guard carries a safety or boundary rationale.
- Hardware and destructive integration tests cannot run accidentally.
- The full race, integration, frontend, static, and CGO release gates remain intact.

## Phase 5 — reduce measured code complexity by ratchet

**Timing:** begin after ADR 0070's production migration removes lifecycle teardown wiring from
`cmd/smd.run`.

The first target is `cmd/smd.run`, currently the largest measured complexity outlier. Extract
bounded composition helpers around existing subsystem boundaries—configuration/logging, storage,
forwarding, radio/FT8, and HTTP—without introducing another generic lifecycle or service framework.
Let the lifecycle graph own lifecycle behavior; helpers should construct and connect concrete
objects.

Subsequent candidates are:

- duplication in `internal/database/sqlite/api_context.go`;
- configuration handler and defaulting complexity;
- large non-safety orchestration functions already named in `.golangci.yml`.

Do not refactor FT8 sequencers or the bridge read loop merely to improve a metric. Their safety and
dispatch complexity warrants change only when a concrete maintenance need exists.

For each slice:

- preserve behavior with characterization and acceptance tests;
- measure complexity before and after;
- delete at least one matching exemption from `.golangci.yml`;
- avoid broad interface or package churn;
- leave the tree releasable after the slice.

Exit criteria:

- `cmd/smd.run` no longer needs its present complexity exemption.
- The exemption list trends downward; new exemptions require an explicit review decision.
- No replacement framework or generic composition layer is introduced.
- CI duration and test readability do not regress materially.

## Phase 6 — validate OSS sustainability

**Estimated effort:** 1–2 focused days plus a real onboarding exercise.

Add only the useful community surface:

- pull-request and issue templates;
- an explicit support matrix for platforms, rigs, build variants, and alpha compatibility;
- several non-RF, non-credential starter issues;
- a public release-build exercise that does not depend on the maintainer's private environment.

Then ask a second person to complete one small change using only repository documentation. Record
every place they need private explanation and fix those points. This cold-onboarding exercise is the
meaningful bus-factor test; adding governance documents alone is not.

Exit criteria:

- A second person completes a small test-backed change without access to dogfood state or private
  credentials.
- Every undocumented intervention discovered during that exercise is either fixed or recorded in
  the authoritative backlog.
- A public/keyless release-shaped build can be produced from a clean environment by following the
  repository instructions.

## Proposed initial backlog tickets

Once the operator accepts and ranks this programme, split its initial work into bounded tickets:

1. **Agent-context kernel** — ✅ **DELIVERED** (`AGENTS.md` is the canonical ≤8 KB kernel,
   `CLAUDE.md` and scoped `CLAUDE.md` are import-only shims, byte-budget checks in CI): distil the
   root and scoped agent instructions, establish `AGENTS.md` as canonical, preserve tested Claude
   compatibility, and add byte-budget checks.
2. **Current capsule** — ✅ **DELIVERED** (`docs/current.md`, ≤2 KB, injected whole by the
   SessionStart hook with Git-derived state): replace the mixed rolling handoff with bounded
   `docs/current.md` injection and Git-derived repository state.
3. **Documentation routing** — ✅ **DELIVERED** (`docs/catalog.json` plus the generated
   `docs/README.md`; the five document classes are live): define the five classes, add the
   live-document catalog, and generate the compact library index.
4. **Living-work decomposition** — ⏳ **IN PROGRESS — NEXT (Phase 1.4)** (W-dossiers exist under
   `docs/work/`, but `docs/backlog.md` (~164 KB) and `docs/dogfood-inbox.md` (~193 KB) are not yet
   bounded indexes and the giant session archive is not yet summarized): turn backlog and dogfood
   inbox into bounded indexes plus routed dossiers; summarize and retire the giant session archive.
5. **Contributor gateway:** `CONTRIBUTING.md`, `SECURITY.md`, `task doctor`, and a clean-workstation
   acceptance exercise.
6. **Current architecture map:** `docs/architecture.md`, Tier-1 registration, internal-link check,
   and live-document reconciliation.
7. **Test ergonomics:** explicit test-lane tasks, contributor guidance, and a source-guard inventory.

The complexity ratchet starts only when lifecycle production migration naturally opens
`cmd/smd.run`; it should not be pulled ahead of the active lifecycle or release-gate work.

## Measures of success

Review these measures after the documentation-library tickets, after the contributor/architecture
tickets, and again after the first complexity slice:

| Measure | Target |
|---|---|
| Root automatic agent kernel | ≤ 8 KB |
| Current-session capsule | ≤ 2 KB |
| Normal / scoped automatic context | ≤ 10 KB / ≤ 20 KB |
| Live-document routing | one canonical owner per topic |
| Ranked backlog index | preferably ≤ 10 KB |
| Clean clone to first focused test | ≤ 15 minutes |
| Focused package feedback | < 15 seconds for the documented examples |
| Fast repository feedback | approximately ≤ 90 seconds on the reference machine |
| Unexplained source-shape guards | zero |
| Tier-1 link/map failures | CI-detected |
| Complexity exemptions | monotonically decreasing after the baseline |
| Cold onboarding | one independent, test-backed contribution completed |

## Non-goals

- Rewriting the DI container, lifecycle graph, or service supervisors.
- Splitting the monorepo or changing the daemon/SPAs product topology.
- Maximizing a coverage percentage or minimizing lines of code.
- Refactoring RF/TX paths without a concrete defect or maintainability trigger.
- Creating a second backlog, roadmap, session log, or contributor manual.
- Building a documentation search service or vector database where a catalog plus `rg` suffices.
- Adding governance ceremony before there is a community that needs it.
