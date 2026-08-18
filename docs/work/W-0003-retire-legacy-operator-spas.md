# W-0003 — Complete the app shell and retire legacy operator SPAs

**Status:** Open
**Priority:** P2 post-ship
**Selected:** 2026-08-18
**Outcome:** `frontend/app` is the sole embedded operator SPA, with the remaining legacy
affordances preserved and the zero-JS manual still independent.

`W-0003` is an immutable identity. Its priority and status may change, but its ranked position
remains in [`docs/backlog.md`](../backlog.md), the only priority owner.

## Verified current state

ADR 0044's original “three independent SPAs” context is now historical. The consolidated app is
the primary client at `/app/`, `/` redirects there, and the former logging SPA's embed and route
were retired on 2026-07-21. The app has the shared shell and theme plus real Operate, Logbook,
Settings, and Map views. Settings is no longer a blocker; only Dashboard remains a placeholder.

The consolidation is nevertheless incomplete:

- [`frontend/embed.go`](../../frontend/embed.go) still embeds the config and logbook builds, and
  [`internal/api/server.go`](../../internal/api/server.go) still serves them at `/config/` and
  `/logbook/` alongside `/app/`;
- the Taskfile, release tasks, and CI still install, test, and build both legacy clients;
- the app's ordinary upload-backfill path omits the legacy logbook's ClubLog-specific amber
  retry-only affordance and `skipped_no_history` result;
- bridge faults are deliberately rendered as raw code plus details in the app, while the retained
  logging source has the operator-facing English catalogue; and
- Logbook and Settings are statically imported by the shell. Only Map is currently lazy-loaded,
  so ADR 0044's per-route code-splitting requirement has not been met.

The old source directories remain useful parity evidence. Their presence is not the defect: live
routes, embedded assets, and mandatory build gates are the retirement boundary.

## Scope

This work item owns completion of the ADR 0044 outcome:

- port and characterize operator-significant behavior still unique to the retained clients;
- move the consolidated app from the `/app/` transition mount to the canonical root with working
  in-shell `/config`, `/logbook`, and Operate deep links;
- remove the config and logbook routes and embeds together, then remove their release and CI build
  gates;
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
   deep-link reload. `/config/` and `/logbook/` no longer load independent legacy bundles. The
   nearest confusable outcome—redirecting the visible entry points while still embedding and
   shipping the old assets—must fail the test.
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
  the separately backlogged whole-log map by accident.
- Should old `/app/*` URLs redirect permanently into their root equivalents for saved bookmarks,
  or remain a temporary compatibility alias? The answer determines when the `/app/` embed mount can
  disappear completely.
- Which retained catalogue entries still correspond to events the consolidated app can receive?
  Porting the entire historical file without enumerating live producers would preserve dead prose
  rather than behavior.
- What preservation tag marks the last release containing the legacy source trees, and is physical
  deletion a later work item rather than part of this retirement?

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
- [`frontend/logging/src/lib/i18n/en.ts`](../../frontend/logging/src/lib/i18n/en.ts) — retained
  operator-facing error wording to reconcile against live event producers.
- [`frontend/logbook/src/lib/ui/LogbookView.svelte`](../../frontend/logbook/src/lib/ui/LogbookView.svelte)
  — retained ClubLog retry-only behavior to characterize.
