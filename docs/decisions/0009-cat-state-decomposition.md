---
number: 0009
title: CAT state decomposition — four state objects with structural ownership
status: Accepted
date: 2026-05-01
refines: 0006
---

# 0009 — CAT state decomposition — four state objects with structural ownership

## Context

ADR 0006 settled the **precedence rule** for CAT state: the bridge wins when connected, the SPA edits otherwise, and the bridge-connect transition is unconditional handover with a notification toast. Implementation was framed as "`catState` fields are mutable; bridge writes when CAT is on, SPA writes when off, with a UI-layer `editable` helper gating the SPA's write affordances."

That framing made `catState` a mode-flipped state object — its authority depended on runtime conditions. The smell surfaced during this session's discussion: **a single object pulling double duty as "rig mirror" and "operator-input store"** is the kind of thing that bites later. Two concrete reasons to reconsider:

1. **Power + linear amp.** Power in CAT-state isn't simply "rig says X or operator types X." It's `effectivePower = ragRawPower × ampMultiplier`, where `rigRawPower` is CAT-reported and `ampMultiplier` is an operator-declared station property. There's no slot for these two distinct concepts in a single mode-flipped `catState`. The pattern recurs for transverter offset, antenna gain, and similar station-shape transformations.

2. **Derived semantics.** `split` and `selectedVfo` should be *projections* over more primitive state — `split = (vfoA !== vfoB)` in CAT-off mode, no field needed. With a mode-flipped `catState`, you either store `split` redundantly or write a runtime branch at every read. Both are messy.

The fix is structural: **partition the state into multiple objects, each with one writer, and make the "current displayed value" a `$derived` view over them.** Static ownership replaces runtime guards.

This ADR is a **refinement of ADR 0006's implementation**, not a supersession. Every behavioural property 0006 settled — the precedence rule, edit-affordance lockout, transition handover, connection-only liveness, split-from-divergence in CAT-off, same-frequency-split limitation — stands. What changes is the data shape that *implements* those properties.

## Decision

**Four state objects.** Each has a single writer; reads happen through a derived view.

### 1. `catState` — pure rig mirror

- **Writer:** bridge SSE only.
- **Readers:** `displayedState` derivation. Components MUST NOT read `catState` directly except inside the derivation itself.
- **Fields:** `enabled` (sourced from configState — see note), `rigIdentity`, `vfoA`, `vfoB`, `mode`, `subMode`, `selectedVfo`, `splitOverride`, `power`, … (grows as more CAT-reported fields land).
- **Initial values:** unset / sentinel until first SSE event.
- **Mutation discipline:** the SPA NEVER writes to `catState`. Programmer error if it does. This is structurally enforced by keeping write paths only inside `lib/states/bridge.svelte.ts` (the SSE consumer); the export shape can omit setters or use a frozen-on-read pattern if discipline drift becomes a concern.
  - **One narrow exception (added 2026-06-15, ADR 0034):** `swapVfoLive` (`actions/rigControl.ts`) optimistically sets `catState.vfoB ← catState.vfoA` on a VFO swap. A dual-VFO rig (FTdx10) pushes both VFOs back after a swap, so confirm-by-push repaints both boxes; but a **single-RX rig (IC-7300)** confirms the swap with a bare CI-V ACK and never pushes VFO-B, and the daemon's read-after-swap only refreshes the operating VFO (→ `catState.vfoA`). After CI-V `07 B0` the rig's VFO-B genuinely holds the old VFO-A, so the SPA reflects that immediately rather than leaving the box stale forever. It mirrors a swap the rig **confirmed** (not invented state), and on a dual-VFO rig the rig's own VFO-B push overwrites it at once — so it's harmless there. This is the sole sanctioned SPA write to `catState`; any other is still programmer error.

### 2. `manualState` — operator's manual edits in CAT-off mode

- **Writer:** SPA only.
- **Readers:** `displayedState` derivation.
- **Fields:** the *subset* of `catState` fields that are operator-writeable: `vfoA`, `vfoB`, `mode`, `subMode`, `selectedVfo`, `power`. Plus any future field where "operator declares X manually" makes sense.
- **Initial values:** hardcoded defaults at boot per ADR 0003 (`DEFAULT_VFO_HZ`, `DEFAULT_MODE`, etc.).
- **Mutation discipline:** the bridge NEVER writes to `manualState`. (Exception: the snapshot-on-disconnect rule below — but that's an effect inside `manualState`'s own module, not a write from the bridge.)

### 3. `configState` — operator configuration from the daemon

- **Writer:** SPA's config-loader (POST /PUT to daemon, then refresh local state).
- **Readers:** `displayedState` derivation; settings UI panels read directly for editing.
- **Fields:** loaded from `/v1/config` per ADR 0003. Schema TBD; relevant sub-section here is **station properties** — `ampMultiplier`, `antennaGain` (future), `transverterOffset` (future), `stationCallsign`, `gridSquare`, plus the `enabled` toggle that controls whether CAT is in use.
- **Lifecycle:** boots from hardcoded fallbacks, fetches `/v1/config` on app start, updates state. Edits PUT to daemon, then update local state on success.
- **Note on shape:** ADR 0009 doesn't dictate whether `configState` and a separate `stationState` are one module or two. The simplest is one `configState` with a `station` sub-section accessed as `configState.station.ampMultiplier`. The decomposition principle in this ADR doesn't depend on which.

