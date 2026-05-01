---
number: 0006
title: CAT-state precedence — bridge wins when connected; SPA edits accepted otherwise; transitions surfaced via notification
status: Accepted
date: 2026-05-01
---

> **Implementation refined by [ADR 0009](0009-cat-state-decomposition.md)** (same day). 0006's behavioural rules — precedence, edit-affordance lockout, transition handover, connection-only liveness, split derivation — all stand. 0009 changes the *data shape*: instead of a single mutable `catState` with runtime-conditional ownership, four state objects (`catState` immutable mirror, `manualState`, `configState`, derived `displayedState`) implement the same rules through static ownership. Read 0009 for the actual implementation; 0006 for the behaviour it produces.

# 0006 — CAT-state precedence rule

## Context

ADR 0004 framed `cat.svelte.ts` as "a reactive view over rig state — not authoritative." That left a gap: when CAT is *off* (operator hasn't enabled it, or the bridge is unreachable, or the rig is powered down), nothing is feeding the view. Either the SPA renders defaults forever, or the operator gets to write into `catState` and the SPA owns it for that period.

Three operator-stated requirements for the manual-operating case (settled this conversation, 2026-05-01):

1. **Manual operating mode is first-class** — operator should be able to type frequencies, swap selected VFO, etc. when CAT is unavailable.
2. **Edit affordances disabled when CAT is operating** — operator can't fight the rig.
3. **Operator-edited values must not be silently lost** when CAT comes online.

Three sub-questions fall out of those requirements: who writes when, what counts as "the bridge is operating," and what happens at the transition between modes.

## Decision

### Three state singletons, partitioned by ownership

| State | Lives in | Source of truth |
|---|---|---|
| `catState` (vfoA, vfoB, mode, subMode, selectedVfo, splitOverride, rigIdentity, enabled) | `lib/states/cat.svelte.ts` | Bridge SSE when CAT connected; SPA edits otherwise |
| `qsoDraftState` (callsign, RST sent/rcvd, name, QTH, country, grid, notes, …) | `lib/states/qsoDraft.svelte.ts` *(planned)* | SPA edits + enrichment results |
| `bridgeState` (connected, lastEventAt) | `lib/states/bridge.svelte.ts` *(planned)* | The EventSource itself |

QSO submit reads from all three at submit-time and POSTs to `/v1/qso`. **No state field is duplicated across singletons** — each field lives in exactly one place.

### Precedence rule

- **When `catState.enabled && bridgeState.connected`:** all CAT-state edit affordances are read-only — frequency inputs, VfoBox click handlers, the Mode dropdown. Only the bridge SSE writes to CAT-state fields.
- **Otherwise:** SPA edits are accepted. The operator can type, click, and dropdown.
- **Edit-enable check** is implemented once as a derived helper: `const editable = $derived(!(catState.enabled && bridgeState.connected))`. Every editable component reads it.

### Liveness

"Connected" is determined by `EventSource.readyState === OPEN`. **Connection-only — no heartbeat in v1.** A connected-but-quiet bridge stays locked to the rig's last-known values; nothing-changing-on-the-rig is indistinguishable from rig-not-responding without a heartbeat, and we accept that ambiguity for v1.

### Transition handover

- **Bridge connect (after disconnect, or first ever):** SPA unconditionally adopts the rig's first reported state. Operator's manual edits prior to that moment are superseded. Framing: this is the *act of CAT handover*, not silent data loss — the operator chose to enable CAT.
- A **notification toast** ("CAT connected — reading rig state") fires once on transition. Surfaces via the planned toast system (`docs/v2-design/notifications.md`). Until that system exists, the transition is silent — flagged as known technical debt.
- **Bridge disconnect:** edit affordances re-enable. Current displayed values stay until the operator changes them; manual edits during disconnect are valid for that period.

### Split derivation in CAT-off mode

- `vfoA`, `vfoB` are `$state` — always editable when SPA can write.
- `split` is a derived getter on `catState`: `splitOverride ?? (vfoA !== vfoB)`.
- CAT-off: `splitOverride === null`, split derives from frequency divergence. Operator typing different frequencies into A and B implicitly enters split.
- CAT-on: bridge writes `splitOverride: boolean` from the rig's reported state, overriding the divergence rule.
- **Accepted limitation:** same-frequency split (rig has SPLIT-on but `vfoA === vfoB`) is not representable in CAT-off mode. Operator must introduce a small divergence. Trigger to revisit: if operators routinely need same-frequency split. Fix is additive (an explicit SPLIT toggle button); no need to design now.

## Alternatives considered

### Bridge wins silently on transition

Naive reading of "rig is authoritative" — operator's typed values overwritten the moment the bridge connects, no signal. Rejected: bad UX, silent data loss is exactly what the operator pushed back against.

### Confirmation prompt on transition

Modal dialog: "Bridge connected. Rig says X, you typed Y. Keep which?" Rejected: introduces friction at the wrong moment. CAT just came back; the operator wants to keep operating. They already chose CAT-on by enabling it; asking again is redundant.

### Preserve manual edits across transition with diff resolution

SPA edits become "intent statements" preserved until the rig sends a *changing* value. Rejected: complex (merge logic, conflict resolution UI), and fights the operator's intent — they enabled CAT *because* they want the rig to be in charge.

### Unconditional read with surfaced transition (chosen)

Bridge wins on transition; toast surfaces the handover. Frame the moment as "CAT taking over," not "operator's edits being overwritten." Operator's manual edits were valid for the CAT-off period; CAT-on resets to whatever the rig actually has.

### Liveness-timeout vs connection-only

Considered tracking last-event-time and re-enabling SPA edits if no event arrived for N seconds. Rejected: rig-not-changing is indistinguishable from rig-not-connected via timeout alone — that ambiguity is exactly what heartbeats fix, and heartbeats are deferred. Connection-only is simpler with a clear operator mental model: "is the SSE channel open?"

## Consequences

**Signed up for:**

- **Three state singletons** rather than one fat `appState`. Each has clear ownership and explicit boundaries.
- **`splitOverride: boolean | null` on `catState`.** The bridge writes it when CAT is on; SPA-off-mode leaves it `null`. The `split` getter handles both. Public API stays single — every consumer reads `catState.split`.
- **Notifications system load-bearing for the CAT-handover toast.** Either build it before the bridge lands, or accept that the first bridge connection has no visible cue. Flag as technical debt in `bridge.md` when that doc is written.
- **Edit-affordance state must be uniformly checked.** A `$derived editable` helper avoids per-component drift. Components that don't follow the helper are bugs.
- **No graceful rig-went-silent detection in v1.** A rig that stops responding while the bridge stays connected appears "live" to the SPA. Acceptable for v1 personal use; revisit if it bites.

**Accepted costs:**

- **Same-frequency split unrepresentable in CAT-off mode.** Documented limitation; additive workaround later.
- **First-ever bridge connect has no toast** until the notifications system exists. Brief technical debt window.

**Gained:**

- **Manual operating without CAT is a supported deployment,** not a degraded fallback. Operators logging FT8 by hand, contesting from a paper notebook, or running a rig with no CAT cable are first-class.
- **One rule for all CAT-state fields.** `vfoA`, `vfoB`, `mode`, `subMode`, `selectedVfo`, split — same precedence, same edit-enable check.
- **Transitions are surfaced, not silent.** The toast on CAT-connect is the discoverable signal.

## Triggers to revisit

- **Silent rig-stalls reported by operators** — rig stops responding to CAT but the bridge stays connected. Then heartbeat is justified. Two shapes available when implementing: (a) bridge emits a `keepalive` SSE event every N seconds — pure transport liveness, or (b) CAT-poll-derived — bridge polls rig anyway, emits a `cat-stalled` event after N consecutive failed polls — semantic. Lean toward (b) when implementing; carries more information.
- **Same-frequency split becomes a real complaint.** Add an explicit SPLIT toggle that sets `splitOverride = true` even when frequencies match.
- **Toast on CAT-connect missed by operators** (transient, easy to look away from). Add a persistent connection-state indicator in a status bar. Orthogonal addition; doesn't change this rule.
- **Multi-operator scenarios emerge.** "One operator, one rig" assumption underpins this rule. Multi-operator gives concurrent CAT-state writers; this rule wouldn't generalise cleanly.
- **Keyboard shortcuts** (ADR 0007, forthcoming) introduce a "force manual override even with CAT on" mode. Could change "CAT lockout" to "CAT-soft-lockout-with-explicit-override." Out of scope here.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — SPA-hosted-by-daemon premise.
- ADR 0003 (`0003-spa-config-daemon-only.md`) — config layering; `catState.enabled` lives in operator config.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — `cat.svelte.ts` framed as a reactive view; basis for this rule.
- ADR 0005 (`0005-enrichment-pipeline-shape.md`) — enrichment results land in `qsoDraftState` per the partitioning specified here.
- ADR 0007 (`0007-keyboard-shortcuts.md`, forthcoming) — keyboard map for at-speed operating; some shortcuts will write to `catState` (e.g. swap-VFO) so this precedence rule applies.
- `docs/v2-design/bridge.md` (forthcoming) — bridge HTTP/SSE surface; heartbeat deferral note will live there.
- `docs/v2-design/notifications.md` (forthcoming) — toast system; CAT-connect handover toast belongs here.
- `frontend/logging/src/lib/states/cat.svelte.ts` — current CAT state; will gain `splitOverride: boolean | null` and have `split` become a derived getter.
- (Planned) `frontend/logging/src/lib/states/bridge.svelte.ts` — `connected` boolean.
- (Planned) `frontend/logging/src/lib/states/qsoDraft.svelte.ts` — in-progress QSO data.
- Memory `project_sm_cat_precedence_rule` — distilled rule for future sessions.
