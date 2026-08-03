/*
    Narrow /v1/config readers for the Logbook page (ported from the shipping
    logbook SPA, ADR 0044). It needs only two blocks — the mailer (is SMTP
    enabled + the operator's default recipient, gating the email-out controls)
    and the masked forwarders list (upload-status colour + backfill destination
    picker, ADR 0039) — so these pick just those rather than pulling in the
    whole config-state machinery. Absent/garbled blocks degrade to
    disabled/empty, never an error — browsing is unaffected.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';
import type { ForwarderInfo } from '../logbook/uploadStatus';

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

export type ForwardersOutcome =
    { kind: 'ok'; forwarders: ForwarderInfo[] } | { kind: 'error'; message: string };

/**
 * Read the masked `forwarders` block from /v1/config — the set of configured
 * forwarders (name/type/enabled) that drives the upload-status colour (the
 * ENABLED ones are E) and the destination picker. The block is present only when
 * ≥1 forwarder is configured; absent/garbled → empty list (no colour, no picker),
 * never an error — browsing is unaffected.
 */
export async function fetchForwarders(signal?: AbortSignal): Promise<ForwardersOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) {
        return { kind: 'error', message: fetched.message };
    }
    if (!fetched.response.ok) {
        return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    }
    const body = await readJsonBody(fetched.response);
    const raw = isPlainObject(body) && Array.isArray(body.forwarders) ? body.forwarders : [];
    const forwarders: ForwarderInfo[] = [];
    for (const f of raw) {
        if (isPlainObject(f) && typeof f.name === 'string' && typeof f.type === 'string') {
            forwarders.push({
                name: f.name,
                // The operator's config.json display name. Absent for a
                // destination they haven't renamed; forwarderLabel falls back.
                label: typeof f.label === 'string' ? f.label : '',
                type: f.type,
                enabled: f.enabled === true,
            });
        }
    }
    return { kind: 'ok', forwarders };
}
