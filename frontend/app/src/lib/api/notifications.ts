// Durable operator-notification ingestion — POST /v1/notifications (W-0001 /
// ADR 0076). Records a browser-originated failure that must survive its toast
// expiry and a page reload. The browser sends ONLY the allowlisted, typed
// fields; the daemon stamps category, severity, occurrence time, and build, and
// builds the canonical stored detail.
//
// Best-effort by design: the result is ignored and safeFetch never throws, so a
// failure here — including an unreachable daemon (the 'network' outcome itself,
// which cannot be recorded when the daemon is the thing that's unreachable) —
// can never disturb the caller's own error handling (e.g. the toast it shows).
import { daemonErrorMessage, isPlainObject, readJsonBody, safeFetch } from './_helpers';

// The export-failure outcomes worth persisting. 'aborted' (operator cancel) and
// 'ok' are deliberately excluded — they are not failures.
export type ExportFailureOutcome = 'no_qsos' | 'invalid' | 'server' | 'network';

// One durable operator notification as served by GET /v1/notifications. `detail`
// is the typed metadata the daemon stored (export.adif_failed: {count, outcome};
// forward.failed: {qso_id, forwarder, action, attempts}); it is `unknown` here so
// the UI narrows it defensively and degrades unknown/future shapes rather than
// stringifying raw content.
export interface NotificationEvent {
    id: number;
    category: string;
    kind: string;
    severity: string;
    occurred_at: string;
    build: string;
    detail: unknown;
}

export type NotificationsOutcome =
    { kind: 'ok'; items: NotificationEvent[] } | { kind: 'error'; message: string };

const transportMessage = (kind: string): string =>
    kind === 'aborted' ? 'Request was cancelled.' : 'Could not reach the daemon.';

// fetchNotifications reads the newest durable notifications (default 50). A fresh
// call on each rail open is the reload path W-0001 must survive.
export async function fetchNotifications(
    limit = 50,
    signal?: AbortSignal
): Promise<NotificationsOutcome> {
    const fetched = await safeFetch(`/v1/notifications?limit=${limit}`, { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    }
    if (!isPlainObject(body) || !Array.isArray(body.items)) {
        return { kind: 'error', message: 'Unexpected notifications response.' };
    }
    return { kind: 'ok', items: body.items as NotificationEvent[] };
}

// count is the number of QSO UUIDs the browser actually submitted for export.
export async function recordExportFailed(
    count: number,
    outcome: ExportFailureOutcome
): Promise<void> {
    await safeFetch('/v1/notifications', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind: 'export.adif_failed', count, outcome }),
    });
    // Result intentionally ignored — recording is best-effort.
}
