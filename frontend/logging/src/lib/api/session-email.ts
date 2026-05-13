/*
    Thin daemon-side wrapper for `POST /v1/session/email`. Returns a
    discriminated union so the SessionPanel can branch on outcome
    without parsing HTTP / JSON envelopes inline. Same shape conventions
    as `lib/api/qso.ts` and `lib/api/config.ts` for consistency.

    Wire contract (handler_session_email.go):
      - 200 OK   → { status: "sent" }
      - 400      → { code: "missing_required_field" | "invalid_field_value" |
                     "invalid_json", message, op? }
      - 503      → { code: "mailer_disabled", message, op? }
      - 502      → { code: "smtp_failure",   message, op? }
      - 5xx other → { code, message, op? }

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

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export interface SessionEmailRequest {
    to: string;
    adif: string;
    /** Optional override; daemon stamps a UTC default when absent. */
    subject?: string;
    /** Optional override; daemon stamps `session-YYYYMMDD-HHMMSS.adi` when absent. */
    filename?: string;
}

export type SessionEmailOutcome =
    | { kind: 'sent' }
    | { kind: 'mailer_disabled'; message: string }
    | { kind: 'invalid'; code: string; message: string }
    | { kind: 'smtp_failure'; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function sendSessionEmail(
    req: SessionEmailRequest,
    signal?: AbortSignal
): Promise<SessionEmailOutcome> {
    const fetched = await safeFetch('/v1/session/email', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;

    if (response.ok) {
        return { kind: 'sent' };
    }

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
