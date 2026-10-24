/*
    Thin daemon-side wrapper for `POST /v1/session/email`. Returns a
    discriminated union so the SessionPanel can branch on outcome
    without parsing HTTP / JSON envelopes inline. Same shape conventions
    as `lib/api/qso.ts` and `lib/api/config.ts` for consistency.

    Wire contract (handler_session_email.go):
      - 200 OK   → { status: "sent", sent: string[], omitted: string[],
                     emailed: string[], date: string }
      - 400      → { code: "missing_required_field" | "invalid_field_value" |
                     "invalid_json" | "no_qsos", message, op? }
      - 503      → { code: "mailer_disabled", message, op? }
      - 502      → { code: "smtp_failure",   message, op? }
      - 5xx other → { code, message, op? }

    The daemon rebuilds the ADIF from the live DB rows keyed by `uuids`,
    so the SPA sends identifiers, not a pre-built blob. On success the
    response reports which UUIDs were durably stamped "forwarded by
    email" (`emailed`) and the UTC YYYYMMDD stamp (`date`); the caller
    marks those session rows sent. A stamp-write failure on the daemon
    (after the mail left) comes back as `emailed: []` — the rows stay
    visually unsent so a re-send or the Logbook SPA can reconcile.
    `sent` and `omitted` (slice 5) partition the request: what the attachment
    held and what could not be found in this archive. They come back as
    `lists`, untouched, or null when any of the three is not a list of strings;
    the Session dialog validates the partition before marking anything.

    `kind` collapses to:
      - 'sent'             — message accepted by the SMTP server.
      - 'mailer_disabled'  — daemon's SMTP block isn't configured; SPA
                             toasts "email not configured" or hides the
                             button entirely (configState.mailer.enabled).
      - 'invalid'          — 400; SPA surfaces the daemon's message in
                             the toast (e.g. "to must be an email
                             address").
      - 'smtp_failure'     — 502; transport error reaching the operator's
                             SMTP server. SPA toasts a generic "send
                             failed; check daemon logs" — the cause is in
                             the daemon log, not on the wire.
      - 'server'           — 5xx other than 502; daemon-internal failure.
      - 'aborted'          — caller cancelled via AbortSignal before a response.
      - 'network'          — fetch threw before a Response (daemon
                             unreachable).
*/

import { EMAIL_TIMEOUT_MS, isPlainObject, readJsonBody } from './_helpers';
import { guardedFetch } from './archive-guard';
import type { EmailLists } from '../operate/sessionAccounting';

export interface SessionEmailRequest {
    to: string;
    /** Canonical UUIDs of the session QSOs to email; daemon rebuilds the ADIF from these. */
    uuids: string[];
    /** Optional override; daemon stamps a UTC default when absent. */
    subject?: string;
    /** Optional override; daemon stamps `session-YYYYMMDD-HHMMSS.adi` when absent. */
    filename?: string;
}

export type SessionEmailOutcome =
    | { kind: 'sent'; emailed: string[]; date: string; lists: EmailLists | null }
    | { kind: 'mailer_disabled'; message: string }
    | { kind: 'invalid'; code: string; message: string }
    | { kind: 'smtp_failure'; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    /** AMBIGUOUS for this write: SMTP may have ACCEPTED the message before
     *  the connection dropped (the daemon's 30 s SMTP/HTTP timeouts), and
     *  the browser can't tell that from connect-refused — so callers warn
     *  "may have gone out", never "definitely failed". `timedOut` marks the
     *  FIRED-timeout subset (F-04b, ADR 0078): callers steer that with the
     *  shared "outcome unknown" lead, while a generic (non-timeout) network
     *  failure keeps its existing cautious wording. */
    | { kind: 'network'; message: string; timedOut?: boolean };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

function stringList(v: unknown): string[] | null {
    return Array.isArray(v) && v.every((u): u is string => typeof u === 'string') ? v : null;
}

// The 200 body: the durable-stamp report plus the slice 5 partition lists. A
// daemon that sent the mail but couldn't stamp returns emailed:[]; a body that
// fails to parse (unexpected shape) degrades to "sent, nothing marked" rather
// than throwing — the mail went out either way.
function sentOutcome(body: unknown): SessionEmailOutcome {
    const ok = isPlainObject(body)
        ? (body as { sent?: unknown; omitted?: unknown; emailed?: unknown; date?: unknown })
        : null;
    const emailed = Array.isArray(ok?.emailed)
        ? ok.emailed.filter((u): u is string => typeof u === 'string')
        : [];
    const date = typeof ok?.date === 'string' ? ok.date : '';
    const sent = stringList(ok?.sent);
    const omitted = stringList(ok?.omitted);
    const exact = stringList(ok?.emailed);
    const lists =
        sent !== null && omitted !== null && exact !== null
            ? { sent, omitted, emailed: exact }
            : null;
    return { kind: 'sent', emailed, date, lists };
}

export async function sendSessionEmail(
    req: SessionEmailRequest,
    signal?: AbortSignal
): Promise<SessionEmailOutcome> {
    const fetched = await guardedFetch(
        '/v1/session/email',
        {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(req),
            signal,
        },
        // Email is a slow external write (daemon SMTP allows 30 s) with real
        // duplicate cost — outlast the daemon's ceiling, never give up first.
        { timeoutMs: EMAIL_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        if (fetched.kind === 'network') {
            return { kind: 'network', message: fetched.message, timedOut: fetched.timedOut };
        }
        return { kind: 'aborted', message: fetched.message };
    }
    const response = fetched.response;

    if (response.ok) return sentOutcome(await readJsonBody(response));

    // Body may not parse if the daemon emits an unexpected payload.
    // Treat an unparseable body as a synthesised error envelope — the
    // caller has nothing actionable to do with a JSON parse exception.
    const body = await readJsonBody(response);
    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;

    if (response.status === 503) {
        return { kind: 'mailer_disabled', message };
    }
    if (response.status === 502) {
        return { kind: 'smtp_failure', message };
    }
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'invalid', code, message };
}
