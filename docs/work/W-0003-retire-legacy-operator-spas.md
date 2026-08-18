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

- [`frontend/embed.go`](../../frontend/embed.go) still embeds the config and logbook builds, and
  [`internal/api/server.go`](../../internal/api/server.go) still serves them at `/config/` and
  `/logbook/` alongside `/app/`;
- the Taskfile, release tasks, and CI still install, test, and build both legacy clients;
- the app's ordinary upload-backfill path omits the legacy logbook's ClubLog-specific amber
  retry-only affordance and `skipped_no_history` result;
- the operator-facing disconnect / bridge-fault wording is ported into the app as of 2026-08-18
  (`operate/bridgeMessages.ts`, shown on the Rig panel's "Bridge:" line; stuck-TX banners were
  already in `ui/TxAlarmBanner.svelte`), resolving the raw-code rendering for known codes; and
- Logbook and Settings are statically imported by the shell. Only Map is currently lazy-loaded,
  so ADR 0044's per-route code-splitting requirement has not been met.

The `frontend/logging` source tree was DELETED on 2026-08-18 after its operator-significant behavior
was ported and characterized (see "Logging SPA retirement" below). The `frontend/config` and
`frontend/logbook` source trees remain as parity evidence for their still-pending retirement. Source
presence is not itself the defect: live routes, embedded assets, and mandatory build gates are the
retirement boundary.

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
  deletion a later work item? DECIDED for `frontend/logging` (2026-08-18): tag
  `legacy-logging-spa-retired`; physical deletion done in this retirement (see "Logging SPA
  retirement" above). Still open for `frontend/config` and `frontend/logbook`.

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
- [`frontend/logbook/src/lib/ui/LogbookView.svelte`](../../frontend/logbook/src/lib/ui/LogbookView.svelte)
  — retained ClubLog retry-only behavior to characterize.
