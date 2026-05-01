---
number: 0011
title: manualState persistence — localStorage per-field, distinct from config layer
status: Accepted
date: 2026-05-01
---

# 0011 — manualState persistence — localStorage per-field

## Context

ADR 0009 established `manualState` as the SPA-side singleton that holds operator edits in CAT-off mode. As implemented in step 2c (same day), it lives in browser memory only — every browser refresh resets all fields to hardcoded defaults from `cat.svelte.ts`. The operator's typed VFO frequencies, mode picks, selected VFO, and so on vanish on reload.

The operator surfaced this as a real problem: *"if I edit the VFO-B field thus indicating split working, and then refresh my browser, the value of VFO-B should remain the same."* The current behaviour fails that expectation.

ADR 0003 explicitly rejected localStorage as a *config* cache because the daemon is the authoritative store for operator config and the SPA is always loaded from the daemon (so reachability is guaranteed). That reasoning was specific to *config* and doesn't apply to *transient operator session-state* — the question this ADR settles.

The distinction:

| Concept | What it is | Source of truth | Persists across refresh? |
|---|---|---|---|
| `configState` | Operator-declared persistent preferences (CAT enabled, amp multiplier, station identity, forwarding creds) | Daemon `/v1/config` (filesystem) | Yes (via daemon — ADR 0003) |
| `manualState` | Transient operator activity in CAT-off mode (current VFO frequencies, current selectedVfo, etc.) | Operator's keyboard | **Decided here** |

ADR 0003's collapse to "no localStorage" applies to `configState` because the daemon already provides cross-device sync and durable storage. `manualState` is a different category — it's *what the operator was doing in this session*, not *what the operator declares to be true about their station*. The right scope for that is "this device, this browser" — which is exactly what localStorage represents.

## Decision

**Persist `manualState` fields to `localStorage`, per-field, under the `sm.manual.<fieldName>` key namespace.** On module load, hydrate fields from localStorage where present; otherwise use the hardcoded defaults from `cat.svelte.ts`. On every field write, mirror to localStorage. Failures (quota exceeded, private browsing, localStorage disabled) are silently swallowed — the SPA continues working with in-memory state.

### Scope

Persisted fields:

- `manualState.vfoA` (number, Hz)
- `manualState.vfoB` (number, Hz)
- `manualState.mode` (string, ADIF MODE)
- `manualState.subMode` (string, ADIF SUBMODE)
- `manualState.selectedVfo` (`'A' | 'B'`)
- `manualState.power` (number, watts)

