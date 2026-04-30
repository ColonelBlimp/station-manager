# v2 design — frontend SPA (Svelte 5)

**Status:** sketched 2026-04-30. No code yet. Captures the layout, daemon-side embed wiring, build pipeline, and CI stance for the browser-SPA logging client. Builds on [ui-toolkit.md](ui-toolkit.md)'s recommendation (browser SPA over Gio/Wails) and [topology.md](topology.md)'s peer-service model (daemon hosts the SPA; SPA talks to bridge and daemon directly).

## Toolkit choice (confirmed 2026-04-30)

**Svelte 5 + Vite + plain TS.** Not SvelteKit. Decided over Vue/React on three grounds:

1. **Compiled-reactivity DOM updates** suit a long-running tab consuming a 10–20 Hz rig SSE stream. Surgical updates of bound nodes only, no VDOM diff. Steady-state CPU stays near zero.
2. **Bundle size for `//go:embed`.** Svelte typically ships 15–30 KB gzipped for an app this size; React/Vue ship 40–100 KB of runtime before app code. Smaller daemon binary, faster cold loads.
3. **Operator's existing fluency.** Already in `CLAUDE.md` code-style notes: "prefer Svelte 5 runes (`$state`, `$derived`, `$props`)." Picking the framework you know is the right move on a solo project — the lessons doc's "build specific, not generic" extends to tooling.

Performance is not the *deciding* factor — every framework hits the 300 ms latency budget easily. Svelte 5's wins are second-order (CPU, bundle size, ergonomics), but second-order wins still matter on a tool you keep open all day.

**Why not SvelteKit:** SvelteKit gives you SSR, file-based routing, and adapters for Vercel/Netlify/Node — none of which apply when the SPA is embedded into a Go binary served by SM's daemon. Plain Svelte + Vite + a tiny client-side router (~3 KB or hand-rolled hash router ~50 lines) is the right shape. Less ceremony, cleaner `dist/` for embed.

## Frontend layout — `frontend/logging/`

```
frontend/logging/
├── package.json
├── vite.config.ts
├── tsconfig.json
├── svelte.config.js
├── .gitignore                       # node_modules/, dist/
├── index.html                       # Vite entry; mounts <App/>
├── public/
│   └── favicon.ico
├── src/
│   ├── main.ts                      # createApp + router init
│   ├── app.svelte                   # top shell: nav tabs + <Route/>
│   ├── lib/
│   │   ├── api.ts                   # fetch wrappers for daemon /v1/*
│   │   ├── bridge.svelte.ts         # EventSource → $state rigState
│   │   ├── config.ts                # daemon.url, bridge.url (defaults localhost)
│   │   └── types.ts                 # Qso, Logbook, RigState — mirror Go DTOs
│   ├── routes/
│   │   ├── log/
│   │   │   ├── +page.svelte         # the logging form
│   │   │   ├── callsign-input.svelte
│   │   │   ├── mode-cycle.svelte    # ports the Gio cycle widget
│   │   │   └── rst-input.svelte
│   │   ├── logbook/
│   │   │   ├── +page.svelte         # paginated QSO list
│   │   │   └── qso-row.svelte
│   │   └── config/
│   │       └── +page.svelte         # daemon URL, bridge URL, forwarding creds
│   └── styles/
│       └── app.css                  # tiny global reset; per-component otherwise
└── dist/                            # build output, embedded by the daemon (gitignored)
```

### Notes on the layout

- **`+page.svelte` naming is borrowed from SvelteKit** but is purely cosmetic — these are regular Svelte components mounted by the router. Could equally be `LogPage.svelte`. Keep the convention if it reads cleanly.
- **`bridge.svelte.ts` is a `$state` module.** One module-level `$state` object holds the live rig state; any component that reads `rigState.freq` re-renders automatically when the EventSource fires. No store boilerplate.
- **`api.ts` is fetch calls only.** SM's API surface is small enough that `fetch` + Svelte's `$derived` covers it. No axios, no react-query equivalent.
- **The Mode cycle widget ports straight from the Gio implementation.** Same idea: render the current mode, click cycles `["USB","LSB","CW",...]`. ~20 lines of Svelte.
- **Config tab doubles as the "where do I find the daemon and bridge" UI.** First-launch UX: defaults to `localhost`, operator overrides for multi-host setups.

### Deliberately not included

- No state-management library — Svelte's runes are the state manager.
- No CSS framework — Svelte scopes component `<style>` blocks automatically, no class-name collisions. Tailwind is fine if it gets pulled in later, but don't pre-scaffold.
- No test framework yet — add Vitest when the first non-trivial component lands.
- No SSR, prerendering, or service worker.

