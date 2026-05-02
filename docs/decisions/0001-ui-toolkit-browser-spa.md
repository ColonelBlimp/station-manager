---
number: 0001
title: UI toolkit — browser SPA hosted by daemon
status: Accepted
date: 2026-04-30
---

# 0001 — UI toolkit — browser SPA hosted by daemon

## Context

The v1 logging app (`cmd/logging`) was being built with [Gio](https://gioui.org/) — a Go-native immediate-mode UI toolkit, single-binary, no embedded webview. Through sessions 19–21, building even modest UI features (status row, three-column QSO entry row, mode-cycle widget) required learning Gio's `Constraints / Flex / Rigid / Flexed` model, single-edge border tricks via `clip.Rect + paint.ColorOp` (since `widget.Border` is all-four-or-nothing), and frame-by-frame layout reasoning. Each step landed cleanly with explanation — but the operator's stated working mode is "small steps, no cognitive debt," and Gio was generating cognitive debt faster than steps were retiring it.

By session 22 the operator had asked whether the toolkit choice itself was wrong. The forcing question: should we keep paying Gio's learning tax for a personal-use logging UI, or switch to a stack where building a form is a 30-second job?

## Decision

Build the v2 logging client as a **browser SPA (Svelte 5 + Vite + plain TypeScript + Tailwind CSS v4)**, embedded into the daemon binary via `//go:embed` and served by the daemon's HTTP layer. Unix-socket-only deployments stay supported (SPA route registered conditionally on `Protocol == "tcp"`).

## Alternatives considered

### Stick with Gio

Gio is single-binary, fast, no webview overhead, and the existing `cmd/logging` work would carry forward unchanged. Rejected because the marginal cost of every new UI feature was high — borders, focus management, and even "common-shape input row" required low-level reasoning the operator didn't enjoy and didn't want to acquire. For a *personal* tool with no shipping deadline, fluency with the toolkit matters more than the toolkit's runtime characteristics. The cost wasn't running Gio; the cost was *thinking in* Gio.

### Wails v2

Wails wraps a webview in a Go app — gives you HTML/CSS/JS frontend ergonomics with Go-side bindings via auto-generated bridge code. Considered as a middle-ground "browser ergonomics without daemon hosting." Rejected on three counts: (a) per-platform webview inconsistencies (WebView2 on Windows, WKWebView on macOS, WebKitGTK on Linux) reintroduce cross-platform debugging the SPA-in-daemon shape avoids by serving the same Chrome/Firefox the operator already uses; (b) Wails's Go↔JS bridge generates code that the daemon doesn't otherwise need, increasing the surface area without removing the HTTP API the v2 daemon already exposes; (c) deployment shape — Wails ships a desktop app, but the v2 daemon-and-clients model (per `docs/v2-design/topology.md`) wants the UI to be reachable from any browser on any device that can hit the daemon, including phones during portable operation.

### Browser SPA hosted by daemon (chosen)

Daemon serves `/v1/*` API and `/` SPA from the same HTTP server. SPA bundle is `//go:embed`-ed at build time. Browser is the operator's existing Chromium. No new runtime to learn beyond the framework. Multi-device naturally — laptop browser, phone browser, tablet browser all hit the same daemon URL. SPA development uses standard Vite HMR with proxy to the daemon for `/v1/*`. Accepted.

Within the SPA stack, **Svelte 5** was chosen over Vue and React on three grounds:

- **Compiled-reactivity DOM updates** suit a long-running tab consuming a 10–20 Hz rig SSE stream. Surgical updates of bound nodes only, no VDOM diff. Steady-state CPU stays near zero.
- **Bundle size for `//go:embed`.** Svelte typically ships 15–30 KB gzipped for an app this size; React/Vue ship 40–100 KB of runtime before app code. Smaller daemon binary, faster cold loads.
- **Operator's existing fluency.** Svelte 5 runes (`$state`, `$derived`, `$props`) are already in `CLAUDE.md` code-style notes. Picking the framework you know is the right move on a solo project — `lessons-for-v2.md`'s "build specific, not generic" extends to tooling.

Performance is not the *deciding* factor — every framework hits the 300 ms latency budget easily (per `docs/v2-design/cat-performance.md`).

### SvelteKit (sub-rejection within Svelte)

SvelteKit gives SSR, file-based routing, and adapters for Vercel/Netlify/Node — none apply when the SPA is `//go:embed`-ed and served from the daemon. Rejected as ceremony without payoff. Plain Svelte + Vite + a small client-side router (~3 KB or hand-rolled hash router ~50 lines) is the right shape.

## Consequences

**Signed up for:**

- Browser-only operating environment for the UI. Operators who want a single-binary desktop app don't get one until/unless we add a Wails wrapper later (still on the table — can wrap the same SPA bundle).
- Node.js in CI for SPA build — ~30 s overhead per build. Lockfile is committed; CI uses `npm ci` for deterministic resolution.
- TCP protocol becomes mandatory for SPA-serving deployments. Headless Unix-socket deployments stay supported via `cfg.Server.ServeSPA = false`.
- Daemon binary grows by the size of the SPA bundle (~50–80 KB gzipped at v1 scope) plus Go's embed overhead.
- Two-terminal dev workflow: `task run:smd` + `task frontend:dev`. Operator confused dev (`:5173`) and embed (`:8080`) URLs once during session 23 — captured in `frontend-spa.md` §"Dev workflow" so it doesn't recur.

**Accepted costs:**

- The Gio code in `cmd/logging` stays in place but is parked. It will be deleted cleanly when the SPA reaches feature parity, not pre-emptively (per `docs/v2-design/ui-toolkit.md`'s closing note).
- Browser security model: anything the SPA does goes through fetch/CORS, so the bridge must set CORS headers when the SPA talks to it directly (per `topology.md`).
- **Serial / CAT cannot live in the SPA.** Gio could have owned the serial port in-process; a browser SPA cannot. Web Serial exists but is Chromium-only with a per-page permission prompt — not viable for a daily-driver logging app. As a direct consequence, **CAT must run somewhere other than the browser** — either in the daemon binary as an internal subsystem (the v2 default per ADR 0013), or in a separately-deployed bridge process for split-host topologies. ADR 0012 originally treated bridge-as-separate-process as the load-bearing topology; ADR 0013 supersedes that and recasts the bridge as a daemon subsystem with the split-deployment shape preserved as an opt-in. This bullet records the causal chain (SPA choice → CAT cannot live with the UI → CAT lives daemon-side) so the constraint isn't re-litigated.

**Gained:**

- Form-heavy UI (logging) becomes ~30-second work per field instead of ~30-minute.
- Cross-device naturally — same daemon URL works from anything with a browser.
- Tailwind CSS v4 (settled later the same day) makes styling a single-line concern rather than a per-widget custom-paint exercise.
- **Free, native operator-controlled zoom and accessibility.** Gio (and Wails, and most native UI toolkits) bake fixed font sizes and field dimensions into the layout. The operator who needs a larger interface — older eyes, late-night session, second monitor across the room — has to wait for the developer to add a font-size setting or a per-widget scale knob. In a browser, `Ctrl-+` works on every screen on day one, and reflowable layout means it actually reflows rather than clipping. For a personal-use tool whose operator is also the only target user, this matters more than it would for a shipping product where the developer can wave the issue away as "add a setting later."

## Triggers to revisit

- **If embedded-SPA hosting in the daemon becomes a maintenance burden.** Specifically: if the SPA route's complexity grows to compete with the daemon's core (log + forward) responsibilities, the SPA could be split into a separate static-file server and the daemon left HTTP-API-only. Current footprint (~30 lines of Go in `internal/api/spa.go` + a tiny embed package) is well below that bar.
- **If a feature emerges that the browser sandbox can't deliver and a native app could.** Examples: direct USB serial access without a bridge intermediary, OS-level global hotkeys for PTT, native audio I/O routing. Current architecture (bridge handles all hardware) avoids this trigger by design — but if the bridge layer turns out to be the wrong shape, this assumption is worth re-examining.
- **If multi-operator scenarios become real.** SM is currently single-operator-per-instance; if that changes, the static-token-in-config auth model from `topology.md` needs to grow up, and the SPA's assumptions about session ownership change. This is more of an auth-shape decision than a toolkit decision, but they touch.
- **If Svelte 5 specifically becomes a problem.** E.g., if runes' reactivity model fights an unforeseen requirement, swapping Svelte for Solid (similar compile-time approach) inside the same Vite/embed shape is straightforward — would write a separate ADR superseding only the Svelte-specific portion of this one.

## References

- `docs/v2-design/ui-toolkit.md` — long-form analysis (Gio vs Wails vs browser SPA) that fed this decision.
- `docs/v2-design/frontend-spa.md` — the SPA scaffold itself (what landed, embed wiring, build pipeline, CI stance).
- `docs/v2-design/topology.md` — daemon-hosts-SPA model; bridge as a peer, not a subordinate.
- `docs/v2-design/cat-performance.md` — performance analysis confirming UI toolkit choice is not latency-critical.
- `docs/v1-analysis/lessons-for-v2.md` § "build specific, not generic" — the project rule that justified picking the framework the operator already knows.
- `frontend/embed.go`, `internal/api/spa.go`, `internal/api/server.go` — the daemon-side wiring.
- Memory: `project_sm_ui_toolkit` — captures the same decision in cross-session memory.
