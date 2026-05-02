# v2 design — UI toolkit choice (resolved)

**Status:** **Resolved 2026-04-30 in [ADR 0001](../decisions/0001-ui-toolkit-browser-spa.md): Option C (browser SPA, Svelte 5 + Vite, embedded into the daemon).** Scaffold landed and verified end-to-end the same day. SPA development is ongoing in `frontend/logging/`; see [frontend-spa.md](frontend-spa.md) for the SPA shape, embed wiring, and build pipeline. This document is preserved as the analysis record that led to the decision; the option-comparison and rationale below are unchanged.

The progression: Wails (v1, never built in v2) → Gio (decided 2026-04-21, spike landed in `cmd/giospike/`, parked) → Svelte 5 SPA (decided 2026-04-30 per ADR 0001).

## The concern

Operator working-mode preference is "small steps, no cognitive debt" (recorded in session-handoff session 21 and `feedback`-class memory). Gio's constraint model + op-stack drawing + lack of stock widgets (no native dropdown, no popup primitive, every-edge-or-no-edge borders) means each new UI element involves a new Gio concept. The learning curve is the cost; the question is whether it's the right cost to pay for what SM v2's UI actually needs to do.

The "Gio gives us native rendering performance" framing turns out not to apply to SM specifically — see [cat-performance.md](cat-performance.md) and below.

## What the UI actually does

Per the [topology decision](topology.md), the v2 logging client is:

- A long-running surface that subscribes to live rig state from the bridge over SSE/WebSocket and renders frequency / mode / VFO.
- A form for QSO entry (callsign, RST sent/rcvd, mode, name/qth/comment, date/times).
- A session list (client-side concept, no daemon endpoint).
- Buttons that POST to the daemon's REST API.

There is **no audio waterfall, no real-time DSP, no FT8 decode, no CAT loop in the UI process**. All of those live in the bridge or are out of v2 scope. The UI's perf budget is "render a frequency text update at 10–20 Hz." Any toolkit handles that easily.

## Options considered

### A — Stick with Gio (current decision)

**Pros:** pure Go, single binary, no webview runtime variance across platforms, work already started (status row, main row, mode picker, label-over-input cells in `cmd/logging/`). Constraint-model thinking transfers to other layout systems even if abandoned.

**Cons:** steep learning curve for a single-user personal tool. Every new widget shape (dropdown, modal, tab strip, scrollable list) is an opportunity to relearn Gio's primitives. Operator's "no cognitive debt" preference is fighting this.

### B — Wails v2 (the fallback)

**Pros:** known-good from v1, mature, ships as "an app" with dock icon and window decorations, frontend stack is anything (Svelte/Vue/React/plain TS), good Go-binding generation. Operator already knows it.

**Cons:** bundling tax (build pipeline, platform-specific webview SDKs), per-platform WebView variance (WebKit / WebView2 / WebKitGTK have subtle differences), v3 still moving. v2 is the version to use if going this way.

### C — Browser SPA hosted by the daemon (recommended)

The daemon **already is** an HTTP server (forwarding endpoints exist, more coming). Add static-file hosting (via `//go:embed frontend/dist`) and a Vite-built SPA. Open `http://localhost:8080/` in any browser.

**Pros:**

- **Simpler deployment** — no webview wrapper, no platform-specific SDKs, single binary still ships (SPA is embedded).
- **Standard web tooling works natively** — Vite HMR, browser devtools, network inspector, Svelte DevTools, no wrapping.
- **Three-tab model fits the architecture** — `localhost:8080/log`, `localhost:8080/logbook`, `localhost:8080/config` become three URL paths against the same daemon. This *is* the daemon + clients model with browser tabs as clients.
- **Mobile / tablet glance is free** — pull up the logbook on a phone on the same Wi-Fi without a second client implementation.
- **Reuses operator's existing TS/Svelte 5 preference** (recorded in `CLAUDE.md` code style notes).
- **Reversible** — if "feels like a real app" matters later, wrap the same SPA in a Wails shell. The SPA still talks to the daemon over HTTP; the wrapping is purely cosmetic.

**Cons:**

