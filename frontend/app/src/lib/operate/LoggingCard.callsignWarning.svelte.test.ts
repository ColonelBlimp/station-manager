// W-0012 unrecognised-callsign slice (operator rulings 2026-11-02, after 0H5RW
// was logged in the UK/EI DX SSB contest). The Phone/CW card warns when the
// lookup explicitly does not recognise the typed call's prefix:
//   AC1 the warning text, under the enrichment panel;
//   AC2 no warning when the lookup could not run or found nothing it could say;
//   AC3 warn only — logging still succeeds, and it can sit beside DUPE;
//   AC4 hidden at once when the call changes or clears, and a late answer for
//       the previous call never brings it back (debounce included).
// FT8 never shows it.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import EnrichmentCard from './EnrichmentCard.svelte';
import { draft, clearDraft, setSubmit, setSerialAllocator, submitState } from './qso.svelte';
import { setEnricher, observeCall, setMyGrid, type Enrichment } from './enrich.svelte';
import { contest } from './contest.svelte';
import { setContestDupeCheck } from './contestDupe.svelte';
import { rig, confirmRig, resetCatLink } from './rig.svelte';

const WARNING = 'No country found for 0H5RW — check the callsign';

function result(over: Partial<Enrichment> = {}): Enrichment {
    return {
        country: '',
        ccode: '',
        dxcc: '',
        isNewEntity: null,
        grid: '',
        name: '',
        qth: '',
        email: '',
        cqZone: '',
        ituZone: '',
        countryNotFound: false,
        ...over,
    };
}

/** 0H5RW is not recognised; every other call is Finland. */
const enricher = vi.fn((call: string) =>
    Promise.resolve(
        call === '0H5RW'
            ? result({ countryNotFound: true })
            : result({ country: 'Finland', ccode: 'FI' })
    )
);

async function typeCall(value: string): Promise<void> {
    await fireEvent.input(screen.getByLabelText('Callsign'), { target: { value } });
}

async function settle(): Promise<void> {
    await vi.advanceTimersByTimeAsync(500); // past the 400 ms lookup debounce
    flushSync();
}

