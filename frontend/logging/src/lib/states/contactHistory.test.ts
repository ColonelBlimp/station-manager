import { describe, it, expect, beforeEach } from 'vitest';
import { flushSync } from 'svelte';
import { contactHistoryState } from './contactHistory.svelte';
import type { ContactHistory } from '../api/contact-history';

/**
 * contactHistoryState — latest /v1/contact-history result + count.
 *
 * Verifies the reactive contract that WorkedPanel + InfoPanel rely on:
 *   - setResult populates items and updates count.
 *   - clear empties items (back to null) and resets count to 0.
 *   - count distinguishes "no fetch yet" from "fetch returned empty";
 *     both read 0 but only one renders the table empty-state.
 */

const makeRow = (overrides: Partial<ContactHistory> = {}): ContactHistory => ({
    id: 1,
    uuid: '00000000-0000-7000-8000-000000000001',
    band: '20m',
    freq: '14.250000',
    mode: 'SSB',
    qso_date: '20260508',
    time_on: '1430',
    name: 'Test',
    country: 'England',
    call: 'M0XYZ',
    rst_sent: '59',
    rst_rcvd: '59',
    notes: '',
    ...overrides,
});

describe('contactHistoryState', () => {
    beforeEach(() => {
        contactHistoryState.clear();
        flushSync();
    });

    describe('setResult / clear', () => {
        it('starts with items=null and count=0', () => {
            flushSync();
            expect(contactHistoryState.items).toBeNull();
            expect(contactHistoryState.count).toBe(0);
        });

        it('setResult with rows populates items and count', () => {
            contactHistoryState.setResult([makeRow(), makeRow({ uuid: 'b' })]);
            flushSync();
            expect(contactHistoryState.items).toHaveLength(2);
            expect(contactHistoryState.count).toBe(2);
        });

        it('setResult with empty array flips items from null → [] (count stays 0)', () => {
            contactHistoryState.setResult([]);
            flushSync();
            // items === null  is "no fetch yet"
            // items === []    is "fetched, no prior contacts"
            // The distinction matters to WorkedPanel's empty-state render.
            expect(contactHistoryState.items).not.toBeNull();
            expect(contactHistoryState.items).toHaveLength(0);
            expect(contactHistoryState.count).toBe(0);
        });

        it('clear resets items to null and count to 0', () => {
            contactHistoryState.setResult([makeRow()]);
            flushSync();
            expect(contactHistoryState.count).toBe(1);
            contactHistoryState.clear();
            flushSync();
            expect(contactHistoryState.items).toBeNull();
            expect(contactHistoryState.count).toBe(0);
        });
    });
});
