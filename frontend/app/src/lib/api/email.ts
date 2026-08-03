/*
    SMTP-scoped /v1/config read + write for the app Settings view (ADR 0044) —
    the port of the standalone config SPA's Email tab.

    Data-safety contract — VERIFIED against the daemon's overlayConfig +
    mergeSmtp (internal/api/handler_config.go):

      - The smtp block is replaced WHOLE when present, so every field rides on
        every save. The password survives that because mergeSmtp keeps the
        stored one whenever the payload's is blank.
      - GET never echoes the password — only `password_set`, whether one is
        stored. So a blank box means "not retyped", and `password` is OMITTED
        rather than sent as "" (see email.svelte.ts buildPayload).
      - `password_clear` is the ONLY way to remove a stored password. Blank
        deliberately does not mean reset — it is what an operator editing the
        host sends every time.
      - Port and timeout are sent as 0 when blank; the daemon resolves them to
        587 / 30 in config.Normalize and echoes the resolved values back, so the
        form shows what was actually stored rather than the hole.
      - NOTHING but `smtp` is sent. The standalone config SPA echoes
        logging_station and station on an email save (config.svelte.ts:1035),
        which would clobber a concurrent identity or power change made between
        our GET and our PUT — the same trap review 2026-07-20 #3 removed from
        the Station section. Omitting them leaves the daemon's blocks untouched.
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';

/** The SMTP block as GET /v1/config reports it — password masked to a flag. */
export interface SmtpEntry {
    enabled: boolean;
    host: string;
    port: number;
    username: string;
    from: string;
    default_recipient: string;
    starttls: boolean;
    timeout_sec: number;
    password_set: boolean;
}

/** What a save sends. `password` rides only when freshly typed. */
export interface SmtpPayload {
    enabled: boolean;
    host: string;
    port: number;
    username: string;
    from: string;
    default_recipient: string;
    starttls: boolean;
    timeout_sec: number;
    password?: string;
    password_clear?: boolean;
}

export type SmtpOutcome = { kind: 'ok'; smtp: SmtpEntry } | { kind: 'error'; message: string };

function toEntry(v: unknown): SmtpEntry {
    const o = isPlainObject(v) ? v : {};
    return {
        enabled: o.enabled === true,
        host: typeof o.host === 'string' ? o.host : '',
        port: typeof o.port === 'number' ? o.port : 0,
        username: typeof o.username === 'string' ? o.username : '',
        from: typeof o.from === 'string' ? o.from : '',
        default_recipient: typeof o.default_recipient === 'string' ? o.default_recipient : '',
        starttls: o.starttls === true,
        timeout_sec: typeof o.timeout_sec === 'number' ? o.timeout_sec : 0,
        password_set: o.password_set === true,
    };
}

export async function fetchEmail(signal?: AbortSignal): Promise<SmtpOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body)) return { kind: 'error', message: 'malformed /v1/config response' };
    return { kind: 'ok', smtp: toEntry(body.smtp) };
}

export async function saveEmail(payload: SmtpPayload, signal?: AbortSignal): Promise<SmtpOutcome> {
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        // Only `smtp` — see the clobber note in the module header.
        body: JSON.stringify({ smtp: payload }),
        signal,
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const err = isPlainObject(body) ? (body as { message?: string }) : null;
        return { kind: 'error', message: err?.message ?? `HTTP ${fetched.response.status}` };
    }
    if (!isPlainObject(body)) return { kind: 'error', message: 'malformed save response' };
    return { kind: 'ok', smtp: toEntry(body.smtp) };
}
