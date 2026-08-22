import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import EditQsoModal from './EditQsoModal.svelte';
import type { LogbookQso } from '../api/logbooks';
import type { QsoPatch } from '../api/qso-patch';
import type { EnrichOutcome, EnrichmentStation } from '../api/enrichment';

// The re-enrichment lookup is the seam under test — mock it so the test controls
// exactly WHEN each callsign's response resolves (a slow A vs a fast B).
vi.mock('../api/enrichment', () => ({ enrichCallsign: vi.fn() }));
import { enrichCallsign } from '../api/enrichment';
const mockEnrich = vi.mocked(enrichCallsign);

beforeEach(() => {
    mockEnrich.mockReset();
});
afterEach(() => {
    vi.restoreAllMocks();
});

function deferred<T>() {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => (resolve = r));
    return { promise, resolve };
}

// A couple of microtasks + a Svelte tick, so a just-resolved lookup's `.then`
// runs and the reactive DOM settles — deterministic, so a NEGATIVE assertion
// (stale data did NOT land) doesn't have to wait out a timeout.
async function flush(): Promise<void> {
    await Promise.resolve();
    await Promise.resolve();
    await tick();
}

function ok(station: EnrichmentStation): EnrichOutcome {
    return {
        kind: 'ok',
        result: {
            callsign: 'TEST',
            station,
            country_source: 'hamnut',
            station_source: 'qrzlookupservice',
        },
    };
}

function rowFor(call: string, overrides: Partial<LogbookQso> = {}): LogbookQso {
    return {
        qso_date: '20260101',
        time_on: '1200',
        call,
        country: '',
        name: '',
        gridsquare: '',
        comment: '',
        ...overrides,
    } as LogbookQso;
}

function baseProps(call: string, overrides: Partial<LogbookQso> = {}, onSave = vi.fn()) {
    return { row: rowFor(call, overrides), saving: false, error: null, onSave, onClose: vi.fn() };
}

const reEnrichBtn = () => screen.getByRole('button', { name: /Re-enrich|Looking up/i });
const saveBtn = () => screen.getByRole('button', { name: /^(Save|Saving)/i });
const input = (label: string) => screen.getByLabelText<HTMLInputElement>(label);

