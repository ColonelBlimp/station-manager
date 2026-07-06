---
number: 0044
title: Consolidate the operator SPAs into a single app shell
status: Proposed
date: 2026-07-06
---

# 0044 — Consolidate the operator SPAs into a single app shell

## Context

The browser-SPA UI (ADR 0001) shipped as **three independent Svelte 5 + Vite
builds** — `frontend/logging` (the live operating UI), `frontend/config`
(station/rig/forwarding setup), and `frontend/logbook` (QSO browse/edit/export) —
plus a fourth surface, the **operator manual**, which is a deliberately zero-JS
Hugo static site (ADR 0036). The daemon embeds the three SPAs via separate
`//go:embed all:*/dist` directives (`frontend/embed.go`) and serves them at
`/`, `/config/`, and `/logbook/` when the listener is TCP and `ServeSPA` is on
(`internal/api/server.go`); the manual is served at `/manual/` by a plain file
handler.

Three pressures make the split worth revisiting now:

1. **The seam is in the wrong place.** The split is by *artifact*, but the
   operator's mental model is by *activity*. FT8 lives inside the "logging" app
   yet is not logging — it is a distinct operating mode that happens to *produce*
   logged QSOs. Phone/CW entry and FT8 are **siblings** that already share one
   session log (`sessionQsosState`, the Session tab, `SessionEmailControls`), not
   a parent and child. The current structure buries that sibling relationship
   under one app's tab bar, and papers the config boundary over with a full-page
   hand-off link (`app.svelte` `setup_done`).

