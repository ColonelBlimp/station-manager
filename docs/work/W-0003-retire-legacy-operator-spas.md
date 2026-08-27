# W-0003 — Complete the app shell and retire legacy operator SPAs

**Status:** Open
**Selected:** 2026-08-18
**Outcome:** `frontend/app` is the sole embedded operator SPA, with the remaining legacy
affordances preserved and the zero-JS manual still independent.

`W-0003` is an immutable identity. Its status may change, while priority and ranked position live
only in [`docs/backlog.md`](../backlog.md).

## Verified current state

ADR 0044's original “three independent SPAs” context is now historical. The consolidated app is
the primary client at `/app/`, `/` redirects there, and the former logging SPA's embed and route
were retired on 2026-07-21. The app has the shared shell and theme plus real Operate, Logbook,
Settings, and Map views. Settings is no longer a blocker; only Dashboard remains a placeholder.

The consolidation is nevertheless incomplete:

- the config and logbook SPAs' embeds and routes were both retired 2026-08-19 (see the two
  retirement sections below). [`frontend/embed.go`](../../frontend/embed.go) now embeds ONLY the app
  build (`appSPA`/`AppFS`), and [`internal/api/server.go`](../../internal/api/server.go) serves only
  `/app/`; `/config`,`/config/` → `/app/config` and `/logbook`,`/logbook/` → `/app/logbook`
  307-redirect (temporary compatibility routes). The **app is now the sole embedded operator SPA**
  (AC4 met);
- the Taskfile, release tasks, and CI gate only the app client now — the config and logbook build
  gates were removed with their retirements;
- the app's upload path presents the legacy logbook's ClubLog-specific retry-only affordance +
  `skipped_no_history` result as of 2026-08-19 (`logbook/logbook.svelte.ts` `destinationRetryOnly`
  + the amber Retry button in `logbook/Logbook.svelte`), so AC2's app-side requirement is met;
- the operator-facing disconnect / bridge-fault wording is ported into the app as of 2026-08-18
  (`operate/bridgeMessages.ts`, shown on the Rig panel's "Bridge:" line; stuck-TX banners were
  already in `ui/TxAlarmBanner.svelte`), resolving the raw-code rendering for known codes; and
- Logbook and Settings are statically imported by the shell. Only Map is currently lazy-loaded,
  so ADR 0044's per-route code-splitting requirement (AC6) has not been met.

