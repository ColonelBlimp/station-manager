// Operate panel visibility.
//
// This was the tile-layout module (ADR 0046): ordered tile ids per column, a drag
// seam, an arrange mode, a global pin and a persistence seam. ADR 0058 retired all of
// it — three weeks of operating produced no arrangement friction, the real complaint
// was consistency between workspaces, and with Rig and Session ambient the board was
// left arranging two tiles.
//
// What remains is the part that was actually load-bearing: which panels are open, and
// the AMBIENT/WORKFLOW split that gives each one a single home. Cards are still
// relocatable by construction (ADR 0045) — that discipline costs nothing and is what
// would make a revisit wiring rather than a refactor.

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

/*
    Panels are classified by PURPOSE, not by workspace — see ambientPanels.test.ts
    for the specification this serves.

    AMBIENT panels are reference: what the radio is doing, what has been worked
    today. Nothing about them is tied to the contact in progress, so they get ONE
    home in every workspace and the rail toggle always puts them in the same place.
    Previously they were board tiles in Phone/CW and an overlay in FT8, so the same
    click produced a panel in a different place depending on mode and the operator
    had to re-find it.

    WORKFLOW panels are contextual to the contact being entered right now — Logging
    is the anchor, Worked is about the callsign in the box and auto-shows on Tab.
    They belong beside the work and stay wherever the workspace puts them.

    Every TileId is in exactly one list: a panel with two homes is the bug this
    split exists to prevent.
*/
export const AMBIENT_TILES: TileId[] = ['rig', 'session'];
export const WORKFLOW_TILES: TileId[] = ['logging', 'worked'];

export function isAmbient(id: TileId): boolean {
    return AMBIENT_TILES.includes(id);
}

export const TILES: Record<TileId, { name: string; component: Component }> = {
    logging: { name: 'Logging', component: LoggingCard },
    worked: { name: 'Worked', component: WorkedPanel },
    session: { name: 'Session', component: SessionPanel },
    rig: { name: 'Rig Control', component: RigPanel },
};

// Default = the logging card only; the info panels are opened on demand (rail
// show/hide, or Worked auto-shows on Tab). Session-scoped by design: ADR 0058
// dropped persistence along with the pin it belonged to, so a reload starts here.
function defaultHidden(): TileId[] {
    return ['worked', 'session', 'rig'];
}

export const layout = $state({
    hidden: defaultHidden(),
});

export function isVisible(id: TileId): boolean {
    return !layout.hidden.includes(id);
}

export function showTile(id: TileId): void {
    if (isVisible(id)) return;
    layout.hidden = layout.hidden.filter((x) => x !== id);
}

export function hideTile(id: TileId): void {
    if (!layout.hidden.includes(id)) layout.hidden = [...layout.hidden, id];
}

export function toggleTile(id: TileId): void {
    if (isVisible(id)) hideTile(id);
    else showTile(id);
}

/** Back to the shipped default — every info panel closed. */
export function resetToDefault(): void {
    layout.hidden = defaultHidden();
}
