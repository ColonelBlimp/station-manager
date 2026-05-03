/*
    Thin daemon-side wrapper for `POST /v1/qso`. Returns a discriminated
    union so the caller can branch on outcome without parsing HTTP / JSON
    error envelopes inline.

    Wire contract (api.md §4.2 + §4.6):
      - 201 Created  → { status: "stored",    id: <int64> }
      - 200 OK       → { status: "duplicate", id: <int64> }   (dedupe-checked, non-force)
      - 4xx          → { code, message, op? }                 (validation / client error)
      - 5xx          → { code, message, op? }                 (server / db error)

    `kind` collapses to:
      - 'stored'     — fresh QSO persisted, draft can be cleared
      - 'duplicate'  — already-logged QSO matched on dedupe key; daemon
                       did not touch the row. Caller decides whether to
                       offer ?force=1 retry.
      - 'validation' — 4xx with a daemon-emitted code (e.g.
                       'invalid_adif', 'callsign_mismatch'). Caller
                       surfaces the message; draft is preserved.
      - 'server'     — 5xx; daemon logged a stack-tagged error. Caller
                       shows a generic retry message; draft is preserved.
      - 'network'    — fetch threw before a Response (daemon unreachable,
                       DNS, CORS preflight failure). Draft preserved.
*/
export type SubmitOutcome =
    | { kind: 'stored'; id: number }
    | { kind: 'duplicate'; id: number }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'network'; message: string };

interface DaemonOk {
    status: 'stored' | 'duplicate';
    id: number;
}

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function submitQso(adif: string, logbookID: number): Promise<SubmitOutcome> {
    let response: Response;
    try {
        response = await fetch(`/v1/qso?logbook=${encodeURIComponent(String(logbookID))}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-adif' },
            body: adif,
        });
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { kind: 'network', message };
    }

    // Body may not parse if the daemon emits an unexpected payload (or
    // a proxy rewrites the response). Treat an unparseable body as a
    // server error rather than throwing — the caller has nothing
    // actionable to do with a JSON parse exception.
    let body: DaemonOk | DaemonError | null = null;
    try {
        body = (await response.json()) as DaemonOk | DaemonError;
    } catch {
        body = null;
    }

    if (response.ok) {
        const ok = body as DaemonOk | null;
        if (ok?.status === 'duplicate') {
            return { kind: 'duplicate', id: Number(ok.id) };
        }
        return { kind: 'stored', id: Number(ok?.id ?? 0) };
    }

    const err = body as DaemonError | null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
