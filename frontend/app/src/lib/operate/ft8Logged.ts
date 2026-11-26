// A completed FT8 exchange (ft8-logged): the enrichment cache's "worked" mark
// (Band Activity flags it re-worked) and the toast, the only "it's logged" signal
// for FT8. The mark is filed under the archive the frame's connection named (ADR
// 0094 update): another archive's contact never marks this archive's stations
// worked, and it starts verification at once.
//
// The Session's ROW is not made here (W-0029 part 6, A8). The row is the
// daemon's word that the contact is a member, read from the daemon; this frame
// only asks for that read. The two words come by two routes and in either
// order, so whether a frame is a repeat is decided by the frames this page has
// confirmed, never by whether the row is already on screen: the daemon's rows
// can be there first, and the toast and the mark must still come, once.
//
// Provenance is decided FIRST. Only a frame filed under this page's archive is
// checked for a repeat delivery (the event is one-shot, but a stray double
// delivery must not add a second mark or toast): UUIDs can exist in two archives
// (`smd import` preserves them), so a foreign frame naming a UUID this page has
// confirmed is not a repeat. Every frame that is not a repeat toasts — the daemon
// did log the contact, wherever it filed it.
import type { LoggedPayload } from '../api/ft8-sse';
import { refreshSessionRows } from './session.svelte';
import { fileUnderArchive } from './archiveStores.svelte';
import { ft8EnrichState } from './ft8Enrich.svelte';
import { toasts } from '../ui/toasts.svelte';

// The contacts this page has confirmed, by UUID. In memory for the page's life:
// a reload confirms nothing again, because the event is not replayed.
const confirmed = new Set<string>();

export function fileFt8Logged(p: LoggedPayload, archiveId: string | null): void {
    const uuid = p.uuid ?? '';
    const call = p.callsign ?? '';
    const band = p.band ?? '';
    let repeat = false;
    fileUnderArchive(archiveId, 'ft8-logged', () => {
        repeat = uuid !== '' && confirmed.has(uuid);
        if (repeat) return;
        if (uuid !== '') confirmed.add(uuid);
        refreshSessionRows();
        // Mark it worked under the profile it was filed on (MFSK/FT4 is FT4's bucket).
        if (call !== '') ft8EnrichState.markWorked(call, band, p.submode === 'FT4' ? 'FT4' : 'FT8');
    });
    if (repeat) return;
    toasts.info(call !== '' ? `QSO logged — ${call}${band ? ` (${band})` : ''}` : 'QSO logged');
}

/** Test seam — forget the frames confirmed between cases. */
export function _resetFt8LoggedForTests(): void {
    confirmed.clear();
}
