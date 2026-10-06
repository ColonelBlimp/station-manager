/*
    Narrow /v1/config readers for the Logbook page (ported from the shipping
    logbook SPA, ADR 0044). It needs only two blocks — the mailer (is SMTP
    enabled + the operator's default recipient, gating the email-out controls)
    and a logbook's destination bindings (upload-status colour + backfill
    destination picker, ADR 0082) — so these pick just those rather than
    pulling in the whole config-state machinery. Absent/garbled blocks degrade to
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
 * Read one logbook's destination BINDINGS (ADR 0082): what its backfill picker
 * offers and what its upload-status colour is judged against. An additional
 * logbook's binding is named `<type>.<logbook uuid>` and only that name routes
 * its QSOs, so the logbook view must never take names from the station's
 * config entries.
 */
export async function fetchLogbookDestinations(
    logbookId: number,
    signal?: AbortSignal
): Promise<ForwardersOutcome> {
    const fetched = await safeFetch(`/v1/logbook/${logbookId}/destinations`, { signal });
    if (!fetched.ok) {
        return { kind: 'error', message: fetched.message };
    }
    if (!fetched.response.ok) {
        return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    }
    const body = await readJsonBody(fetched.response);
    const raw = isPlainObject(body) && Array.isArray(body.destinations) ? body.destinations : [];
    const forwarders: ForwarderInfo[] = [];
    for (const f of raw) {
        if (isPlainObject(f) && typeof f.name === 'string' && typeof f.type === 'string') {
            forwarders.push({
                name: f.name,
                label: typeof f.label === 'string' ? f.label : '',
                type: f.type,
                enabled: f.enabled === true,
            });
        }
    }
    return { kind: 'ok', forwarders };
}