The `frontend/logging` source tree was DELETED on 2026-08-18 after its operator-significant behavior
was ported and characterized (see "Logging SPA retirement" below). The `frontend/config` route,
embed, and build gates were retired 2026-08-19 once its parity gaps were restored, and its source
tree was then DELETED (see "Config SPA retirement" below), preserved under the
`legacy-config-spa-retired` tag. The `frontend/logbook` route, embed, and build gates were likewise
retired 2026-08-19 once its last gap (the ClubLog retry-only workflow) was ported (see "Logbook SPA
retirement" below), preserved under the `legacy-logbook-spa-retired` tag. With that, NO legacy
operator SPA remains. Source
presence is not itself the defect: live routes, embedded assets, and mandatory build gates are the
retirement boundary.

## Slice A — app moved to the canonical root (2026-08-27)

The app now serves at the canonical root `/`, retiring the `/app/` transition mount (AC1/AC5/AC7):

- [`internal/api/server.go`](../../internal/api/server.go): a root `GET /` SPA mount replaces the
  `StripPrefix("/app")` mount and the `GET /{$}` → `/app/` redirect; the four `/config`,`/logbook`
  307 redirects are gone — those are now shell routes served by `index.html`. `/app`,`/app/` and
  any `/app/{path}` **301-redirect** (permanent, for saved bookmarks) to their root equivalents,
  query string preserved (`redirectAppToRoot`). All still inside the `Protocol=="tcp" && *ServeSPA`
  gate, so a headless daemon registers none of it.
- [`internal/api/spa.go`](../../internal/api/spa.go): the honest-404 guard now covers the `/v1` AND
  `/debug/pprof` server namespaces (bare + subtree) — load-bearing now that every unmatched path
  reaches the root catch-all rather than being quarantined under `/app`.
- [`vite.config.ts`](../../frontend/app/vite.config.ts): `base: '/'`; `dist/index.html` rebuilt with
  root asset refs. The router needed NO change — it derives from `BASE_URL` and is base-agnostic
  (`BASE=''` at root); its `/app` comments/tests were reframed as the former deployment.
- Docs (AC7): the canonical [`api-endpoints.md`](../v2-design/api-endpoints.md) SPA-route contract
  was rewritten in the same change; the app README "scaffold" status → "shipped".

Tests: `spa_test.go` rewritten (root-serves-shell; shell routes serve index; `/app`→root 301 with
query; root base marker; `/v1`+`/debug/pprof` guard; gate-off 404s). `handler_pprof_test.go` now
validates pprof precedence (profiling on) and the SPA guard (profiling off) at root;
`security_headers_test.go` reprobed. **AC1, AC5, AC7 met; AC6 (lazy loading) is Slice B.**

## Slice B — route-level lazy loading (2026-08-27, AC6)

Operate (Phone/CW) stays the eager default; the heavy views load on demand:

- [`App.svelte`](../../frontend/app/src/App.svelte): Logbook and Settings converted from static
  imports to `{#await import()}` (mirroring the existing Map lazy load), so each is its own chunk
  fetched only when that view opens.
- [`Operate.svelte`](../../frontend/app/src/lib/operate/Operate.svelte): the FT8 subtree (`Ft8View`)
  split out of the eager Operate chunk via `{#await import('./Ft8View.svelte')}`, so a Phone/CW load
  never carries it.

Evidence (production build manifest, same toolchain): the eager entry `index.js` dropped
**383.05 → 198.76 kB** (gzip 116.21 → 59.35), with new on-demand chunks `Logbook` (27 kB),
`Settings` (65 kB), and `Ft8View` (43 kB) alongside the existing `MapView` (150 kB); the shared
`daemonClock`/`_helpers` deps stay eagerly modulepreloaded. The lazy view chunks are not in the
entry, so they are not fetched on a Phone/CW first load. The frontend suite (1344 tests) stays green
— the FT8-mode Operate tests assert the ambient host, which renders synchronously regardless of the
lazy `Ft8View`. **AC6 met.** All seven acceptance criteria are now satisfied.

## Logging SPA retirement (2026-08-18)

The `frontend/logging` source tree — its route + embed + build gates already gone since 2026-07-21 —
was deleted by operator direction once its operator-significant behavior was ported into the app and
characterized. Preservation: annotated tag **`legacy-logging-spa-retired`** on the last commit that
still contained the tree (`565e178ff9f9387dc3ffe34b3b2bf1859a5edff4`); `git show
legacy-logging-spa-retired` recovers the full source and its detailed structure, which is why the
per-file archaeology is not duplicated in the live docs.

**Replacement map** (retired logging surface → current app home, under `frontend/app/src/lib/`):

| Retired logging surface | App home |
|---|---|
| QSO-entry keyboard shortcuts (`QsoPanel.handleKeydown`) | `operate/LoggingCard.svelte` (`windowKeydown` / `pileupKeydown` / `functionKeydown` / `callKeydown`) |
| Rig-control keymap + actions (`rigControl.ts`) | `operate/RigKeys.svelte` + `operate/rig.svelte.ts` |
| Pile-up stack (`callsignStack`, `StackingDrawer`) | `operate/callsignStack.svelte.ts` + `operate/CallsignStackPanel.svelte` / `PileupDrawer.svelte` |
| Stuck-TX safety banners (`bridge.txalarm.*`) | `ui/TxAlarmBanner.svelte` |
| Rig-disconnect / bridge-fault wording (`i18n/en.ts` `bridge.*`) | `operate/bridgeMessages.ts` |
| Session tab / QSO edit (`SessionPanel`, `QsoEditOverlay`) | `operate/SessionPanel.svelte` + `logbook/EditQsoModal.svelte` |
| FT8 view (`Ft8Panel`, `ft8.svelte.ts`) | `operate/Ft8*.svelte` + `operate/ft8.svelte.ts` (ADR 0067) |

**Restored before deletion** (dropped in the consolidation; ruled selective regressions by the
operator 2026-08-18, re-ported with characterization tests + reversion proofs):

- **F2 lookup-only "peek"** — `operate/LoggingCard.svelte` (`functionKeydown`): reveal the
  worked-before panel for a valid call WITHOUT starting the QSO timer.
- **Recent-comments paste-list picker** — `operate/CommentField.svelte` +
  `operate/commentHistory.svelte.ts`.
- **Mouse-accessible stack `≡` icon** — `operate/LoggingCard.svelte` (pointer equivalent of
  Shift+Enter).

**Accepted as deliberate replacements** (documented, not restored):

- The logging **Start/Stop timer buttons** and `stopQso` → the app auto-starts the clock on callsign
  commit and uses **F3** to freeze Time Off / start the clock.
- The VFO-input **Enter-commit / ESC-cancel** edit buffer → the app's CAT-off frequency field is a
  **directly-bound input**, edited in place.

`keyboard-shortcuts.md` now describes only current app behavior, and no live document or routing
metadata references the deleted tree; the ADRs and archives that mention it are records and keep
their historical text.

## Config SPA retirement (2026-08-19)

The `frontend/config` client was retired once its parity gaps were restored. Unlike logging, the
config SPA was still fully live (route + embed + build gates), so this is the full boundary removal.

**Parity restoration first** (the field-level audit found three affordances the app had dropped;
all restored on `main`, each TDD + reversion-proved, every clean-room review resolved):

| Restored parity gap | App home (`frontend/app/src/lib/`) |
|---|---|
| QSL defaults editor (`qsl_via` / `qslmsg` / `qsl_sent_via`) | `config/StationSection.svelte` + `config/station.svelte.ts` (presence-aware per-block save) |
| CAT master switch (`bridge_enabled`) | `config/RigsSection.svelte` + `config/bridgeEnabled.svelte.ts` (save-on-toggle, timeout reconcile, restart-pending note) |
| Rig model / FT8-mode / MY_RIG editing | `config/RigsSection.svelte` + `config/rigs.svelte.ts` (MY_RIG resolved live per QSO; all else restart) |
| Rig add / delete | `config/rigs.svelte.ts` `addRig`/`deleteRig` (immediate structural writes: id = max+1, first-rig-active, delete repoints the active default in-PUT, disabled at ≤1, timeout reconcile on add) |

**Accepted as deliberate (not restored):** the config SPA's FT8 decode highlight colours (operator
ruling 2026-08-05) and its explicit-`""` ft8_mode/my_rig state (the config SPA's own `canonRig`
collapses `""` to inherit identically — the app matches that parity limitation).

