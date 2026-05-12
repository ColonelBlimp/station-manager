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
 * the Mode dropdown, VfoBox click-to-swap, and keyboard shortcuts that
 * mutate VFO/mode (planned per ADR 0007).
 *
 * **Persistence (ADR 0011):** manualState fields persist to
 * localStorage under the `sm.manual.<fieldName>` namespace. On module
 * load each field hydrates from localStorage if present, else uses the
 * hardcoded default from `cat.svelte.ts`. Every write mirrors back to
 * localStorage via a per-field `$effect`. Failure modes (quota, private
 * browsing, disabled storage) are silently swallowed — the SPA falls
 * back to in-memory state without breaking. Distinct from the
 * daemon-authoritative `configState` (ADR 0003): manualState is
 * transient session activity per device; configState is operator
 * preference per the operator.
 *
 * **Snapshot-on-disconnect rule (ADR 0009, recommended):** when the
 * bridge transitions from connected→disconnected, manualState SHOULD
 * adopt catState's most recent values for value continuity. That
 * effect lives in bridge.svelte.ts when SSE wiring lands. Snapshot
 * writes also persist via the localStorage mirror.
 */

import { DEFAULT_VFO_HZ, DEFAULT_MODE, DEFAULT_SUB_MODE, DEFAULT_SELECTED_VFO } from './cat.svelte';

const KEY_PREFIX = 'sm.manual.';

function loadString(field: string, fallback: string): string {
    try {
        const raw = localStorage.getItem(KEY_PREFIX + field);
        return raw ?? fallback;
    } catch {
        return fallback;
    }
}

function loadNumber(field: string, fallback: number): number {
    try {
        const raw = localStorage.getItem(KEY_PREFIX + field);
        if (raw === null) return fallback;
        const n = Number(raw);
        return Number.isFinite(n) ? n : fallback;
    } catch {
        return fallback;
    }
}

function loadVfo(field: string, fallback: 'A' | 'B'): 'A' | 'B' {
    try {
        const raw = localStorage.getItem(KEY_PREFIX + field);
        if (raw === 'A' || raw === 'B') return raw;
        return fallback;
    } catch {
        return fallback;
    }
}

function save(field: string, value: unknown): void {
    try {
        localStorage.setItem(KEY_PREFIX + field, String(value));
    } catch {
        // Storage unavailable / quota exceeded / private browsing —
        // fall back to in-memory state. Don't break the SPA.
    }
}

class ManualState {
    vfoA: number = $state(loadNumber('vfoA', DEFAULT_VFO_HZ));
    vfoB: number = $state(loadNumber('vfoB', DEFAULT_VFO_HZ));
    mode: string = $state(loadString('mode', DEFAULT_MODE));
    subMode: string = $state(loadString('subMode', DEFAULT_SUB_MODE));
    selectedVfo: 'A' | 'B' = $state(loadVfo('selectedVfo', DEFAULT_SELECTED_VFO));
}

export const manualState = new ManualState();

// Per-field effects mirror operator-edits to localStorage. Each
// effect tracks only its own field, so writes to other fields don't
// cause spurious re-saves. $effect.root is needed because this is
// module-level (outside a component lifecycle).
//
// Per-effect first-run latch: each $effect fires once on module-load
// with the just-hydrated value, which would re-save the loaded value
// back to the same key — pointless I/O on every page load. Latching
// inside each effect body (rather than via a shared module flag) is
// required because sync code after $effect.root() runs before the
// inner effects fire, so a top-level flag would already be true on
// first run.
function mirrorField<T>(read: () => T, field: string): void {
    let firstRun = true;
    $effect(() => {
        const v = read();
        if (firstRun) {
            firstRun = false;
            return;
        }
        save(field, v);
    });
}

$effect.root(() => {
    mirrorField(() => manualState.vfoA, 'vfoA');
    mirrorField(() => manualState.vfoB, 'vfoB');
    mirrorField(() => manualState.mode, 'mode');
    mirrorField(() => manualState.subMode, 'subMode');
    mirrorField(() => manualState.selectedVfo, 'selectedVfo');
});
