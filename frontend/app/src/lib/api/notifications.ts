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
import { safeFetch } from './_helpers';

// The export-failure outcomes worth persisting. 'aborted' (operator cancel) and
// 'ok' are deliberately excluded — they are not failures.
export type ExportFailureOutcome = 'no_qsos' | 'invalid' | 'server' | 'network';

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
