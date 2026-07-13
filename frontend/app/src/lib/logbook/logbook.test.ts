import { afterEach, describe, expect, it } from 'vitest';
import { logbookState } from './logbook.svelte';
import type { LogbookQso } from '../api/logbooks';

function qso(id: number, uuid?: string): LogbookQso {
    return { id, uuid, call: `C${id}` };
}

afterEach(() => {
    logbookState.clearSelection();
    logbookState.rows = [];
});

// The email-out payload keys off UUIDs, but selection is by numeric id and spans
// pages — so the UUID has to be captured at toggle time, not read back from the
// (page-only) `rows`. These tests pin that capture + the markEmailed mirror.
describe('logbook selection → email UUIDs', () => {
    it('toggleRow captures the UUID; selectedUuids lists the selected rows', () => {
        logbookState.toggleRow(qso(1, 'u1'));
        logbookState.toggleRow(qso(2, 'u2'));
        expect(logbookState.selectedCount).toBe(2);
        expect(logbookState.selectedUuids.sort()).toEqual(['u1', 'u2']);
    });

    it('toggling a row off drops both its id and its UUID', () => {
        const a = qso(1, 'u1');
        logbookState.toggleRow(a);
        logbookState.toggleRow(qso(2, 'u2'));
        logbookState.toggleRow(a); // off
        expect(logbookState.selectedCount).toBe(1);
        expect(logbookState.selectedUuids).toEqual(['u2']);
    });

    it('UUIDs survive paging away — selection is not derived from the visible page', () => {
        logbookState.rows = [qso(1, 'u1')];
        logbookState.toggleRow(logbookState.rows[0]);
        logbookState.rows = [qso(9, 'u9')]; // operator paged forward; row 1 no longer visible
        expect(logbookState.selectedUuids).toEqual(['u1']);
    });

    it('a selected row without a UUID is counted but excluded from the email payload', () => {
        logbookState.toggleRow(qso(1)); // no uuid (pre-UUID legacy import)
        logbookState.toggleRow(qso(2, 'u2'));
        expect(logbookState.selectedCount).toBe(2);
        expect(logbookState.selectedUuids).toEqual(['u2']);
    });

    it('toggleAllVisible selects then clears the visible rows and their UUIDs', () => {
        logbookState.rows = [qso(1, 'u1'), qso(2, 'u2'), qso(3, 'u3')];
        logbookState.toggleAllVisible();
        expect(logbookState.selectedUuids.sort()).toEqual(['u1', 'u2', 'u3']);
        logbookState.toggleAllVisible();
        expect(logbookState.selectedCount).toBe(0);
        expect(logbookState.selectedUuids).toEqual([]);
    });

    it('clearSelection drops ids and UUIDs together', () => {
        logbookState.toggleRow(qso(1, 'u1'));
        logbookState.clearSelection();
        expect(logbookState.selectedCount).toBe(0);
        expect(logbookState.selectedUuids).toEqual([]);
    });

    it('markEmailed flips only the matching visible rows to forwarded-by-email', () => {
        logbookState.rows = [qso(1, 'u1'), qso(2, 'u2')];
        logbookState.markEmailed(['u1']);
        expect(logbookState.rows[0].sm_fwrd_by_email_status).toBe('Y');
        expect(logbookState.rows[1].sm_fwrd_by_email_status).toBeUndefined();
    });
});