describe('LoggingCard: unrecognised-callsign warning', () => {
    beforeEach(() => {
        vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
        observeCall(''); // reset the shared lookup state
        resetCatLink();
        rig.cat = 'off';
        confirmRig();
        submitState.duplicate = false;
        clearDraft();
        contest.on = false;
        enricher.mockClear();
        setEnricher(enricher);
    });
    afterEach(() => {
        clearDraft();
        observeCall('');
        contest.on = false;
        setContestDupeCheck(null);
        setSerialAllocator(null);
        vi.useRealTimers();
    });

    it('AC1: warns under the enrichment panel when the prefix is not recognised', async () => {
        render(LoggingCard);
        await typeCall('0H5RW');
        await settle();
        const warning = screen.getByTestId('callsign-not-recognised');
        expect(warning).toHaveTextContent(WARNING);
        expect(screen.getByTestId('enrichment-column')).toContainElement(warning);
    });

    it('AC2: no warning when the lookup returned nothing (could not run)', async () => {
        setEnricher(() => Promise.resolve(null));
        render(LoggingCard);
        await typeCall('0H5RW');
        await settle();
        expect(screen.queryByTestId('callsign-not-recognised')).toBeNull();
    });

    it('AC2: no warning when the lookup threw', async () => {
        setEnricher(() => Promise.reject(new Error('offline')));
        render(LoggingCard);
        await typeCall('0H5RW');
        await settle();
        expect(screen.queryByTestId('callsign-not-recognised')).toBeNull();
    });

    it('AC2: no warning for a recognised call', async () => {
        render(LoggingCard);
        await typeCall('OH5RW');
        await settle();
        expect(screen.queryByTestId('callsign-not-recognised')).toBeNull();
    });

    it('AC4: hides at once when the call changes, before the new lookup runs', async () => {
        render(LoggingCard);
        await typeCall('0H5RW');
        await settle();
        expect(screen.getByTestId('callsign-not-recognised')).toBeInTheDocument();

        await typeCall('OH5RW'); // inside the debounce: no new lookup yet
        flushSync();
        expect(screen.queryByTestId('callsign-not-recognised')).toBeNull();
    });

    it('AC4: hides at once when the contact is cleared', async () => {
        render(LoggingCard);
        await typeCall('0H5RW');
        await settle();
        clearDraft();
        flushSync();
        expect(screen.queryByTestId('callsign-not-recognised')).toBeNull();
    });

    it('AC4: a late answer for the previous call never brings the warning back', async () => {
        let resolveLate: (e: Enrichment) => void = () => {};
        setEnricher(
            (call) =>
                new Promise<Enrichment>((resolve) => {
                    if (call === '0H5RW') resolveLate = resolve;
                    else resolve(result({ country: 'Finland', ccode: 'FI' }));
                })
        );
        render(LoggingCard);
        await typeCall('0H5RW');
        await vi.advanceTimersByTimeAsync(500); // 0H5RW's lookup is now in flight

        await typeCall('OH5RW'); // corrected before the answer arrives
        resolveLate(result({ countryNotFound: true })); // inside OH5RW's debounce
        await vi.advanceTimersByTimeAsync(0);
        flushSync();
        expect(screen.queryByTestId('callsign-not-recognised')).toBeNull();

        await settle(); // OH5RW's own lookup lands
        expect(screen.queryByTestId('callsign-not-recognised')).toBeNull();
    });

    it('AC3: logging succeeds while the warning is visible', async () => {
        const logged: string[] = [];
        setSubmit((q) => {
            logged.push(q.callsign);
            return Promise.resolve({ ok: true as const });
        });
        render(LoggingCard);
        await typeCall('0H5RW');
        await settle();
        expect(screen.getByTestId('callsign-not-recognised')).toBeInTheDocument();

        draft.dateOn = '2026-10-31';
        draft.timeOn = '14:10';
        flushSync();
        window.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true })
        );
        await vi.advanceTimersByTimeAsync(0);
        flushSync();
        expect(logged).toEqual(['0H5RW']);
    });

    it('AC3: the warning and the contest DUPE warning can show together', async () => {
        vi.setSystemTime(Date.UTC(2026, 9, 31, 13, 0));
        setContestDupeCheck(() => Promise.resolve(true));
        contest.on = true;
        contest.id = 'UKEIDXSSB';
        contest.start = '2026-10-31';
        rig.band = '15m';
        render(LoggingCard);
        await typeCall('0H5RW');
        await settle();
        expect(screen.getByTestId('contest-dupe')).toBeInTheDocument();
        expect(screen.getByTestId('callsign-not-recognised')).toHaveTextContent(WARNING);
    });

    // The verdict is a result for the warning only: the shared card still reads
    // it as "No match", exactly as it did when the seam collapsed it to null.
    it('the shared enrichment card shows "No match" for a not-recognised call', async () => {
        render(EnrichmentCard, { call: '0H5RW' });
        await settle();
        expect(screen.getByText('No match')).toBeInTheDocument();
    });

    // Review 2026-11-02 (P2): a country miss must not hide station enrichment.
    // HamNut can say found:false while the station lookup still returns a grid;
    // the bearing, distance, path controls and local time stay visible, and the
    // country warning is still Phone/CW's alone.
    describe('country miss with a station result', () => {
        const mixed = (call: string): Promise<Enrichment> =>
            Promise.resolve(
                call === '0H5RW'
                    ? result({ countryNotFound: true, grid: 'KP20', name: 'Pekka' })
                    : result({ country: 'Finland', ccode: 'FI' })
            );
        beforeEach(() => {
            setMyGrid('KH78AN');
            setEnricher(mixed);
        });
        afterEach(() => setMyGrid(''));

        it('the shared card keeps the station-derived display, not "No match"', async () => {
            render(EnrichmentCard, { call: '0H5RW' });
            await settle();
            expect(screen.queryByText('No match')).toBeNull();
            expect(screen.getByText(/° · .* km/)).toBeInTheDocument();
            expect(screen.getByTitle('Short path')).toBeInTheDocument();
            expect(screen.getByText(/UTC[+−]\d+/)).toBeInTheDocument();
            expect(screen.queryByText(/No country found/)).toBeNull(); // FT8's card: no warning
        });

        it('Phone/CW shows the station display and the country warning together', async () => {
            render(LoggingCard);
            await typeCall('0H5RW');
            await settle();
            expect(screen.getByText(/° · .* km/)).toBeInTheDocument();
            expect(screen.getByTestId('callsign-not-recognised')).toHaveTextContent(WARNING);
        });
    });

    it('FT8: the shared enrichment card never shows it', async () => {
        render(EnrichmentCard, { call: '0H5RW' });
        await settle();
        expect(enricher).toHaveBeenCalledWith('0H5RW', expect.anything());
        expect(screen.queryByText(/No country found/)).toBeNull();
    });
});