## Daemon-side wiring

### New package — `frontend/`

The Go embed file lives at the repo root next to the SPA project so `//go:embed` paths are clean:

```
frontend/
├── embed.go                         # package frontend; //go:embed logging/dist
└── logging/
    ├── package.json
    ├── ... (everything above)
    └── dist/                        # build output
```

```go
// frontend/embed.go
package frontend

import (
    "embed"
    "io/fs"
)

//go:embed all:logging/dist
var loggingSPA embed.FS

// LoggingFS returns the built logging SPA rooted at the dist folder so
// http.FileServer can serve index.html as the directory root.
func LoggingFS() (fs.FS, error) {
    return fs.Sub(loggingSPA, "logging/dist")
}
```

Daemon imports `github.com/ColonelBlimp/station-manager/frontend`. The Go package sits adjacent to the assets it embeds — discoverable, no copy-step gymnastics.

### Route registration

In `internal/api/server.go` (`New()`'s mux setup, after the `/v1/*` and operational routes):

```go
// SPA — served at the root. Anything not matched by /v1/* falls through
// to the SPA's index.html so client-side routing handles /log, /logbook,
// /config, etc. See docs/v2-design/topology.md for why this lives here.
if cfg.Server.Protocol == "tcp" && cfg.Server.ServeSPA {
    spaFS, err := frontend.LoggingFS()
    if err != nil {
        return nil, errors.New(op).WithErr(err).WithMsg("loading embedded SPA")
    }
    mux.Handle("GET /", spaHandler(spaFS))
}
```

Helper in `internal/api/spa.go`:

```go
// spaHandler serves static SPA assets with SPA-fallback behaviour: if
// the requested path doesn't match a real file, return index.html so
// the client-side router can resolve it. Required because the browser
// will request /log directly on refresh and there's no /log file in
// dist — only /index.html.
func spaHandler(spa fs.FS) http.Handler {
    fileServer := http.FileServer(http.FS(spa))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        f, err := spa.Open(strings.TrimPrefix(r.URL.Path, "/"))
        if err != nil {
            r.URL.Path = "/"
        } else {
            f.Close()
        }
        fileServer.ServeHTTP(w, r)
    })
}
```

Go 1.22+'s `http.ServeMux` pattern matching makes `GET /` the lowest-priority match — it only fires when no `/v1/*` route matched. The catch-all is naturally bounded.

### Signature change

`api.New()` would change from `*Server` to `(*Server, error)` because `frontend.LoggingFS()` can fail. One-line update at the call site in `cmd/smd/main.go`.

### Protocol: TCP becomes mandatory for SPA-serving deployments

The daemon currently supports Unix-socket protocol (`internal/api/server.go:104`). Browsers can only speak TCP, so any deployment that hosts the SPA must use TCP. Add a config flag:

- `cfg.Server.ServeSPA bool` — default `true` for TCP, `false` for Unix-socket.
- Lenient: a Unix-socket headless daemon stays a supported deployment shape (per the topology doc's "narrow daemon scope" — daemon is useful without a UI).
- Strict alternative considered and rejected: don't fail at startup if `unix && ServeSPA`. Just don't register the route.

## Build pipeline — Taskfile

```yaml
tasks:
  frontend:install:
    desc: Install frontend dependencies (one-off + after package.json changes)
    dir: frontend/logging
    cmds:
      - npm ci

  frontend:dev:
    desc: Run Vite dev server with HMR; expects daemon running on :8080
    dir: frontend/logging
    cmds:
      - npm run dev      # default :5173, proxies /v1/* to daemon

  frontend:build:
    desc: Production build of the SPA (consumed by go:embed)
    dir: frontend/logging
    cmds:
      - npm run build

  build:smd:
    desc: Build the daemon with embedded SPA
    deps: [frontend:build]
    cmds:
      - go build -o build/smd ./cmd/smd

  run:smd:
    desc: Run the daemon (assumes SPA already built; use frontend:build first)
    cmds:
      - go run ./cmd/smd
```

### Two development modes

1. **Hot iteration:** `task run:smd` in one terminal, `task frontend:dev` in another. Vite dev server runs on `:5173` with HMR; `vite.config.ts` proxies `/v1/*` to `:8080` so the SPA hits the real daemon. Edit a `.svelte` file, browser updates instantly. Daemon doesn't rebuild.
2. **Single-binary verify:** `task build:smd` produces a binary with the SPA baked in. Run it, hit `localhost:8080`, see the embedded build. Used to verify embed + for releases.

### `vite.config.ts` proxy block

```ts
export default {
  plugins: [svelte()],
  server: {
    port: 5173,
    proxy: {
      '/v1': 'http://localhost:8080',
    },
  },
}
```

Bridge URL is configurable in the SPA's own config (different host typically) so it's not proxied here.

## CI / release stance

**Decision (2026-04-30):** **CI builds the SPA before `go build`. `dist/` is NOT committed to git.**

- CI workflow gains a Node setup step (LTS) + `task frontend:install && task frontend:build` before the Go build (and before any Go test that touches the embed).
- The embed itself enforces ordering — `//go:embed all:logging/dist` fails at compile time if `dist/` is absent, so a missed build step surfaces immediately rather than producing a working binary that serves nothing.
- `.gitignore` will exclude `frontend/logging/dist/` and `frontend/logging/node_modules/`.
- Release tarballs / binaries built via `goreleaser` (or whatever the eventual release path is) need the same prerequisite. **TODO when the release pipeline gets revisited:** wire `frontend:build` into the goreleaser pre-build hook.
- Local dev: first-time setup runs `task frontend:install` once, then `task frontend:build` whenever a fresh daemon binary is needed. HMR dev mode (`task frontend:dev`) doesn't need `dist/` because Vite serves from memory.

**Rejected alternative:** committing `dist/` to git. Avoids Node in CI but pollutes history with build artifacts and creates merge-conflict noise on every frontend change. Not worth the saved CI minute.

## What this changes elsewhere

### Things that get added when the scaffold lands

- `frontend/` directory with the project skeleton.
- `frontend/embed.go` Go package.
- `internal/api/spa.go` (or inline) — the `spaHandler` helper.
- `cfg.Server.ServeSPA bool` config field with TCP-default-true logic.
- Taskfile entries above.
- `.gitignore` updates for `frontend/logging/node_modules/` and `frontend/logging/dist/`.

### Things that don't change yet

- **No new daemon endpoints.** The SPA consumes the existing `/v1/*` surface; new endpoints get added when the SPA actually needs them. The logging form's POST `/v1/qso` already exists; lookup/enrichment endpoints will be added when the form's hamnut/QRZ fields wire up.
- **`cmd/logging/` (Gio app) is left alone.** Per [ui-toolkit.md](ui-toolkit.md)'s note, the Gio code is small enough to abandon cleanly when the SPA is the confirmed path. Don't pre-emptively delete; leave until the SPA reaches feature parity.

## Smallest first commit

The "skeleton works end-to-end" milestone:

1. Create `frontend/logging/` with `package.json`, `vite.config.ts`, `index.html`, `src/main.ts`, `src/app.svelte` (renders "hello").
2. Create `frontend/embed.go`.
3. Wire the SPA route into `internal/api/server.go` behind the `ServeSPA` flag.
4. Add the Taskfile entries.
5. Verify: `task build:smd && ./build/smd`, hit `localhost:8080` in a browser, see "hello".

After that lands: client-side routing, the rig SSE wiring (`bridge.svelte.ts`), the logging form. Each its own commit.

## Open questions for the next session

- **Router choice.** `svelte-spa-router` (~3 KB, well-maintained) vs hand-rolled hash router (~50 lines, zero deps). Lean toward hand-rolled given the three-route shape.
- **CSS approach.** Plain scoped component CSS to start; revisit Tailwind if the form grows complex enough that utility classes save time.
- **Bridge URL discovery.** Static config in the SPA, defaulting to `http://localhost:<bridge-port>`. mDNS / Bonjour is overkill for personal use (per topology.md). Confirm the default port to use for the bridge once the bridge service is built.
- **CORS on the bridge.** Bridge sets `Access-Control-Allow-Origin` either `*` (single-user) or scoped to the daemon's origin. Lands in the bridge's HTTP setup, not here. Tracked in `bridge.md` work, not in this doc.
- **Auth.** None for LAN-only use. For "daemon on a VPS, bridge at home over the open internet," static token in config + `Authorization` header per topology.md §Authentication. Out of scope for the first scaffold.

## Cross-references

- [ui-toolkit.md](ui-toolkit.md) — the toolkit-choice analysis this builds on
- [topology.md](topology.md) — daemon hosts SPA; SPA talks to bridge and daemon directly
- [cat-performance.md](cat-performance.md) — why the UI's contribution to latency is in the noise
- `internal/api/server.go` — the existing daemon HTTP surface that grows the SPA route
- `cmd/smd/main.go` — daemon entry point (wires `api.New()`)
- Memory: `project_sm_ui_toolkit` — pending update once the toolkit decision is finalised in code
