// Operate-surface UI state: which info panel (card-below) is open, and whether
// the pile-up drawer is open. Shared across Operate / UtilRail / InfoPanel /
// PileupDrawer. (Left-nav + theme + right-rail collapse live in lib/ui/state;
// this is the per-surface state.)

export type Panel = 'worked' | 'session' | 'details' | 'rig';

export const operate = $state({
    panel: null as Panel | null,
    pileup: false,
});

export function togglePanel(p: Panel): void {
    operate.panel = operate.panel === p ? null : p;
}

export function closePanel(): void {
    operate.panel = null;
}

export function setPileup(open: boolean): void {
    operate.pileup = open;
}
