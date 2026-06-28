/*
    Minimal config reader for the logbook SPA — it needs only the mailer block
    (is SMTP enabled, and the operator's default recipient) to drive the
    "email selected rows" controls. The logging + config SPAs read the full
    /v1/config; the logbook SPA deliberately picks just the one block rather than
    pulling in the whole config-state machinery for a single feature.

    GET /v1/config always returns the mailer block resolved (enabled + an optional
    default_recipient); a missing/garbled body degrades to "disabled, no default"
    so the email controls hide rather than throw.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export interface MailerInfo {
    /** SMTP is configured + enabled on the daemon; gates the email controls. */
    enabled: boolean;
    /** Pre-fills the recipient input (the operator's QSL manager), or '' if unset. */
    defaultRecipient: string;
}

export type MailerOutcome = { kind: 'ok'; mailer: MailerInfo } | { kind: 'error'; message: string };

export async function fetchMailer(signal?: AbortSignal): Promise<MailerOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) {
        return { kind: 'error', message: fetched.message };
    }
    if (!fetched.response.ok) {
        return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    }
    const body = await readJsonBody(fetched.response);
    const mailer = isPlainObject(body) && isPlainObject(body.mailer) ? body.mailer : {};
    return {
        kind: 'ok',
        mailer: {
            enabled: mailer.enabled === true,
            defaultRecipient:
                typeof mailer.default_recipient === 'string' ? mailer.default_recipient : '',
        },
    };
}
