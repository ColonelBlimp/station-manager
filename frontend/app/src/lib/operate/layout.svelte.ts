// Operate tile layout (ADR 0046). The layout is a PLAIN SERIALISABLE VALUE —
// ordered tile ids per column + a hidden set — NOT pixel coordinates, so the
// same shape persists to localStorage today or a config.json op-profile field
// later. Persistence is an INJECTED seam (setLayoutPersistence, wired in
// main.ts). State-driven: the board/drag mutate THIS; the DOM is the view.
//
// Cards are unchanged (ADR 0046): the registry only names them + points at the
// component to render; the board positions them, never reshapes them.

import type { Component } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import WorkedPanel from './WorkedPanel.svelte';
import SessionPanel from './SessionPanel.svelte';
import RigPanel from './RigPanel.svelte';

export type TileId = 'logging' | 'worked' | 'session' | 'rig';

export const ALL_TILES: TileId[] = ['logging', 'worked', 'session', 'rig'];

// Tiles the right rail lets you show/hide (the on-demand info panels — the
// logging card is the fast-path anchor, not a rail toggle).
export const RAIL_TILES: TileId[] = ['worked', 'session', 'rig'];

export const TILES: Record<TileId, { name: string; component: Component }> = {
    logging: { name: 'Logging', component: LoggingCard },
    worked: { name: 'Worked', component: WorkedPanel },
    session: { name: 'Session', component: SessionPanel },
    rig: { name: 'Rig', component: RigPanel },
};

export interface LayoutValue {
    columns: TileId[][];
    hidden: TileId[];
}

const COLUMN_COUNT = 2;

// Default = faithful to the pre-tile surface: just the logging card visible;
// the info tiles are added on demand (rail show/hide, or Worked auto-shows on
// Tab). Shown tiles pack into the shortest column (col 1, beside the tall
// logging card).
function defaultLayout(): LayoutValue {
    return { columns: [['logging'], []], hidden: ['worked', 'session', 'rig'] };
}

const clone = (l: LayoutValue): LayoutValue => ({
    columns: l.columns.map((c) => c.slice()),
    hidden: l.hidden.slice(),
});

// Guard a loaded/stale value: keep only known tiles, exactly COLUMN_COUNT
// columns, every tile placed-or-hidden exactly once (a tile the saved layout
// predates lands in hidden — forward-compat).
function normalise(l: LayoutValue): LayoutValue {
    // Plain array (not a Set) — a small, non-reactive local dedupe helper.
    const seen: TileId[] = [];
    const take = (ids: TileId[] | undefined): TileId[] => {
        const out: TileId[] = [];
        for (const id of ids ?? []) {
            if (ALL_TILES.includes(id) && !seen.includes(id)) {
                seen.push(id);
                out.push(id);
            }
        }
        return out;
    };
    const columns: TileId[][] = [];
    for (let i = 0; i < COLUMN_COUNT; i++) columns.push(take(l.columns?.[i]));
    const hidden = take(l.hidden);
    for (const id of ALL_TILES) if (!seen.includes(id)) hidden.push(id);
    return { columns, hidden };
}

export const layout = $state({
    current: defaultLayout(),
    arranging: false,
    pinned: false,
});

// ---- Persistence seam (injected in main.ts) -------------------------------
// localStorage today; a config.json op-profile field later — this module is
// unchanged either way. The key is profile/sub-mode composable at the seam.
export interface LayoutStore {
    load: () => LayoutValue | null;
    save: (l: LayoutValue) => void;
    clear: () => void;
}

let store: LayoutStore | null = null;

export function setLayoutPersistence(s: LayoutStore): void {
    store = s;
    const saved = s.load();
    // A saved layout means the operator pinned one previously — adopt it and
    // reflect the pinned state; otherwise stay on the non-destructive Default.
    if (saved) {
        loadInto(normalise(saved));
        layout.pinned = true;
    }
}

function persistIfPinned(): void {
    if (layout.pinned) store?.save(clone(layout.current));
}

// ---- Arrange mode ---------------------------------------------------------
export function setArranging(on: boolean): void {
    layout.arranging = on;
}
export function toggleArranging(): void {
    layout.arranging = !layout.arranging;
}

// ---- Show / hide ----------------------------------------------------------
export function isVisible(id: TileId): boolean {
    return !layout.current.hidden.includes(id);
}

export function showTile(id: TileId): void {
    if (isVisible(id)) return;
    layout.current.hidden = layout.current.hidden.filter((x) => x !== id);
    // Stack BELOW the logging card (into its column), reproducing the pre-tile
    // vertical layout — info appears under the log form, not beside it. Arrange
    // mode is where the operator drags a tile into the second column for a
    // side-by-side layout. Col 0 is the fallback if logging is hidden.
    let target = layout.current.columns.findIndex((c) => c.includes('logging'));
    if (target < 0) target = 0;
    layout.current.columns[target].push(id);
    persistIfPinned();
}

export function hideTile(id: TileId): void {
    layout.current.columns = layout.current.columns.map((c) => c.filter((x) => x !== id));
    if (!layout.current.hidden.includes(id)) layout.current.hidden.push(id);
    persistIfPinned();
}

export function toggleTile(id: TileId): void {
    if (isVisible(id)) hideTile(id);
    else showTile(id);
}

// ---- Drag (board-driven, state as source of truth) ------------------------
// The board live-reorders during a drag (setColumnsLive, no persist), then
// commits once on pointer-up.
export function setColumnsLive(columns: TileId[][]): void {
    layout.current.columns = columns.map((c) => c.slice());
}
export function commitLayout(): void {
    persistIfPinned();
}

// ---- Pin / reset ----------------------------------------------------------
// Mutate the current layout IN PLACE (columns + hidden), rather than replacing
// the `current` object — the same reactive pattern as show/hide/drag, so the
// board reflows identically. (Replacing the parent object should be reactive
// too, but staying on the proven-working mutation shape removes any doubt.)
function loadInto(l: LayoutValue): void {
    layout.current.columns = l.columns;
    layout.current.hidden = l.hidden;
}

export function togglePin(): void {
    layout.pinned = !layout.pinned;
    if (layout.pinned) {
        store?.save(clone(layout.current)); // save all
    } else {
        store?.clear();
        loadInto(defaultLayout()); // unpin → back to Default
    }
}

export function resetToDefault(): void {
    loadInto(defaultLayout());
    persistIfPinned();
}