**Route ruling (2026-08-19):** `/config` and `/config/` **307-redirect** to `/app/config`,
registered **only inside** the `Protocol == "tcp" && *ServeSPA` block (a headless daemon 404s
`/config`); no `ConfigFS` is retained. The redirect is a TEMPORARY compatibility route — removed
when the app moves to the canonical root and `/config` becomes the shell route itself.

**Boundary removed in this retirement:** the `/config/` StripPrefix route → the 307 redirect
(`internal/api/server.go`, with Go route tests for both paths and for redirect-absence when
`ServeSPA` is off); the `configSPA` embed + `ConfigFS()` (`frontend/embed.go`); the
`frontend:config:*` Taskfile tasks + deps; the CI "Config SPA gate"; and the `config` entries in the
release/dev/local-CI SPA loops. The canonical HTTP reference
([`api-endpoints.md`](../v2-design/api-endpoints.md)) changed in the same commit.

**Preservation + deletion:** the `frontend/config` source tree was DELETED after the boundary
removal. Preservation: annotated tag **`legacy-config-spa-retired`** on the last commit that still
contained the tree (`feccc67eff39aacee48eb8f141418ad6b03b0e7f`); `git show legacy-config-spa-retired`
recovers the full source, which is why the per-file archaeology is not duplicated in the live docs.
The `docs/catalog.json` and `docs/README.md` `frontend/config/**` scopes were dropped (the app config
code at `frontend/app/src/lib/config/**` remains scoped), and the live design docs were scrubbed.

## Logbook SPA retirement (2026-08-19)

The `frontend/logbook` client was retired once its last operator-significant gap was ported. This is
the FINAL legacy-SPA retirement: with it, `frontend/app` is the sole embedded operator SPA (AC4).

**Parity restoration first (AC2).** A field-level audit confirmed the app's logbook is a superset of
the logbook SPA in every dimension EXCEPT the ClubLog / no-bulk-backfill **retry-only** workflow —
the one gap AC2 names. (The earlier-assumed "QSL-awaiting view / edit-history viewer / search" were
never built in the logbook SPA — only aspirational comments — so there was nothing to port there.)
The gap closed (TDD + reversion-proved, clean-room review clean), daemon-backed by the EXISTING
`POST /v1/forwarder/{name}/uploads` response, no new endpoint:

