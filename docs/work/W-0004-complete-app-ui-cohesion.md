# W-0004 — Complete app UI cohesion and ambient build identity

**Status:** Open — palette decisions required
**Priority:** P2 post-ship
**Selected:** 2026-08-18
**Outcome:** The consolidated app presents trustworthy build identity and a coherent, readable
visual system across its supported themes and FT8 occupancy states.

`W-0004` is an immutable identity. Its priority and status may change, but its ranked position
remains in [`docs/backlog.md`](../backlog.md), the only priority owner.

## Verified current state

The old “UI cohesion across all SPAs” premise is mostly historical. `frontend/app` already owns one
semantic token layer in [`app.css`](../../frontend/app/src/styles/app.css), a light/dark toggle, an
OS-preference default, pre-paint theme selection, persisted local override, and cross-tab theme
synchronization. ADR 0044 deliberately chose `localStorage` for that device-local override.
W-0003 owns retiring the remaining config/logbook builds; this item does not need to converge their
styles first.

Three narrower outcomes remain:

- [`Sidebar.svelte`](../../frontend/app/src/lib/ui/Sidebar.svelte) displays the literal
  `v2.0.0-alpha.1`, while the running daemon's authoritative build string and environment are
  already available from `GET /v1/version`. The browser title remains the static “Station Manager”.
- The canonical HTTP reference says a development daemon is marked with a DEV pill and tab-title
  prefix. The retained legacy clients implement that behavior, but the primary app does not.
- The FT8 Channels and Spectrum views still use their first-pass literal red/green/amber/orange
  classes. A later light-mode dogfood report said the busy/clear fills and recommendation markers
  wash out or read incorrectly; no subsequent palette revision is present. The exact replacement
  palette was never selected.

The earlier request for named operator-selectable themes also remains undecided. It must not be
confused with the shipped light/dark theme, nor silently implemented by reviving the vestigial FT8
CQ-highlight settings that the current app intentionally preserves but does not render.

## Scope

This work item owns the remaining consolidated-app cohesion outcome:

- replace hard-coded build identity with the running daemon's `/v1/version` facts at an ambient
  surface and in the browser title;
- preserve the existing fail-soft app boot while making development and release daemons visually
  distinguishable;
- select and apply occupancy colors that remain clear in both light and dark themes; and
- decide whether the shipped light/dark pair is the complete theme feature or whether named
  operator-selectable palettes still have a concrete use case.

It does not own legacy-SPA retirement or route migration (W-0003), a general component redesign,
the FT8 waterfall, changes to occupancy/proximity logic, mobile layout, the zero-JS manual, or a new
version endpoint. It does not authorize deleting or repurposing existing config fields merely
because their current UI is vestigial.

## Operator-observable acceptance criteria

1. The expanded app shell shows the exact `daemon` build returned by `/v1/version`; no source
   constant can claim a release that is not running. The nearest confusable outcome—a new daemon
   still labelled `v2.0.0-alpha.1`—must fail the test.
2. The browser title carries ambient build identity without hiding the current view's identity.
   A development daemon is unmistakably marked in both the shell and title; a release daemon is
   not falsely labelled DEV. The standalone Map tab follows the same rule.
3. If `/v1/version` is unavailable or malformed, Operate and Settings still load and the identity
   surface says unavailable or stays neutral; it never falls back to a plausible fabricated
   version. A later successful fetch or page reload recovers normally.
4. The existing light/dark selection still applies before first paint, persists for that browser,
   and synchronizes to an already-open Map tab. The work must characterize these shipped behaviors
   before changing theme state.
5. For Channels, busy, clear, selected, and recommended states remain distinguishable in both
   themes. For Spectrum, signal shading, clear/near/sharing footprints, selected offset, and
   recommended markers remain distinguishable in both themes. Fixtures include adjacent and
   overlapping states so a palette that works only on an empty strip cannot pass.
6. Color is not the sole carrier of selected/recommended state or operator-facing status; existing
   arrows, symbols, text, titles, and accessible names remain accurate after the palette change.
7. If named palettes remain in scope after the operator decision, every selected palette uses the
   same semantic tokens and passes criteria 4–6. If they are declined, the backlog and canonical
   references say explicitly that light/dark is the supported theme set.

## Decisions required before implementation

- Are named palettes still wanted beyond the shipped light/dark pair? If yes, which concrete
  palettes justify the feature, and is the choice device-local like ADR 0044's current toggle or a
  station-wide config setting? A station-wide choice changes the config/API contract and needs its
  own weighed decision.
- Which light and dark occupancy palette reads correctly to the operator? Review representative
  Channels and Spectrum fixtures before choosing values; do not invent a contrast threshold or
  palette from prose alone.
- What exact title grammar keeps the current view, build, and DEV marker readable without producing
  an excessively long browser tab label?
- Should a transient version-fetch failure retry in-session, or is an honest unavailable state
  until reload sufficient? This is presentation policy, not a reason to block app startup.

## Verification standard

Write failing component/state assertions first for injected release, development, malformed, and
unreachable version responses. Prove the old hard-coded chip fails the authoritative-version case.
Use deterministic occupancy fixtures covering every state in criterion 5 and inspect them in both
themes at the operator's chosen palette; source-class assertions alone cannot prove readability.
Run the app's lint, format check, Svelte check, and Vitest suite. No RF, audio device, CAT rig, or
hardware-dependent action is needed.

## References

- [`docs/backlog.md`](../backlog.md) — authoritative ranking.
- [`ADR 0044`](../decisions/0044-consolidate-operator-spas-into-one-shell.md) — one token system,
  dark variant, and device-local theme override.
- [`W-0003`](W-0003-retire-legacy-operator-spas.md) — legacy route/embed/build retirement boundary.
- [`docs/v2-design/api-endpoints.md`](../v2-design/api-endpoints.md) — current `/v1/version` wire
  contract and documented DEV distinction.
- [`Ft8OccupancyStrip.svelte`](../../frontend/app/src/lib/operate/Ft8OccupancyStrip.svelte) and
  [`Ft8OccupancySpectrum.svelte`](../../frontend/app/src/lib/operate/Ft8OccupancySpectrum.svelte) —
  current occupancy state presentation.
