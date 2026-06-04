import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import SessionPanel from './SessionPanel.svelte';
import { sessionQsosState, type SessionQso } from '../../states/sessionQsos.svelte';
import { qsoEditState } from '../../states/qsoEdit.svelte';
import { configState } from '../../states/config.svelte';
import { fetchQso, type FetchQsoOutcome } from '../../api/qso-update';

/**
 * SessionPanel — edit-overlay open-race guard (review 2026-06-04 H2).
 *
 * The Edit GET is async; by the time it resolves the operator may have
 * closed the overlay (ESC / cancel) or opened a different row. These
 * tests pin that a stale GET never re-opens a dismissed overlay (test 1)
 * and never clobbers a newer open (test 2). fetchQso is mocked with
 * deferred promises so the test controls exactly when each GET resolves;
 * the assertions read qsoEditState, which openEdit populates only when
 * the populate guard passes.
 */

vi.mock('../../api/qso-update', () => ({
    fetchQso: vi.fn(),
}));

const mockFetchQso = vi.mocked(fetchQso);

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => {
        resolve = r;
    });
    return { promise, resolve };
}

const makeRow = (uuid: string, callsign: string): SessionQso => ({
    uuid,
    callsign,
    name: '',
    freqHz: 14_250_000,
    band: '20m',
    rstSent: '59',
    rstRcvd: '59',
    mode: 'USB',
    timeOn: '14:30',
    qsoDate: '2026-06-04',
    country: '',
    distanceKm: '',
    adif: '',
});

const okOutcome = (uuid: string, callsign: string): FetchQsoOutcome => ({
    kind: 'ok',
    qso: { uuid, call: callsign },
});

function editButton(callsign: string): HTMLButtonElement {
    const btn = document.querySelector<HTMLButtonElement>(
        `button[aria-label="Edit QSO with ${callsign}"]`
    );
    if (btn === null) throw new Error(`no Edit button rendered for ${callsign}`);
    return btn;
}

describe('SessionPanel — edit-overlay open race (H2)', () => {
    beforeEach(() => {
        mockFetchQso.mockReset();
        sessionQsosState.clear();
        qsoEditState.close();
        configState.mailer.enabled = false; // hide the Emailed column
    });

    afterEach(() => {
        cleanup();
        sessionQsosState.clear();
        qsoEditState.close();
    });

    it('drops a GET that resolves after the overlay was closed (no re-open)', async () => {
        sessionQsosState.add(makeRow('uuid-A', 'M0AAA'));
        const d = deferred<FetchQsoOutcome>();
        mockFetchQso.mockReturnValue(d.promise);

        render(SessionPanel);
        await tick();

        await fireEvent.click(editButton('M0AAA'));
        // beginOpen ran synchronously inside openEdit; the GET is pending.
        expect(qsoEditState.open).toBe(true);
        expect(qsoEditState.loading).toBe(true);

        // Operator dismisses the overlay (ESC / cancel) before the GET lands.
        qsoEditState.close();
        await tick();
        expect(qsoEditState.open).toBe(false);

        // The late GET resolves — the populate guard must drop it so the
        // dismissed overlay does not silently re-open.
        d.resolve(okOutcome('uuid-A', 'M0AAA'));
        await tick();
        await tick();

        expect(qsoEditState.open).toBe(false);
        expect(qsoEditState.callsign).toBe(''); // never populated
    });

    it('a late GET for a closed row does not clobber a newly-opened row', async () => {
        sessionQsosState.add(makeRow('uuid-A', 'M0AAA'));
        sessionQsosState.add(makeRow('uuid-B', 'M0BBB'));

        const dA = deferred<FetchQsoOutcome>();
        const dB = deferred<FetchQsoOutcome>();
        mockFetchQso.mockReturnValueOnce(dA.promise).mockReturnValueOnce(dB.promise);

        render(SessionPanel);
        await tick();

        // Open A (GET-A pending), close it, then open B (GET-B pending).
        await fireEvent.click(editButton('M0AAA'));
        expect(qsoEditState.uuid).toBe('uuid-A');
        qsoEditState.close();
        await tick();

        await fireEvent.click(editButton('M0BBB'));
        expect(qsoEditState.uuid).toBe('uuid-B');
        expect(qsoEditState.open).toBe(true);

        // GET-A resolves LATE — must not populate, because the overlay
        // now shows B (uuid mismatch).
        dA.resolve(okOutcome('uuid-A', 'M0AAA'));
        await tick();
        await tick();
        expect(qsoEditState.uuid).toBe('uuid-B');
        expect(qsoEditState.callsign).not.toBe('M0AAA');

        // GET-B resolves — B populates normally (guard passes).
        dB.resolve(okOutcome('uuid-B', 'M0BBB'));
        await tick();
        await tick();
        expect(qsoEditState.uuid).toBe('uuid-B');
        expect(qsoEditState.callsign).toBe('M0BBB');
    });
});