2. **Triplicated plumbing that is already drifting.** Each build re-implements the
   fetch/timeout API client (`lib/api/_helpers.ts` exists three times — and the
   session-198 fetch-timeout hardening landed in `logging`'s copy only), the
   SSE/bridge connection, the `types.Qso` mirror, enrich, the QSO-edit modal, and
   a Tailwind theme layer. Cross-cutting concerns — most visibly **theming and
   dark mode** — must be re-authored and kept in sync across three codebases,
   which is precisely the class of change that is painful across separate builds
   and trivial inside one shell.

3. **A structural rhyme with ADR 0043.** That ADR split the *backend*
   `internal/api` god-package into per-surface handler packages (`qso`, `logbook`,
   `rig`, `ft8`, `config`, `enrich`, `session`, `static`, …). The client is the
   mirror image: the same surfaces want to be **destinations in one shell over one
   API client**, rather than three apps each carrying a partial, drifting copy of
   the client layer.

This ADR is explicitly a **post-ship** architecture cycle. The 7Q8AC ship gate
is met on the current three-SPA structure; this is architecture debt, not a
shipping defect, and the live operating UI is not put on the operating table
before the first external operator has it working.

## Decision

Consolidate `logging`, `config`, and `logbook` into **one Svelte 5 + Vite
application** — `frontend/app/` — with a persistent app shell (nav + status
home / dashboard), client-side routing, one shared `lib/` (api client, states,
UI components, theme), and a single embedded `dist/` served from `/`. The
**operator manual stays a separate zero-JS Hugo site** (ADR 0036) linked from
the shell, not absorbed into it. Consolidation is therefore **3 → 1, not 4 → 1.**

The shell's destinations mirror the ADR-0043 API surfaces: **Dashboard**,
**Operate** (Phone/CW + FT8 as sibling modes over the shared session log),
**Logbook**, **Settings** (the config surface — nav label "Settings"; route stays
`/config`, and the sub-decisions below still call it "config"), and a link out to
the **Manual**. First-run setup becomes a shell-level gate/route rather than logic
embedded in the logging app.

The **shared theme system is built as the shell's foundation, from the first
commit** — one set of semantic design tokens, a dark-mode variant, and a single
toggle — and every surface is migrated *onto* it. Theming is a first-class
concern of the new app, not a post-merge reconciliation of three drifted Tailwind
layers. (Unifying theming is one of the three drivers for this ADR; retrofitting
it after the merge would forfeit most of that win and re-introduce exactly the
drift we are consolidating to remove.)

## Alternatives considered

### Leave the three SPAs split (do nothing)

Everything works and is dogfooded daily. Rejected because the three drivers above
are real and compounding: the FT8/logging seam mis-models the domain, the
plumbing triplication is already drifting (the timeout fix reached one of three
copies), and every future cross-cutting change (theme, dark mode, a shared status
widget, a new operating mode) pays the triplication tax again. The cost of acting
rises with each feature added to any of the three apps.

### Split FT8 out into its own fourth SPA (`/ft8/`)

Addresses the stated seam pain surgically. Rejected because it makes the sprawl
*worse* (four SPAs → five) and, more importantly, it fights the data model: FT8
and Phone/CW **share one session log and one email-out**. Splitting them into
separate builds would force that shared state across an app boundary. The seam
fix is to make them *siblings in one app*, not to give FT8 its own silo.

### Absorb the manual into the shell too (4 → 1)

Superficially tidier. Rejected on ADR 0036's own rationale: the manual is
zero-JS on purpose — printable, readable when the SPA bundle is broken or the
link is too poor to boot the app, and dependency-light. For the 7Q8AC flaky-link
context, "the docs still render when the app doesn't" is a *feature*, and folding
the manual into a JS shell is a regression. It stays a sibling static site,
linked from the shell.

### Adopt SvelteKit as the shell framework

SvelteKit gives filesystem routing, layouts, and code-splitting out of the box.
Rejected because it is built around SSR/adapters and a server runtime, which
fights the `//go:embed` static-serve model the daemon depends on; the
static-adapter path works but drags in machinery (and a larger dependency
surface, against the project's minimize-dependencies idiom) far beyond what a
localhost single-operator SPA needs. A plain Vite build with a small client
router keeps the existing embed/serve contract unchanged.

### One build via a workspace/monorepo, still emitting three bundles

Share `lib/` through a workspace package but keep three entry points. Rejected
because it solves only the plumbing-triplication driver, not the seam driver: the
operator still lands in three separate apps with no shared shell, nav, or status
home, and theming still has three mount points to style. It is more build
complexity for a partial win.

## Consequences

**Signed up for (good):**

- **One API client, one bridge/SSE layer, one `types.Qso` mirror, one theme
  system.** Dark mode and any future cross-cutting concern are authored once. The
  drift that already bit `_helpers.ts` becomes structurally impossible.
- **The domain is modelled correctly.** Operate hosts Phone/CW and FT8 as peers
  over the shared session log; the config hand-off interstitial becomes in-app
  navigation; the informal loading/setup/main gate in `logging/app.svelte`
  becomes an explicit shell gate.
- **Unchanged daemon serve contract.** `spaHandler` already index-falls-back
  unknown paths, returns 404 for `/v1/*`, and serves index for directories, so a
  single `/` catch-all serving one client-routed shell needs no new server
  behaviour. `server.go` *sheds* the `/config/` and `/logbook/` mounts; `/manual/`
  and `/v1/*` stay as higher-priority patterns.
- **`frontend/embed.go` collapses** from three `//go:embed all:*/dist` to one, and
  `scripts/release*.sh` builds one SPA instead of three.
- **Theme system, built first, from one baseline.** One set of semantic design
  tokens with a dark variant, applied via a `data-theme` attribute on the root; a
  single toggle that defaults to the OS `prefers-color-scheme` and persists the
  operator's override to `localStorage` (works offline; one switch, one place).
  The `logging` app's existing tokens (`bg-focus`, `text-ink`, `line-soft`,
  `focus-ring`, …) are the most evolved of the three and are the **baseline** the
  other surfaces migrate onto — "adopt one, retire two," not "invent a fourth."
  **Caveat:** the token/utility *nomenclature* is not frozen — merging three
  vocabularies is the natural moment to rationalize the Tailwind semantic-utility
  names, so expect a naming pass during consolidation rather than treating the
  current `logging` names as final.
- **API *usage* simplifies, even though the endpoint count does not.** The
  daemon's route surface is set by client needs, and the union of needs is
  unchanged — no endpoint is retired. What simplifies is how the client *uses* it:
  one shared bootstrap hydration (`GET /v1/config` once, not once per app — less
  traffic on a poor link), one owned stream lifecycle for `/v1/rig/events` +
  `/v1/ft8/events` instead of per-app EventSources, and — with a live Logbook view
  now sitting beside Operate — a natural first consumer for the **already-published
  but currently-unconsumed `qso.stored`/`updated`/`deleted` events spine** (ADR
  0043): one subscription can refresh both the session log and the logbook grid,
  retiring the current POST-response-add + `ft8-logged` + logbook-refetch triad
  *without adding daemon surface*.

**Accepted (cost):**

- **A large, mechanical-but-risky frontend migration.** Three `lib/` trees merge
  into one; the genuine hazard is *name/state collisions* (three `configState`s,
  overlapping component names, three Tailwind token sets that must reconcile to
  one). This is the bulk of the work and where regressions hide — it needs
  characterization coverage (Vitest/Playwright) on the operating flow before and
  after.
- **One bundle now spans all surfaces.** Without care, a pure Phone/CW session
  downloads FT8 + logbook + config code. On the 7Q8AC link this matters, so
  **per-route code-splitting via dynamic `import()` is a requirement, not an
  optimization** — Operate loads eagerly; Logbook/Config/FT8-heavy views lazy.
- **A client router is introduced** where there was none (see open sub-decisions).
- **All-eggs-one-basket for the UI.** A shell-level boot failure takes down every
  surface at once, where today a broken config build leaves logging up. Mitigated
  by the shell staying thin, the manual staying an independent zero-JS fallback,
  and the daemon/API being wholly unaffected.
- **The dashboard tempts a new aggregate endpoint** (`GET /v1/dashboard` bundling
  rig state + forwarding-queue depth + QSO counts). Resisted by policy: the
  dashboard composes existing endpoints and subscribes to the events hub, per ADR
  0043's "consume parsimoniously / announce, don't command." Left undisciplined,
  consolidation is API-surface-*additive* here — the one place the "consolidation
  shrinks the API" intuition inverts.

**Build approach:** prototype the shell + dashboard as a **static HTML/CSS mock
in the target substrate** — Tailwind v4 + the logging app's `@theme` tokens, with
a working `data-theme` dark toggle — to settle IA/nav, the dashboard tile set, the
`startup_view` landing options, and the theme system *before* the `lib/` merge
(honours the project's "design SPA UX before building" rule). **Tailwind Plus**
(personal license; the gitignored `.tailwindplus/` archive) is used as a
**layout/IA reference** for the shell frame + destinations — patterns adapted into
SM's own `@theme`-token markup, **not** committed verbatim. This is both
licence-clean (SM is a functional open-source End Product, which the Tailwind Plus
license expressly permits — what it forbids is redistributing the components
*separately* as extractable assets) and consistent with the theming-first
foundation (adapting to tokens is what buys dark mode + one vocabulary). The mock is
question-scoped and throwaway; it de-risks the cheap half (layout, theme) and by
design says nothing about the expensive half (the three-`lib/` merge without
state-model collisions; per-route code-splitting holding the Operate first-load
down on a poor link), which is de-risked in code, not HTML — a polished mock is
not evidence the consolidation is de-risked.

**Sub-decisions (endorsed 2026-07-06 by the operator; finer points settle at build):**

- **Routing model — History-API real paths** (`/`, `/operate`, `/logbook`,
  `/config`, `/setup`), adopted **provisionally** ("for now; we'll see how this
  pans out"). `spaHandler` already index-falls-back unknown paths, so deep-link +
  refresh work; real paths are bookmarkable and match existing `/config/`
  `/logbook/` muscle memory (301 the old paths to the new in-shell routes).
  Implement with a ~40-line hand-rolled router (a `route` `$state` off
  `location.pathname` + `pushState` + `popstate`), not a dependency — consistent
  with the minimize-dependencies idiom. Hash routing stays the documented fallback
  (see Triggers) if a future non-TCP/CDN-ish topology can't guarantee the index
  fallback.
- **Dashboard — a lean status home** (rig connection state, **forwarding-queue
  depth/health**, today's QSO count, quick-nav cards), composed from existing
  endpoints + the events hub, **no new daemon surface** (do not mint
  `GET /v1/dashboard`). Endorsed in principle. **Open finer point — the default
  landing view is itself an operator preference, not fixed:** some operators will
  want to open straight into Operate → FT8 or Operate → Phone/CW rather than the
  dashboard. Model it as a small config setting (`startup_view`: `dashboard` |
  `operate-ft8` | `operate-phone` | `last-used`), decided when the shell is built;
  the dashboard stays the default and is always one nav click away. This keeps the
  status home cheap while honouring "what the operator wants to see first."
- **Config — folded in as the `/config` route** (retire the separate mount and the
  full-page hand-off link), which is what kills a `_helpers.ts` and theme copy.
  Config keeps its distinct *lifecycle* (set once, operate daily) as a separate
  shell destination — that's an information-architecture concern, not a reason to
  keep it a separate build.

## Triggers to revisit

- If the `lib/` merge surfaces state-model collisions that can't be reconciled
  without reshaping domain state (not just renaming), stop — the apps may be more
  divergent than the shared-plumbing premise assumes, and a workspace-with-shared-
  `lib` (rejected above) becomes the safer partial step.
- If per-route code-splitting can't keep the Operate first-load at or below
  today's `logging` bundle on a throttled link, the single-bundle cost is real —
  reconsider whether Config/Logbook stay separately-loaded surfaces.
- If a second client topology appears (the parked split-host `cmd/bridge`, a
  headless/CLI consumer, live multi-device sync per ADR 0043's deferred session-
  log consumer), re-check whether one shell still fits all consumers before
  extending it.
- If the manual ever needs live/interactive content, revisit whether it should
  join the shell (weighed against losing the zero-JS fallback — ADR 0036).

## References

- ADR 0001 — UI toolkit: browser SPA (the topology this ADR extends).
- ADR 0002 / 0003 — SPA config shape / daemon-only config surface.
- ADR 0036 — operator manual as an embedded zero-JS site (why the manual stays
  separate).
- ADR 0043 — coupling principles + the `internal/api` per-surface split (the
  server-side mirror of this client-side consolidation; enumerate-all-consumers;
  the deferred unified session-log event consumer).
- `docs/v1-analysis/lessons-for-v2.md` — enumerate all API consumers before
  designing; build specific not generic.
- Anchored code: `frontend/embed.go` (three embeds → one); `internal/api/
  server.go:260-276` (SPA route mounts); `internal/api/spa.go` (index fallback +
  `/v1/`→404 + directory→index, which make a single catch-all safe);
  `frontend/logging/src/app.svelte` (the informal shell/gate + config hand-off
  link this ADR formalizes); `frontend/{logging,config,logbook}/src/lib/api/
  _helpers.ts` (the triplicated, drifting client layer).
- Shell mock (build-approach artifact): `docs/v2-design/shell-mock/index.html` —
  the throwaway static Tailwind-v4 + `@theme`-tokens prototype for settling IA +
  the theme system before the `lib/` merge. Delete once the real shell ships.

## Amendment (2026-07-06) — operating-surface design settled via the shell mock

An extended mock-iteration session settled the shell's IA and the operating
surface's interaction model well beyond what the original ADR sketched. The mock
(`docs/v2-design/shell-mock/index.html`) is the **visual spec**; this amendment is
the **behaviour spec** for the Svelte build. Decisions:

- **Three-column shell.** Left **nav rail** (destinations) · centre **content** ·
  right **util rail** (operating info panels). Both rails **collapse full↔narrow**,
  **independently**, **persisted** to `localStorage`; both **default expanded**
  (labels help new operators). Narrow = icon-only rail with hover flyouts.
- **Right util rail is Operate → Phone/CW-scoped.** Hidden on Dashboard / Logbook /
  Settings / FT8; its content offset + the header's top-right extension are gated
  on the same flag so other views use full width. Panels: **Worked · Session ·
  Details · Rig · Pile-up**.
- **Panel model.** Worked/Session/Details/Rig open a **card *below* a compact,
  fixed-size logging card** (one panel at a time; the rail button highlights).
  **Auto-open on the relevant event:** Worked on **callsign entry**; Rig on a
  **CAT change** (as a **badge** on the rail button, never a force-takeover of the
  card, so it can't yank away a panel you're reading).
- **CAT / rig gate.** A header **freq/mode/band chip** is the always-visible glance
  anchor *and* the gate's status light (live / manual / warning). The rig fields
  (freq/mode/band/power) live in the **Rig panel**, editable. **When CAT is off,**
  entering Operate **auto-opens the Rig card and blocks logging until an explicit
  Set/Confirm** — confirm **once per band** (pre-filled from last session, so it's
  a fast confirm not data entry), the gate **auto-lifts if CAT comes online**, and
  the chip shows the state throughout. When CAT is on, values are read-mostly and
  correct automatically. (Generalises to a "confirm must-be-right session state
  before logging" pattern — e.g. operator profile in a contest.)
- **Desktop-only.** 64rem `min-width` floor; below it the page scrolls, never
  reflows. No mobile/tablet layout — a tablet can't host the daemon (serial/audio/
  CGO), and real mobile is a separate effort tied to online/smcloud access.
- **Theme.** One `@theme` token system, `[data-theme]` dark swap, `@custom-variant`
  so `dark:` follows the attribute. Values from Tailwind Plus, adapted to tokens.

**Pile-up drawer — implementation requirements** (captured from mock niggles;
deliberately *not* solved in the throwaway mock — they need reactive layout):

- **Docked, not transient**: opened from the right rail; **stays open while the
  queue has items**. Starts at the **header bottom** (`top: header-height`), not
  full height.
- **Never overlaps the logging card** — maintain a **≥2rem gap**. When width
  tightens, **the content/card moves (and/or scrolls), the drawer holds position**;
  the fixed-size logging card must remain **fully visible and accessible** (you log
  *from* the card while working the queue *in* the drawer — non-negotiable).
- **No flash on rail collapse**: don't tie the closed drawer's rest transform to an
  animating variable (the rail-width var), or it re-animates when the rail
  collapses. Park it off-screen by a fixed amount / gate its transition to
  open↔close only.

**Build.** `frontend/app/` (Vite + Svelte 5) scaffolded 2026-07-06 — a clean-slate
consolidated SPA (the three existing SPAs had bloated / drifted). Design system
carried 1:1 from the mock (`src/styles/app.css`). Built up step by step: shell
chrome → routing → operating surface → the CAT-gate **state model in runes**
(`$state`/`$derived` — the mock's growing vanilla-JS state was the signal to
switch) against **stubbed data**; backend plumbed later. `frontend/app/` is a new
directory — it does not touch the shipping `frontend/logging/`, so it can't
destabilise the 7Q8AC release; the only cost is attention.
