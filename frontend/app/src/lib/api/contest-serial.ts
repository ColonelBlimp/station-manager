/*
    `POST /v1/contest-serial?logbook=&contest=&start=&draft=` — the daemon's durable
    sent-serial counter (W-0023 weekend slice; handler_contest_serial.go).

      - 200 OK → { serial: number } — the next serial, consumed for good; the
        same draft asking again straight after gets the same number back.
      - 4xx/5xx → { code, message }.

    A write to the active archive, so it carries the archive guard. Any
    failure leaves the Sent # empty for the operator to type: allocation never
    blocks logging.
*/

import { isPlainObject, readJsonBody } from './_helpers';
import { guardedFetch } from './archive-guard';

export type ContestSerialOutcome =
    { kind: 'ok'; serial: number } | { kind: 'error'; message: string };

export async function allocateContestSerial(
    logbook: number,
    contest: string,
    start: string,
    draft: string
): Promise<ContestSerialOutcome> {
    const params = new URLSearchParams({ logbook: String(logbook), contest, start, draft });
    const fetched = await guardedFetch(`/v1/contest-serial?${params.toString()}`, {
        method: 'POST',
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const body = await readJsonBody(fetched.response);
    if (fetched.response.ok) {
        const serial = isPlainObject(body) ? body.serial : undefined;
        if (typeof serial === 'number' && Number.isInteger(serial) && serial > 0) {
            return { kind: 'ok', serial };
        }
        return { kind: 'error', message: 'the daemon returned no serial' };
    }
    const message =
        isPlainObject(body) && typeof body.message === 'string'
            ? body.message
            : `HTTP ${fetched.response.status}`;
    return { kind: 'error', message };
}