### 4. `displayedState` — derived view, no own storage

- **Writer:** none — `$derived.by(...)` only.
- **Readers:** every component that wants to render or use CAT-state values.
- **Fields:** projections over `catState`, `manualState`, `configState`, gated on `bridgeState.connected`.
- **Examples:**

```ts
displayedState = $derived.by(() => {
    const live = catState.enabled && bridgeState.connected;
    const rawPower = live ? catState.power : manualState.power;
    return {
        vfoA:        live ? catState.vfoA        : manualState.vfoA,
        vfoB:        live ? catState.vfoB        : manualState.vfoB,
        mode:        live ? catState.mode        : manualState.mode,
        subMode:     live ? catState.subMode     : manualState.subMode,
        selectedVfo: live ? catState.selectedVfo : manualState.selectedVfo,
        split: live
            ? (catState.splitOverride ?? false)
            : (manualState.vfoA !== manualState.vfoB),
        rawPower,
        effectivePower: rawPower * configState.station.ampMultiplier,
        rigIdentity: live ? catState.rigIdentity : '',
    };
});
```

`displayedState` is what every component reads. The QSO-submit action reads from `displayedState` and `qsoDraftState` (per ADR 0006's partitioning) — never from `catState` directly.

### Snapshot rule on CAT-off transition (recommended, not mandated)

When the bridge disconnects (CAT-on → CAT-off), `manualState` SHOULD adopt the most recent `catState` field values. This means:

- **First-ever boot, no CAT contact yet:** `manualState` = hardcoded defaults. Operator types from defaults.
- **First CAT connect:** `displayedState` reads `catState`. `manualState` still holds defaults (or the operator's prior typing, untouched).
- **CAT disconnect after first connect:** snapshot `catState` → `manualState`. Operator continues from where the rig left off, not from old defaults or stale typed values.
- **CAT reconnect after disconnect period:** unconditional read of rig state (per ADR 0006). `manualState` holds whatever the operator typed during the disconnect, but `displayedState` reads from `catState` again.

The snapshot is a one-way effect inside `manualState`'s own module, watching `bridgeState.connected`. The bridge module never writes to `manualState`.

This rule is **recommended** because it provides continuity of values across CAT cycles. The alternative (manualState wholly orthogonal — never touched by transitions) is simpler but means "stale yesterday's manual frequency" can pop back up after a CAT-off transition. **Implementation may choose either**; the structural decomposition in this ADR doesn't depend on the snapshot rule.

### What about the `editable` helper from ADR 0006?

Still needed, for **write-affordance UI state** — disabling buttons, making inputs read-only, etc. The structural decomposition prevents the SPA from corrupting `catState` even if `editable` is forgotten somewhere, but `editable` remains the source of truth for "should this UI element accept input." So the rule from 0006 stands; the structural change just makes it less load-bearing.

## Alternatives considered

### Mode-flipped `catState` with `editable` runtime guard (ADR 0006's original framing)

Single state object, runtime-conditional ownership. Rejected on grounds developed in this session's discussion: smell is real (operator's own description); power-with-amp doesn't fit the model; `split` and `selectedVfo` either get redundant storage or per-read branches; tests can't assert ownership. Discipline-by-convention is fragile as more writers (keyboard shortcuts, future features) come online.

### Tagged single object — every field carries `{ value, source }`

`catState.vfoA = { value: 14_250_000, source: 'manual' | 'rig' }`. Reading is `catState.vfoA.value`; writing checks `source` for permission. Rejected: more verbose at every read site, and tags become a new thing to maintain. Doesn't match how fields are consumed.

### Two-object split (`catState` + `manualState`) without `configState` distinction

Earlier in the discussion this was the "Option B'" — split the writeable subset, keep `catState` immutable from SPA. Rejected (or rather, expanded into the four-object form) when the power-with-amp example surfaced: amp multiplier doesn't belong in either `catState` (rig-reported) or `manualState` (per-edit operator input). It's a **station property** — operator-declared, persistent, sourced from operator config. Forcing it into one of the two existing objects either pollutes `catState` with non-rig data or pollutes `manualState` with non-per-edit data. The third object (`configState` / `stationState`) is correct.

### Four objects (chosen)

`catState` (rig mirror) + `manualState` (per-edit operator input) + `configState` (operator-declared persistent properties) + `displayedState` (derived). Each object has one writer; the derived view is what components read. Static ownership; no runtime guards needed for *reads*.

## Consequences

**Signed up for:**

- **Four state objects** instead of one. Implementation cost: ~80 lines for the derivation + adjustments to the bridge module to write to `catState`, the SPA's editable components to write to `manualState`, and the config loader to populate `configState`.
- **One read pattern across the SPA: `displayedState`.** Components don't read `catState` or `manualState` directly. This is a discipline rule that needs to be reinforced when reviewing new components. Eslint/grep audits for `catState\.` reads outside the derivation file are cheap to set up if drift becomes a concern.
- **`displayedState` is `$derived.by(...)` and depends on multiple state objects.** Svelte 5's reactivity is granular per-field, so consumers only re-render when their specific field changes. Performance impact is negligible at this scale.
- **The snapshot-on-CAT-off rule** (if adopted) is one effect in `manualState`'s module. Small.
- **Tests can assert ownership invariants.** A test that the bridge module never writes to `manualState`, or that the SPA never writes to `catState`, is straightforward — and the structural shape makes it almost tautological.
- **`editable` helper from ADR 0006 stays.** Same role, just less load-bearing — it gates *UI affordances*, not data integrity.

**Accepted costs:**

- **More machinery for the operator-edit path.** A frequency commit goes: VfoInput → onCommit → `manualState.vfoA = hz` → `displayedState.vfoA` recomputes → input re-renders. One more hop than the current `catState.vfoA = hz` direct write. The hop is mechanical and trivial, but it's there.
- **The "what does this component read?" question gets one more answer: "it reads `displayedState`, not the underlying state objects."** Newcomers (future-Claude, future-you, anyone who joins) need to learn the pattern. The ADR is the documentation.
- **Snapshot rule is judgement-call territory.** Operator UX preference might land on either side; we accept that this might iterate.
- **Three state-object files instead of one.** Slightly more directory clutter; minimal cost.

**Gained:**

- **Static ownership** for the most-touched state in the SPA. Bugs that try to write to the wrong place are programmer errors visible at type/import level, not runtime behaviour drift.
- **Power + amp + transverter offset + antenna gain** all fit naturally as derived computations in `displayedState`. The pattern scales without further architectural change.
- **`split` and `selectedVfo` are structurally derived.** No-one writes to `displayedState.split = true`; it's not a writeable field. To set split (in CAT-off mode), you change a VFO frequency. This is the immutability the operator's instinct was reaching for.
- **`configState` partitioning naturally.** ADR 0003 implementation now has a clearer target: `configState` is one of the four state objects, with a station sub-section that stations properties live in.
- **Test surface clearer.** "Bridge populates `catState` correctly" and "SPA edits populate `manualState` correctly" are independent tests. "Display value is correct under all bridge states" is a `displayedState` test that doesn't need a stub bridge or stub UI.

## Triggers to revisit

- **`displayedState` becomes a perf bottleneck.** Extremely unlikely at SM's scale — Svelte 5's granular reactivity means consumers subscribe to specific fields. If it ever happens, splitting `displayedState` into multiple smaller derived views (or using `$derived` per field rather than one `$derived.by` block) is the fix.
- **A field doesn't fit the four-object model.** E.g. a value that's both rig-reportable and operator-overridable mid-CAT-on (something we've explicitly rejected for v1 with the "edit affordances disabled when CAT operating" rule, but a future feature might want). Then the model gains a fifth object or a derivation refinement. Not a structural problem; this ADR's bones can flex.
- **Multi-rig.** "Rig 1" and "Rig 2" might want their own `catState` instances. Then either a per-rig keyed map of `catState` objects, or per-rig state-object files. Same decomposition principle, applied per-rig.
- **The snapshot-on-disconnect rule turns out wrong.** Operator might prefer truly orthogonal `manualState`. Adjustable without changing 0009's structure.
- **The "components only read `displayedState`" rule erodes.** If new components routinely read `catState` directly because the derived view is missing the field they want, that's a signal `displayedState` needs to grow. Mechanical.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — SPA hosted by daemon.
- ADR 0003 (`0003-spa-config-daemon-only.md`) — `configState` is the SPA's view of `/v1/config`. ADR 0009's `configState` sub-section is what 0003 was implying.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — daemon owns persistence (config), SPA owns UI reactivity (the four state objects + derivation).
- ADR 0006 (`0006-cat-state-precedence-rule.md`) — the **behavioural** rules this ADR refines structurally. ADR 0006 stays as written; its references will be updated to point at 0009 as the implementation refinement.
- ADR 0007 (`0007-keyboard-shortcuts.md`) — `editable` helper used by CAT-mutating shortcuts; helper still needed under 0009 for write-affordance UI state.
- Memory `project_sm_cat_precedence_rule` — will be updated to reflect the four-object decomposition.
- (Planned) `frontend/logging/src/lib/states/cat.svelte.ts` — refactored to be SPA-read-only mirror of bridge SSE events.
- (Planned) `frontend/logging/src/lib/states/manual.svelte.ts` — operator's manual edits in CAT-off mode.
- (Planned) `frontend/logging/src/lib/states/config.svelte.ts` — operator config from daemon.
- (Planned) `frontend/logging/src/lib/states/displayed.svelte.ts` — derived view, what components read.
- (Planned) `frontend/logging/src/lib/states/bridge.svelte.ts` — SSE transport + the snapshot-on-disconnect effect, as named in ADR 0006.
