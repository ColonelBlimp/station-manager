# Station Manager — Session Handoff

**Purpose:** rolling handoff document across Claude sessions. Captures what was
done in the previous session, where the repo currently is, and **what the next
session should pick up**. Read this first when starting a session — it exists
precisely so we don't re-derive state or redo finished work.

**How to use this document:**

- **At session start:** read top-to-bottom. The "Current state" section tells
  you where the repo is. The "Next steps" section tells you what to do. If the
  next session's goals have already been set, work from them.
- **At session end:** the assistant updates this document before stopping.
  Move anything in "Next steps" that was completed into "What happened this
  session" with a date. Leave anything unfinished in "Next steps" and add new
  items discovered during the session.
- **Rolling window:** keep roughly the last 2–3 sessions of history in "What
  happened." Older entries can be summarized or elided — the long-form record
  lives in the git history, the v1-analysis docs, and the memory files.
- **Durable facts go in memory files,** not here. This document is for
  transitory session-to-session state. If something is stable across all
  future sessions (a project invariant, a user preference, a design rule),
  capture it in a memory file under `~/.claude/projects/.../memory/`.

---

## Current state (as of 2026-04-30, session 22 — UI toolkit resolved (Svelte SPA), CAT codec baseline captured, frontend scaffold landed and verified end-to-end)

### Session 22 work (architecture conversation captured into v2-design docs; SPA scaffold landed with both daemon-embed and Vite-dev paths verified live via Chrome DevTools MCP)

**Three open questions surfaced; UI toolkit, CAT-perf, and CSS-approach decisions all resolved by end of session. Frontend scaffold sketched, implemented, and verified end-to-end.**

**What landed:**

- **`cmd/logging` UI step:** Mode picker added. New file `cmd/logging/mode_cycle.go` — a click-to-advance cycle button over `[]string{"USB", "LSB", "CW"}`. Wired into `qsoSectionTop` as a 4th cell next to Callsign/RST Sent/RST Rcvd, using a `labeledMode(theme)` helper that mirrors `labeledInput`'s vertical label-over-content shape. Cycle button rebuilt to match `borderedInput` exactly (1 dp `gray500` border, 4 dp corner radius, 6 dp uniform inset, body1 text) so heights align across the row. Pinned to fixed 60 dp width via `gtx.Constraints.Min.X = gtx.Constraints.Max.X = gtx.Dp(modeFieldWidth)` so frame width doesn't reflow as the label changes.

- **Tailwind v4 palettes added to `cmd/logging/colours.go`:** `red50–red950`, `gray50–gray950`, `green50–green950` alongside the existing indigo palette. Each block has a header comment listing canonical `oklch(L C H)` tuples for traceability. `borderedInput` (in `main.go`) now uses `gray500` for its border (`inputBorderColor` const removed).

- **UI toolkit decision resolved 2026-04-30: browser SPA, Svelte 5 + Vite + plain TS, embedded into the daemon via `//go:embed`.** Operator concern that prompted the reconsideration: Gio learning curve fights the "small steps / no cognitive debt" working-mode preference. Three options analysed (stick with Gio / fall back to Wails v2 / browser SPA hosted by daemon). Operator confirmed Svelte 5 over Vue/React on three grounds: compiled-reactivity DOM updates suit a long-running tab consuming a 10–20 Hz rig SSE stream; bundle size matters when the SPA is `go:embed`-ed; existing fluency removes cognitive overhead. Performance is not the deciding factor (see topology and cat-performance docs). [`docs/v2-design/ui-toolkit.md`](v2-design/ui-toolkit.md) was updated with the resolution; the SPA scaffold itself is captured in [`docs/v2-design/frontend-spa.md`](v2-design/frontend-spa.md).

- **Frontend SPA scaffold designed AND landed AND verified.** `frontend/logging/` (Svelte 5 + Vite 6 + plain TS + Tailwind CSS v4, *not* SvelteKit). Daemon-side: new `frontend/embed.go` package owns `//go:embed all:logging/dist`, `internal/api/spa.go` is the `spaHandler` (with index.html-fallback for client-side routing), `internal/api/server.go` registers `GET /` conditionally on `cfg.Server.Protocol == "tcp" && *cfg.Server.ServeSPA` so Unix-socket headless deployments stay supported. **Implementation simplification vs. the original sketch:** `LoggingFS()` returns just `fs.FS` (no error) — `fs.Sub` on a hard-coded valid embed path is infallible at runtime; the panic-on-error stays as a programmer-error guard but is unreachable. This kept `api.New()`'s signature stable, so no test changes were needed. **Tailwind CSS v4 added during scaffolding** — the original "plain scoped component CSS to start" stance was reversed in favour of v4's CSS-first config (`@tailwindcss/vite` plugin, single `@import "tailwindcss";`). Verified via Chrome DevTools MCP that all utilities apply correctly. **CI builds the SPA before `go build`; `dist/` is NOT committed** (except a placeholder `dist/index.html` so first-time builds compile before anyone runs `npm install`). Full file inventory + verification tables in [`docs/v2-design/frontend-spa.md`](v2-design/frontend-spa.md) §"Scaffold landed and verified".

- **End-to-end smoke test landed via Chrome DevTools MCP.** Two paths verified:
  - **Path A (daemon-embedded placeholder):** `task build && ./build/bin/smd` → `GET /v1/healthz` 200 → `GET /` returns embedded `dist/index.html` → page title + h1 + body text confirmed via accessibility-tree snapshot in Chromium. Plus `internal/api/spa_test.go` covers the same handler at unit level (root + four fallback paths).
  - **Path B (Vite dev server with the real Svelte 5 + Tailwind app):** `task frontend:install` + `task frontend:dev` → 17 network requests all 200 (one cosmetic 404 for `favicon.ico`) → Svelte 5 `$state` rune rendered → seven Tailwind v4 utility classes verified via computed-style readback (oklch colours, viewport min-height, font stack, font weights). The whole frontend toolchain works end-to-end: Vite + `@sveltejs/vite-plugin-svelte` + `@tailwindcss/vite` + Svelte 5 + the `$state` rune + HMR.

- **Topology refined:** the bridge is a **peer** of the daemon, not a subordinate. Load-bearing distinction = host-bound (rig wires) vs network-shaped (storage + HTTP). Bridge owns CAT/serial/PTT/audio (host-bound by physics); daemon owns log + forwarding + SPA hosting (network-shaped, can live anywhere). **The two never talk to each other — they share clients.** Client subscribes to bridge for live rig state, submits QSOs to daemon with freq/mode in the payload. Earlier wording about "daemon brokers events from the bridge" was wrong if "brokers" implied bridge → daemon → client; the correct model is bridge → client and daemon → client as parallel channels. Enables four deployment topologies (all-on-one, server+shack, remote operating, multi-rig) without code changes. Captured in [`docs/v2-design/topology.md`](v2-design/topology.md).

