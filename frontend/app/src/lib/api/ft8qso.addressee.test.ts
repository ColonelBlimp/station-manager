import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setArchiveBindingGate, setArchiveMismatchSink, setBoundArchive } from './archive-guard';
import { ft8AnswerExchange, startFt8Qso } from './ft8qso';

// Calling the station a heard line is ADDRESSED TO (operator, 2026-11-17). The app names
// the slot the line was actually heard in and says the station is its addressee; the
// daemon judges freshness on that line and listens in the opposite slots (codex-132: the
// app must not shift the slot itself, or a line still inside the limit is refused as stale).

let bodies: Record<string, unknown>[];

beforeEach(() => {
    bodies = [];
    setBoundArchive('01a163a7-0f04-7f69-85e9-255664b58d11');
    setArchiveBindingGate(() => false);
    setArchiveMismatchSink(() => {});
    vi.stubGlobal(
        'fetch',
        vi.fn((_input: string, init?: RequestInit) => {
            const raw = typeof init?.body === 'string' ? init.body : '{}';
            bodies.push(JSON.parse(raw) as Record<string, unknown>);
            return Promise.resolve(
                new Response('{}', { status: 202, headers: { 'Content-Type': 'application/json' } })
            );
        })
    );
});

afterEach(() => {
    vi.unstubAllGlobals();
});

describe('ft8AnswerExchange', () => {
    it('names the exchange for what the operator acted on', () => {
        expect(ft8AnswerExchange({ fd: false })).toBe('standard');
        expect(ft8AnswerExchange({ fd: true })).toBe('fd');
        expect(ft8AnswerExchange({ fd: false, type4: true })).toBe('type4');
        expect(ft8AnswerExchange({ fd: false, type4: false, addressee: true })).toBe('addressee');
    });
});

describe('startFt8Qso to the addressee of a heard line', () => {
    it('sends the heard slot as it is, marks the addressee, and never a grid', async () => {
        await startFt8Qso('VK9XY', 'RE78', '2026-10-23T12:00:15Z', 1500, 14.074, 'addressee');
        expect(bodies).toHaveLength(1);
        expect(bodies[0]).toMatchObject({
            their_call: 'VK9XY',
            their_grid: '', // the line's grid is its sender's, never the addressee's
            slot_utc: '2026-10-23T12:00:15Z',
            addressee: true,
        });
        expect(bodies[0]).not.toHaveProperty('mode');
        expect(bodies[0]).not.toHaveProperty('their_snr');
    });

    it('an ordinary answer is unchanged: no addressee field', async () => {
        await startFt8Qso('K1ABC', 'FN42', '2026-10-23T12:00:00Z', 1500, 14.074);
        expect(bodies[0]).toMatchObject({ their_call: 'K1ABC', their_grid: 'FN42' });
        expect(bodies[0]).not.toHaveProperty('addressee');
    });
});
