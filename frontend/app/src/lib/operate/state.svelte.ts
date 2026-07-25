// Operate-surface UI state: the pile-up drawer and the export/email overlay.
// Tile visibility + arrangement live in lib/operate/layout.svelte
// (ADR 0046) — the single-slot `panel` model was retired with the tile board.
// (Left-nav + theme + right-rail collapse live in lib/ui/state.)

export const operate = $state({
    pileup: false,
    // Export/email dialog (opened from the Session tile's header action).
    exportOpen: false,
});

export function openExport(): void {
    operate.exportOpen = true;
}

export function closeExport(): void {
    operate.exportOpen = false;
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