- **CAT codec perf analysed and baselined.** Code-read of `internal/cat/codec.go` and `rigdb.go`. Real hot-spots identified at file:line — `Status{}` fresh-allocated per `Decode` call (`codec.go:55`) is the biggest cost; `lookupState` linear scan with `bytes.EqualFold` (`codec.go:107`) is second. The "map and rehashing" the operator was recalling is **not** `rigDB` (that's cold-path init-only) — it was the per-frame `Status` map allocation. Ranked optimisations Tier 1/2/3 documented. **Baseline benchmarks added at `internal/cat/codec_bench_test.go`**, captured 2026-04-30 on Intel i3-10100F: `BenchmarkDecode` 197.5 ns/op / 352 B/op / 3 allocs/op, `BenchmarkLookupState` 60.05 ns/op / 16 B/op / 1 alloc/op. The codec is an order of magnitude faster than the doc's pre-bench estimate — at 100 ms poll cadence it's 0.0002% of the budget. **Decision (operator-confirmed): don't refactor.** Code stays as-is; benchmarks remain as a regression guard. Real latency dominators are bridge-side (poll interval, USB-serial latency timer, TCP_NODELAY, async/auto-tx mode). The Tier-1 refactor (slice-of-tags or caller-owned map) is only worth doing if multi-client SSE fan-out later shows GC pressure correlating with codec allocations in a `runtime/trace`. Captured in [`docs/v2-design/cat-performance.md`](v2-design/cat-performance.md).

**Files added:**

- `docs/v2-design/ui-toolkit.md` — Gio vs Wails vs browser-SPA analysis (decision resolved end of session)
- `docs/v2-design/topology.md` — bridge/daemon/client peer model, deployment scenarios, CORS/auth/discovery practicalities
- `docs/v2-design/cat-performance.md` — codec hot-spot analysis cited to file:line, ranked optimisations, baseline benchmark numbers
- `docs/v2-design/frontend-spa.md` — SPA scaffold, embed wiring, build pipeline, CI stance, "Scaffold landed and verified" verification tables
- `cmd/logging/mode_cycle.go` — Mode cycle-button widget
- `internal/cat/codec_bench_test.go` — baseline `BenchmarkDecode` + `BenchmarkLookupState`
- `frontend/embed.go` — Go embed package owning the SPA `dist/` filesystem
- `frontend/logging/` — full scaffold: `package.json`, `vite.config.ts`, `tsconfig.json`, `svelte.config.js`, `index.html`, `src/main.ts`, `src/app.svelte`, `src/styles/app.css` (Tailwind import), `dist/index.html` (placeholder), `package-lock.json`
- `internal/api/spa.go` — `spaHandler` with index.html-fallback
- `internal/api/spa_test.go` — covers root + four fallback paths

**Files modified:**

- `internal/config/config.go` — `cfg.Server.ServeSPA *bool` with TCP-default-true logic
- `internal/api/server.go` — imports `frontend`; conditionally registers `GET /` SPA route
- `Taskfile.yml` — `frontend:install` (auto-detects lockfile), `frontend:dev`, `frontend:build`, `build:smd`
- `.gitignore` — ignores `frontend/logging/{node_modules,dist/*}` except `dist/index.html`
- `build/config.json` — switched to TCP `127.0.0.1:8080` for the smoke test (daemon configurable per deployment)

### Next session

- **Land the first real route.** Pick between `svelte-spa-router` (~3 KB dep) and a hand-rolled hash router (~50 lines). Add `/log` as the first route stub. The Tailwind v4 toolchain is verified working; use it.
- **Bridge HTTP/SSE surface** — concrete API design to drop into `bridge.md`. Endpoints: `GET /v1/rig` (snapshot), `GET /v1/rig/events` (SSE stream), `POST /v1/rig/freq`, `POST /v1/rig/mode`, plus the existing rigctld TCP frontend unchanged. CORS (`Access-Control-Allow-Origin: *` for single-user, scoped to daemon origin for stricter setups). Reconnection semantics on the SSE side.
- **`bridge.svelte.ts` rig-state module** — once the bridge SSE shape is settled. Module-level `$state` holds `{freq, mode, vfo}`; any component that reads it re-renders on EventSource updates. ~30 lines.
- **Client-side QSO payload shape** — agree what the client sends to `POST /v1/qso`. Since the client is the only thing that knows current freq/mode (subscribed to bridge), it must include those fields in the QSO submission. Daemon takes them as authoritative.
- **CI workflow update** — add Node ≥22 setup + `task frontend:install && task frontend:build` before the Go build/test steps. The `//go:embed` enforces ordering — missing `dist/` is a compile error, so a botched CI step surfaces immediately. (Lockfile is now committed, so CI can use `npm ci` for deterministic resolution.)
- **Trivial cleanup:** drop `frontend/logging/public/favicon.ico` (any file) to silence the cosmetic 404 the Vite smoke test surfaced. Or accept it.
- **Chrome DevTools MCP env note** — see memory `reference_chrome_devtools_mcp_setup`. The plugin's launch config is read from `~/.claude/plugins/cache/chrome-devtools-plugins/chrome-devtools-mcp/<version>/.claude-plugin/plugin.json`; if you upgrade the plugin and Chromium-not-Chrome breaks browser automation again, re-add `--executablePath=/usr/bin/chromium-browser --isolated` to that file's `mcpServers.args` array.
- **`cmd/logging/` (Gio app) is left alone for now.** Per `ui-toolkit.md` and `frontend-spa.md`, it stays in place until the SPA reaches feature parity, then gets abandoned cleanly. Don't pre-emptively delete.

### Session 21 work (UI frame built up in single-concept steps; entry-row helpers parked for later)

**Working mode (operator-set):** *"work through building the logging app UI with you in very small steps — I don't want cognitive debt."* Each step landed one new concept with a short explanation: Constraints (Min/Max), Flex axes, `Rigid` vs `Flexed`, single-edge rules via `clip+paint` (since `widget.Border` is all-four-or-nothing), border composition (panels compose like HTML boxes — adjacent borders sit side-by-side, no `border-collapse`).

### Session 21 work (UI frame built up in single-concept steps; entry-row helpers parked for later)

**Working mode (operator-set):** *"work through building the logging app UI with you in very small steps — I don't want cognitive debt."* Each step landed one new concept with a short explanation: Constraints (Min/Max), Flex axes, `Rigid` vs `Flexed`, single-edge rules via `clip+paint` (since `widget.Border` is all-four-or-nothing), border composition (panels compose like HTML boxes — adjacent borders sit side-by-side, no `border-collapse`).

**What landed:**

- **`run()` reset to a bare event loop, helpers kept in place.** The session-20 three-column QSO entry row was *not deleted* — `layoutUI`, `labeledInput`, `borderedInput`, the callsign/RST editors, focus logic — they're parked as package-level helpers and constants in `main.go`, just no longer called from `run()`. Intent: reintroduce them inside the green left panel once the outer frame is finalised. Go is fine with unused package-level funcs; the imports they pull in (`material`, `widget`) are still used by the helpers themselves.

- **`task run:logging` added to `Taskfile.yml`** — builds *only* `cmd/logging` and runs the binary (skips daemon + full-module build). Picks up `SM_WORKING_DIR` from `.env` like the existing `run` task. Faster cycle when iterating on UI.

- **Top frame assembled, three nested layers, debug-coloured borders so each layer is identifiable on screen:**
  - Outer: `layout.Flex{Axis: Vertical}` in `run()`'s `FrameEvent`, with `statusRow()` and `mainRow()` as `Rigid` children.
  - **`statusRow()`** — full-width, fixed height (currently `unit.Dp(40)`), single 1dp red rule along the *bottom edge only*. `widget.Border` paints all four sides, so for a single-edge rule we drop `widget.Border` and use `clip.Rect(...).Push(ops)` + `paint.ColorOp` + `paint.PaintOp` to fill a 1px-tall image rectangle along the bottom. Pattern: clip-as-shape — the clip's geometry *is* the painted shape; `PaintOp{}` paints the entire active clip.
  - **`mainRow()`** — full-width, fixed height (currently `unit.Dp(400)`), framed by a 1dp blue `widget.Border` (all four sides). Inner content is a `layout.Flex{Axis: Horizontal}` split 2/3 + 1/3 via `Flexed(2, …)` / `Flexed(1, …)`. Weights are relative numbers, not percentages.
  - **Inner panels** — left panel has a 1dp green border, right panel a 1dp yellow border, both via `borderedPanel(c color.NRGBA) layout.Widget`. Wraps `fillPanel` (a primitive that pins `Min = Max` and returns those dims) in a `widget.Border`. Same higher-order shape as `labeledInput` / `borderedInput` — function takes config, returns `layout.Widget`. The blue outer border + adjacent green/yellow borders produce visible 2px seams; per session-21 confirmation, that's the expected "borders compose, they don't merge" behaviour (CSS analogue: no `border-collapse`).

- **Debug colours split into `cmd/logging/colours.go`.** Centralised so `main.go` doesn't carry `image/color` purely for var declarations. Currently holds: `inputBorderColor` (`#101828` — production input outline), `statusRowBorderColor` (red), `mainRowBorderColor` (blue), `leftPanelBorderColor` (green), `rightPanelBorderColor` (yellow). The four debug colours are flagged as "temporary debug" in their doc comments — they come out when each layer gets its real fill / contents.

- **New imports in `main.go`** to support the frame work: `image`, `image/color`, `gioui.org/op/clip`, `gioui.org/op/paint`.

**Helper inventory in `cmd/logging/main.go` after session 21:**

| Helper | Status | Returns | Purpose |
|---|---|---|---|
| `statusRow()` | live | `layout.Widget` | top status strip with bottom-edge rule |
| `mainRow()` | live | `layout.Widget` | 2/3 + 1/3 row beneath status |
| `fillPanel` | live | `layout.Dimensions` | bare "claim my assigned space" primitive |
| `borderedPanel(c)` | live | `layout.Widget` | parameterised colour-bordered fill panel |
| `layoutUI(...)` | parked | `layout.Dimensions` | three-column QSO entry row from session 20 |
| `labeledInput(...)` | parked | `layout.Widget` | label-above-input vertical pair |
| `borderedInput(...)` | parked | `layout.Widget` | fixed-width outlined editor |
| `loadConfig(path)` | live | `(config.Config, error)` | flag → env → cwd → defaults config resolution |

### Next session (session 21 — *superseded by session 22's Next session block above; preserved for the sub-items still relevant if the Gio path is kept*)

- **Replace debug border colours with real fills/contents.** The four debug colours (red status-row rule, blue main-row frame, green/yellow inner panels) come out as each layer gets its real treatment. Status row is the natural first one — see below.
- **Status row contents** (red debug rule comes out): `Logging Mode: [Normal ▾] | Logbook: <name> | Rig: <model> | Session Time: hh:mm:ss`. Session time = monotonic counter started at window-open. Inset the row contents 8dp horizontally so text doesn't sit flush against the window edge.
- **Reintroduce the three-column QSO entry row inside the green left panel** of `mainRow`. The helpers (`layoutUI`, `labeledInput`, `borderedInput`) and editor variables are already parked in `main.go` — the work is wiring them through the `mainRow()` left panel instead of the current `borderedPanel(leftPanelBorderColor)`. Restore the post-layout `key.FocusCmd{Tag: &callsign}` first-frame focus call.
- **Right panel content** — 1/3-width pane: session list (per `project_sm_session_scope` memory: client-side, no daemon endpoints). For now a placeholder header + empty list view is enough.
- **Register `smclient` as the first real iocdi service with a dependency.** Precondition for a working Log Contact button (POST `/v1/qso` to the daemon). Stub `hamnut`, `qrz`, `enrichment`, `email`, `rigloop` as iocdi-shaped placeholders with green `Initialize()` and TODO bodies so the service graph is fully visible from `cmd/logging/container.go`.
- **Remaining v1-layout rows** (after the entry row is back inside the left panel): Row 2 — Name / Qth / Comment (textarea via `widget.Editor` with `SingleLine = false`); Row 3 — Date picker, Time On UTC, Time Off UTC, Log Contact (filled button → smclient.Submit), Clear (outline button → reset editors).
- **Mode dropdown** (Row 1 completion): Gio has no stock dropdown — choose between a cycle-button (click cycles SSB/CW/FT8/…) and a small custom menu. Worth a small spike.
- **Frequency readout + VFO-A/B** (Row 1 completion): blocked on `rigloop`. Will land once that service exists.
- **Drop `//go:build gio` from `cmd/giospike/main.go`** (or delete `cmd/giospike/` entirely — spike's job is done; see memory `project_sm_ui_toolkit`). CI has the deps.
- **`internal/rigconfig` composition function** — still unblocked. Expected shape: `rigconfig.Compose(types.RigConfig, cat.RigDefinition) (serial.Config, error)`. Absorbs the ~15 LOC inline helper duplicated in `cmd/catcli/` and `cmd/giospike/`.
- **Open item from session 16 still outstanding:** mystery `FD` prefix on FTdx10 in AI mode. Investigate opportunistically.

### Session 20 work (toolchain modernisation + cmd/logging wired into iocdi + three-column QSO entry row)

**What landed:**

- **Go 1.26.2 bump.** `go.mod` updated from `go 1.25.0` → `go 1.26.2` (operator installed the toolchain locally; CI picks it up automatically via `actions/setup-go` with `go-version-file: go.mod`). Clean `go mod tidy`; full build + `go test -race ./...` green post-bump.

- **`go vet ./...`** — zero findings after the toolchain bump. Already part of CI.

- **Modernize pass via `gopls/modernize`.** Dry-run surfaced ~130 findings; applied the safe bucket across 29 files in one pass, reverted two judgment items for review:
  - **Safe (applied):** `interface{}` → `any`, `for i := 0; i < n; i++` → `for i := range n`, `if x > y { x = y }` → `min`/`max`, `[]byte(fmt.Sprintf(...))` → `fmt.Appendf`, `m[k]=v` loop → `maps.Copy`, `b.N` → `b.Loop()`, `go func() + wg.Wait()` → `wg.Go(fn)` (Go 1.25 `WaitGroup.Go`), `context.WithCancel` in tests → `t.Context()`, `slices.Contains`, `reflect.TypeOf((*T)(nil)).Elem()` → `reflect.TypeFor[T]()` (hand-written code only).
  - **Skipped (generated code):** all hits under `internal/database/sqlite/models/*.go` — sqlboiler output, per CLAUDE.md rule "sqlboiler-generated models are not hand-edited."
  - **Judgment item #1 — applied:** `internal/types/rig.go:39` `json:"overrides,omitempty"` → `json:"overrides,omitzero"`. The original tag was a no-op (omitempty has no effect on nested struct fields); `omitzero` (Go 1.24+) actually omits when the struct is zero-valued, matching the doc-comment's "missing = inherit" promise.
  - **Judgment item #2 — skipped:** `internal/iocdi/internal.go:29` offered `reflect.Type.Fields()` iteration (new in Go 1.26). Low value for the cost — the rewrite saves two lines but introduces a `field := field` shadow in DI-container hot-path reflection code; kept the explicit index loop.

- **zerolog deprecation fixed.** `internal/logging/event.go:405` — `zerolog.Dict()` is deprecated because it doesn't preserve the parent event's stack, hooks, or context. Swapped to `e.event.CreateDict()`. `zerolog` import remains for level constants and type references elsewhere in the file.

- **`Taskfile.yml` build target now emits the logging-app binary.** Added `go build -o build/bin/logging ./cmd/logging` alongside the existing `smd` line, so `task build` produces both `build/bin/smd` and `build/bin/logging`.

- **CI Gio Linux deps finalised.** Two-pass fix: initial apt list missed `libx11-xcb-dev` and `libxfixes-dev` (Gio's pkg-config requires `x11-xcb` and `xfixes`, both shipped as separate Debian/Ubuntu packages from `libx11-dev`). Final list: `libwayland-dev libxkbcommon-dev libxkbcommon-x11-dev libvulkan-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev libx11-dev libx11-xcb-dev libxcb1-dev libxcursor-dev libxfixes-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev`.

- **`cmd/logging` wired into iocdi.** `cmd/logging/container.go` (new) exposes `buildContainer(cfg config.Config) (*iocdi.Container, error)`, mirroring `cmd/smd`'s pattern: registers the `config` service (instance) and the `logging` service (type via `reflect.TypeFor[*logging.Service]()`), sets the `LiteralProvider` so `logging.Service.WorkingDir` (`di.inject:"workingdir"`) resolves from `cfgSvc.WorkingDir()`, then calls `container.Build()` to fire `Initialize()` in dependency order. Uses the `errors.Op = "logging.app.main.buildContainer"` convention. A leading comment lists the six services still-to-register so the service graph is visible in source even before implementations exist: `smclient`, `hamnut`, `qrz`, `enrichment`, `email`, `rigloop`.

  `cmd/logging/main.go` now:
  - Parses `-config` flag, loads config via `loadConfig` (same resolution order as `cmd/smd`: explicit path → `$SM_WORKING_DIR/config.json` → `./config.json` → `config.DefaultConfig(cwd)`).
  - Calls `buildContainer(cfg)`, resolves `*logging.Service` out of the container.
  - Spawns the Gio goroutine, passing the logger into `run()`. Logger emits `"logging app started"` / `"logging app stopped"` bookends; `loggerSvc.Close()` runs before `os.Exit` in the shutdown path.
  - `DestroyEvent` returns `nil` cleanly when `e.Err == nil` (previously wrapped nil as an error).

- **First QSO entry row rendered** (three fixed-width labeled inputs, left-aligned, horizontal flex, top of the 16dp-inset window):
  - **Callsign** — `unit.Dp(130)` (~10 proportional chars), hint `"G0ABC"`, receives initial focus on first `FrameEvent` via `gtx.Execute(key.FocusCmd{Tag: &callsign})` *after* `layoutUI` (Gio's focus command resolves against registered event.Ops, so it must run post-layout).
  - **RST Sent** — `unit.Dp(60)` (~3 digits), default value `"59"` set via `SetText("59")`.
  - **RST Rcvd** — same width, default `"59"`.
  - All three are `widget.Editor` with `SingleLine = true, Submit = true`. All share a `widget.Border` frame (1dp outline in `#101828`, 4dp corner radius, 6dp inner inset). Input border colour is a package-level `color.NRGBA` constant — named, not parameterised, per session-20 decision below.
  - Labels use `material.Body2` (≈12sp) rather than `Body1` (≈14sp) — slight reduction kept tight vertical rhythm as the row grew to three columns.
  - Helpers: `labeledInput(th, label, ed, hint, width)` returns a vertical flex (label stacked above input), `borderedInput(th, ed, hint, width)` returns the fixed-width outlined editor. Both return `layout.Widget`, matching Gio's `material.*` idiom. Kept in `main.go` — splitting to `widgets.go` is teed up for when a third widget type lands (not a third instance of the same type).

- **v1 layout reference logged** (operator shared a v1 logging-window screenshot). Alignment notes for v2:
  - **Kept:** status row (`Logging Mode: [Normal ▾] | Logbook: <name> | Rig: <model> | Session Time: hh:mm:ss`), three-row entry block (Row 1: Callsign / RST Sent / RST Rcvd / Mode / VFO-A/B + freq + band; Row 2: Name / Qth / Comment; Row 3: Date / Time On UTC / Time Off UTC / Log Contact / Clear), bottom sub-tab strip (`Worked / Details / My Station / Session`) over a QSO table.
  - **Dropped:** the v1 top-level tab strip (`Logging` / `Control` as siblings). In v2, `cmd/logging` and a future `cmd/control` (CAT client) are separate binaries — no in-app nav between them.

- **Widget-abstraction discussion closed** (operator asked whether to split widgets into reusable blocks). Outcome: the current function-returning-`layout.Widget` shape is the Gio idiom; don't promote to a framework prematurely. Named constants over parameters for colour/inset/size. Extract to a `cmd/logging/widgets.go` file once a third *kind* of widget lands (dropdown, date picker, etc.), not on the third instance of the same kind. Promote to an `internal/ui` package only when a second app (`cmd/logbook`, `cmd/config`) needs the same widget — that's when the API has two uses to fit, rather than speculation.

*(Session 20's "Next session" goals are superseded by session 21's "Next session" block above. Service-registration, mode dropdown, and rig-loop-blocked items were rolled forward; layout-frame items have been taken.)*

### Session 19 work (cmd/logging main window + CI Linux deps for Gio)

**What landed:**

- **`cmd/logging/main.go` — empty 1024×751 Gio main window.** Fixed-size window constants (`windowWidth = unit.Dp(1024)`, `windowHeight = unit.Dp(751)`), title "Station Manager — Logging". Standard Gio event loop: `DestroyEvent` exits cleanly (wrapped through `errors.Op = "logging.app.main.run"` so the shutdown path uses the project error convention), `FrameEvent` renders an empty frame. No widgets yet — this is the shell on which the iocdi-wired service graph and the first QSO-entry row will be built.
  - Import note: Gio's `op` package is aliased as `giop` so it doesn't collide with the `errors.Op` constant used inside `run()`.

- **CI Linux deps for Gio** (`.github/workflows/ci.yml`) — `apt-get install` step added before `go vet`. Installs: `libwayland-dev`, `libxkbcommon-dev`, `libxkbcommon-x11-dev`, `libvulkan-dev`, `libgles2-mesa-dev`, `libegl1-mesa-dev`, `libffi-dev`, `libx11-dev`, `libx11-xcb-dev`, `libxcb1-dev`, `libxcursor-dev`, `libxfixes-dev`, `libxrandr-dev`, `libxinerama-dev`, `libxi-dev`, `libxxf86vm-dev`. Two iterations — the first pass missed `libx11-xcb-dev` and `libxfixes-dev`, which Gio's pkg-config requires via `x11-xcb` and `xfixes` (both ship as separate Debian/Ubuntu packages from `libx11-dev`).

**Follow-up teed up but not taken this session:**

- `//go:build gio` tag on `cmd/giospike/main.go` can now be removed — CI has the deps. Per the session-18 plan this change is meant to land alongside the CI update; keeping them separate this session because the user asked only for the scaffold + CI fix. Remove when picking up next session.
- `cmd/giospike/` can also be deleted entirely per memory `project_sm_ui_toolkit` (spike's job is done). The operator's call — preserved for now as a working reference.

### Outcome (superseded by session 20)

Session 20 picked up from here — see the "Next session" block under the session-20 heading above. Container wiring (config + logging + LiteralProvider) and the first QSO entry row (Callsign / RST Sent / RST Rcvd) landed; Go 1.26 bump and modernize pass came along for the ride.

### Session 18 work (daemon accidental-self-DoS floor + structure.md amendment + logging-app DI decision)

**What landed:**

- **Daemon hardening floor** (driven by the scenario "a user's cron job floods the submit endpoint and knocks the daemon over with 500s"):
  - `internal/config/config.go` — four new `ServerConfig` fields with defaults: `MaxConcurrentRequests=128`, `MaxEventSubscribers=16`, `SubmitRatePerSec=20`, `SubmitRateBurst=40`. Documented in-line with the threat model (accidental self-DoS, not malicious).
  - `internal/api/limits.go` (new, ~110 LOC) — `loadLimiter` with a buffered-channel semaphore (concurrent cap), a mutex-guarded subscriber counter (SSE cap), and a lazy-refill token bucket (submit cap). No background goroutines.
  - `internal/api/middleware.go` — three new methods: `limitConcurrent` (wraps full mux, exempts `/v1/events`, returns 503 `server_busy`), `limitEventSubscribers` (wraps `/v1/events`, 503), `limitSubmitRate` (wraps `POST /v1/qso`, 429 `rate_limited`).
  - `internal/api/server.go` — routes for `POST /v1/qso` and `GET /v1/events` now use `mux.Handle(...)` with per-route wrappers; outer chain is `limitConcurrent(recoverPanic(mux))`.
  - `internal/api/limits_test.go` (new) — 5 tests; edge case caught during implementation: `allowSubmit` must advance `submitLastFill` unconditionally (even on negative elapsed) or subsequent calls see negative elapsed forever. Full suite green.
  - `docs/v2-design/api.md` §6 rewritten to recognize accidental self-DoS as a milestone-1 concern (not just a TCP-exposure concern) and to record the minimal-floor as "implemented" with trigger conditions for the fuller hardening items (TCP binding, non-owner clients, multi-client workload).

- **CI fix:** `cmd/giospike/main.go` now has `//go:build gio`. CI's `go build ./...` skips it (no Gio system deps installed on the runner); local build uses `go build -tags gio ./cmd/giospike/`. When `cmd/logging` (the real Gio app) lands, CI gets a one-line `apt-get install` for Vulkan/Wayland/xkbcommon dev packages and the build-tag gate is removed.

- **`docs/v2-design/structure.md` amended** to reflect the Gio pivot:
  - Decisions #2 and #3 each carry a `> Superseded 2026-04-21 by decision #7` banner (historical rationale preserved — the rule "module boundaries earn their keep via independent build tooling or dependency isolation" still stands; it just no longer applies because we have no Wails apps).
  - New decision #7: "Gio UI toolkit replaces Wails; all apps stay in the root module" — records spike validation, structural consequence (no `go.work`, no `apps/`), CI wrinkle (Linux C build deps).
  - "Deliberately absent from milestone 1" — `apps/logging/…` bullet rewritten as `cmd/logging/…`.
  - "Target layout for milestone 2" — replaced `apps/*/go.mod`-and-`go.work` diagram with single-`go.mod`-extra-`cmd/` diagram.

- **Logging app scaffold started.** Operator has begun work on `cmd/logging`. Not yet reviewed in this session — the user started it in parallel.

**Design decision taken (iocdi in cmd/logging):**

Initial lean was *don't use iocdi* — argued that a Gio app with "config + logging + reader loop" doesn't need a framework. The user corrected the premise by sharing the real service inventory for the logging app: callsign-online lookup (QRZ), prefix/country lookup (Hamnut), enrichment orchestrator, email-out-to-QSL-manager, plus config/logging/smclient/rigloop. That's ~8–10 services with real interdependencies (enrichment depends on hamnut + qrz + config; email depends on config; rigloop depends on config for rig selection) AND the enrichment-never-blocks-logging invariant, which demands each external service declare its graceful-degradation path at startup — exactly iocdi's `Initialize()` phase.

**Decision (2026-04-22):** `cmd/logging` uses iocdi from day one. Reasons:
1. Several services (QRZ, Hamnut, email) are lift-and-shift from v1 and already iocdi-shaped.
2. The enrichment-never-blocks-logging invariant needs a principled "validate / warn / continue" hook per service, which iocdi provides via `Initialize()`.
3. Ordering matters across the graph; iocdi's container enforces what would otherwise be a hand-maintained init order in `main.go` that drifts over time.
4. Consistency with `cmd/smd` — same pattern, same failure modes, reduced cognitive load.

The earlier "CLAUDE.md says build specific" pull still stands but its target was v1's reflection-based adapter framework, not DI. iocdi survived the v2 cull specifically because it solves a real problem — and `cmd/logging` at this service count has the same problem.

### Outcome (superseded by session 19)

Session 19 picked up from here — see the "Next session" block under the session-19 heading above. Empty `cmd/logging` window landed; CI got Gio deps; iocdi service registration still open.

### Session 17 work (Gio UI spike — toolkit decision)

### Session 17 work (Gio UI spike — toolkit decision)

**Goal:** evening-scale throwaway spike to decide whether Gio can carry the v2 logging app, or whether we fall back to Wails.

**What landed:**

- `cmd/giospike/main.go` — ~250-LOC Gio app wired to a live FTdx10. Hard-codes `rigID = "yaesu-ftdx10"` and `portPath = "/dev/ttyUSB0"`. On startup: opens port via `serial.Open` (with inline `serial.Config`-from-`RigSerial` helper, same shape as catcli), starts a reader goroutine, sends `INIT` to enable AI push-state, sends `READ` to seed current VFO/mode/etc. into the UI without waiting for a knob twirl.
- Reader goroutine: `port.ReadResponseBytes` → `cat.Decode` → folds `VFOAFREQ` / `VFOBFREQ` / `MAINMODE` into a `rigState` snapshot → publishes to a buffered channel and calls `w.Invalidate()`.
- Main loop: blocks on `w.Event()`, drains the channel inside `FrameEvent`, renders three readout rows + callsign editor + Log button. Log prints a draft QSO to stdout (no DB, no validation beyond non-empty).
- `gioui.org v0.9.0` added to `go.mod` (plus transitive: `golang.org/x/image`, `github.com/go-text/typesetting`, `golang.org/x/exp/shiny`, `eliasnaur.com/font`, `gioui.org/shader`, `github.com/go-text/typesetting-utils`).

**Linux build deps installed** (system-level, not in go.mod):

- `vulkan-headers`, `vulkan-loader-devel`, `libxkbcommon-x11-devel` (the first build on a fresh machine will need these).
- Wayland / X / xkbcommon / Xcursor / Xfixes devel packages were already present.

**Bugs hit + fixed during the spike:**

1. **First run: no updates in the UI.** Cause: main loop's non-blocking `select` checked the channel once, then `w.Event()` blocked forever because the reader wasn't calling `w.Invalidate()`. Fix: reader now calls `w.Invalidate()` after each channel push, and the main loop drains the channel inside the `FrameEvent` handler.
2. **Second run: channel pushes happening but UI still stale.** Cause: I guessed the tag names (`VFO-A`, `VFO-B`, `MODE`) rather than checking `yaesu-ftdx10.json`. Real tags are `VFOAFREQ`, `VFOBFREQ`, `MAINMODE`. Fix: corrected the keys + added `log.Printf` of every successful decode so the stream is observable.
3. **Third run: updates live but fields empty until a knob is touched.** Cause: only `INIT` (= `AI1;ID;`) was sent; the rig only broadcasts state when something changes. Fix: follow `INIT` with `READ` (= `FA;FB;ST;VS;MD0;MD1;PC;`) to seed the current state.

**Decision:** commit to Gio for the v2 logging app. Operator's verdict after live-rig validation: "we can build a clean UI from this and keep the whole thing with Go." Recorded in memory (`project_sm_ui_toolkit.md`). `cmd/giospike/` stays in the tree as a working reference; it gets deleted when the real logging app lands.

### Session 16 work (CAT/serial data layer + characterization tests)

### Session 16 work (CAT/serial data layer + characterization tests)

**What landed:**

- `internal/serial` brought across from v1, audited, drift-fixed (drift = v1 errors API `.Msg`/`.Err` → v2 `.WithMsg`/`.WithErr`, `types.SerialConfig` moved into the serial package as `serial.Config`, `go.bug.st/serial v1.6.4` added to go.mod, tiny Open() port-leak fix, cmd/catcli SIGINT handler, README+DEV merged into doc.go).
- `internal/cat/rigs/yaesu-ftdx10.json` and `yaesu-ft710.json` authored, lifted from v1's battle-tested `internal/config/defaults.go` on the v1 branch (3 commands INIT/READ/PLAYBACK, 8 states ID/FA/FB/ST/VS/MD0/MD1/PC, v1 tag names VFOAFREQ etc.).
- `internal/cat/rig.go` — types: `RigDefinition`, `RigSerial` (now including RTS/DTR/WriteTimeoutMS per v1), `RigTiming`, `Command`, `State`, `Marker`, `ValueMapping`.
- `internal/cat/rigdb.go` — `//go:embed rigs/*.json` + `Lookup(id)`, `List()`, stubbed `RegisterExternalDir(dir)`.
- `internal/cat/rigdb_test.go`, `reference_test.go`, `decode_fixtures_test.go`, `encode_fixtures_test.go` — 38 subtests green. `reference_test.go` holds frozen v1-faithful mirrors of lookup/decode/encode as the §4 Step 0 acceptance criteria.
- `internal/types/rig.go` — `RigConfig` + `RigOverrides` DTO (stdlib-only, shape per cat-serial-reuse.md §3c).
- `docs/v2-design/cat-serial-reuse.md` continuously updated — §1a blockers resolved, §3 rig-database story, §3c three-type split, §4 Step 0 marked done, §6 decision log extended (incl. v1-provenance entry and FTdx10 manual-verification entry), §7.5 updated, §9 session pickup updated.
- FTdx10 verified against `FTDX10_CAT_OM_ENG_2308-F.pdf` — every command/state/mode-code confirmed. Recorded in §6 decision log.

**FT-710 manual verification (completed at session resume):**

- `ID = 0800 (Fixed)` — FT-710's identity code. Added `{"key": "0800", "value": "FT-710"}` to the `IDENTITY` state in `yaesu-ft710.json`.
- `ST` on FT-710 only supports `0=OFF` / `1=ON` — no `2=ON+` like the FTdx10. Removed the `{"key": "2", "value": "ON+"}` mapping.
- `VS` on FT-710: `P1=0: MAIN Band: VFO-A / SUB Band: VFO-B`, `P1=1: MAIN Band: VFO-B / SUB Band: VFO-A`. Operationally equivalent to FTdx10's "VFO-A/B operation" — kept v1's `VFO-A` / `VFO-B` labels.
- `MD`, `FA`/`FB`, `PC`, `PB`, `AI` — identical to FTdx10 (all 16 mode codes incl. `E=PSK`/`F=DATA-FM-N`, 9-digit Hz range `000030000-075000000`, 3-digit power 005-100, `PB0%s;` template, AI USB-only with power-off reset).
- Fixture tests updated: replaced the old "raw passthrough" FT-710 ID case with `ID0800 → IDENTITY: "FT-710"` and `ID9999 → IDENTITY: ""`; added `ST=2 on FT-710 → SPLIT: ""` to pin the rig-specific difference. 41 subtests green.
- `cat-serial-reuse.md` §6 decision log has the matching FT-710 verification entry.

### §4 Step 1 landed (real codec)

- `internal/cat/codec.go` — `Decode(def, line) (Status, error)` with `ErrNoMatch` sentinel, `Encode(def, name, args...) ([]byte, error)` with `ErrUnknownCommand` sentinel, and unexported `lookupState` helper. Logic byte-for-byte equivalent to `referenceLookup`/`referenceDecode`/`referenceEncode` in `reference_test.go`.
- `decode_fixtures_test.go` / `encode_fixtures_test.go` — swapped `reference*` calls for `cat.Decode` / `cat.Encode`; tests renamed `TestDecode` / `TestEncode`.
- `codec_equivalence_test.go` (new) — runs every fixture through BOTH the real codec and the frozen reference, asserts identical output. Drift detection: catches any divergence between the two even if the fixture table is also updated.
- 76 subtests green total in `internal/cat`.

### `cmd/catcli` relocated + extended for live rig verification

- Moved from `internal/serial/cmd/catcli/` to top-level `cmd/catcli/` (§7.4 closed).
- New `-rig <id>` flag — looks the rig up in `cat.Lookup`, uses its serial defaults, pipes every framed response through `cat.Decode`, prints raw bytes plus the extracted tag map.
- New `-init` flag — sends the rig's `INIT` command via `cat.Encode` at startup (enables AI push-state mode on Yaesu rigs).
- Without `-rig`, behaviour is unchanged from before (pure serial diagnostic, raw bytes).
- End-to-end validation path: `catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -listen` → live decoded state stream.
- First real wiring of `serial.Port` + `cat.Lookup` + `cat.Decode`/`Encode`. Inline `serial.Config`-from-`RigSerial` conversion (~15 LOC) foreshadows `internal/rigconfig`.

### Live-rig validation landed

Operator plugged in the FTdx10, ran `catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -listen`, and confirmed end-to-end:

- `INIT` burst sent cleanly.
- `ID0761` received, decoded as `IDENTITY: FTdx10`.
- Live `FA` VFO-A broadcasts tracked as the operator turned the knob, each decoded to `VFOAFREQ: <9 digits>`.
- Mode change decoded: `MD02 → MAINMODE: USB`.
- SIGINT on the listen loop produced a clean `serial.ReadResponseBytes: serial: port closed` error and graceful exit.

The FTdx10 in AI mode broadcasts ~15 prefixes v1 never configured (`IF`, `SS`, `NB`, `RF`, `AC`, `RM`, `RG`, `MG`, `ML`, `GT`, `SH`, `BI`, `KR`, plus a mystery `FD` not in the manual). These surface as `[no match]` in catcli, which is the correct behaviour — v1 ignored them silently; we flag them louder. Decision recorded in cat-serial-reuse.md §6: do NOT pre-broaden the state table; expand only when a specific downstream feature needs a specific prefix. `FD` logged as an open item in §8 for future investigation.

### Outcome (superseded by session 18)

Session 18 picked up from here — see the "Next session" block under the session-18 heading above.

### Session 15 work: bridge design simplified, YAGNI question on the table

Started with the three "next options" from session 14 (alpha checkpoint, second
real forwarder, bridge/CAT design). User reasoned through dependencies:
alpha checkpoint needs a logging client, which needs the bridge → bridge is the
real blocker → picked bridge for this session.

**What landed:**

- `docs/v2-design/bridge.md` created and then substantially rewritten in the
  same session as the design was re-examined.
- Pointer updates: `docs/v2-design/structure.md`, this handoff, and
  `project_sm_serial_bridge` memory now point to the new doc instead of the
  memory-as-canonical + hypothetical `multi-rig.md`.
- NDJSON-over-Unix-socket transport **confirmed decided** (was recorded
  pre-v2 in `design-decisions-log.md` and `invariants.md`; was wrongly hedged
  as "probably SSE" in memory and in the first draft of bridge.md — corrected).

**What the re-examination produced:**

1. **The bridge is much smaller than the 2026-04-14 two-frontend design.**
   Daemon absorbing the QSO-logging concern means port ownership decouples
   from logging, so most of the multiplexing rationale disappears.
2. **No rigctld TCP frontend.** WSJT-X/JTDX own their own rigs' ports
   directly in the v2 architecture — no shared-rig scenario with them.
3. **No PTY virtual serial ports.** Same reason.
4. **The bridge, if built, is SM-internal only** — mediates between
   logging app + future CAT control app on the same rig. Third-party apps
   never touch it.
5. **Correct layering pinned:** `internal/serial` for port I/O (no protocol
   knowledge), `internal/cat` for CAT protocol encoding/decoding (no I/O),
   bridge as glue.
6. **SM apps cooperate on write boundaries**, so the bridge needs no
   per-rig-protocol client-side framing logic (I over-engineered this in
   the first draft; user called it out).
7. **Kenwood is NOT an outlier** — same family as Yaesu (ASCII + `;`);
   only Icom CI-V is binary.
8. **v1 UI lag was almost certainly Wails IPC, not a bridge concern.**
   A Unix socket hop adds <1ms; Wails backend↔frontend JSON adds 10-100x that.

**Open at session end (in `docs/v2-design/bridge.md §6`):**

- **YAGNI: build the bridge now, or defer?** The logging app currently can own
  its rig's port directly via `internal/cat` + `internal/serial` — no bridge
  needed today. A CAT control app is a "strong possibility," not a commitment.
  Deferring costs nothing **if** `internal/cat` is given a pluggable transport
  abstraction from the start (`SerialTransport` today → `SocketTransport` the
  day a second app exists). User leaning toward defer at session end.

### SSE event stream: complete (stages 1–4 landed, docs updated)

`GET /v1/events` serves the firehose of the five settled events
(`qso.stored`, `qso.updated`, `qso.deleted`, `forward.succeeded`,
`forward.failed`). End-to-end proof in
`internal/api/handler_events_e2e_test.go`: a real HTTP client opens
the stream, the logging path commits a QSO via `POST /v1/qso`, the
worker runs and submits to the stub forwarder, and the client
receives both `qso.stored` and `forward.succeeded` frames in
monotonic-ID order.

Shape settled and pinned in `docs/v2-design/api.md §4.5` — wire
format, payload shapes, reconnect semantics, slow-reader policy,
keepalive. The "deferred to implementation" items for SSE in §6
are now closed.

**Stage 1 — `internal/events` hub**:

- Plain `*Hub` (not a DI service type; registered as an instance).
- `NewHub()`, `Publish(name, payload)`, `Subscribe() (<-chan Event, unsub)`,
  `Close()`, `SubscriberCount()` (for test rendezvous).
- 64-event per-subscriber buffer. Publish is non-blocking and
  under a mutex so publish order is preserved; full buffer → the
  hub closes that subscriber's channel and drops it from the map.
- Monotonic event IDs assigned inside the Publish mutex so IDs
  match on-the-wire order.
- 12 unit tests including a race-detector soak for concurrent
  publish + subscribe/unsubscribe.

**Stage 2 — emit wiring + DI injection**:

- `events.ServiceName = "eventhub"`; `cmd/smd/main.go` calls
  `events.NewHub()` and registers via `container.RegisterInstance`
  before `container.Build` so anything with a
  `di.inject:"eventhub"` field gets the same `*Hub`.
- `qsoservice.Service` gained `Hub *events.Hub`; Submit/Update/Delete
  publish `qso.stored` / `qso.updated` / `qso.deleted` AFTER tx
  commit (so a rolled-back write never emits).
- `DeleteQsoByIDTx` now returns `(logbookID, error)` so the delete
  path can emit an accurately-scoped `qso.deleted` without a second
  DB round-trip (sqlboiler `FindQso` was already running).
- `worker.Worker` gained a required `*events.Hub` constructor
  parameter; `markSuccess`/`markFailed` publish
  `forward.succeeded` / `forward.failed` AFTER the DB mark call
  succeeds. `Attempts` is read as `int(row.Attempts) + 1` because
  the DB `Mark*` methods increment internally before the write.
- `api.Server.New` takes the hub (held for stage 3's handler).

**Stage 3 — `GET /v1/events` handler**:

- `internal/api/handler_events.go`. Sets
  `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
  `Connection: keep-alive`, `X-Accel-Buffering: no` (harmless on
  unix sockets, cleans TCP mode behind nginx).
- Disables the per-request write deadline via
  `http.ResponseController.SetWriteDeadline(time.Time{})` — without
  this, Go's `WriteTimeout` would cut idle-but-healthy SSE
  connections every `WriteTimeoutSec`.
- Subscribe → select on `r.Context().Done()` + hub channel +
  30 s keep-alive ticker. Frames are `id: %d\nevent: %s\ndata: %s\n\n`.
  On channel close (hub close or slow-reader eviction) or write
  error: return → defer unsubscribe.
- 7 handler tests + 2 e2e-with-worker tests covering delivery,
  shape, multi-event ordering, client disconnect unsub, hub close,
  slow-reader eviction, keep-alive, insert happy path, terminal
  failure path.

**Stage 4 — e2e tests**: see above; live `httptest.NewServer`
wrapping `srv.httpServer.Handler` so a real HTTP client can stream
frames while the worker goroutine ticks.

**Bonus fix** — the M2 race the review had accepted as theoretical
was surfaced reliably by `-race`: `spawnForwarderWorkers` now does
`wg.Add(1)` synchronously BEFORE `safego.Go`, with an `isRespawn`
closure flag so respawn paths still re-increment after a panic.
Stable under 10 consecutive race-detector runs of
`TestSpawnForwarderWorkers_HappyPath_Single` where it previously
flaked ~40% of the time.

### QRZ port: complete (stages 1–8 all landed)

The 8-stage QRZ port is done. Insert / update / delete are
live-validated against real QRZ; the ADIF upload-status stamp
rides on success; each forwarder owns its own retry defaults;
and the daemon binary's ldflags-injected Version now threads
into `qrz.UserAgent` and `adif.ProgramVersion` at startup.

Station Manager v2 can now push QSOs to QRZ.com end-to-end
through the daemon's forwarding pipeline. The stub forwarder
remains available for plumbing tests.

**Stage 1 — Forwarder interface extension** (session 12, committed):

- `Forwarder.Submit` gained a `priorUpstreamID string` parameter so
  the worker can pass QRZ's LOGID (captured on the earlier successful
  insert's `Result.UpstreamID`) through to the delete call.
- New `AdifPrefix() string` method: declarative metadata telling the
  worker which ADIF upload-status field pair to stamp on the QSO row
  on success (`QRZCOM_QSO_UPLOAD_STATUS` / `QRZCOM_QSO_UPLOAD_DATE`
  for QRZ, `CLUBLOG_*` for ClubLog, `""` for stub / custom webhooks).
  v1 did this stamp from inside the QRZ service; v2 moves it to the
  worker so forwarders stay pure plugins.

**Stage 2 — QRZ package skeleton** (session 13):

- `internal/forwarding/qrz/qrz.go`: `Type = "qrz"`,
  `AdifFieldPrefix = "QRZCOM"`, registry `init()`, `New` with
  credentials validation, stubbed `Submit` that returns Terminal
  until stage 4 lands the real HTTP call.
- `internal/forwarding/qrz/qrz_test.go`: 9 tests covering registry
  round-trip, happy path, malformed/missing credentials, ctx
  cancellation.
- **Credentials shape decided**: `{"api_key": "..."}` — only.
  QRZ enforces the callsign/logbook match server-side (every QSO's
  `STATION_CALLSIGN` must match the logbook's callsign, or QRZ
  rejects the record); keeping a local copy of the callsign would
  only introduce config-drift risk without a correctness guarantee.
  `forwarding.md` §2 updated.

**Stage 3 — Response parser + classifier** (session 13):

- `internal/forwarding/qrz/response.go`: `parseResponse(body)` (pure
  function, `net/url.ParseQuery`-based) and `classifyResponse(act,
  resp)` split into per-action helpers (`classifyInsert`,
  `classifyUpdate`, `classifyDelete`). `AUTH` short-circuits across
  all actions. No substring matching on `REASON` text — QRZ's
  documented per-action RESULT sets are unambiguous.
- `internal/forwarding/qrz/response_test.go`: 26 tests covering the
  full per-action matrix (see `forwarding-implementation.md` §8.1).
- **Key classification refinement**: for `action=delete`, QRZ's
  single-LOGID delete makes `RESULT=FAIL` unambiguously mean "LOGID
  not found". We reclassify it as `OutcomeSuccess` — the record's
  absence upstream matches intent. `RESULT=PARTIAL` on a
  single-LOGID delete is treated as Terminal (shouldn't occur in
  practice).

**Stage 4 — HTTP Submit for insert + update** (session 13):

- `internal/forwarding/qrz/qrz.go`: real `Submit` implementation
  with `buildForm` (insert = `ACTION=INSERT + ADIF`; update =
  `ACTION=INSERT + OPTION=REPLACE + ADIF`) and `classifyHTTPStatus`
  (408/429/5xx → Transient; other non-2xx → Terminal; 2xx falls
  through to body parse). Delete still returns Terminal "deferred
  to stage 5".
- Package-level knobs: `DefaultEndpoint = "https://logbook.qrz.com/api"`,
  `DefaultHTTPTimeout = 30 * time.Second`, `var UserAgent =
  "station-manager/dev"` (to be overridden from `cmd/smd/main.go`
  alongside the blank import in stage 8).
- Package-internal `newWithEndpoint(apiKey, endpoint, client)` —
  tests use it to point at `httptest.NewServer.URL`; production
  code goes through public `New` with the real endpoint.
- `submit_test.go`: 18 httptest-based tests covering transport
  class (network error, ctx cancel, 408/429/500/400/401), body
  class (OK/FAIL/AUTH/REPLACE on insert+update), malformed bodies,
  request-shape assertions (method, KEY, ACTION, OPTION=REPLACE
  on update, ADIF payload, User-Agent), delete-deferred guard,
  unknown-action fallthrough.
- **Live harness** at `internal/forwarding/qrz/live_test.go`
  (`//go:build manual`, gated by `QRZ_TEST_API_KEY` +
  `QRZ_TEST_CALLSIGN` env vars loaded from `.env`):
  - `TestLive_InsertThenUpdate` — quick round-trip with t.Cleanup
    delete; `task test:qrz-live`.
  - `TestLive_InteractiveFlow` — insert → pause → update → pause →
    delete, with `[Enter]` prompts between steps so the operator
    can inspect the record on QRZ.com. `task test:qrz-live-interactive`.
  - **Gotcha recorded**: `go test` feeds the test binary a closed
    stdin, so `bufio.Scanner(os.Stdin)` returns EOF immediately.
    Interactive test opens `/dev/tty` directly to read the
    controlling terminal — Unix-only (Linux/macOS is fine for the
    operator's setup).
- **Live-validated end-to-end**: insert → LOGID returned, update
  with `OPTION=REPLACE` returns the same LOGID (confirming in-place
  update rather than new record), raw delete cleans up. Real QRZ
  response shapes match our parser's assumptions exactly.
- **DB-level verification in the live harness is deferred to
  stage 6** — when `MarkUploadSuccessWithAdifStampWithContext`
  lands, that's the fresh multi-table tx code that earns a
  real-stack check. Today's layered tests (worker + SQLite in-memory
  with stub forwarder; QRZ unit + live with no DB) cover the seam
  transitively.

**Stage 5 — Delete via `Submit` + worker-side LOGID lookup** (session 13):

- `internal/database/sqlite/api_context.go`: new
  `FetchInsertUpstreamIDWithContext(ctx, qsoID, forwarderName)
  (string, error)`. Returns the `upstream_id` from the one
  successful insert row for the (qso, forwarder) pair, `""` when
  no match, non-nil error only for infrastructure failures.
  `ORDER BY modified_at DESC LIMIT 1` is defensive — the schema
  already enforces `UNIQUE(qso_id, forwarder_name, action)` so
  only one row can match in practice. Tests: `service_test.go`
  gains 6 cases covering happy path, scope (forwarder, action,
  status) filtering, the UNIQUE-constraint guard, and input
  validation.
- `internal/forwarding/worker/worker.go`: new
  `resolvePriorUpstreamID` helper called from `processRow`. For
  `action=delete`, consults the DB lookup before `Submit`:
  infra error → transient, empty result → terminal
  ("no upstream id for delete — no successful insert found"),
  non-empty → passed through as `priorUpstreamID`. Worker tests:
  `TestWorker_Delete_NoPriorInsert_IsTerminalImmediately`,
  `TestWorker_Delete_WithPriorInsert_PassesUpstreamID`,
  `TestWorker_InsertAndUpdate_DoNotTriggerLookup`. The existing
  `TestWorker_SoftDeletedQso_DeleteStillForwards` was updated to
  seed a prior successful insert (the pre-stage-5 test would now
  fail on "no upstream id"). Added a `recordingForwarder`
  helper that captures Submit arguments for delete-path tests.
- `internal/forwarding/qrz/qrz.go`: `buildForm` delete branch
  assembles `ACTION=DELETE + LOGIDS=priorUpstreamID`. Empty
  `priorUpstreamID` here is a caller bug (worker should have
  short-circuited) — classified Terminal with clear error, no
  HTTP fired. Tests: replaced `TestSubmit_Delete_DeferredToStage5`
  with 4 new cases (OK/Success, FAIL/idempotent-success,
  EmptyPriorID/Terminal, RequestShape verifying
  `ACTION=DELETE`/`LOGIDS=...`/no ADIF/no OPTION).
- Live harness: `TestLive_InsertThenUpdate` cleanup and
  `TestLive_InteractiveFlow` delete phase both converted from the
  raw HTTP helper to `Submit(..., Delete, logID)`. Dropped
  `liveDelete` helper and its dead imports (`io`, `net/url`,
  `strings`).
- **CI fix (bundled)**: `internal/database/sqlite/internal.go`
  adds `cache=shared` to the DSN options when path is `:memory:`.
  Under `-race` timing in CI, a single pooled connection could
  transiently drop and be replaced with a fresh private
  `:memory:` DB, producing "no such table: qso_upload". Shared
  cache makes all pool connections to the same DSN see the same
  in-memory DB; file-backed paths unchanged.

**Stage 6 — ADIF-stamp wiring** (session 13):

- `internal/database/sqlite/api_context.go`: new
  `MarkUploadSuccessWithAdifStampWithContext(ctx, id, upstreamID, qsoID, adifPrefix)`.
  Single transaction, two writes:
  1. `qso_upload` row transitions (same shape as
     `MarkUploadSuccessWithContext`).
  2. `qso.additional_data` gets `json_set` for
     `$.<prefix>_qso_upload_status = "Y"` (adif.YesString) and
     `$.<prefix>_qso_upload_date = today UTC YYYYMMDD`.
  One-fails-all-fail holds: either both writes land or the tx
  rolls back. Regex validator on adifPrefix (`^[A-Z][A-Z0-9]*$`)
  as defense-in-depth.
- **Schema discovery**: the `qso` table has no per-destination
  ADIF columns — `types.Qso.QrzComUploadDate` /
  `QrzComUploadStatus` ride inside `additional_data`, not as
  columns. json_set on additional_data is the right landing
  place and matches the "additional_data absorbs ADIF spec
  evolution" invariant. No schema migration needed for the
  stamp, and none needed for future forwarders either.
- `internal/forwarding/worker/worker.go`: `markSuccess` now
  dispatches — `fwd.AdifPrefix() != "" && row.Action != Delete`
  → ADIF-stamp variant; else plain variant. Delete never
  stamps (soft-deleted local QSO); prefix-less forwarders
  (stub, custom webhooks) never stamp.
- Tests: sqlite gets 6 new cases (happy path with round-trip
  via `FetchQsoByID`; raw-SQL verification of JSON blob keys;
  prefix-agnostic test using a notional CLUBLOG prefix; invalid
  prefix rejection including injection-style strings; missing
  upload row / missing qso row with rollback verification).
  Worker gets 3 new cases via a local `stampingForwarder` type
  (insert+prefix stamps, delete+prefix doesn't stamp, empty
  prefix doesn't stamp).
- **Generalisability**: adding a new forwarder (ClubLog, LoTW,
  ...) requires zero sqlite/worker/migration changes. The
  forwarder's package returns its `AdifPrefix()`, and future
  `types.Qso` fields (for ADIF export round-trip) get the
  matching JSON tag.

**Stage 7 — Retry-defaults ownership refactor** (session 13):

- `internal/forwarding/registry.go`: new
  `RegisterDefaultRetry(typeName, retry)` + `DefaultRetryFor(typeName)`.
  Companion map alongside the existing constructor registry.
  `Register` panics on empty type / duplicate / nil ctor; the new
  `RegisterDefaultRetry` adds validation parity with
  `worker.New` (panics on MaxAttempts < 1, InitialBackoffSec < 1,
  or MaxBackoffSec < InitialBackoffSec) so an invalid default never
  survives to spawn.
- `internal/forwarding/qrz/qrz.go`: exports
  `DefaultRetry = {5 attempts, 60s initial, 1800s max}`, tuned for
  the QRZ web API + the operator's slow/unreliable link. Registered
  in `init()`.
- `internal/forwarding/stub/stub.go`: exports
  `DefaultRetry = {3 attempts, 1s initial, 5s max}`. Tight values —
  stub is for plumbing verification; tests that want to exercise
  backoff set `Config.Retry` directly.
- `cmd/smd/main.go`: `spawnForwarderWorkers` now resolves retry
  via `forwarding.DefaultRetryFor(fc.Type)` when `fc.Retry` is
  absent. A type with neither a config override nor a registered
  default is a setup error and fails startup loudly with a clear
  message naming both the forwarder instance and the type. The
  package-level `defaultForwarderRetry` fallback is deleted.
- Tests: registry gets 6 new cases (register + lookup, missing
  type, empty-type panic, duplicate panic, invalid-config panic
  for each of the three RetryConfig fields). qrz and stub each
  get an `TestInit_RegistersDefaultRetry` asserting the var is
  exported and registered consistently.
- **Consequence for future forwarders**: ClubLog / LoTW / eQSL
  each ship their own `DefaultRetry` with values appropriate to
  that upstream's quirks (LoTW's batch acknowledgements,
  ClubLog's daily windows, ...). No main.go changes to land a
  new forwarder.

**Stage 8 — Wire-up + docs** (session 13, final):

- `cmd/smd/main.go`: added regular import of
  `internal/forwarding/qrz` (so the init() registers the
  constructor and default retry, AND so main can set
  `qrz.UserAgent`) and blank import of
  `internal/forwarding/stub` (registration only). The
  blank-import style is preserved for forwarders main.go
  doesn't otherwise reference.
- `cmd/smd/main.go`: at the top of `run()`, two package vars
  are now overridden from the ldflags-bound `Version`:
  ```go
  qrz.UserAgent      = "station-manager/" + Version
  adif.ProgramVersion = Version
  ```
  This thread ensures ADIF exports' PROGRAMVERSION header
  and QRZ's User-Agent both reflect the actual binary
  version.
- `internal/adif/consts.go`: `ProgramVersion` flipped from
  `const` to `var` (default `"dev"`) with a doc comment
  explaining the override mechanism; `ProgramID` stays
  `const` (identity marker, not version-dependent).
- Ldflags smoke check:
  `go build -ldflags "-X main.Version=1.2.3-test" ./cmd/smd`
  builds cleanly.

Full suite green under `-race`. v2's forwarding subsystem is
complete end-to-end: ingest → queue → worker → forwarder →
upstream → outcome → qso_upload + ADIF stamp, with live QRZ
validated.

### Forwarding subsystem code review (session 13, end-of-session)

In-depth subagent review of the 8-stage port landed at
`docs/reviews/forwarding-subsystem.md`. Headline: 0 high · 6
medium · 7 low · 5 positives. No correctness bugs, no invariant
violations, no credential leakage.

Triaged and **actioned** in the same commit series:

- **M2** — `spawnForwarderWorkers` now takes a `*sync.WaitGroup`;
  `run()` waits (bounded) for workers to drain after
  `server.Shutdown` before the deferred `dbSvc.Close()` fires.
  Matches the E2E test harness shape; stops the "database is
  closed" log spam on every clean shutdown.
- **M3** — `FetchInsertUpstreamIDWithContext` changed to
  `ORDER BY created_at DESC` so the defensive fallback (if the
  UNIQUE constraint ever relaxes) picks the most-recently-inserted
  row regardless of what retry bookkeeping did to `modified_at`.
- **M4** — Document-only invariant comments at
  `ClaimPendingUploadsWithContext` and `spawnForwarderWorkers`
  pinning "one worker per forwarder_name" and its single
  enforcement point.
- **M5** — Three sections of `forwarding-implementation.md` that
  referenced the deleted `defaultForwarderRetry` rewritten for the
  per-package `DefaultRetry` + `RegisterDefaultRetry` shape.
- **M6** — Added HTML-proxy-body and multi-line `REASON` tests to
  freeze QRZ's real-world failure modes. `cmd/smd/main.go` spawn-path
  coverage landed in session 14 (task #29): `cmd/smd/main_test.go`
  covers 7 `spawnForwarderWorkers` paths + `loadConfig`'s 4 resolution
  modes.
- **L1** — Deleted unused `response.Fields` map.
- **L2** — Parse `action.Parse` once at the top of `processRow`;
  `fetchQsoForAction` now switches on the typed value.
- **L3** — Deleted hand-rolled `itoa`/`containsSubstring`/`indexOf`
  helpers in worker tests; import `strconv` / `strings`.
- **L5** — Multi-byte UTF-8 case (`QRZCOMé`) added to the
  invalid-prefix test slice.
- **L7** — Hardcoded contact callsign in the live harness changed
  from `2E0TEST` to `W1AW/T` (ARRL HQ portable-temporary).

Triaged and **accepted as-is** with rationale pinned in the review
doc's Resolution status section:

- **M1** — Worker wedging a row `in_progress` when a mark-call DB
  write fails. The daemon-restart `ResetOrphanedUploadsWithContext`
  sweep is the documented safety net; the failure requires a
  tx-commit error or sqlboiler Update failure on SQLite — vanishingly
  rare.
- **L4** — `qrz.Forwarder` concurrent-safety docstring is slightly
  imprecise but not misleading.
- **L6** — `bodySnippet` byte-boundary truncation is theoretical
  (QRZ responds in ASCII).

**Task #29** — `cmd/smd/main.go` test coverage (spawn-path +
lifecycle) completed in session 14; see M6 above.

Full suite green under `-race` after every fix; ldflags build
smoke-check passes. Forwarding subsystem is **review-complete**
and ready for the next phase.

### Forwarder subsystem thin-slice complete (session 11)

Design: `docs/v2-design/forwarding.md` is the authoritative shape;
the 11-stage thin slice below implements it end-to-end. All 11
stages landed in session 11. The spine — POST → queue → worker →
forwarder submit → persist outcome → pull endpoint — is covered by
a regression test at `internal/api/handler_e2e_test.go` for the
insert / update / delete actions.

**What's still deferred from milestone 1c** (tracked below as
follow-ups, not thin-slice scope):

- **Real QRZ forwarder implementation.** The stub exercises the
  plumbing; porting v1's `internal/upload/qrz/` into
  `internal/forwarding/qrz/` is milestone-1c work but was not part
  of the 11-stage slice.
- **SSE event stream (`GET /v1/events`).** Terminal transitions
  (`in_progress → uploaded` / `failed`) are the emit sites per
  forwarding.md §7, but the stream itself hasn't been built yet.
  The worker code has comments marking the emit points.
- **Manual re-queue / dead-letter cleanup endpoints** (forwarding.md
  §11). Deferred by design; no design pressure yet.

### Session 11 progress (2026-04-18)

**Design doc landed.** `docs/v2-design/forwarding.md` settles the
internal shape of the forwarder subsystem: constraints, terminology,
fan-out config, `Forwarder` interface, per-destination worker
topology, retry policy, queue-row data shape (§6), lifecycle and
status transitions, `SafeGo` recovery, v1 migration, acceptance.
Walked through the flow step-by-step with the user, which surfaced
several refinements:

- **Ham services are effectively singleton per operator.** One QRZ,
  one ClubLog, one LoTW per operator; `forwarder_name` defaults to
  the type string. The `name`/`type` split exists for rename safety
  (historical rows stay interpretable when an operator relabels a
  destination), not because we expect multi-instance deployments.
  Memory: `project_sm_ham_services_singleton.md`.
- **Retry defaults live in the forwarder package, not config.** Each
  upstream's tolerances are different; `qrz.New` knows what QRZ can
  take. Operators only write a `retry` block in config when they
  need to override.
- **Config reload is off the table.** Restart required for config
  changes. Live reload introduces real complexity (in-flight
  attempts, credential rotation) without matching operator benefit.
- **Slow-link operator-environment defaults** went into the doc:
  `tick_interval_sec=120`, `batch_size=5`, matching v1 operational
  values. Memory: `project_sm_operator_network.md`.

**Implementation plan: 11-stage thin slice**, each stage a
committable unit:

| # | Stage | Status |
|---|-------|--------|
| 1 | Schema update — split `service` into `forwarder_name`+`forwarder_type`, add `next_attempt_at`, `upstream_id` | **done** |
| 2 | Config surface — `ForwarderConfig`/`RetryConfig` in types, `Forwarders[]` on `Config`, defaults + validation, `Forwarders()` accessor | **done** |
| 3 | `internal/forwarding/` — `Forwarder` interface, `Outcome`/`Result`, `Action` alias, init()-time `Register`/`Build` registry | **done** |
| 4 | Stub forwarder — `internal/forwarding/stub/`, modes: `always_success`, `always_transient`, `always_terminal`, `flap_n` | **done** |
| 5 | `safego` helper — landed as `internal/safego/` (not `internal/utils`; cycle avoided), callback-based, ctx-aware cooldown | **done** |
| 6 | DB methods — `ClaimPendingUploadsWithContext` (atomic `UPDATE ... RETURNING`), `MarkUpload{Success,TransientRetry,Failed}WithContext`, `ResetOrphanedUploadsWithContext`, `FetchUploadsByQsoIDWithContext` | **done** |
| 7 | Wire ingest — `submit.go`/`update.go` loops read `config.Forwarders` filtered by enabled + action_filter; new `qsoservice.Delete` atomically soft-deletes + enqueues delete rows | **done** |
| 8 | Worker loop — `internal/forwarding/worker/` per-forwarder tick + claim + submit + persist | **done** |
| 9 | Startup wiring — `main.go` orphan sweep + spawn workers via SafeGo | **done** |
| 10 | Pull endpoint — `GET /v1/qso/:id/uploads` | **done** |
| 11 | E2E integration test — POST → observe row transition to `uploaded` | **done** |

**Stage 1 cleanup (incidentally resolved):**
- `uploadRetryCooldown` + `defaultUploadBatchLimit` constants
  deleted from `sqlite/consts.go` — the M8 `TODO(forwarder)` is
  closed. Retry cadence now lives in per-forwarder config / the
  forwarder package's own defaults.
- `types.RequiredConfigs` + `config.Service.RequiredConfigs()`
  deleted (its one field `QsoForwardingRowLimit` was consumed only
  by the now-deleted legacy worker code; the replacement lives in
  `ForwarderConfig.BatchSize`).
- Legacy v1 worker methods (`InsertQsoUploadWithContext`,
  `FetchPendingUploadsWithContext`, `UpdateQsoUploadStatusWithContext`
  and their non-ctx wrappers) deleted from the sqlite package;
  their three tests likewise removed. Stage 6 added the new
  purpose-built replacements.

**Stage 4 — stub forwarder.** `internal/forwarding/stub/` implements
`Forwarder` with four modes (`always_success`, `always_transient`,
`always_terminal`, `flap_n`) selected via the credentials blob. Ctx
cancellation short-circuits before the call counter bumps so tests
can assert on "how many real submits happened" cleanly. Registers
under type `"stub"` via `init()`; 11 tests covering validation,
each mode, flap transition, ctx-cancel, and round-trip via
`forwarding.Build`.

**Stage 5 — `internal/safego/`.** Deviation from the draft doc:
lives in its own package, not `internal/utils`. Cause: `logging`
already imports `utils`, so putting `*logging.Service` in utils
would create a cycle. The landed shape takes a `PanicHandler`
callback instead of a concrete logger — zero dependency on logging,
callers wire the log format. Signature also gained a `ctx` parameter
so the cooldown sleep is interrupted by shutdown rather than
spawning a final respawn that immediately exits. Cooldown is an
`atomic.Int64` (nanoseconds) after the race detector caught a real
race between `t.Cleanup` and still-running goroutines. `docs/v2-
design/forwarding.md §9` rewritten to match as-implemented shape.

**Stage 6 — upload-queue DB surface.** Six methods, all worker-
facing. `ClaimPendingUploadsWithContext` is the atomic
`UPDATE ... RETURNING *` from the design doc, scoped to a single
forwarder so two workers never compete. `modified_at` is driven by
`trg_qso_upload_set_updated_at` so the mark/claim statements don't
touch it manually; SQLite's default `recursive_triggers=off` prevents
the trigger's own UPDATE from re-firing. Empty `upstream_id` is
stored as NULL rather than the empty string. New
`QsoUploadModelToType` adapter flattens nullable columns for
callers that don't care about null-vs-value. 13 integration tests
cover claim ordering, forwarder scoping, future-`next_attempt_at`
gating, each mark method, orphan sweep, and pull-endpoint fetch.

**Stage 6b — sqlboiler refactor (post-review).** User flagged that
four of the Stage-6 methods were using raw SQL where sqlboiler's
type-safe builders would do. Refactored `MarkUploadSuccess`,
`MarkUploadTransientRetry`, `MarkUploadFailed` to the load-then-save
pattern (`FindQsoUpload` → mutate fields → `Update(ctx, h, boil.Infer())`);
refactored `ResetOrphanedUploads` to `models.QsoUploads(...).UpdateAll(...)`.
`ClaimPendingUploadsWithContext` kept as raw with an expanded doc
comment naming the two sqlboiler limitations that justify the
exception (`UPDATE ... RETURNING *`, `WHERE id IN (SELECT ... LIMIT N)`
subquery-same-table). Bonus: Mark* now correctly surface
`errors.ErrNotFound` for nonexistent row IDs — the raw version was
silently no-oping, a latent bug. Preference saved as
`feedback_sqlboiler_default.md` memory.

**Stage 7 — ingest → forwarders wired.** `qsoservice.Service` gains
a `Config *config.Service` DI field. New
`internal/qsoservice/forwarders.go` helper
(`shouldEnqueue(fc, action) bool`) centralises the enabled-and-
action-filter check for all three ingest sites. `submit.go` loop
swaps the stub for `s.Config.Forwarders()`; `update.go` activates
its commented hook with the same pattern. New
`internal/qsoservice/delete.go` introduces the first domain-level
`Delete(ctx, id)` that atomically soft-deletes the QSO and enqueues
`delete`-action queue rows under one tx (one-fails-all-fail). DB
layer gains `DeleteQsoByIDTx(ctx, tx, id)` for the tx-scoped
soft-delete; the old `DeleteQsoByIDWithContext` is deleted (its one
caller, `handleDeleteQso`, now goes through `qsoservice.Delete`).
`testServer` split into `testServerWithCfg(t, mutate)` so tests can
populate `cfg.Forwarders` before construction. 6 new HTTP-level
tests verify enabled→row-inserted, disabled→skipped, action-filter
exclusion, update-path enqueue, delete-path enqueue + soft-delete,
and delete-with-zero-forwarders.

**Stage 8 — worker loop.** `internal/forwarding/worker/` lands the
per-destination goroutine the design calls for. `Worker` holds a
resolved `Config` (name, tick, batch, retry) plus references to
`*sqlite.Service`, `*logging.Service`, and a `forwarding.Forwarder`.
`Run(ctx)` runs an initial tick then selects on a `time.Ticker`
until ctx cancels; each tick calls `ClaimPendingUploadsWithContext`
for its forwarder_name and iterates rows, calling the forwarder's
`Submit` and persisting the outcome via `MarkUpload{Success,
TransientRetry,Failed}`. Soft-delete handling per forwarding.md §4
is implemented: `insert`/`update` with a soft-deleted QSO marks the
row failed; `delete` falls back to
`FetchQsoByIDIncludingDeletedWithContext` so the upstream still gets
told. Backoff (`backoff.go`) implements §5's exponential +20% jitter
with an overflow cap at `maxBackoffShift=30`. 16 tests across
`worker_test.go` (positive outcomes via real sqlite + stub
forwarder) and `backoff_test.go` (pure-function bounds). Test
helpers `runUntil(t, w, h, qsoID, match)` and `runFor(t, w, d)`
replace an earlier fixed-sleep `runOnce` shape that flaked under
`-race` load; the polling approach is deterministic regardless of
scheduler latency. New sqlite method
`FetchQsoByIDIncludingDeletedWithContext` uses `models.NewQuery` +
`qm` mods — sqlboiler's re-exported query builder — to sidestep the
auto-filter on `deleted_at IS NULL` that `FindQso` and
`models.Qsos(...)` bake in; column/table references still come from
generated constants. Memory `feedback_sqlboiler_default.md`
expanded with the `models.NewQuery` idiom so future sessions reach
for it before `queries.Raw`.

**Stage 9 — startup wiring in `cmd/smd/main.go`.** Blank import
`_ "internal/forwarding/stub"` registers the stub type. After
migrations run, `ResetOrphanedUploadsWithContext` sweeps any
`in_progress` rows back to `pending` with a 10s context; log line
fires only when the count is non-zero. A `workerCtx/workerCancel`
pair is constructed before the HTTP server starts, so workers live
exactly for the daemon's lifetime. A new
`spawnForwarderWorkers(ctx, fwds, db, logger) error` helper loops
`cfg.Forwarders`, skips disabled entries, builds each forwarder via
`forwarding.Build`, resolves retry (per-entry override or the
package-level `defaultForwarderRetry = {5, 60, 3600}` fallback, a
temporary stand-in until real forwarders supply their own per
forwarding.md §2), constructs a `worker.Worker`, and launches it
under `safego.Go` with `respawn=true`. Panic handler logs
structured fields (`goroutine`, `panic`, `stack`) through the
daemon logger. Shutdown ordering: `workerCancel()` fires **before**
`server.Shutdown(ctx)` so in-flight forwarder HTTP calls abort
promptly and workers stop starting new DB ops against the
about-to-close handle.

**Stage 10 — pull endpoint `GET /v1/qso/:id/uploads`.**
`internal/api/handler_uploads.go` implements a thin handler: parse
id (400 on bad), existence-probe via
`FetchQsoByIDIncludingDeletedWithContext` (404 only for
genuinely-unknown ids; soft-deleted QSOs still return their rows
because the delete-action forwarding work remains observable),
fetch via `FetchUploadsByQsoIDWithContext`, normalise nil→empty
slice, return `{"items": [...]}`. Route wired in `server.go`. Five
handler tests cover: two-forwarder happy path with stable
`(forwarder_name, action)` ordering, no-forwarders → literal
`"items":[]` (not `null`), soft-deleted QSO → still returns rows,
unknown id → 404 `not_found`, invalid id → 400 `invalid_id`.

**Stage 11 — end-to-end acceptance test.**
`internal/api/handler_e2e_test.go`, three scenarios, all using the
existing `testServerWithCfg` harness plus real `worker.Worker`
goroutines (plain `go` + `sync.WaitGroup` for deterministic
shutdown — `safego`'s respawn path is tested in its own package):
`TestE2E_InsertReachesUploaded` (POST, both upload rows reach
`uploaded`, `attempts=1`, `upstream_id=stub-ok`),
`TestE2E_UpdateReachesUploaded` (POST → settle → PATCH → wait for
update row to upload), `TestE2E_DeleteReachesUploaded` (POST →
settle → DELETE → wait for delete row to upload, asserts canonical
`GET /v1/qso/{id}` now 404s while the uploads endpoint still shows
the rows). Helpers: `startE2E(t, fwds...)` spawns workers with a
50ms tick and returns a shutdown closure registered as
`t.Cleanup` backstop; `waitForUploads(t, srv, qsoID, match)` polls
at 25 ms with a 3 s deadline, logs the last observed rows on
timeout.

### Current state (as of 2026-04-17 end-of-session 10)

### Milestones 1 and 1b both complete, full code review landed

Milestone 1 (submit a QSO) and milestone 1b (QSO CRUD, logbook
management, list, contest-dupe, contact history, version) are both
complete and CI-green under `-race`. The daemon now exposes the
full set of endpoints the logging-app and logbook-app need for
milestone 2+.

**Session 10 focus was hardening, not new features.** A full
independent code review (`docs/reviews/milestone-1b-review.md`)
surfaced 23 findings across high/medium/low severity; every one
has been addressed. The codebase is now in a "clean slate for
forwarder" state — no known bugs, no convention drift, no dead
code outstanding.

### Session 10 headline changes

- **H1 — concurrent-submit race plugged.** Pre-transaction dedupe
  check + UNIQUE-constraint catch-and-reclassify; deterministic
  test (`TestSubmitQso_ConcurrentDuplicate`) locks it in.
- **ADIF export moves entirely client-side.** `POST /v1/logbook/{id}/export`
  is dropped from the roadmap; clients that need ADIF use
  `internal/adif` as a library. Backup story is forwarding to
  online services, not file dumps. See
  `memory/project_sm_session_scope.md`.
- **SQL call-site audit items 1–2 landed.** New
  `LogbookCallsignByIDWithContext` on the submit hot path; new
  composite partial index `idx_qso_logbook_date_time` for cursor
  pagination.
- **M6 proactive fix for one-fails-all-fail.** `qsoservice.Update`
  is now transactional with a commented forwarder hook inside the
  tx envelope, so the forwarder PR just drops the
  `InsertQsoUploadTx(action.Update)` loop into the existing slot.
- **Daemon lifecycle is defer-based.** `cmd/smd/main.go` delegates
  to `run() error` with defers for logger + db cleanup; `fatal()`
  is gone. Failures at any startup step unwind cleanly through
  registered defers.
- **Dead code swept.** `qsoservice.FreqMHzToKHzString`,
  `sqlite.Service.ExecContext`, `sqlite.Service.QueryContext`,
  `fatal()`, and the unused-error-return in `adif.parseRecords` —
  all deleted. No functional change; less noise.
- **Convention sweep.** All 9 residual `fmt.Errorf` call sites in
  the sqlite package converted to `errors.New(op).WithErr(err).WithMsg(...)`.
  Four handler tests moved off English-message substring matching
  onto structured decode. Eight `fmt.Sscanf` sites converted to
  `unmarshalJSON`. Contact-history LIKE pattern anchored on slash
  (`X/%`) so coincidental prefixes no longer match.
- **Panic handling added (post-review).** `main()` has a
  `defer recover()` with `ExitError`/`ExitPanic` exit-code
  constants so a supervisor can tell a panic from a graceful
  error exit. `api.recoverPanic` middleware wraps the mux — any
  handler panic is structurally logged and returns a generic 500
  envelope (no panic-value leak). Worker-goroutine recovery is a
  noted follow-up for when the forwarder lands.
- **`goccy/go-json` dep dropped (post-review).** Adapters now use
  stdlib `encoding/json`; go.mod / go.sum cleaned. Consistency
  restored — one JSON library, fewer external deps.

Commits covering session 10 are in the `main` branch; the review
doc has a resolution note pointing at them.

### Milestone 1b progress

| Step | Scope | Status |
|------|-------|--------|
| 1. Logbook CRUD | `GET/POST/PATCH/DELETE /v1/logbook` | **done** (session 8) |
| 2. QSO fetch/edit/delete | `GET/PATCH/DELETE /v1/qso/:id` | **done** (session 9) |
| 3. QSO list with pagination | `GET /v1/logbook/:id/qso` | **done** (session 9) |
| 4. Contest dupe check | `GET /v1/contest-dupe` | **done** (session 9) |
| 5. Contact history | `GET /v1/contact-history` | **done** (session 9) |
| 6. Version | `GET /v1/version` | **done** (session 9) |

### FREQ added to dedupe-key inputs (session 9)

The dedupe-key hash was expanded from
`CALL|BAND|MODE|QSO_DATE|TIME_ON` to
`CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON`. Aligns with ADIF-spec
guidance on QSO identity and distinguishes same-call/same-time
contacts on different frequencies (net ops, split, frequency
hopping). FREQ is the normalized integer-kHz string so "14.074" /
"14074" / "14.0740" all hash to the same key.

No schema change — `dedupe_key` is just a hash column. No migration
needed pre-1.0.

### PATCH design decisions (session 9)

- **Immutable fields:** `id`, `logbook_id`, `station_callsign`,
  `dedupe_key`, forwarding state (`sm_*`, `qrzcom_*`), enrichment
  (`country_details`, `contact_history`). Always restored from the
  existing row after `json.Unmarshal` overlay. Clients cannot rewrite
  them via PATCH even if they include those keys in the body.
- **Dedupe-key recompute:** if any of CALL/BAND/MODE/FREQ/QSO_DATE/
  TIME_ON change, the key is recomputed. A new key that collides with
  another QSO in the same logbook returns 409 `duplicate_key`. No
  `force=true` bypass on edit — edit is never allowed to create a
  duplicate.
- **No parallel patch struct.** PATCH accepts a JSON body matching
  the canonical `types.Qso` shape. `json.Unmarshal` overlays present
  keys onto a copy of the existing QSO; missing keys leave fields
  alone. Adding an ADIF field to `types.Qso` automatically becomes
  editable via PATCH with no second change.

### DELETE is soft-delete only (session 9)

`DELETE /v1/qso/:id` flips `deleted_at`. The daemon's job is "log +
forward"; any hard-delete / purge tooling is a logbook-app concern.
Soft-deleted rows are hidden from `FindQso` (sqlboiler's generated
WHERE clause filters `deleted_at IS NULL`), so subsequent GETs
return 404. The partial unique index on `dedupe_key` is scoped
`WHERE deleted_at IS NULL`, so soft-deleting a QSO frees its dedupe
key — the same (call, band, mode, freq, date, time) can be re-logged
after deletion.

### FREQ is MHz on the external surface (session 9)

`types.Qso.Freq` was storing the integer-kHz string, leaking a
storage unit out through the HTTP API and the "QSO stored" log line.
Fixed: `types.Qso.Freq` is the ADIF-native MHz decimal string
(e.g. `"14.074"`) everywhere above the adapter; the sqlite adapter
is the only place that knows about integer-kHz storage.

- `utils.ParseFreqMHz(string) (int64, error)` and
  `utils.FormatFreqMHz(int64) string` are the kHz↔MHz bridge,
  co-located with the other freq helpers.
- The old `qsoservice.FreqMHzToKHzString` helper was removed.
- The sqlite `freq` column is still INTEGER kHz (per v2-design
  decision: SQLite likes integers for sortable/indexable storage;
  translation happens in the daemon).
- Dedupe-key hash still uses the int-kHz string internally for
  deterministic numeric normalization ("7.050" / "7.0500" / "7050"
  all collapse to the same integer).

### Cursor-based QSO list pagination (session 9)

`GET /v1/logbook/{id}/qso?after=<cursor>&limit=<N>` per api.md §4.4.
Forward-only, DESC sort on `(qso_date, time_on, id)` — newest first.
Cursor is opaque base64url-encoded JSON `{"d","t","i"}`. Response
shape: `{"items": [...], "next_cursor": null | "<token>"}`.

- `ServerConfig.DefaultPageLimit` (50) and `ServerConfig.MaxPageLimit`
  (500) added. Clients that omit `?limit` get the default; values
  above max are silently clamped; non-positive values are 400.
- Soft-deleted rows are hidden (sqlboiler default).
- Opt-in "show deleted" is deferred — logbook-app concern per the
  narrow-daemon invariant. When the logbook-app is built we'll add
  `?include_deleted=true` symmetrically across GET/LIST.

### Contest-dupe endpoint (session 9)

`GET /v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>&mode=<mode>`
returns `{"duplicate": bool}`. Mode is optional: omit for band-only
contests (ARRL DX), include for band+mode contests (CQ WW).

- Narrow purpose-built endpoint rather than filters on the list
  endpoint — contest operators hit this path hard and the answer
  is a single boolean.
- **Contest isolation via logbook-per-contest, not separate DB
  file.** The logbook abstraction is designed for exactly this
  partitioning; contest-dupe queries are `WHERE logbook_id = ?` so
  they're inherently scoped to the contest's logbook with no
  cross-contamination. See the `project_sm_session_scope.md` memory
  for the related "logging session stays client-side" decision.
- `IsContestDuplicateByLogbookIDWithContext` widened to take an
  optional `mode` argument.

### QSO submit path tightened (session 8)

The submit endpoint now requires `?logbook=<id>` and validates:
- The logbook exists (404 if not)
- The logbook's callsign matches STATION_CALLSIGN (400
  `callsign_mismatch` if not)
- Auto-create logic removed — logbooks must be created explicitly
  via `POST /v1/logbook` before QSOs can be submitted

### Code style decisions (session 8)

- **`errors.Op` convention standardised:** all ops use
  `const op errors.Op = "package.FuncName"` pattern. Handler ops
  changed from URL-path-style (`api.v1.qso.submit`) to function-name
  style (`api.handleSubmitQso`).
- **`writeError` uses `errors.Op`** not plain string for the op
  parameter and the `ErrorResponse.Op` field.
- **No `fmt.Errorf` in packages that use `internal/errors`** — the
  `errors.New(op).WithErr(err).WithMsg(...)` pattern is the standard
  for all error paths.

### Listener protocol is config-driven

`ServerConfig.Protocol` (default `"unix"`, alternative `"tcp"`)
controls `net.Listen`. The stale-socket cleanup only runs for Unix
protocol. This keeps the door open for network deployment (daemon on
a Pi) without any code changes — just a config change.

### Dev environment

- `build/config.json` — dev config with debug logging
- `build/db/` — sqlite database
- `build/log/` — daemon log files
- `task run` — builds and runs the daemon using `SM_WORKING_DIR`
  from `.env`
- `task build` — compiles all packages + daemon binary
- `.github/workflows/ci.yml` — CI passes cleanly on GitHub

### Repo state

**Branches:**
- `main` — milestone 1b complete, session-10 hardening landed. CI green.
- `v1` at `0e158ec` — unchanged since session 2.

---

## What happened in session 10 (2026-04-17)

### Goals set for the session

- Finish the SQL call-site audit items 1–2 (started late session 9).
- Do a full pre-forwarder code review to catch drift/bugs before
  the much larger forwarder subsystem lands.
- Address everything the review surfaces.

### What got done

1. **SQL audit wins (items 1–2 of the session-9 list).**
   - Added `LogbookCallsignByIDWithContext` — `SELECT callsign …`,
     skips full-row scan + adapter; submit hot path now uses it.
   - Added composite partial index
     `idx_qso_logbook_date_time ON qso (logbook_id, qso_date DESC,
     time_on DESC, id DESC) WHERE deleted_at IS NULL` for cursor
     pagination. `EXPLAIN QUERY PLAN` confirms the planner seeks
     directly on the index with no temp B-tree for ORDER BY.
   - Added directly to `0001_init.up.sql` rather than a new
     migration file — pre-1.0, no data to migrate.

2. **Export-endpoint reversal.** Previously-nominated
   `POST /v1/logbook/{id}/export` dropped from the roadmap.
   Rationale in the session-scope memory and in `api.md` §5.
   ADIF is a client/admin concern; daemon's backup story is
   forwarding to online services.

3. **Full milestone-1b review** (`docs/reviews/milestone-1b-review.md`).
   Independent agent pass with CLAUDE.md + memory files as context.
   23 findings: 2 high, 9 medium, 12 low. All addressed in the
   same session:

   **High:**
   - **H1**: concurrent-submit race (two workers passing the pre-tx
     dedupe check, second losing on UNIQUE index, surfacing as 500
     instead of 200-duplicate). Fixed with constraint-error catch
     and re-query. Deterministic regression test added.
   - **H2**: dead `qsoservice.FreqMHzToKHzString` still in tree
     despite session-9 handoff claiming removal. Deleted. `math`
     import dropped with it.

   **Medium:**
   - **M1 + M2**: shared `readBody` / `readJSONBody` helpers on
     `*Server`; logbook POST/PATCH now honour `MaxBodyBytes`;
     `*http.MaxBytesError` detected via `errors.As` instead of
     stdlib string match.
   - **M3**: SQL schema comment and `types.Qso.DedupeKey` docstring
     now name FREQ in the dedupe-key list.
   - **M4**: `sqlite.Service.Close` resets `initOnce` +
     `isInitialized` so re-init cycles work. Cycle test added.
   - **M5**: dead `ExecContext` + `QueryContext` deleted (also
     eliminates the context-cancel leak).
   - **M6**: `qsoservice.Update` is now transactional, mirroring
     Submit's shape. Commented forwarder hook in place for the
     future `InsertQsoUploadTx(action.Update)` loop.
   - **M7**: all 9 residual `fmt.Errorf` sites in the sqlite
     package converted to the `errors.New(op).WithErr(err).WithMsg(...)`
     pattern. Two `fmt` imports dropped.
   - **M8**: `uploadRetryCooldown` annotated with a `TODO(forwarder)`
     pointer naming the expected config shape
     (`ForwarderConfig.RetryCooldownSec`). Deferred to the
     forwarder PR by design.
   - **M9**: four handler tests moved off English-message substring
     matching onto structured decode (`ErrorResponse`/`types.Logbook`).

   **Low (all 12 addressed):**
   - **L1**: `writeJSON`/`writeError` are now methods on `*Server`
     with encode-error logging; 81 call sites converted.
   - **L2**: `Server.Shutdown` removes the Unix socket file
     (best-effort, gated on `s.protocol == "unix"`).
   - **L3**: "smd stopped" log moved above `loggerSvc.Close()`.
   - **L4**: `main` now delegates to `run() error` with
     defer-based cleanup; `fatal()` deleted.
   - **L5**: `LIMIT 1` in `SchemaVersionWithContext` annotated as
     defensive.
   - **L6 (broadened)**: contact-history LIKE pattern changed
     from `X%` to `X/%` — anchors on slash, matches portable
     variants (M0CMC/P) but excludes coincidental prefixes
     (M0CMCE). Two new regression tests.
   - **L7**: `missingCoreTables` checks `rows.Err()` after the
     `for rows.Next()` loop.
   - **L8**: `validTestQso` uses canonical MHz `"7.050"` instead
     of legacy kHz `"7050"`.
   - **L9**: sqlite `doc.go` lifecycle description corrected —
     Migrate is a distinct call, Close resets init guard.
   - **L10**: `config_test.go` now asserts `DefaultPageLimit=50`,
     `MaxPageLimit=500`, `MaxContactHistoryResults=100`.
   - **L11**: 8 `fmt.Sscanf` JSON-substring-matching sites
     converted to `unmarshalJSON` + structured decode.
   - **L12**: `adif.parseRecords` error return dropped (dead
     path; caller check collapsed).

4. **Panic handling added** (post-review, user-initiated).
   - `cmd/smd/main.go`: `ExitError` / `ExitPanic` constants (ExitOK
     is implicit — Go's default on clean return). `main()` wraps
     `run()` with a `defer recover()` that prints a `PANIC:`-prefixed
     stderr line + `debug.Stack()` and exits `ExitPanic`. `run()`'s
     own defers (logger close, dbSvc close) still fire first as the
     panic unwinds through its frame.
   - `internal/api/middleware.go`: new `recoverPanic` middleware on
     `*Server`. Wraps the mux so any panic in a handler logs through
     `logging.Service` with panic value + stack + method + path, then
     writes a generic 500 `internal_error` envelope. The panic value
     is deliberately NOT surfaced to the client (could leak
     internals; full detail stays in the log).
   - Two regression tests (`TestRecoverPanic_CatchesAndReturns500`,
     `TestRecoverPanic_NoPanicPassesThrough`) — including a canary
     assertion that the panic message doesn't bleed into the
     response body.
   - Worker-goroutine recovery (`safeGo` helper) intentionally
     deferred until the forwarder PR spawns its first worker — the
     pattern template is noted here so the forwarder author can
     copy it from `recoverPanic`.

5. **`goccy/go-json` dropped from the dependency tree** (user pref).
   - Two adapter files (`internal/database/sqlite/adapters/model_to_type.go`
     and `type_to_model.go`) switched from `github.com/goccy/go-json`
     to stdlib `encoding/json`. Drop-in — `Marshal` / `Unmarshal`
     signatures are identical. `go mod tidy` removed the dependency
     from both `go.mod` and `go.sum`.
   - Rationale: at this daemon's scale (~146 QSO/s per stress test)
     the performance delta is below the noise floor; stdlib preference
     per CLAUDE.md; one fewer external dep to carry. The adapter's
     prior use of goccy was inherited from sqlboiler-generated
     idioms, not a deliberate choice.

### Coverage summary end-of-session

All tests green under `-race` after every finding. One new test
family:
- `TestSubmitQso_ConcurrentDuplicate` — H1 regression.
- `TestService_InitOpenCloseInitOpen` — M4 cycle regression.
- `TestCreateLogbook_BodyTooLarge`, `TestUpdateLogbook_BodyTooLarge`,
  `TestCreateLogbook_InvalidJSON` — M1 regressions.
- `TestLogbookCallsignByID` — new sqlite helper.
- `TestContactHistory_PortableSuffixMatches`,
  `TestContactHistory_CoincidentalPrefixExcluded` — L6 regressions.
- `TestRecoverPanic_CatchesAndReturns500`,
  `TestRecoverPanic_NoPanicPassesThrough` — panic-handling
  middleware (post-review).

### Design decisions made / reaffirmed

- **No daemon-side ADIF export endpoint.** Captured in
  `memory/project_sm_session_scope.md` as explicit "do not propose."
- **`qsoservice.Update` shape matches Submit** (tx envelope).
  Forwarder-hook placeholder inside the tx makes the later
  extension mechanical.
- **MaxBodyBytes is enforced on every handler that reads a body.**
  `readBody` / `readJSONBody` are the single enforcement point.
- **Contact-history prefix match is portable-only** (`X` OR
  `X/suffix`). The looser `LIKE X%` shape is gone.
- **`cmd/smd/main.go` follows the `run() error` pattern.**
  Cleanups are defers; startup failures unwind them in LIFO order.
- **Panic handling is two-layered.** Top-level `main` defer catches
  anything that escapes `run()` and exits with `ExitPanic` (2) so
  process supervisors can distinguish it from startup errors
  (`ExitError`, 1). A `recoverPanic` middleware on the HTTP mux
  catches handler panics, logs them structurally, and returns a
  generic 500 envelope (panic value stays server-side).
- **`encoding/json` is the only JSON library.** Dropped
  `goccy/go-json`. At this scale stdlib is fine and the "minimise
  external deps" rule wins over marginal throughput gains.

### Parked follow-ups

- SQL audit item 3 — dead-method sweep of the six sqlite methods
  with only test callers (`FetchContactedStationByCallsign`,
  `FetchCountryByCallsign`, `FetchCountryByName`,
  `FetchPendingUploads`, `UpdateQsoUploadStatus`,
  `FetchQsoSliceByLogbookId`, `FetchQsoCountByLogbookId`). The
  last two forwarder-queue methods will get real callers when the
  forwarder lands. The enrichment ones will get real callers in
  milestone 2.
- SQL audit item 4 — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history queries under
  `?logbook=` filter. Flagged under "wait for a concrete problem."

---

## What happened in session 9 (2026-04-17)

### Goals set for the session

- Implement milestone 1b step 2 (QSO fetch/edit/delete)
- Extend the stress test to exercise new read/edit/delete paths

### What got done

1. **`GET /v1/qso/{id}`** — `handleGetQso` in
   `internal/api/handler_qso.go`. Parses `{id}`, calls
   `FetchQsoByIdWithContext`, maps `ErrNotFound` → 404. Soft-deleted
   rows return 404 because `FindQso` already filters
   `deleted_at IS NULL`.

2. **`PATCH /v1/qso/{id}`** — `handleUpdateQso` + `qsoservice.Update`
   in `internal/qsoservice/update.go`. First iteration built a
   `QsoPatch` struct with pointer fields per editable attribute; was
   torn out and rebuilt to use `types.Qso` directly via
   `json.Unmarshal` overlay + stash-restore of immutables. The
   rewrite prevents drift with `types.Qso` and honors the "adding an
   ADIF field is a one-line change" invariant. Validation errors
   come back as `*SubmitError`; collision → `duplicate_key` → 409.
   No `force=true` bypass.

3. **`DELETE /v1/qso/{id}`** — `handleDeleteQso` + sqlite
   `DeleteQsoByIDWithContext`. Soft-delete via sqlboiler's
   `qso.Delete(ctx, h, false)`. Returns 404 if the QSO doesn't exist
   or is already soft-deleted. No FK check — QSO is the child. Test
   `TestDeleteQso_FreesDedupeKey` locks in the behavior that
   soft-deletion frees the dedupe-key slot (thanks to the partial
   unique index `WHERE deleted_at IS NULL`).

4. **FREQ added to dedupe-key inputs** —
   `ComputeDedupeKey(call, band, mode, freq, qsoDate, timeOn)`.
   Tests, call sites (submit + update), and the `dedupeChanged`
   check in `Update` all updated in lockstep. See "FREQ added to
   dedupe-key inputs" above for the rationale.

5. **`IsSubmitError` uses `errors.As`** instead of a direct type
   assertion. Future-proofs against anyone wrapping a `*SubmitError`
   with `%w` or the `internal/errors` builder.

6. **Stress test expanded** — `TestStress_20Clients_50QSOs` now runs
   submit → fetch (verify call) → PATCH(freq) (verify dedupe-key
   recomputed) → DELETE (verify 204, verify subsequent GET 404) per
   QSO. 1000 QSOs, zero errors across all four operations, race
   detector clean. End-to-end round trip ~17–18 ms.

7. **Types-package audit** — searched for exported types outside
   `internal/types` that cross package boundaries. Concluded (with
   user agreement) that types move into `internal/types` only when
   there is actual cyclic-dependency risk, not prophylactically. No
   migrations made; `adif.Record`, `qsoservice.SubmitResult`,
   `qsoservice.SubmitError` stay in their own packages.

8. **FREQ leak fix — MHz is the canonical external form.**
   `types.Qso.Freq` was holding the integer-kHz string, so HTTP
   responses and log lines returned `"14074"` instead of `"14.074"` —
   violating the ADIF-follows-spec invariant. Fix: added
   `utils.ParseFreqMHz` / `utils.FormatFreqMHz`, moved the
   MHz↔kHz boundary into the sqlite adapter, made `types.Qso.Freq`
   canonical MHz everywhere above the adapter. DB column stays
   INTEGER kHz (user decision: SQLite prefers integers for sortable
   storage). `qsoservice.FreqMHzToKHzString` removed; dedupe-key
   hash still uses the int-kHz string internally for determinism.
   Adapter tests had nonsense `Freq: 14250000` values (14.25 GHz
   in kHz) — fixed to realistic `14250` / `"14.250"` in the process.

9. **`IsContestDuplicateByLogbookIDWithContext` widened** to take
   an optional `mode` argument for band+mode contests, in
   preparation for the contest-dupe endpoint.

10. **`GET /v1/logbook/{id}/qso`** — forward-cursor pagination.
    New sqlite method `FetchQsoPageByLogbookWithContext` uses a
    tuple predicate on `(qso_date, time_on, id)` DESC and fetches
    `limit+1` to detect "has more" cheaply. Handler encodes/decodes
    an opaque base64url JSON cursor. `ServerConfig.DefaultPageLimit`
    (50) and `ServerConfig.MaxPageLimit` (500) added. Nine tests
    including a three-page walk with full ordering reconstruction
    and soft-delete-hidden assertion.

11. **`GET /v1/contest-dupe`** — narrow purpose-built endpoint.
    Validates `logbook`, `call`, `band` (required) and `mode`
    (optional). Returns `{"duplicate": bool}`. 15 tests covering
    band-only / band+mode hits and misses, soft-delete exclusion,
    logbook-scoping (hit in logbook A must NOT match in logbook B —
    the whole point of the logbook-per-contest pattern), and all
    validation error paths.

12. **`GET /v1/contact-history`** — "have I ever worked this call"
    lookup for the logging-app's draft panel. Required: `?call=`.
    Optional: `?logbook=` to restrict to a single logbook (default
    scope is all logbooks). Returns `{"items": [...]}` capped at
    `Server.MaxContactHistoryResults` (default 100). 10 tests
    including a **latent-bug fix** in the underlying sqlite query:
    the existing `Call = ? OR Call LIKE ?%` group was not
    parenthesised, so AND-ing additional predicates (logbook_id,
    the implicit `deleted_at IS NULL`) bound tighter than the OR
    and silently leaked rows. Wrapping the OR in `qm.Expr(...)`
    fixed it. The old code had the same issue but no test
    exercised it, so nothing caught the leak.

13. **`GET /v1/version`** — diagnostic. Returns
    `{"daemon":"<build>","go":"<runtime>","schema":{"version":N,"dirty":bool}}`.
    The daemon build version comes from `cmd/smd/main.go`'s
    package-level `Version` var, overridable via
    `go build -ldflags "-X main.Version=..."`. Schema version is
    pulled from `schema_migrations` (golang-migrate's table).
    `api.New` signature extended to accept `daemonVersion string`.

### Coverage summary end-of-session

| Package | Coverage |
|---------|----------|
| `internal/api` | full CRUD + list + contest-dupe handler tests; 1000-QSO stress round trip |
| `internal/qsoservice` | `Update` and `Submit` exercised via api tests; dedupe unit tests cover freq |
| `internal/database/sqlite` | new `DeleteQsoByIDWithContext`, `FetchQsoPageByLogbookWithContext`; widened contest-dupe method |
| `internal/utils` | new freq-conversion helpers with round-trip tests |

Full suite race-detector clean.

### Design decisions made

- **`types.Qso` is the canonical DTO** for HTTP/service boundaries.
  Do not build parallel `XPatch` / `XRequest` structs that duplicate
  field lists from `types.X`. Use `json.Unmarshal` overlay + stash-
  restore of immutables instead. Captured as a memory.
- **types package rule is pragmatic, not prophylactic.** Exported
  types move to `internal/types` only when an actual cycle could
  form, not as a preventive measure. Captured as a memory.
- **FREQ is part of QSO identity.** Dedupe key now includes FREQ per
  ADIF-spec guidance. Schema unchanged.
- **FREQ on the external surface is MHz** (ADIF-native). kHz is the
  sqlite storage unit; translation lives in the adapter, not
  anywhere above it.
- **PATCH design:** immutable fields always restored server-side,
  dedupe inputs recomputed on change, collision rejected with 409,
  no force bypass on edit.
- **DELETE is always soft-delete at the daemon.** Hard-delete stays
  a logbook-app concern.
- **Pagination is forward-cursor only**, newest-first, opaque token.
  Soft-deleted rows are always hidden. Opt-in "show deleted" is
  deferred until the logbook-app needs it.
- **Contest isolation via logbook-per-contest, not separate DB
  file.** `logbook_id` partition gives the contest-dupe endpoint
  false-positive-free scoping by construction.
- **Logging session is entirely client-side.** No `session_id`
  column, no `/v1/session` endpoints. The logging app keeps an
  in-memory list of QSOs submitted since Start, uses existing
  PATCH/DELETE for edits, and formats the end-of-session email
  payload client-side from data it already has (or re-fetches via
  `GET /v1/qso/{id}`). Captured as a memory.
- **No daemon-side ADIF export endpoint.** Export is a
  client/admin concern, not a daemon concern. Clients that need
  ADIF page through the QSO list and serialize client-side using
  `internal/adif` (which is a regular Go library, not HTTP-wrapped).
  The "real" backup story is forwarding to online services
  (QRZ, LoTW, SM-online) — that's the daemon's redundancy
  mechanism. Filesystem backup of the sqlite file is a user/OS
  concern.

---

## Session 8 (2026-04-17, compressed)

Implemented milestone 1b step 1 (full logbook CRUD: list, get,
create, update, delete with FK-safe soft-delete). Tightened QSO
submit to require `?logbook=<id>` with existence + callsign-match
validation. Standardised `errors.Op` pattern across handlers
(`api.FuncName`). Added `IsValidCallsign` at three layers (schema
CHECK, handler, domain). Fixed `UpsertLogbookWithContext` latent
no-op-on-existing bug. Listener protocol made config-driven
(`unix` / `tcp`). 20-client × 50-QSO stress test green with
~146 QSO/s baseline. sqlite package coverage 0% → 66.9%.

Design decisions fixed in this session and carried forward:
logbooks are created explicitly (no auto-create); logbook
callsign is immutable after creation; workflow-driven milestone-1b
order (fetch/edit/delete before enrichment).

---

## Session 7 (2026-04-16, compressed)

Reviewed all 8 carry-forward packages and wrote
`docs/v2-design/milestones.md`. Implemented milestone 1: daemon,
config, qsoservice (validation/dedupe/atomic write), API handlers,
error envelope, healthz. Dev environment + GitHub Actions CI.
5 bugs fixed during testing. 25 tests across 4 packages. Schema
cleanup (removed session table/apikey, added dedupe_key column).

---

## Sessions 1–6 (compressed summaries)

### Session 1 (2026-04-14)

v2 rewrite decision. Completed v1 analysis (5 docs). Tagged
`pre-ft8-removal` and `v1.0.0`. Created `v1` branch. Three v1 bug
fixes before tagging.

### Session 2 (2026-04-15)

Wrote `docs/v2-design/structure.md` (6 structural decisions). Rewrote
`CLAUDE.md`. Deleted v1 CI workflows. Commit `5ef55c1`.

### Session 3 (2026-04-16)

Big restructure: reshaped main into v2 milestone-1 layout. 730 files
changed, 68,934 deletions. Scaffolded `cmd/smd`, `internal/api`,
`internal/qsoservice`, `internal/config`. Commit `0010b6e`.

### Session 4 (2026-04-16)

Short session. Added `Taskfile.yml`. Deleted remaining v1 leftovers.
Commit `1ee2ced`.

### Session 5 (2026-04-17)

Wrote `docs/v2-design/api.md`. Strengthened invariants. Full
`internal/errors` review (11 findings). Wrote `internal/logging`
review doc (14 findings). Two feedback memories.

### Session 6 (2026-04-18)

Applied all 14 `internal/logging` findings. Fixed embedding bug.
Both `internal/errors` and `internal/logging` reached v2 final state.

---

## Next steps (priority order)

### The immediate next action (post-review, pick a phase)

QRZ port complete, review triage complete, Task #29 (cmd/smd/main.go
tests) complete in session 14, SSE event stream complete in session
14. The forwarding subsystem + its live notification surface is
**done** — the next session picks one of three directions below.

My standing recommendation is a **daemon-only alpha checkpoint**:
cut a tagged build, dogfood via curl + SSE + the existing HTTP
endpoints, and use the results to inform the next subsystem
choice (a second real forwarder vs. bridge/CAT vs. client work).
The forwarding + events surface is the minimum viable
daemon-side feature set; running it against real QSOs for a
week will surface gaps cheaper than guessing at the next
subsystem. If alpha feels premature, the second-best option is
a second real forwarder (ClubLog or LoTW) — it validates the
"prefix-agnostic plumbing" claim and gives the SSE stream more
to say. Bridge/CAT is a larger effort with its own design doc
still to write.

The 8-stage QRZ plan is retained below for historical context;
do **not** re-derive the design decisions captured in it.

**QRZ API reference** (from the operator's paste of QRZ's developer
guide — use this, not an inferred version):

- Endpoint: `https://logbook.qrz.com/api`, HTTP POST with
  `application/x-www-form-urlencoded`.
- User-Agent header required (≤128 chars, should include callsign
  + app name for identifiability).
- **INSERT**: `ACTION=INSERT`, `KEY=<apikey>`, `ADIF=<single-record>`.
  Response: `RESULT=OK|FAIL|REPLACE` + `LOGID` + `COUNT`.
- **UPDATE**: no native update — use `ACTION=INSERT` +
  `OPTION=REPLACE`. Response `RESULT=REPLACE` when it overwrote a
  duplicate. This is what v1 did.
- **DELETE**: `ACTION=DELETE`, `LOGIDS=<id>` (comma list for many).
  Response: `RESULT=OK|PARTIAL|FAIL` + `COUNT`.

**Resolved design decisions** (don't re-open):

- **`Forwarder.Submit` signature**: `(ctx, qso, action, priorUpstreamID string)`
  (stage 1). Worker populates `priorUpstreamID` from the prior
  insert row's `upstream_id` for delete actions only.
- **`Forwarder.AdifPrefix()`** (stage 1). QRZ returns `"QRZCOM"`.
  Worker stamps `QRZCOM_QSO_UPLOAD_STATUS="Y"` +
  `QRZCOM_QSO_UPLOAD_DATE=today` on success (insert/update, not
  delete — soft-deleted QSOs don't export). Failures/transients
  stamp nothing.
- **Delete LOGID wiring**: option A from the session-12 discussion.
  Worker does a DB lookup before `Submit`; forwarder receives LOGID
  via `priorUpstreamID`; empty lookup → terminal "no upstream id
  for delete".
- **QRZ credentials shape**: `{"api_key": "..."}` only — QRZ
  enforces the callsign/logbook match server-side, so a local
  `callsign` field would only introduce drift risk without a
  guarantee. (stage 2, landed)
- **QRZ response classification** (stage 3, landed): per-action
  matrix in `response.go` and `forwarding-implementation.md` §8.1.
  Short form: `RESULT=AUTH` → Terminal (global); `RESULT=OK` /
  `RESULT=REPLACE` → Success with `UpstreamID = LOGID`;
  `RESULT=FAIL` on delete → **Success** (idempotent);
  `RESULT=FAIL` elsewhere → Terminal; `RESULT=PARTIAL` / unknown
  on any action → Terminal; missing `LOGID` on claimed-OK insert →
  Terminal. Transport-level errors (HTTP 4xx/5xx, network, timeout)
  are classified at the `Submit` call site in stage 4 — network
  and 5xx/429 → Transient, 4xx → Terminal.
- **Retry-defaults ownership** (stage 7): each forwarder package
  exports `var DefaultRetry types.RetryConfig`.
  `spawnForwarderWorkers` in `cmd/smd/main.go` looks it up by type.
  Delete the `defaultForwarderRetry` temporary fallback.
- **Test creds**: operator has a QRZ test logbook with `USER` and
  API key in env vars. Used for manual integration verification
  after code lands — **not** for automated tests.
- **Automated tests**: `httptest.NewServer` everywhere, hermetic
  and CI-safe.

**Remaining stages** (each is a committable unit):

| # | Stage | Status |
|---|-------|--------|
| 1 | Extend `Forwarder` interface (`AdifPrefix`, `priorUpstreamID`) | **done** (session 12) |
| 2 | `internal/forwarding/qrz/` skeleton — credentials struct (`api_key` only), `New`, `Type()="qrz"`, `AdifPrefix()="QRZCOM"`, registry init, stubbed Submit, validation tests | **done** (session 13) |
| 3 | Response parser + classification function — `parseResponse` + `classifyResponse` with per-action helpers (`classifyInsert`/`Update`/`Delete`); `AUTH` global, single-LOGID-delete `FAIL` → Success; 26 unit tests | **done** (session 13) |
| 4 | Insert + update `Submit` — real HTTP, `buildForm` + `classifyHTTPStatus`, `DefaultEndpoint`/`DefaultHTTPTimeout`/`UserAgent`, package-internal `newWithEndpoint`; 18 httptest tests + live harness (`TestLive_InsertThenUpdate` quick, `TestLive_InteractiveFlow` with `/dev/tty` pauses); live-validated against real QRZ | **done** (session 13) |
| 5 | Delete `Submit` + worker LOGID lookup — `FetchInsertUpstreamIDWithContext` (defensive ORDER BY, UNIQUE-constraint-aware), worker `resolvePriorUpstreamID` short-circuit, QRZ `buildForm` delete branch; CI fix for `:memory:` + `-race` flake (DSN `cache=shared`); live harness delete via `Submit` | **done** (session 13) |
| 6 | ADIF-stamp wiring — `MarkUploadSuccessWithAdifStampWithContext` writes both the qso_upload transition and a `json_set` stamp on `qso.additional_data` in one tx (no new columns; matches the "additional_data absorbs ADIF spec evolution" invariant); worker `markSuccess` dispatch gates on AdifPrefix + action; prefix-agnostic so new forwarders land without sqlite/migration changes | **done** (session 13) |
| 7 | Retry-defaults ownership refactor — per-forwarder `DefaultRetry` vars, `forwarding.RegisterDefaultRetry` / `DefaultRetryFor` registry companions, `spawnForwarderWorkers` lookup-by-type + loud error for missing defaults, hardcoded `defaultForwarderRetry` deleted | **done** (session 13) |
| 8 | Import `internal/forwarding/qrz` in `cmd/smd/main.go` (regular import — main sets qrz.UserAgent); wired `qrz.UserAgent = "station-manager/" + Version` and `adif.ProgramVersion = Version` at the top of run(); flipped `adif.ProgramVersion` from const to var; ldflags smoke-check passes | **done** (session 13) |

### Follow-ups after the QRZ port

1. **Alpha checkpoint.** Tag a build, dogfood the daemon against
   real QSOs for a week: ingest via `POST /v1/qso` (curl or a
   disposable script), QRZ forwarding on, SSE stream tailed with
   `curl -N` or a browser `EventSource`. The forwarding +
   events surface is the smallest self-contained daemon-side
   feature set; real use will surface gaps cheaper than guessing.
   **My standing recommendation for the next phase.**

2. **A second real forwarder (ClubLog / LoTW / eQSL)**. Exercises
   the "prefix-agnostic generic plumbing" claim. Would validate
   the registry + `DefaultRetry` ownership pattern in anger. Also
   a good smoke test for whether the stage-6 ADIF-stamp json_set
   generalises as cleanly as we think it does.

3. **Bridge / CAT design — substantial progress session 15, now at a
   decision point.** Design is in `docs/v2-design/bridge.md`, rewritten
   in-session from a two-frontend shape to a much smaller Unix-socket-only
   SM-internal multiplexer. The live question is **§6 YAGNI: build now or
   defer?** User lean at session end is *defer*, with `internal/cat` given
   a pluggable transport abstraction (§8.3) so the deferred path costs
   nothing. Recommended next-session work order:

   **a. Answer §6.** Everything else depends on this.
   **b. If deferred:** settle §8.3 (`internal/cat` transport abstraction
      shape) as a design-only exercise. This unblocks the logging app for
      milestone 2 without foreclosing the bridge.
   **c. If built now:** sequence is (i) `internal/cat` transport abstraction,
      (ii) NDJSON schema (§8.1), (iii) bridge implementation, (iv) logging
      app wired through `SocketTransport`, (v) defer CAT control app to its
      own design session.

   My recommendation: **defer the bridge, but do §8.3 now.** Keeps the
   logging app on the fastest path (direct `SerialTransport`) and makes the
   eventual switch to a bridge mechanical.

### Parked follow-ups (low priority, not blockers)

- **Dead-method sweep (SQL audit item 3).** Several sqlite methods
  have only test callers today. The former forwarder-queue
  candidates (`FetchPendingUploads`, `UpdateQsoUploadStatus`) have
  already been deleted in session 11 — they were v1 worker code,
  replaced by the stage-6 purpose-built methods. The remaining
  low-signal methods
  (`FetchQsoSliceByLogbookId`, `FetchQsoCountByLogbookId`,
  `FetchQsoByDedupeKey`'s no-context wrapper,
  `FetchContactedStationByCallsign`, `FetchCountryByCallsign`,
  `FetchCountryByName`) still need a specific "delete or keep"
  decision. Enrichment methods likely return in milestone 2; the
  QSO list helpers may be dead. Park until we know.
- **SQL audit item 4** — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history with
  `?logbook=` filter. Defer until a concrete performance
  complaint surfaces.

### v2 design work

- **Pick the ORM/generator approach** → `docs/v2-design/db-layer.md`.
  sqlboiler stays until there's a reason to change.
- **Multi-rig as first-class assumption** — bridge-side shape now
  captured in `docs/v2-design/bridge.md` (first-class from day one
  in the bridge). Data-model side (rig id on `types.Qso`, logbook
  schema impact) still open; address when rig control construction
  starts.

### Deferred features

- **Logging-app text-file fallback reconciliation** — milestone 2+.
- **Enrichment / contacted_station population** — milestone 2.
  Client-side concern; daemon submit path stays fast and network-free.
- **Daemon dashboard / monitoring UI** — post-milestone 2.

### v1 branch follow-ups

- Data race candidate fix (session 6) not yet verified on v1 branch.
- Hardcoded QRZ forwarder — v2 concern, unlikely to be fixed on v1.

### Maintenance

- Compress session 9 after session 11 lands (session 8 is already
  compressed at end of session 10).
- Update this file at end of every session.
