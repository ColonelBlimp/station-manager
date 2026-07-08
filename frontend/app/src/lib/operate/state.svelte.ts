// Operate-surface UI state: which info panel (card-below) is open, and whether
// the pile-up drawer is open. Shared across Operate / UtilRail / InfoPanel /
// PileupDrawer. (Left-nav + theme + right-rail collapse live in lib/ui/state;
// this is the per-surface state.)

export type Panel = 'worked' | 'session' | 'rig';

export const operate = $state({
    panel: null as Panel | null,
    pileup: false,
    // Export/email dialog (opened from the Session panel's header action).
    exportOpen: false,
    // Contact-detail overlay (opened from the Worked panel's View action while
    // a QSO is underway) — the everything-we-know view, and the deliberate
    // edit-any-field surface that replaced the always-on Details card.
    contactOpen: false,
});

export function openExport(): void {
    operate.exportOpen = true;
}

export function closeExport(): void {
    operate.exportOpen = false;
}

export function openContact(): void {
    operate.contactOpen = true;
}

export function closeContact(): void {
    operate.contactOpen = false;
}

export function togglePanel(p: Panel): void {
    operate.panel = operate.panel === p ? null : p;
}

export function closePanel(): void {
    operate.panel = null;
}

export function setPileup(open: boolean): void {
    operate.pileup = open;
}

// Focus hand-back seam: LoggingCard registers its callsign input; chrome
// (UtilRail) returns focus there after a DELIBERATE panel action on the
// read-only panels (Worked/Session). Deliberate-click only — the Worked
// panel also auto-opens when a lookup lands, and stealing focus then would
// yank the cursor out of whatever field the operator is typing in.
let callsignInput: HTMLInputElement | null = null;

export function registerCallsignInput(el: HTMLInputElement | null): void {
    callsignInput = el;
}

export function focusCallsign(): void {
    callsignInput?.focus();
}