| Restored gap (AC2) | App home (`frontend/app/src/lib/`) |
|---|---|
| `skipped_no_history` parse | `api/uploads.ts` (`EnqueueResult`) |
| No-bulk-backfill detection + notice | `logbook/logbook.svelte.ts` (`NO_BULK_BACKFILL_TYPES = {'clublog'}`, `destinationRetryOnly`; the "N skipped — never uploaded live; use an ADIF export" notice) |
| Visually-distinct retry action | `logbook/Logbook.svelte` (amber "Retry failed uploads to {label}" button + ADIF tooltip when `destinationRetryOnly`) |

**Route ruling (2026-08-19, mirrors config):** `/logbook` and `/logbook/` **307-redirect** to
`/app/logbook`, registered only inside the `Protocol == "tcp" && *ServeSPA` block; no `LogbookFS` is
retained. TEMPORARY — removed when the app moves to the canonical root.

**Boundary removed in this retirement:** the `/logbook/` StripPrefix route → the 307 redirect
(`internal/api/server.go`, with Go route tests for both paths + redirect-absence when `ServeSPA` is
off); the `logbookSPA` embed + `LogbookFS()` (`frontend/embed.go`, removing the now-obsolete
`TestSpaHandler_ServesLogbookIndex`); the `frontend:logbook:*` Taskfile tasks + deps; the CI "Logbook
SPA gate"; and the `logbook` entries in the release/dev/local-CI SPA loops. The canonical HTTP
reference ([`api-endpoints.md`](../v2-design/api-endpoints.md)) changed in the same commit.

**Preservation + deletion:** the `frontend/logbook` source tree was DELETED after the boundary
removal. Preservation: annotated tag **`legacy-logbook-spa-retired`** on the last commit that still
contained the tree (`56a34f1f7903cd1e2b0c9203e8c5287045ffa836`); `git show legacy-logbook-spa-retired`
recovers the full source. The `docs/catalog.json` / `docs/README.md` `frontend/logbook/**` scope was
dropped, `frontend/embed.go`'s package doc records all three retirements, and `.gitignore` +
`DEVELOPING.md` were scrubbed. `frontend/app` is now the only entry anywhere in the SPA embed/gate/
ignore surfaces.

## Scope

This work item owns completion of the ADR 0044 outcome:

- port and characterize operator-significant behavior still unique to the retained clients;
- move the consolidated app from the `/app/` transition mount to the canonical root with working
  in-shell `/config`, `/logbook`, and Operate deep links;
- remove the config and logbook routes and embeds and their release and CI build gates — DONE,
  staged not together: config first (2026-08-19), then logbook (2026-08-19) once its ClubLog
  retry-only gap was ported (AC2). Each route became a temporary redirect (`/config`→`/app/config`,
  `/logbook`→`/app/logbook`); those redirects' own removal follows when the app moves to the
  canonical root;
- preserve the manual as an independently served zero-JS site; and
- finish the shell acceptance requirements that prevent consolidation from degrading first-load
  behavior, including route-level lazy loading.

The work may land in separately releasable parity and retirement slices, but a route must not be
removed before its behavior is characterized and present in the consolidated app.

This item does not authorize deleting the legacy source trees without the already-decided
preservation tag, absorbing the manual, adding a bespoke dashboard aggregate endpoint, changing
daemon domain boundaries, redesigning the operator workflows, or initiating RF or hardware tests.
The separate whole-log Dashboard map remains separately ranked work.

## Operator-observable acceptance criteria

1. `/`, `/operate`, `/logbook`, and `/config` open the consolidated shell, including after a direct
   deep-link reload. MET (config + logbook both retired 2026-08-19): `/config`,`/config/` and
   `/logbook`,`/logbook/` no longer load an independent legacy bundle — they 307-redirect to
   `/app/config` and `/app/logbook`, and neither the config nor the logbook assets are embedded or
   gated (the nearest confusable outcome — redirecting a visible entry point while still
   embedding/shipping its old bundle — must fail the test). When the app reaches the canonical root,
   these redirects are themselves removed and `/config`/`/logbook` become shell routes.
2. A ClubLog-type destination presents a visually distinct retry-only action, explains that only
   failed live uploads can be retried, and reports `skipped_no_history` rows as requiring ADIF
   export. Tests key the rule on forwarder type rather than a configured destination name.
3. Known rig disconnect, bridge fault, and stuck-TX codes render operator-actionable wording rather
   than raw internal codes. Unknown codes remain visible and diagnosable without displaying
   uncontrolled secrets or third-party response text.
