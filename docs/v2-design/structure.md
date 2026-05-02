# Station Manager v2 — Repository Structure and Module Design

**Status:** First entry in `docs/v2-design/`, written 2026-04-15 after the v2 rewrite decision was made (2026-04-14). Captures the structural decisions from session 2 of v2 work.

**Purpose:** Capture the *why* of v2's structural choices so we don't re-derive them later. Every decision here has a rationale. If a future session disagrees with a decision, update it explicitly and record the new reasoning — don't silently drift away from these.

**How this document relates to others:**

- `docs/v1-analysis/*` — analysis of v1 (what v1 is, what worked, what didn't). The inputs to v2 design.
- `docs/v2-design/*` — design decisions being made for v2 as construction proceeds. This file is the first; siblings like `api.md`, `milestones.md`, etc. come later.
- `docs/session-handoff.md` — rolling cross-session state. Durable design decisions live here in `v2-design/`, not in the handoff.
- Memory files (`~/.claude/projects/.../memory/`) — quick-reference facts that persist across sessions. This document is the long-form record; memory entries may cross-reference it.

---

## The decisions

### 1. Monorepo on the `main` branch of the existing repo

**Decision:** v2 is built on `main` of the existing Station Manager git repository. v1 is preserved on the `v1` branch and at the `v1.0.0` tag. Both paths live in the same repo; there is no separate v2 repository.

**Why:**

- Solo-dev personal project. Cross-repo ceremony (coordinated commits, version-pinning between repos, separate issue trackers, separate CI) is overhead imported from multi-team contexts that don't apply here.
- Everything the v2 daemon, clients, and bridges need to share (types, ADIF parsing, DB layer, config, errors, DI, enums) is small enough to live comfortably in one repo.
- Carrying v1 alongside v2 on branches lets the user run v1 from the `v1` branch for daily ham radio ops while v2 is under construction on `main`. Both paths are always available from one checkout.

**Alternative considered:** separate `station-manager-v2` repository. Rejected — for a solo dev, the only benefit is psychological ("clean slate") and it's strictly paid for with ongoing cross-repo coordination pain.

**Related:** `project_sm_restructure` memory note. `docs/v1-analysis/design-decisions-log.md` → "v2 rewrite vs. v1 incremental refactor."

---

### 2. Single Go module at milestone 1; `go.work` returns at milestone 2

> **Superseded 2026-04-21 by decision #7 (Gio), then again 2026-04-30 by ADR 0001 (browser SPA).** Neither Gio nor SPA brings `go.work` back — a single `go.mod` at the repo root covers every Go binary in v2 through every milestone. The SPA is embedded as a Go `//go:embed` asset; it has its own npm/Vite toolchain in `frontend/logging/` but is not a Go module. The rationale below still explains *why* module boundaries were only going to come back for Wails-tooling reasons; that reason no longer exists.


**Decision:** Milestone 1 of v2 uses a **single `go.mod`** at the repo root. Everything under `cmd/` and `internal/` lives in this one module. `go build ./...` just works; there is no `go.work` file.

When the first Wails thin client is reintroduced in milestone 2, `go.work` comes back — but **only** to accommodate the Wails apps, each of which gets its own module for frontend-tooling reasons.

**Why:**

- Module boundaries in Go earn their keep for exactly two reasons: **independent build tooling** (Wails/Vite/npm is the canonical example — the frontend build needs its own tsconfig, package.json, etc.) or **dependency isolation** (keeping one binary's deps out of another's `go.sum`).
- Pure Go binaries (`smd`, `importer`, any future bridges) have neither need. They have no frontend tooling, and for Station Manager's dependency footprint, the cost of a shared `go.sum` is negligible.
- A single `go.mod` is one less thing to reason about during the hard-thinking milestone 1 phase where the daemon's API, domain layer, and internal shape are all being decided at once.
- Adding `go.work` later is a 10–15-minute mechanical migration: create `go.work`, add `./apps/logging`, tidy up. Not a process burden.

**Alternative considered:** start with `go.work` from day one and an empty `apps/` directory reserved for future Wails clients. Rejected because empty-reserved slots accumulate imaginary requirements before the real ones arrive, and introduce workspace ceremony before it's justified. YAGNI applies to module boundaries.

---

### 3. Only Wails apps get their own modules; pure Go binaries stay in the root module

> **Superseded 2026-04-21 by decision #7 (Gio), then again 2026-04-30 by ADR 0001 (browser SPA).** There are no Wails apps in v2, no Gio apps either. The client apps are browser SPAs embedded in the daemon binary. Every Go binary — daemon, importer, future bridge — lives in the root module, built via `go build ./cmd/<name>`. The SPA brings its own npm/Vite toolchain in `frontend/logging/` (separate package.json), but that is not a Go module concern. The rationale below is preserved because the underlying rule ("module boundaries earn their keep via independent build tooling or dependency isolation; pure Go binaries have neither") is still how we decide whether a future package ever needs its own module.


**Decision:** The only things that get their own `go.mod` files in v2 are the Wails client applications. Every pure Go binary — the daemon `smd`, `importer`, any future bridges like `sm-serial-bridge` or `wsjtx-bridge` — lives in the root module and is built via `go build ./cmd/<name>`.

**Why:**

- The drivers for "a binary needs its own module" are independent build tooling and dependency isolation (see decision #2).
- Wails apps need their own modules because their frontends bring in a distinct toolchain (Vite, npm, TypeScript) that Go tooling doesn't know about — each Wails app's backend must live beside its frontend, with its own `go.mod`, so the Wails CLI can drive the build.
- Pure Go binaries share the same toolchain, the same `go build`, and benefit from sharing a single `go.sum`. Splitting them into separate modules adds replace directives, multiple tidy passes, and mental overhead with no corresponding benefit.

**Concrete consequence:** in milestone 2, `go.work` will list exactly these entries:

- `.` (the root module, containing `cmd/smd`, `cmd/importer`, all of `internal/`)
- `./apps/logging`
- `./apps/logbook`
- `./apps/config`

Each Wails app imports from the root module via its full path (`github.com/ColonelBlimp/station-manager/internal/smclient`, etc.). `go.work` wires local development to live source; released builds resolve through normal module versioning.

---

### 4. Shared `internal/` — all binaries import from a single shared library tree

**Decision:** `internal/types`, `internal/adif`, `internal/database/sqlite`, `internal/errors`, `internal/iocdi`, and the rest of the carry-forward library packages live in a single shared `internal/` tree. Every binary in the monorepo (smd, importer, Wails clients, future bridges) imports from the same shared library code. There are no per-app private copies of shared packages, and there are no sub-modules inside `internal/`.

**Why:**

1. **`types.Qso` coherence is load-bearing.** `types.Qso` mirrors the ADIF specification (see `docs/v1-analysis/invariants.md` → "types.Qso follows ADIF"). If logging and logbook had drifted copies of `types.Qso`, it would produce a whole class of silent bugs: a field added for logging would be missing from logbook, so a QSO logged from one app wouldn't display correctly in the other. Shared types eliminates this entire bug class by construction.

2. **Solo-dev release coordination is trivial.** The "coordinated atomic release" concern is imported from distributed-team contexts — it matters a lot when multiple teams own multiple services and need deploy windows. For Station Manager, releasing is `git tag v2.x.y && go build ./... && wails build`. That is five minutes of mechanical work, not a process burden. There is no team to coordinate with, no deploy window to hit, no canary to manage.

3. **Shared bugfixes are a feature, not a cost.** If a bug is discovered in `internal/adif` callsign parsing, you want every binary to pick up the fix the next time it's rebuilt — not "fixed in logging but still broken in logbook." Shared `internal/` guarantees this automatically. The alternative ("fix lands in logging first and drifts to logbook later when someone remembers") is strictly worse for a ham radio log.

4. **Emergency single-binary builds still work.** If `smd` has a critical fix and the Wails frontends happen to be broken for some unrelated reason, `go build ./cmd/smd` at the current tip produces a fresh daemon binary. The shared-code guarantee just means the next time logging is rebuilt, it'll also have the fix — naturally, not by process.

**Alternative considered:** per-app private library packages — each Wails app and each `cmd/` binary owns its own copy of the helpers it needs, no shared `internal/`. Rejected because:

- 3–5× maintenance multiplier on shared code
- `types.Qso` drift between apps is guaranteed over time, producing the exact class of bug that decision #4's rationale #1 exists to prevent
- Duplicate ADIF parsing, duplicate error handling, duplicate DI wiring
- More complex CI (test each app's private copy)
- For a solo dev, strictly worse. This pattern only makes sense at a scale we don't have.

---

### 5. Source sharing and wire compatibility are separate axes

**Decision:** Source sharing (decision #4: shared `internal/`) and wire compatibility (how smd's HTTP API evolves over time) are **two independent design concerns** and should not be conflated. The decision on one does not determine the decision on the other.

- **Source sharing** gives you code-level coherence and bugfix amplification. It is a *convenience*.
- **Wire compatibility** is about whether an old client binary can talk to a new daemon binary (or vice versa). It is the *actual contract* between processes.

**Why this distinction matters:** if `smd` changes the JSON shape it emits for `GET /qso/:id`, **every client breaks the next time it's rebuilt** — even clients that share source with the daemon. Shared source does not give you wire compatibility for free. The two axes need different disciplines:

- **Source sharing discipline:** the monorepo tag (`v2.x.y`) represents a coherent point in time across all binaries. Build everything from that tag and they all agree on `types.Qso`, `internal/adif`, etc.
- **Wire compatibility discipline:** HTTP API versioning. URL prefixes (`/v1/qso`, `/v2/qso`), Accept-version headers, backward-compatible field additions (never field removals) in the JSON shape, deprecation windows.

**Release-model translation:** the monorepo tag and the HTTP API version are two separate versions that can move at different rates. A source-level release (`v2.3.1` bugfix) might keep the HTTP API at `/v1`; a later source-level release (`v2.4.0`) might introduce `/v2` endpoints while still serving `/v1` for older clients; and so on.

**Milestone 1 simplification:** start with one API version (`/v1/`) and don't worry about breaking it. Introduce versioning discipline — deprecation windows, parallel `/v2/` endpoints — *when there's a reason to*, not prophylactically. For milestone 1, the daemon and the `curl`-based test suite and (eventually) the first Wails client all build from the same monorepo tag, so wire compatibility is trivially satisfied. Versioning becomes interesting in milestone 3+ if/when you want to run an old client against a new daemon.

**Rule of thumb to remember:** *the wire is where compatibility lives; the source is where convenience lives.* Share all the source and still have clean compatibility semantics by versioning the wire. Share zero source and still have a mess if the wire is undisciplined.

---

### 7. Gio UI toolkit replaces Wails; all apps stay in the root module

> **Superseded 2026-04-30 by ADR 0001 (browser SPA, Svelte 5 + Vite).** The Gio path was abandoned 9 days after this decision. The v2 client apps are now browser SPAs embedded in the daemon via `//go:embed`. `cmd/giospike/` is a parked spike; `cmd/logging/` (the Gio logging app skeleton) is parked, not deleted, until SPA feature parity is reached. The decision text below is preserved as the historical record. The current shape: `frontend/logging/` (Svelte 5 + Vite + Tailwind v4) is the active client; the daemon serves it at `GET /` when `Protocol=tcp && ServeSPA=true`. See ADR 0001 and `frontend-spa.md`.

**Decision:** The v2 client apps — `logging`, `logbook`, `config`, plus any future siblings — are **pure-Go Gio applications**, not Wails web-view applications. They live under `cmd/` in the root module, alongside `cmd/smd` and `cmd/catcli`, and are built with `go build ./cmd/<name>`. The `apps/` directory originally reserved for Wails apps is **not created** in v2; it has no role.

**Why:**

- Validated 2026-04-21 by the `cmd/giospike/` evening-scale live-rig spike (FTdx10 VFO/mode readout + one-line QSO entry wired through `internal/serial` + `internal/cat`). Streaming redraw via `w.Invalidate()` from a background goroutine, text input, and button/layout behaved well. Operator verdict: "we can build a clean UI from this and keep the whole thing with Go." See `project_sm_ui_toolkit` memory note.
- Gio is a pure-Go library. It has no JavaScript frontend, no Vite/npm toolchain, no code generation — none of the reasons that drove Wails apps to want their own `go.mod` under `go.work` (decision #3).
- By decisions #2 and #3's own rule — "module boundaries earn their keep via independent build tooling or dependency isolation" — a Gio app has neither need. It is structurally identical to `cmd/smd`: a pure-Go main package that imports from `internal/*`.
- One `go.mod` covers every binary. `go build ./...` continues to work end-to-end. No `go.work` migration is ever needed for this project.

**Concrete consequence:** the milestone-2 layout below (originally targeting `apps/logging/go.mod` under `go.work`) is replaced by additional `cmd/<name>/` directories. CI's `go build ./...` handles everything; the only operational wrinkle is that Gio has non-trivial Linux system dependencies (Vulkan headers, Wayland/xkbcommon devel packages) that the CI runner needs — when the first Gio app under `cmd/` lands, CI gets an `apt-get install` step and any build-tag gating on the throwaway `cmd/giospike/` is removed.

**Alternative considered:** keep Wails as originally planned. Rejected on the spike's outcome plus the operator's preference to avoid a JS/webview stack for a Go-centric single-user desktop tool. Gio's tradeoff (non-trivial Linux C build deps) is less painful than Wails' tradeoff (a second toolchain, bindings generation, an embedded browser runtime) for this project.

**Related:** `project_sm_ui_toolkit` memory. `docs/session-handoff.md` session 17 block. `cmd/giospike/main.go` as the working reference (deleted when the real logging app lands).

---

### 6. `internal/*` packages get their own module only in two rare cases

**Decision:** Do not split `internal/*` into sub-modules by default. A package moves out of the flat `internal/` tree only in two situations:

1. **It gets published externally.** For example, `internal/adif` matures into a general-purpose ADIF library that other Go ham radio projects want to depend on. At that point it leaves `internal/` entirely and becomes its own **repo** (e.g., `github.com/ColonelBlimp/go-adif`) — not just its own module in this monorepo. A separate-module-but-same-repo intermediate step is almost never the right shape; either a package is monorepo-private (the Go `internal/` idiom gives you visibility control already) or it's a published library (which wants its own repo for tagging, issue tracking, and release independence).

2. **It has exotic dependencies that shouldn't leak.** If a package pulls in CGO or a large framework whose `go.sum` entries you don't want in `smd`'s build, you have a real reason to isolate it. Even then, the cleaner solution is usually "make it its own `cmd/` binary" (isolated by process, not just by module) rather than "split the library" (isolated by module but still linked into the same binary).

**Default:** flat single `internal/`, split only when forced. YAGNI applies here exactly as it does to decisions #2 and #3.

---

## Target layout for milestone 1

```
station-manager/
├── cmd/
│   ├── smd/              # NEW — daemon binary (main entry point)
│   ├── importer/         # KEEP — ADIF bulk importer
│   ├── server/           # reserved slot (future SM-Online server)
│   └── tools/            # reserved slot (future dev tools)
├── internal/
│   ├── api/              # NEW — daemon HTTP handlers, thin over qsoservice
│   ├── qsoservice/       # NEW — daemon domain layer (what facade used to hold)
│   ├── smclient/         # NEW — Go HTTP client for daemon (milestone 2 consumer)
│   ├── types/            # KEEP — types.Qso follows ADIF
│   ├── adif/             # KEEP
│   ├── database/sqlite/  # KEEP (with simplified adapters/ from 2026-04-14)
│   ├── errors/           # KEEP — internal usage; HTTP serialization TBD
│   ├── iocdi/            # KEEP — home-grown DI
│   ├── enums/            # KEEP — per-concept subpackages
│   ├── config/           # KEEP
│   ├── logging/          # KEEP — zerolog abstraction
│   ├── lookup/hamnut/    # KEEP
│   ├── lookup/qrz/       # KEEP
│   ├── forwarding/qrz/   # KEEP (fan-out redesign deferred to milestone 2+)
│   ├── maidenhead/       # KEEP
│   ├── utils/            # KEEP
│   └── email/            # KEEP
├── docs/
│   ├── v1-analysis/      # KEEP — the v2 spec source
│   ├── v2-design/        # where v2 decisions are captured
│   │   └── structure.md  # this file
│   └── session-handoff.md
├── go.mod
└── go.sum
```

**New package names (decided 2026-04-15):** `cmd/smd` (unix daemon convention, cf. `sshd`, `systemd`), `internal/qsoservice` (domain service layer — what the v1 `facade` package used to hold for logging), `internal/api` (HTTP handler layer, thin wrappers over `qsoservice`). `internal/smclient` was originally planned as a Go HTTP client for the daemon (consumed by the Wails thin clients starting in milestone 2); **never created** — the milestone-2 client became a browser SPA per ADR 0001 and talks to the daemon directly from the browser, so no Go HTTP client is needed.

---

## Deliberately absent from milestone 1

Each item has a reason for not being in milestone 1. Listing them explicitly so that future sessions don't reintroduce them by accident or wonder why they aren't there.

- **`cmd/logging`, `cmd/logbook`, `cmd/config`** — the three v2 client apps (originally Gio per decision #7; **superseded 2026-04-30 by ADR 0001 — these are now browser SPAs at `frontend/logging/` etc., embedded in the daemon binary**). `cmd/logging/` exists as a parked Gio skeleton; `cmd/logbook/` and `cmd/config/` were never created as Gio apps. Milestone 2+. (The docs originally placed these under `apps/logging/` etc. because they were going to be Wails apps with their own `go.mod`; decision #7 collapsed them back into `cmd/`; ADR 0001 then moved them to `frontend/<name>/` Svelte SPAs.)

- **`internal/serial`, `internal/cat`, `internal/ptt`** — rig control. These stay on the v1 branch until a v2 consumer is being built. Rig control is a *client* concern per the narrow-daemon-scope invariant (see `docs/v1-analysis/invariants.md`). The daemon does not own the rig. Carrying these packages into `internal/` before their consumer exists would clutter the v2 tree with unused code. They come back in milestone 3+ as dependencies of the `cmd/sm-serial-bridge` binary, or of a future dedicated rig-control client.

- **`internal/database/` top-level** (server-side database layer), **`internal/database/postgres/`**, **`internal/adapters/`** — the server-side cluster. These travel together to a future `station-manager-online` server repo, not into v2 client. They exist in v1 on the `v1` branch; they will be relocated (not deleted) when the SM-Online server work becomes real.

- **`internal/listeners/` and `internal/listeners/handlers/wsjtx/`** — dead code. Never ran in a working configuration (see `docs/v1-analysis/bug-inventory.md` → "WSJT-X UDP listener is dead code"). V2's WSJT-X ingest plan is completely different: a separate `cmd/wsjtx-bridge` client that translates WSJT-X UDP → daemon HTTP, scheduled for milestone 3+. Nothing in the v1 `internal/listeners` framework carries forward.

- **`internal/audio/`** — status pending. Was part of the FT8 pipeline in v1; needs a reverse-dependency check after FT8 was removed in the v1.0.0 cleanup. If no non-FT8 consumer remains, don't carry forward. If something uses it (voice keyer? SSB playback? general WAV handling?), it comes back when *that* something is being built, not speculatively.

- **Multi-destination forwarder fan-out redesign.** The hardcoded QRZ forwarder comes over as-is for milestone 1 (one destination, works). The redesign (`ForwarderConfig` with enable/disable, action filter, per-destination credentials, fan-out at ingest) is a milestone-2-or-later task. See `docs/v1-analysis/bug-inventory.md` → "Hardcoded QRZ forwarder" and `docs/v1-analysis/design-decisions-log.md` → same.

---

## Target layout for milestone 2 (client apps)

> **Superseded 2026-04-30 by ADR 0001 (browser SPA).** The Gio-app layout below was the milestone-2 plan under decision #7. Per ADR 0001 the client apps are now Svelte 5 SPAs in `frontend/<name>/`, embedded in the daemon binary via `//go:embed`. The current shape is captured below; the prior Gio layout is preserved as the historical record.

**Current shape (as of 2026-05-02):**

```
station-manager/
├── go.mod                        # root module — still the only module
├── go.sum
├── cmd/
│   ├── smd/                      # daemon
│   ├── catcli/                   # diagnostic CLI for serial/CAT testing
│   ├── importer/                 # ADIF bulk importer
│   ├── logging/                  # parked Gio skeleton (decision #7 era);
│   │                             # not deleted until SPA reaches feature parity
│   ├── giospike/                 # throwaway Gio spike (decision #7 era);
│   │                             # not deleted until SPA reaches feature parity
│   ├── server/                   # reserved slot (future SM-Online server)
│   └── tools/                    # reserved slot (future dev tools)
├── frontend/
│   └── logging/                  # ACTIVE — Svelte 5 + Vite + Tailwind v4
│                                 # SPA, embedded into the daemon binary
├── internal/                     # ALL shared Go library code, flat tree
│   ├── api/                      # daemon HTTP handlers
│   ├── qsoservice/               # domain layer
│   ├── adif/                     # ADIF parser/writer
│   ├── cat/                      # CAT controller (parked with cmd/logging)
│   ├── config/                   # JSON config loader
│   ├── database/sqlite/          # storage
│   ├── enums/                    # band/mode/upload-status enums
│   ├── errors/                   # tagged-error pattern
│   ├── events/                   # SSE hub
│   ├── forwarding/               # multi-destination forwarders + worker
│   │   ├── qrz/
│   │   ├── stub/
│   │   └── worker/
│   ├── iocdi/                    # home-grown DI
│   ├── logging/                  # zerolog wrapper
│   ├── safego/                   # panic-recovering goroutine spawn
│   ├── serial/                   # serial driver (parked with cmd/logging)
│   ├── types/                    # types.Qso etc., ADIF-aligned
│   └── utils/                    # helpers
└── docs/
```

`internal/smclient/` from the original milestone-2 plan was a Go HTTP client for the daemon, designed to be consumed by Wails-era backends. It was never created. The SPA talks to the daemon directly via `fetch()` / `EventSource` from the browser; no Go HTTP client is needed.

**`internal/bridge/` is the only outstanding structural addition** per ADR 0013 — a daemon subsystem (rig SSE, rigctld TCP, AUTO-mode CAT, PTT arbitration). When it lands it joins the flat `internal/` tree alongside `internal/forwarding/`. `internal/serial/` and `internal/cat/` already exist (carry-forward from v1) and will be the bridge subsystem's dependencies.

---

### Original Gio milestone-2 layout (pre-ADR 0001, preserved as record)

```
station-manager/
├── go.mod
├── go.sum
├── cmd/
│   ├── smd/
│   ├── catcli/
│   ├── importer/
│   ├── logging/                  # Gio logging app
│   ├── logbook/                  # Gio logbook app
│   ├── config/                   # Gio config app
│   └── giospike/                 # throwaway spike
├── internal/
│   ├── api/
│   ├── qsoservice/
│   ├── smclient/                 # HTTP client, consumed by the Gio apps
│   ├── types/
│   ├── serial/
│   ├── cat/
│   └── ...everything from milestone 1...
└── docs/
```

Each Gio app imports from `internal/*` directly — no inter-module wiring, no replace directives, no workspace file. `go build ./...` covers every binary. `go build ./cmd/<name>` produces an individual one. The single tradeoff versus the original Wails plan is that CI now needs Gio's Linux system dependencies (Vulkan headers, Wayland/xkbcommon devel packages) installed before `go build ./...` can compile the real `cmd/logging` binary; a one-line `apt-get install` step in CI covers it when the first non-spike Gio binary lands.

---

## Migration from main's current state to milestone 1

> **Status: COMPLETED (audited 2026-05-02).** The restructure described below ran in the session-8 cluster. Current `main` IS the milestone-1 target layout: Wails `apps/` directory removed, `go.work` removed, single `go.mod` at the repo root, daemon (`cmd/smd`) shipped, new internal packages (`internal/api`, `internal/qsoservice`, `internal/events`, `internal/safego`) created, and `internal/forwarding/` reshaped to multi-destination. The migration plan below is preserved as the historical record of *what* the restructure did. Subsequent UI-toolkit pivots (Gio 2026-04-21, then SPA 2026-04-30 per ADR 0001) shifted the client-side architecture but did not undo the milestone-1 daemon restructure.

The current `main` branch (as of 2026-04-15, at commit `66e0af3`) still contains the v1 Wails apps, the v1 `go.work` with five modules, the server-side database cluster, and other code that doesn't belong in v2 milestone 1. A restructure commit reshapes main into the milestone-1 layout.

**To be deleted from main** (preserved on the `v1` branch, so nothing is lost):

- `apps/config/`, `apps/logbook/`, `apps/logging/` — entire directories, including their frontends, facades, tests, and `go.mod` files
- `go.work` — collapse to single `go.mod` at repo root
- `internal/database/*.go` (top-level server-side database files: `service.go`, `interface.go`, etc.)
- `internal/database/postgres/` — relocates to future server repo, not needed in v2 client
- `internal/adapters/` (and its `converters/` subtree) — relocates with server-side database cluster
- `internal/listeners/handlers/wsjtx/` — dead code
- `internal/listeners/` — verify no other consumers first, then delete
- `internal/audio/` — reverse-dependency check first; delete if no non-FT8 consumer remains
- The existing root `go.mod` is kept but pruned of any dependencies that were only needed by deleted packages

**To be created in the restructure commit:**

- `cmd/smd/main.go` — scaffolded entry point (can be a stub initially)
- `internal/api/` — empty package with `doc.go` explaining intent
- `internal/qsoservice/` — empty package with `doc.go` explaining intent
- `docs/v2-design/structure.md` — this file

**To be preserved in place (no changes):**

- All of `internal/types`, `internal/adif`, `internal/database/sqlite/`, `internal/errors`, `internal/iocdi`, `internal/enums`, `internal/config`, `internal/logging`, `internal/lookup/*`, `internal/forwarding/qrz`, `internal/maidenhead`, `internal/utils`, `internal/email`
- `cmd/importer/` — carried forward as-is
- `cmd/server/`, `cmd/tools/` — empty reserved slots stay

**Commit shape:** one restructure commit is cleaner in git history than phased commits for a clean-slate rewrite — "reshape main into v2 milestone 1 layout." The `v1` branch is the safety net; anything being deleted from main is still recoverable from there. Phased commits would leave main in intermediate half-built states that are harder to reason about.

---

## Open items deferred to later v2-design documents

These are known design questions that don't belong in `structure.md` and will be settled in their own documents as construction proceeds:

- **HTTP API shape** — endpoint enumeration, URL structure, JSON envelope format, SSE event schema, error response shape, how `internal/errors.Op` serializes over the wire. → `docs/v2-design/api.md`.
- **Milestone plan** — concrete definition of milestone 1 "done," milestone 2 scope, milestone 3 scope. → `docs/v2-design/milestones.md`.
- **ORM / query generator choice** — sqlboiler (status quo, v1 uses it) vs. Bob vs. sqlc vs. hand-rolled. The transformation layer between DB rows and `types.Qso` exists regardless; the choice is ergonomics-level. → `docs/v2-design/db-layer.md` when v2 DB design starts.
- **Forwarder configuration model redesign** — the multi-destination fan-out shape. → `docs/v2-design/forwarding.md` when it becomes a milestone 2+ concern.
- **`internal/errors` HTTP serialization** — how the v1 `errors.Op` pattern crosses the HTTP boundary. → part of `docs/v2-design/api.md`.
- **Multi-rig as a first-class assumption** — where in the data model does a rig identifier live, does the logbook schema need a rig table, how does the serial/CAT bridge expose multi-rig state. Bridge-side shape is now captured in `docs/v2-design/bridge.md` (multi-rig is first-class in the bridge from day one); the data-model side — whether `types.Qso` carries a rig identifier, logbook schema impact — is still open and will be addressed when rig control construction starts.
- **Whether `cmd/server` gets populated as the SM-Online server in this repo or moves to a separate one** — undecided; depends on how the server work shapes up.

---

## Related documents

- `docs/v1-analysis/invariants.md` — load-bearing rules (ADIF alignment, narrow daemon scope, enrichment never blocks logging, one-fails-all-fail, etc.). These constrain v2 design.
- `docs/v1-analysis/design-decisions-log.md` — v1 decisions with keep/change/delete verdicts, including the "v2 rewrite vs. refactor" execution-path entry.
- `docs/v1-analysis/lessons-for-v2.md` — patterns to apply and avoid. Decisions in this file are informed by the lessons there; any apparent conflict should be reconciled explicitly.
- `docs/v1-analysis/bug-inventory.md` — the "do not recreate these" list for v2.
- `docs/session-handoff.md` — rolling session state.
- Memory: `project_sm_restructure`, `project_sm_v2_analysis`, `project_sm_design_invariants`.
