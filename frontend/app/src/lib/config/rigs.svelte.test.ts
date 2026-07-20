import { afterEach, describe, expect, it, vi } from 'vitest';
import { rigsState } from './rigs.svelte';

afterEach(() => {
    vi.restoreAllMocks();
    // Reset the singleton between cases (load() short-circuits if selectedId set).
    rigsState.rigs = [];
    rigsState.defaultRigId = 0;
    rigsState.selectedId = null;
    rigsState.loaded = false;
    rigsState.error = '';
});

function mockJSON(status: number, body: unknown) {
    vi.stubGlobal(
        'fetch',
        vi.fn(
            (_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(
                    new Response(JSON.stringify(body), {
                        status,
                        headers: { 'Content-Type': 'application/json' },
                    }),
                ),
        ),
    );
}

function rigsBody(defaultRigId: number, ids: number[]) {
    return {
        default_rig_id: defaultRigId,
        rigs: ids.map((id) => ({ id, model: `model-${id}`, port: `/dev/rig${id}` })),
        catalogue: [],
    };
}

describe('rigsState', () => {
    it('load populates the list + default and pre-selects the active rig', async () => {
        mockJSON(200, rigsBody(2, [1, 2, 3]));
        await rigsState.load();
        expect(rigsState.loaded).toBe(true);
        expect(rigsState.rigs.map((r) => r.id)).toEqual([1, 2, 3]);
        expect(rigsState.defaultRigId).toBe(2);
        expect(rigsState.selectedId).toBe(2); // the default is pre-selected
        expect(rigsState.selected?.model).toBe('model-2');
    });

    it('pre-selects the first rig when the default id is not in the list', async () => {
        mockJSON(200, rigsBody(99, [4, 5]));
        await rigsState.load();
        expect(rigsState.selectedId).toBe(4);
    });

    it('leaves selection null when there are no rigs', async () => {
        mockJSON(200, rigsBody(0, []));
        await rigsState.load();
        expect(rigsState.rigs).toHaveLength(0);
        expect(rigsState.selectedId).toBeNull();
        expect(rigsState.selected).toBeNull();
    });

    it('select changes the detailed rig', async () => {
        mockJSON(200, rigsBody(1, [1, 2]));
        await rigsState.load();
        rigsState.select(2);
        expect(rigsState.selected?.id).toBe(2);
    });

    it('surfaces an error (never throws) on a non-2xx load', async () => {
        mockJSON(500, { message: 'boom' });
        await rigsState.load();
        expect(rigsState.loaded).toBe(false);
        expect(rigsState.error).not.toBe('');
    });
});