4. The daemon binary embeds one operator SPA. Ordinary install, build, release, and CI paths build
   and gate `frontend/app`, not the retired config or logbook clients; the manual remains a separate
   static embed.
5. API routes retain precedence over the root SPA fallback: known `/v1/*` behavior is unchanged,
   unknown API paths return API-style 404s rather than `index.html`, and headless/non-TCP daemon
   configurations expose no SPA routes.
6. Operate loads eagerly while Logbook, Settings, and FT8-heavy views are separate lazy chunks.
   Under the same build and throttling conditions, the Operate first load is no larger or slower
   than the legacy logging baseline required by ADR 0044.
7. The canonical HTTP reference changes in the same commit as route behavior, and tests distinguish
   the new root fallback from the old exact-root redirect.

## Decisions required before implementation

- Does the lean status Dashboard and its `startup_view` preference block ADR 0044 closure, or move
  to a separate ranked dossier once the retirement boundary is complete? Dashboard must not absorb
  the separately backlogged whole-log map by accident. (Frontend-app review **F-06** routes here:
  the default route and the first sidebar item both land on an empty Dashboard placeholder
  — `router.svelte.ts:36`, `App.svelte:112` — so the canonical-root/default-route decision owns
  what `/` opens; deciding it must not authorize building the whole-log Dashboard.)
- Consolidation cleanup carried by this item: stale scaffold/route documentation left by the
  retirement — `frontend/app/vite.config.ts`'s "logging SPA still owns the root" comment (now false;
  all legacy SPAs retired) and the app README's "scaffold" status (frontend-app review **F-09**, the
  non-build-identity half; the build-identity half belongs to W-0004).
- Should old `/app/*` URLs redirect permanently into their root equivalents for saved bookmarks,
  or remain a temporary compatibility alias? The answer determines when the `/app/` embed mount can
  disappear completely.
- Which retained catalogue entries still correspond to events the consolidated app can receive?
  Porting the entire historical file without enumerating live producers would preserve dead prose
  rather than behavior.
- What preservation tag marks the last release containing the legacy source trees, and is physical
  deletion a later work item? DECIDED for `frontend/logging` (2026-08-18): tag
  `legacy-logging-spa-retired`; physical deletion done in that retirement (see "Logging SPA
  retirement" above). DECIDED for `frontend/config` (2026-08-19): tag `legacy-config-spa-retired` on
  the last commit still containing the tree; physical deletion done in that retirement (see "Config
  SPA retirement" above). DECIDED for `frontend/logbook` (2026-08-19): tag `legacy-logbook-spa-retired`
  on the last commit still containing the tree; physical deletion done in this retirement (see
  "Logbook SPA retirement" above). No legacy source trees remain.

## Verification standard

Use characterization tests before moving each remaining behavior. Frontend tests must cover both
the correct and confusable ClubLog/error-rendering states; Go route tests must exercise direct
reloads, API precedence, disabled-SPA topology, and absence of retired mounts. Inspect the built
manifest or network requests to prove lazy chunks are not fetched on an initial Phone/CW load, and
compare bundle/load measurements using the same toolchain and conditions. Finish with the focused
frontend checks, affected Go package tests (including `-race` for any lifecycle/state concurrency
change), and the full local release gate when the implementation is ready.

## References

- [`docs/backlog.md`](../backlog.md) — authoritative ranking.
- [`ADR 0044`](../decisions/0044-consolidate-operator-spas-into-one-shell.md) — selected shell,
  routing, manual, theme, and first-load constraints.
- [`docs/v2-design/api-endpoints.md`](../v2-design/api-endpoints.md) — canonical current SPA route
  contract; update with behavior.
- Operator-facing bridge error wording (was the retired logging SPA's `i18n/en.ts`) — the
  disconnect/fault strings are ported to
  [`operate/bridgeMessages.ts`](../../frontend/app/src/lib/operate/bridgeMessages.ts); the full
  catalogue is preserved under the `legacy-logging-spa-retired` tag.
- ClubLog retry-only behavior (was the retired logbook SPA's `LogbookView.svelte`) — ported to
  [`logbook/Logbook.svelte`](../../frontend/app/src/lib/logbook/Logbook.svelte) +
  [`logbook/logbook.svelte.ts`](../../frontend/app/src/lib/logbook/logbook.svelte.ts); the full
  source is preserved under the `legacy-logbook-spa-retired` tag.