These are the entire current `ManualState` field set. As new fields are added (per ADR 0009's decomposition), they follow the same pattern.

### Key namespace

`sm.manual.<fieldName>` — e.g. `sm.manual.vfoA`, `sm.manual.selectedVfo`. Per-section, per-field keys (not one big JSON blob) so partial corruption of one field doesn't poison the others.

The `sm.config.*` namespace was reserved by the (now superseded) ADR 0002 for a hypothetical config localStorage cache. ADR 0003 removed that cache; `sm.config.*` is now unused. `sm.manual.*` reserved here doesn't collide.

### Hydration: parse + validate per field

Each field has an explicit parser that handles the localStorage-as-string-only nature:

- `vfoA`, `vfoB`, `power`: `Number(s)`, fall back to default if `Number.isNaN`.
- `mode`, `subMode`: pass-through string.
- `selectedVfo`: validate against `'A' | 'B'`, fall back to default if neither.

Invalid stored values silently fall back to the hardcoded default. Don't crash on corrupted localStorage.

### Mirroring writes

A `$effect.root` block at module load registers per-field effects that mirror writes to localStorage. Svelte 5's reactivity tracks the read inside each effect, so the effect re-runs when (and only when) that specific field changes.

This is symmetric: writing `manualState.vfoA = 21_200_000` triggers the effect which writes `"21200000"` to `localStorage['sm.manual.vfoA']`. The next refresh reads it back.

### Failure handling

`localStorage` access can throw in three scenarios:

- **Quota exceeded** — operator has so much data in localStorage that writes fail. SM's payload is a few hundred bytes; this is implausible.
- **Private browsing** — some browsers disable localStorage in private/incognito mode.
- **localStorage explicitly disabled** — operator has turned it off in browser settings.

Both load and save paths wrap their localStorage access in try/catch and silently fall back — load returns the default, save no-ops. The SPA continues working in-memory; the operator just loses the persistence benefit.

### What's not persisted

- `catState` — written only by the bridge SSE; persistence makes no sense (would write the wrong data on the next session before the bridge reconnects).
- `bridgeState` — transport state; refreshed naturally on reconnect.
- `configState` — daemon-authoritative per ADR 0003.
- `qsoDraftState` (planned) — same persistence question will arise; the same pattern can apply when that state object lands. Out of scope for this ADR; flag if it becomes an issue.

### Cross-tab semantics

Deferred. If the operator opens two SPA tabs, each tab maintains its own in-memory `manualState`. localStorage `storage` events fire across tabs but the v1 implementation does *not* listen for them — the two tabs would diverge until both are refreshed.

For SM's single-operator personal-use case this is unlikely to bite. **Trigger to revisit:** if the operator routinely runs multiple SPA tabs (e.g. logging from desktop browser and a tablet browser open to the same daemon).

## Alternatives considered

### Persist via the daemon (`PUT /v1/operator-state` or extend `/v1/config`)

Add a daemon-side endpoint that stores manual-state in the daemon's filesystem. Cross-device sync (operator switches from laptop to tablet, picks up where they left off).

Rejected for v1: the round-trip cost on every commit is overweight for transient session state, and cross-device sync isn't a real need for SM's single-operator-single-machine usage. Daemon-side persistence makes sense for *operator preferences* (configState) where the value is "what's true about this operator," not for *operator activity* where the value is "where this operator was in this session." Trigger to revisit if multi-device usage emerges.

### Lump manualState into a single localStorage JSON blob

`localStorage['sm.manual'] = JSON.stringify({vfoA: ..., vfoB: ..., ...})`. One key, one parse on load, one stringify on save.

Rejected: per-field keys are slightly more code but vastly more robust. Corrupted JSON in a single key wipes all fields; corrupted single-field stays isolated. Future field additions don't need migration logic to handle "old payload doesn't have field X." The size cost is trivial.

### Don't persist; refresh = clean slate (current behaviour)

Argued briefly: refresh is *meant* to reset state; persisting through refresh might mask bugs. Some apps adopt this stance.

Rejected: the operator explicitly asked for persistence. The reasoning is sound — typed VFO values represent ongoing operating activity, not a "I want to start over" signal. An operator who genuinely wants a clean slate has other affordances (close the tab, clear browsing data, future explicit "reset" button).

### Use `sessionStorage` instead of `localStorage`

`sessionStorage` is per-tab and clears when the tab closes. Refresh preserves; tab close does not.

Rejected: the operator's stated requirement is that values *should remain the same* after refresh. The case "tab closes, operator opens a new one, expects values back" wasn't explicitly discussed but the spirit of the request points to localStorage's stronger lifetime. If "tab close = clean slate" turns out to be wanted later, switching to sessionStorage is a one-line change.

## Consequences

**Signed up for:**

- **`manualState` module gains hydration + mirroring** — ~30 lines: per-field load helpers with parsers, per-field save helpers, an `$effect.root` block registering one effect per field. Trivial.
- **localStorage usage** under the `sm.manual.*` namespace. Document the namespace in the file's doc comment so future developers know where it lives.
- **Test discipline** — `Vfos.test.ts`'s `beforeEach` already resets `manualState` fields explicitly. With persistence, those resets cascade to localStorage via the mirroring effect, keeping tests deterministic. May need to clear localStorage explicitly in tests if test bleed becomes an issue. Watch for it.
- **Two persistence layers** in the SPA, each with different scope: localStorage for transient session activity (`manualState`), daemon for declared preferences (`configState`). Document this distinction so future "where does this go?" questions resolve cleanly.
- **`qsoDraftState` (planned) faces the same question** when it lands. Likely answer: same pattern (localStorage). Flag at that time.

**Accepted costs:**

- **Cross-tab divergence.** Two tabs see different `manualState` until both refresh. v1 limitation.
- **Stale data after long absence.** Operator opens the SPA after a week away; `manualState` shows what they were doing a week ago. Probably fine; if not, an explicit "reset" button is a future addition.
- **Storage failure modes** are silently swallowed. Operator in private browsing experiences "refresh resets values" without explanation. Acceptable; adding a one-time toast informing them is a future polish if needed.

**Gained:**

- **The operator's typed values survive refresh.** Stated requirement met.
- **Per-device session continuity.** Switch the rig off, walk away, come back tomorrow, refresh — values are still there. No need to retype.
- **Foundation for `qsoDraftState` persistence** when that state object lands.

## Triggers to revisit

- **Multi-tab usage becomes routine.** Add `storage` event listener so tabs sync.
- **Cross-device sync becomes a real need.** Promote to daemon-side storage (extend `/v1/config` or add a sibling endpoint).
- **localStorage failure modes start hitting operators.** Add a one-time toast informing them values won't persist (e.g., "private browsing detected; manual edits won't survive refresh").
- **Field-set grows past ~10 fields.** The per-field key approach starts to feel verbose; consider grouping into sub-blobs (one for VFOs, one for mode, etc.) at that point.
- **Operator wants explicit "reset to defaults" button.** Add a UI affordance that clears `sm.manual.*` and resets `manualState` to hardcoded defaults.
- **`qsoDraftState` lands.** Decide whether the same persistence pattern applies (likely yes); update this ADR's references accordingly.

## References

- ADR 0003 (`0003-spa-config-daemon-only.md`) — the no-localStorage decision for *config*. This ADR is the parallel decision for *transient state*; the two together form a coherent persistence story (declared preferences → daemon, session activity → localStorage).
- ADR 0009 (`0009-cat-state-decomposition.md`) — defines `manualState` and its role; this ADR adds persistence to it.
- ADR 0006 (`0006-cat-state-precedence-rule.md`) — the snapshot-on-disconnect rule (recommended) writes to `manualState`, so persisted snapshots survive refresh too.
- `frontend/logging/src/lib/states/manual.svelte.ts` — implementation site.
- Memory `project_sm_spa_config_layering` — captures the daemon-only-for-config rule; this ADR refines the broader persistence story.
- Memory `project_sm_cat_precedence_rule` — captures the four-object decomposition; manualState's persistence is implementation detail beneath that.
