/*
    The completion times of one contest occurrence's contacts, for the header's
    contest timers (W-0023 weekend slice). Reads the logbook's paged list
    (GET /v1/logbook/{id}/qso, newest first) and stops at the first contact
    dated before the occurrence's start date, so it reads only the contest's
    pages, not the whole log. Read-only. A failed read is null: the timers show
    nothing rather than a partial count taken as complete.
*/

import { fetchQsoPage, type LogbookQso } from './logbooks';

const PAGE = 500;

/** Epoch ms of a contact's completion: QSO_DATE_OFF/TIME_OFF, else the start. */
function completionMs(q: LogbookQso): number | null {
    const date = q.qso_date_off || q.qso_date || '';
    const time = q.time_off || q.time_on || '';
    const d = /^(\d{4})(\d{2})(\d{2})$/.exec(date);
    const t = /^(\d{2})(\d{2})(\d{2})?$/.exec(time);
    if (d === null || t === null) return null;
    return Date.UTC(+d[1], +d[2] - 1, +d[3], +t[1], +t[2], +(t[3] ?? 0));
}

export async function fetchContestCompletions(
    logbook: number,
    contestId: string,
    start: string
): Promise<number[] | null> {
    const out: number[] = [];
    let after: string | undefined;
    for (;;) {
        const page = await fetchQsoPage(logbook, PAGE, after);
        if (page.kind !== 'ok') return null;
        const passedStart = collect(page.items, contestId, start, out);
        if (passedStart || page.nextCursor === null) return out;
        after = page.nextCursor;
    }
}

/** Add one page's occurrence completions to out; true once a contact dated
 *  before the start date is reached (the list is newest first). */
function collect(items: LogbookQso[], contestId: string, start: string, out: number[]): boolean {
    for (const q of items) {
        if ((q.qso_date ?? '') < start) return true;
        if (q.contest_id !== contestId || q.app_sm_contest_start !== start) continue;
        const ms = completionMs(q);
        if (ms !== null) out.push(ms);
    }
    return false;
}
