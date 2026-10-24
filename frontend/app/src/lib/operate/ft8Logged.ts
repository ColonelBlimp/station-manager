// A completed FT8 exchange (ft8-logged) onto the Session list: the row, the
// enrichment cache's "worked" mark (Band Activity flags it re-worked), and the
// toast — the only "it's logged" signal for FT8. The row and the mark are filed
// under the archive the frame's connection named (ADR 0094 update): another
// archive's contact never lands in this archive's list or marks its stations
// worked, and it starts verification at once.
//
// Provenance is decided FIRST. Only a frame filed under this list's archive is
// checked for a repeat delivery (the event is one-shot, but a stray double
// delivery must not add a second row, mark or toast): UUIDs can exist in two
// archives (`smd import` preserves them), so a foreign frame naming a UUID this
// list shows is not a repeat. Every frame that is not a repeat toasts — the daemon
// did log the contact, wherever it filed it.
import type { LoggedPayload } from '../api/ft8-sse';
import { addSessionQso, session, sessionModeLiteral } from './session.svelte';
import { fileUnderArchive } from './archiveStores.svelte';
import { ft8EnrichState } from './ft8Enrich.svelte';
import { toasts } from '../ui/toasts.svelte';

export function fileFt8Logged(p: LoggedPayload, archiveId: string | null): void {
    const uuid = p.uuid ?? '';
    const call = p.callsign ?? '';
    const band = p.band ?? '';
    let repeat = false;
    fileUnderArchive(archiveId, 'ft8-logged', () => {
        repeat = uuid !== '' && session.qsos.some((q) => q.uuid === uuid);
        if (repeat) return;
        addSessionQso({
            uuid: uuid || undefined,
            callsign: call,
            timeOn: p.time_on ?? '',
            band,
            // The row shows the ADIF submode when the pair has one (an FT4 exchange
            // is filed as MFSK/FT4 and reads "FT4"), the mode otherwise —
            // sessionModeLiteral, the same rule the edit path applies when it
            // writes the merged row back.
            mode: sessionModeLiteral(p.mode ?? 'FT8', p.submode),
            rstSent: p.rst_sent ?? '',
            rstRcvd: p.rst_rcvd ?? '',
            name: p.name ?? '',
            country: p.country ?? '',
            comment: '',
        });
        // Mark it worked under the profile it was filed on (MFSK/FT4 is FT4's bucket).
        if (call !== '') ft8EnrichState.markWorked(call, band, p.submode === 'FT4' ? 'FT4' : 'FT8');
    });
    if (repeat) return;
    toasts.info(call !== '' ? `QSO logged — ${call}${band ? ` (${band})` : ''}` : 'QSO logged');
}