describe('EditQsoModal re-enrichment generation safety (F-02)', () => {
    it('a slow lookup for A cannot modify the form after the callsign changes to B', async () => {
        const dA = deferred<EnrichOutcome>();
        mockEnrich.mockReturnValueOnce(dA.promise);
        render(EditQsoModal, { props: baseProps('A1AA') });

        await fireEvent.click(reEnrichBtn());
        await fireEvent.input(input('Callsign'), { target: { value: 'B2BB' } });

        dA.resolve(ok({ country: 'CountryA', name: 'NameA', gridsquare: 'AA11aa' }));
        await flush();

        expect(input('Country').value).toBe('');
        expect(input('Name').value).toBe('');
        expect(input('Grid').value).toBe('');
    });

    it('B can start a new lookup without waiting for the aborted A request', async () => {
        const dA = deferred<EnrichOutcome>();
        const dB = deferred<EnrichOutcome>();
        mockEnrich.mockReturnValueOnce(dA.promise).mockReturnValueOnce(dB.promise);
        render(EditQsoModal, { props: baseProps('A1AA') });

        await fireEvent.click(reEnrichBtn()); // A in flight
        await fireEvent.input(input('Callsign'), { target: { value: 'B2BB' } }); // aborts A
        await fireEvent.click(reEnrichBtn()); // B starts without A having finished
        expect(mockEnrich).toHaveBeenCalledTimes(2);

        dB.resolve(ok({ country: 'CountryB', name: 'NameB' }));
        await flush();
        expect(input('Country').value).toBe('CountryB');
    });

    it('a late A response cannot replace B result or loading state', async () => {
        const dA = deferred<EnrichOutcome>();
        const dB = deferred<EnrichOutcome>();
        mockEnrich.mockReturnValueOnce(dA.promise).mockReturnValueOnce(dB.promise);
        render(EditQsoModal, { props: baseProps('A1AA') });

        await fireEvent.click(reEnrichBtn());
        await fireEvent.input(input('Callsign'), { target: { value: 'B2BB' } });
        await fireEvent.click(reEnrichBtn());

        dB.resolve(ok({ country: 'CountryB', name: 'NameB' }));
        await flush();
        expect(input('Country').value).toBe('CountryB');

        dA.resolve(ok({ country: 'CountryA', name: 'NameA' })); // A arrives late
        await flush();
        expect(input('Country').value).toBe('CountryB');
        expect(input('Name').value).toBe('NameB');
    });

    it('changing to B after a successful A lookup drops A hidden dxcc/cqz/ituz/cont from the save patch', async () => {
        const dA = deferred<EnrichOutcome>();
        mockEnrich.mockReturnValueOnce(dA.promise);
        const onSave = vi.fn();
        render(EditQsoModal, { props: baseProps('A1AA', {}, onSave) });

        await fireEvent.click(reEnrichBtn());
        dA.resolve(
            ok({ country: 'CountryA', name: 'NameA', dxcc: '291', cqz: '5', ituz: '8', cont: 'NA' })
        );
        await flush();

        await fireEvent.input(input('Callsign'), { target: { value: 'B2BB' } });
        await flush();

        await fireEvent.click(saveBtn());
        expect(onSave).toHaveBeenCalledTimes(1);
        const patch = onSave.mock.calls[0][0] as QsoPatch;
        expect(patch.call).toBe('B2BB');
        expect(patch.dxcc).toBeUndefined();
        expect(patch.cqz).toBeUndefined();
        expect(patch.ituz).toBeUndefined();
        expect(patch.cont).toBeUndefined();
    });

    it('unmounting aborts and invalidates the in-flight lookup; a fresh mount starts clean', async () => {
        const dA = deferred<EnrichOutcome>();
        let signal: AbortSignal | undefined;
        mockEnrich.mockImplementationOnce((_c, s) => {
            signal = s;
            return dA.promise;
        });
        const { unmount } = render(EditQsoModal, { props: baseProps('A1AA') });

        await fireEvent.click(reEnrichBtn());
        expect(signal?.aborted).toBe(false);
        unmount();
        expect(signal?.aborted).toBe(true);

        dA.resolve(ok({ country: 'CountryA' })); // resolves after unmount — discarded, no throw
        await flush();

        // Reopening is a fresh instance with no leaked A state.
        render(EditQsoModal, { props: baseProps('B2BB') });
        expect(input('Country').value).toBe('');
    });

    it('retracts a lookup-written value on callsign change, but keeps an operator edit', async () => {
        const dA = deferred<EnrichOutcome>();
        mockEnrich.mockReturnValueOnce(dA.promise);
        render(EditQsoModal, { props: baseProps('A1AA', { country: 'OrigCountry' }) });

        await fireEvent.click(reEnrichBtn());
        dA.resolve(ok({ country: 'CountryA', name: 'NameA' }));
        await flush();
        expect(input('Country').value).toBe('CountryA'); // lookup overwrote
        expect(input('Name').value).toBe('NameA');

        // Operator edits Name AFTER the lookup wrote it.
        await fireEvent.input(input('Name'), { target: { value: 'OperatorName' } });

        // Callsign change invalidates: the unedited Country (still the lookup's value)
        // is restored to its pre-lookup value; the operator-edited Name is kept.
        await fireEvent.input(input('Callsign'), { target: { value: 'B2BB' } });
        await flush();
        expect(input('Country').value).toBe('OrigCountry');
        expect(input('Name').value).toBe('OperatorName');
    });

    // codex dc15e188 [P2]: re-enrich is only invalidated by a CALLSIGN change. A
    // second lookup for the SAME callsign that returns partial data must UPDATE the
    // fields it carries and LEAVE the rest — never retract a field the first lookup
    // already filled (the retract belongs to invalidation, not re-apply).
    it('a partial second lookup for the same callsign keeps the first lookup other fields (visible and hidden)', async () => {
        const d1 = deferred<EnrichOutcome>();
        const d2 = deferred<EnrichOutcome>();
        mockEnrich.mockReturnValueOnce(d1.promise).mockReturnValueOnce(d2.promise);
        const onSave = vi.fn();
        render(EditQsoModal, { props: baseProps('A1AA', {}, onSave) });

        await fireEvent.click(reEnrichBtn());
        d1.resolve(ok({ country: 'CountryA', name: 'NameA', dxcc: '291', cqz: '5' }));
        await flush();
        expect(input('Country').value).toBe('CountryA');

        // Second re-enrich, SAME callsign, returns only a name (no country, no extras).
        await fireEvent.click(reEnrichBtn());
        d2.resolve(ok({ name: 'NameA2' }));
        await flush();
        expect(input('Country').value).toBe('CountryA'); // visible field NOT erased
        expect(input('Name').value).toBe('NameA2'); // the field it carried IS updated

        // The first lookup's hidden DXCC/zone corrections still ride along on Save —
        // the partial response must not drop them (codex 88916515 [P2]).
        await fireEvent.click(saveBtn());
        const patch = onSave.mock.calls[0][0] as QsoPatch;
        expect(patch.dxcc).toBe('291');
        expect(patch.cqz).toBe('5');
    });
});