- Not "an app" in the dock — a tab in a browser. For a personal tool used at a desk this is genuinely fine; for a public product it would matter.
- Operator types `localhost:8080` to launch (or pins a tab / makes a desktop shortcut).
- Cross-origin call from SPA (daemon origin) to bridge (different host when bridge is on the rig PC and daemon is elsewhere) needs CORS. One config line on the bridge.

## Performance is not the deciding factor

For the UI-toolkit decision, the relevant question is "can the toolkit hit the operator's <300ms dial-spin perceptibility threshold." End-to-end the chain is:

| Stage | Typical | Notes |
|---|---|---|
| Rig internal + serial transport | 10–45 ms | Dominated by USB-serial latency timer (FTDI default 16 ms) and rig response time |
| CAT poll interval (avg = ½ period) | 50 ms (100 ms poll) | Eliminated entirely with rig auto-tx modes |
| Bridge → SSE dispatch (loopback) | <1 ms | TCP_NODELAY required, otherwise +40 ms |
| Bridge → SSE dispatch (LAN) | 1–10 ms | One Wi-Fi hop |
| Browser handle event + DOM update | <1 ms | Single text node, fast path |
| Frame sync to display | 8–17 ms | 60 Hz vsync; 120 Hz halves this |

Common case: ~95–115 ms total. Practical worst case: ~200–230 ms. The UI toolkit's contribution is **5–15 ms** in the frame paint stage. Going Gio over a browser saves ~10 ms in a 300 ms budget — invisible.

The real latency dominators (CAT poll cadence, USB-serial latency timer, async/auto-tx mode, TCP_NODELAY) are all bridge-side concerns and exist identically with both UI toolkits.

## Recommendation

**Go with C (browser SPA).** Picks the best-fit tool for what SM v2's UI actually does, removes the toolkit-choice problem entirely (any web stack works), uses operator's existing TS/Svelte 5 preference, removes a build-pipeline complication. The "cognitive debt" concern goes away because operator is back on familiar ground.

The Gio code in `cmd/logging/` is small (~30 lines per layout cell) and the constraint thinking transfers — switching costs are an evening, not a week.

## What changes if we pivot to C

- `cmd/logging/` becomes either deleted or a thin Wails shell (decide later; not load-bearing).
- New `frontend/logging/` (Svelte 5 + Vite + TS) — fetch + EventSource against the daemon and bridge.
- Daemon grows a `/v1/qso`, `/v1/lookup/{call}`, `/v1/forward/status`, etc. surface — but those endpoints are needed regardless of which UI consumes them.
- The CAT/rigloop event stream becomes an SSE endpoint on the **bridge** (per [topology.md](topology.md)), not on the daemon.

## What changes if we pivot to B (Wails)

- Replace `cmd/logging/main.go`'s Gio code with `cmd/logging/main.go` Wails entry point + `frontend/` Svelte project.
- Same backend service shape; different IPC mechanism (Wails events instead of SSE).
- Slightly less reusable than C (no mobile glance, no second-tab model, more bundling complexity).

## Decision status

**Resolved 2026-04-30: Option C (browser SPA, Svelte 5 + Vite, embedded into the daemon).** Operator confirmed Svelte 5 preference and the CI-builds-SPA-before-go-build stance. Detailed scaffold + wiring + build pipeline captured in [frontend-spa.md](frontend-spa.md). The Gio code in `cmd/logging/` is left in place until the SPA reaches feature parity, then abandoned cleanly.

## Cross-references

- [frontend-spa.md](frontend-spa.md) — the SPA scaffold, embed wiring, and build pipeline (the realisation of Option C)
- [topology.md](topology.md) — daemon/bridge/client peer model that the UI choice plugs into
- [cat-performance.md](cat-performance.md) — why the toolkit isn't a perf-driving factor
- `docs/v1-analysis/design-decisions-log.md` — original 2026-04-21 Gio decision
- Memory: `project_sm_ui_toolkit` — captures the 2026-04-30 Svelte 5 SPA decision (with the 2026-04-21 Gio choice and 2026-04-30 pivot recorded)
- ADR: [0001-ui-toolkit-browser-spa](../decisions/0001-ui-toolkit-browser-spa.md) — the canonical decision record
