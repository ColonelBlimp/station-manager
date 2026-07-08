// Session ADIF download — POST /v1/session/export. The daemon rebuilds the
// ADIF from the live DB rows (the fully enriched stored record — MY_* block,
// DXCC, zones, lat/lon — NOT the SPA's pre-submit subset) and archives a
// backup under exports/sent-adif/, then returns the document as an
// attachment. Same send-UUIDs-not-a-blob contract as session-email; the SPA
// never composes ADIF itself.

import { safeFetch, readJsonBody, isPlainObject } from './_helpers';

export type SessionExportOutcome =
    | { kind: 'ok'; filename: string; body: string }
    | { kind: 'no_qsos'; message: string }
    | { kind: 'invalid'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
}

// Pull the daemon's suggested name out of Content-Disposition; fall back to a
// generic one if the header is absent/odd (the file still downloads fine).
function filenameFrom(header: string | null): string {
    const m = header ? /filename="?([^"]+)"?/.exec(header) : null;
    return m ? m[1] : 'session.adi';
}

export async function exportSessionAdif(
    uuids: string[],
    signal?: AbortSignal
): Promise<SessionExportOutcome> {
    const fetched = await safeFetch('/v1/session/export', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ uuids }),
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;

    if (response.ok) {
        const body = await response.text();
        return {
            kind: 'ok',
            filename: filenameFrom(response.headers.get('Content-Disposition')),
            body,
        };
    }

    const parsed = await readJsonBody(response);
    const err = isPlainObject(parsed) ? (parsed as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status === 400 && code === 'no_qsos') {
        return { kind: 'no_qsos', message };
    }
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'invalid', code, message };
}

// Save text content to a file via a transient object URL. Isolated so the
// dialog stays declarative and this DOM-poking is unit-mockable.
export function downloadTextFile(filename: string, content: string): void {
    const blob = new Blob([content], { type: 'application/octet-stream' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
}
