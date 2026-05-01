/**
 * Operator's manual edits in CAT-off mode.
 *
 * Per ADR 0009 (`docs/decisions/0009-cat-state-decomposition.md`),
 * this holds the *subset* of CAT-state fields that the operator can
 * manually set when no live rig source is driving values. The bridge
 * NEVER writes to manualState — that's the structural ownership rule
 * that makes the four-object decomposition work.
 *
 * **Read path:** components do NOT read manualState directly. They
 * read `displayedState`, which picks between catState (live rig) and
 * manualState (operator edits) based on the three flags from
 * configState/bridgeState.
 *
 * **Write path:** SPA components write here via VfoInput's onCommit,
 * the Mode dropdown, VfoBox click-to-swap (planned), and keyboard
 * shortcuts that mutate VFO/mode (planned per ADR 0007).
 *
 * **Defaults** are imported from `cat.svelte.ts` — both states share
 * the same hardcoded fallbacks per ADR 0003's bootstrap-fallback
 * pattern. When `/v1/config` wires up later, operator-defined
 * defaults will override these at app start.
 *
 * **Snapshot-on-disconnect rule (ADR 0009, recommended):** when the
 * bridge transitions from connected→disconnected, manualState SHOULD
 * adopt catState's most recent values for value continuity. That
 * effect lives in bridge.svelte.ts when SSE wiring lands; manualState's
 * own module never reads or writes catState.
 */

import {
    DEFAULT_VFO_HZ,
    DEFAULT_MODE,
    DEFAULT_SUB_MODE,
    DEFAULT_SELECTED_VFO,
    DEFAULT_POWER_WATTS,
} from './cat.svelte';

class ManualState {
    vfoA: number = $state(DEFAULT_VFO_HZ);
    vfoB: number = $state(DEFAULT_VFO_HZ);
    mode: string = $state(DEFAULT_MODE);
    subMode: string = $state(DEFAULT_SUB_MODE);
    selectedVfo: 'A' | 'B' = $state(DEFAULT_SELECTED_VFO);
    power: number = $state(DEFAULT_POWER_WATTS);
}

export const manualState = new ManualState();
