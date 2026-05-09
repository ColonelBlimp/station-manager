import { describe, it, expect, beforeEach } from 'vitest';
import { flushSync } from 'svelte';
import { sessionQsosState, type SessionQso } from './sessionQsos.svelte';

/**
 * sessionQsosState — per-tab session log + sessionStorage persistence.
 *
 * Verifies the contract SessionPanel + InfoPanel rely on:
 *   - add appends and bumps count.
 *   - update replaces the row keyed by uuid (used by edit overlay later).
 *   - clear empties the list.
 *   - sessionStorage round-trip works (the F5-survival contract).
 */

const makeQso = (overrides: Partial<SessionQso> = {}): SessionQso => ({
    uuid: '00000000-0000-7000-8000-000000000001',
    callsign: 'M0XYZ',
    name: 'Test',
    freqHz: 14_250_000,
    band: '20m',
    rstSent: '59',
    rstRcvd: '59',
    mode: 'USB',
    timeOn: '14:32',
    qsoDate: '2026-05-09',
    country: 'England',
    distanceKm: '350',
    adif: '<call:5>M0XYZ<eor>',
    ...overrides,
});

describe('sessionQsosState', () => {
    beforeEach(() => {
        sessionQsosState.clear();
        sessionStorage.clear();
        flushSync();
    });

    describe('add / count', () => {
        it('starts empty with count=0', () => {
            flushSync();
            expect(sessionQsosState.items).toHaveLength(0);
            expect(sessionQsosState.count).toBe(0);
        });

        it('add appends and bumps count', () => {
            sessionQsosState.add(makeQso());
            sessionQsosState.add(makeQso({ uuid: 'b', callsign: 'G7ABC' }));
            flushSync();
            expect(sessionQsosState.items).toHaveLength(2);
            expect(sessionQsosState.count).toBe(2);
            expect(sessionQsosState.items[1].callsign).toBe('G7ABC');
        });

        it('add preserves submit order (oldest first)', () => {
            sessionQsosState.add(makeQso({ uuid: 'a', callsign: 'AAA' }));
            sessionQsosState.add(makeQso({ uuid: 'b', callsign: 'BBB' }));
            sessionQsosState.add(makeQso({ uuid: 'c', callsign: 'CCC' }));
            flushSync();
            expect(sessionQsosState.items.map((q) => q.callsign)).toEqual(['AAA', 'BBB', 'CCC']);
        });
    });

    describe('update', () => {
        it('replaces the row matching the given uuid', () => {
            sessionQsosState.add(makeQso({ uuid: 'a', callsign: 'AAA' }));
            sessionQsosState.add(makeQso({ uuid: 'b', callsign: 'BBB' }));
            flushSync();

            sessionQsosState.update(
                'b',
                makeQso({ uuid: 'b', callsign: 'BBB-EDITED', name: 'New' })
            );
            flushSync();

            expect(sessionQsosState.items[1].callsign).toBe('BBB-EDITED');
            expect(sessionQsosState.items[1].name).toBe('New');
            // Other rows untouched.
            expect(sessionQsosState.items[0].callsign).toBe('AAA');
        });

        it('is a no-op when uuid is not present', () => {
            sessionQsosState.add(makeQso({ uuid: 'a' }));
            flushSync();
            sessionQsosState.update('does-not-exist', makeQso({ uuid: 'x', callsign: 'XYZ' }));
            flushSync();
            expect(sessionQsosState.items).toHaveLength(1);
            expect(sessionQsosState.items[0].callsign).toBe('M0XYZ');
        });
    });

    describe('clear', () => {
        it('empties the list and resets count to 0', () => {
            sessionQsosState.add(makeQso());
            sessionQsosState.add(makeQso({ uuid: 'b' }));
            flushSync();
            expect(sessionQsosState.count).toBe(2);
            sessionQsosState.clear();
            flushSync();
            expect(sessionQsosState.count).toBe(0);
            expect(sessionQsosState.items).toHaveLength(0);
        });
    });

    describe('sessionStorage round-trip', () => {
        it('persists to sessionStorage on add', () => {
            sessionQsosState.add(makeQso({ uuid: 'persisted', callsign: 'PPP' }));
            const raw = sessionStorage.getItem('sm.session.qsos');
            expect(raw).not.toBeNull();
            const parsed = JSON.parse(raw!) as SessionQso[];
            expect(parsed).toHaveLength(1);
            expect(parsed[0].callsign).toBe('PPP');
        });

        it('persists empty list on clear', () => {
            sessionQsosState.add(makeQso());
            sessionQsosState.clear();
            const raw = sessionStorage.getItem('sm.session.qsos');
            expect(raw).not.toBeNull();
            expect(JSON.parse(raw!)).toEqual([]);
        });

        it('add round-trips a non-trivial record without losing fields', () => {
            const original = makeQso({
                uuid: 'rt',
                callsign: 'RTX',
                country: 'Malawi',
                distanceKm: '8123',
                adif: '<call:3>RTX<band:3>20m<eor>',
            });
            sessionQsosState.add(original);
            const raw = sessionStorage.getItem('sm.session.qsos');
            const parsed = JSON.parse(raw!) as SessionQso[];
            expect(parsed[0]).toEqual(original);
        });
    });
});
