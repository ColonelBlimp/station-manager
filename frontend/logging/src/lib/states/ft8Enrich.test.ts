import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';

// Mock the two daemon-endpoint wrappers so observe()'s lookups resolve under
// test control. The state module's only side effects are these two calls plus
// reads of configState.
vi.mock('../api/enrichment', () => ({ enrichCallsign: vi.fn() }));
vi.mock('../api/contest-dupe', () => ({ fetchContestDupe: vi.fn() }));

import { enrichCallsign } from '../api/enrichment';
import { fetchContestDupe } from '../api/contest-dupe';
import { ft8EnrichState } from './ft8Enrich.svelte';
import { configState } from './config.svelte';

const enrichMock = vi.mocked(enrichCallsign);
const dupeMock = vi.mocked(fetchContestDupe);

// Let the chained .then/allSettled microtasks settle, then flush reactivity.
async function settle(): Promise<void> {
    await new Promise((r) => setTimeout(r, 0));
    flushSync();
}

function okEnrich(ccode: string, name?: string) {
    return {
        kind: 'ok' as const,
        result: {
            callsign: 'X',
            country: { ccode, name },
            country_source: 'hamnut' as const,
            station_source: 'none',
        },
    };
}

beforeEach(() => {
    ft8EnrichState.clear();
    configState.defaultLogbook.id = 1;
    enrichMock.mockReset();
    dupeMock.mockReset();
    enrichMock.mockResolvedValue(okEnrich('GB'));
    dupeMock.mockResolvedValue({ kind: 'ok', duplicate: false });
    flushSync();
});

afterEach(() => {
    vi.restoreAllMocks();
});

describe('ft8EnrichState.observe', () => {
    it('fills flag and worked from both endpoints', async () => {
        dupeMock.mockResolvedValue({ kind: 'ok', duplicate: true });
        ft8EnrichState.observe('G3XYZ', '20m');
        await settle();

        expect(enrichMock).toHaveBeenCalledWith('G3XYZ');
        expect(dupeMock).toHaveBeenCalledWith({
            logbook: 1,
            call: 'G3XYZ',
            band: '20m',
            mode: 'FT8',
        });
        expect(ft8EnrichState.info('G3XYZ', '20m')).toEqual({ flag: '🇬🇧', worked: true });
    });

    it('carries the country name (for the flag tooltip) alongside the flag', async () => {
        enrichMock.mockResolvedValue(okEnrich('GB', 'England'));
        ft8EnrichState.observe('G3XYZ', '20m');
        await settle();
        expect(ft8EnrichState.info('G3XYZ', '20m')?.country).toBe('England');
    });

    it('carries is_new_entity from enrichment (the far-right new-DXCC marker)', async () => {
        enrichMock.mockResolvedValue({
            kind: 'ok' as const,
            result: {
                callsign: 'X',
                country: { ccode: 'GB', is_new_entity: true },
                country_source: 'hamnut' as const,
                station_source: 'none',
            },
        });
        ft8EnrichState.observe('G3XYZ', '20m');
        await settle();
        expect(ft8EnrichState.info('G3XYZ', '20m')?.isNewEntity).toBe(true);
    });

    it('is fail-soft: a flag lookup error still leaves the worked answer', async () => {
        enrichMock.mockRejectedValue(new Error('hamnut down'));
        dupeMock.mockResolvedValue({ kind: 'ok', duplicate: false });
        ft8EnrichState.observe('K1ABC', '40m');
        await settle();

        expect(ft8EnrichState.info('K1ABC', '40m')).toEqual({ worked: false });
    });

    it('skips the worked lookup when there is no logbook', async () => {
        configState.defaultLogbook.id = 0;
        ft8EnrichState.observe('K1ABC', '20m');
        await settle();

        expect(dupeMock).not.toHaveBeenCalled();
        expect(ft8EnrichState.info('K1ABC', '20m')).toEqual({ flag: '🇬🇧' });
    });

    it('skips the worked lookup when the band is unknown but still flags', async () => {
        ft8EnrichState.observe('K1ABC', '');
        await settle();

        expect(dupeMock).not.toHaveBeenCalled();
        expect(ft8EnrichState.info('K1ABC', '')).toEqual({ flag: '🇬🇧' });
    });

    it('looks a key up at most once (dedupe)', async () => {
        ft8EnrichState.observe('K1ABC', '20m');
        await settle();
        ft8EnrichState.observe('K1ABC', '20m');
        ft8EnrichState.observe('K1ABC', '20m');
        await settle();

        expect(enrichMock).toHaveBeenCalledTimes(1);
        expect(dupeMock).toHaveBeenCalledTimes(1);
    });

    it('treats a band change as a new key and re-looks-up', async () => {
        ft8EnrichState.observe('K1ABC', '20m');
        await settle();
        ft8EnrichState.observe('K1ABC', '40m');
        await settle();

        expect(dupeMock).toHaveBeenCalledTimes(2);
        expect(dupeMock).toHaveBeenLastCalledWith({
            logbook: 1,
            call: 'K1ABC',
            band: '40m',
            mode: 'FT8',
        });
    });

    it('clear() drops all decorations', async () => {
        ft8EnrichState.observe('K1ABC', '20m');
        await settle();
        expect(ft8EnrichState.info('K1ABC', '20m')).toBeDefined();

        ft8EnrichState.clear();
        flushSync();
        expect(ft8EnrichState.info('K1ABC', '20m')).toBeUndefined();
    });
});
// The highlight colours moved to daemon config (configState.ft8Display); their
// persistence is covered by the daemon config tests + configState hydration, not
// here.
