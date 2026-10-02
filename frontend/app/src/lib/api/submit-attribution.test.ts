/*
    GET /v1/submit-attribution (ADR 0085, RS1): what a Phone / CW submit is
    stored with. Strict: all three fields present as strings — "" is a
    known-empty value — or the read FAILS (null), so a draft records missing
    attribution rather than a guess.
*/
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchSubmitAttribution } from './submit-attribution';

afterEach(() => vi.restoreAllMocks());

function respond(status: number, body: unknown): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

describe('fetchSubmitAttribution', () => {
    it('reads all three fields, known-empty included', async () => {
        respond(200, { my_rig: '', operator: '7Q5MLV', my_name: 'Marc' });
        expect(await fetchSubmitAttribution('7Q5MLV')).toEqual({
            myRig: '',
            operator: '7Q5MLV',
            myName: 'Marc',
        });
    });
    it('a missing or non-string field is a failed read, not a guess', async () => {
        respond(200, { my_rig: 'FTdx10', operator: '7Q5MLV' });
        expect(await fetchSubmitAttribution('7Q5MLV')).toBeNull();
        respond(200, { my_rig: 'FTdx10', operator: '7Q5MLV', my_name: null });
        expect(await fetchSubmitAttribution('7Q5MLV')).toBeNull();
    });
    it('an HTTP failure is a failed read', async () => {
        respond(503, { code: 'server_busy' });
        expect(await fetchSubmitAttribution('7Q5MLV')).toBeNull();
    });
    it('asks for the operator the submit sends — empty included', async () => {
        respond(200, { my_rig: '', operator: 'G0XYZ', my_name: 'Guest' });
        const f = vi.mocked(fetch);
        await fetchSubmitAttribution('');
        expect(f.mock.calls[0][0]).toBe('/v1/submit-attribution?operator=');
        await fetchSubmitAttribution('G0/XYZ');
        expect(f.mock.calls[1][0]).toBe('/v1/submit-attribution?operator=G0%2FXYZ');
    });
});
