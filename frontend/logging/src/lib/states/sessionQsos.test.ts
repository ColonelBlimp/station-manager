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

    describe('markEmailed', () => {
        it('stamps emailedDate on the matching uuids only', () => {
            sessionQsosState.add(makeQso({ uuid: 'a', callsign: 'AAA' }));
            sessionQsosState.add(makeQso({ uuid: 'b', callsign: 'BBB' }));
            sessionQsosState.add(makeQso({ uuid: 'c', callsign: 'CCC' }));
            flushSync();

            sessionQsosState.markEmailed(['a', 'c'], '20260531');
            flushSync();

            expect(sessionQsosState.items[0].emailedDate).toBe('20260531');
            expect(sessionQsosState.items[1].emailedDate).toBeUndefined();
            expect(sessionQsosState.items[2].emailedDate).toBe('20260531');
        });

        it('persists the stamp to sessionStorage', () => {
            sessionQsosState.add(makeQso({ uuid: 'a' }));
            sessionQsosState.markEmailed(['a'], '20260531');
            const parsed = JSON.parse(sessionStorage.getItem('sm.session.qsos')!) as SessionQso[];
            expect(parsed[0].emailedDate).toBe('20260531');
        });

        it('is a no-op for an empty uuid set (daemon could not stamp)', () => {
            sessionQsosState.add(makeQso({ uuid: 'a' }));
            flushSync();
            sessionQsosState.markEmailed([], '20260531');
            flushSync();
            expect(sessionQsosState.items[0].emailedDate).toBeUndefined();
        });

        it('is a no-op when the date is empty', () => {
            sessionQsosState.add(makeQso({ uuid: 'a' }));
            flushSync();
            sessionQsosState.markEmailed(['a'], '');
            flushSync();
            expect(sessionQsosState.items[0].emailedDate).toBeUndefined();
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
            });
            sessionQsosState.add(original);
            const raw = sessionStorage.getItem('sm.session.qsos');
            const parsed = JSON.parse(raw!) as SessionQso[];
            expect(parsed[0]).toEqual(original);
        });
    });
});
