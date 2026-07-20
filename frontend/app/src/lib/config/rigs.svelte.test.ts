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
    rigsState.catalogue = {};
    rigsState.draft = null;
    rigsState.saving = false;
    rigsState.serialPorts = [];
    rigsState.audioAvailable = false;
    rigsState.capture = [];
    rigsState.playback = [];
});

// Route-aware fetch mock: /v1/rigs (GET), /v1/hardware (GET), /v1/config (PUT).
// Returns the captured PUT bodies for the save assertions.
function mockCluster(rigsBody: unknown, hardwareBody?: unknown): string[] {
    const puts: string[] = [];
    const resp = (body: unknown) =>
        Promise.resolve(
            new Response(JSON.stringify(body), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
    vi.stubGlobal(
        'fetch',
        // The app always calls fetch with a string URL + a string JSON body, so
        // the mock narrows to those (avoids RequestInfo/BodyInit stringification).
        vi.fn((url: string, init?: RequestInit) => {
            if ((init?.method ?? 'GET') === 'PUT') {
                puts.push(init?.body as string);
                return resp({});
            }
            if (url.includes('/v1/hardware')) {
                return resp(hardwareBody ?? { serial_ports: [], audio: { available: false } });
            }
            return resp(rigsBody);
        })
    );
    return puts;
}

function mockJSON(status: number, body: unknown) {
    vi.stubGlobal(
        'fetch',
        vi.fn((_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
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

    it('re-selects when the previously-selected rig disappears on reload', async () => {
        mockJSON(200, rigsBody(1, [1, 2]));
        await rigsState.load();
        rigsState.select(2);
        expect(rigsState.selectedId).toBe(2);

        // A later load returns a list without rig 2 — the selection must not be
        // left stranded (null detail despite rigs existing); it falls back.
        mockJSON(200, rigsBody(1, [1, 3]));
        await rigsState.load();
        expect(rigsState.selected).not.toBeNull();
        expect(rigsState.selectedId).toBe(1); // fell back to the default
    });

    it('ft8ModeFor distinguishes inherit (nil), leave-current (""), and override', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn((_u: RequestInfo | URL, _i?: RequestInit) =>
                Promise.resolve(
                    new Response(
                        JSON.stringify({
                            default_rig_id: 1,
                            rigs: [
                                { id: 1, model: 'ftdx10', port: '/dev/a' }, // absent → inherit
                                { id: 2, model: 'ftdx10', port: '/dev/b', ft8_mode: '' }, // leave current
                                { id: 3, model: 'ftdx10', port: '/dev/c', ft8_mode: 'RTTY-U' }, // override
                            ],
                            catalogue: [{ id: 'ftdx10', name: 'FTdx10', ft8_mode: 'DATA-U' }],
                        }),
                        { status: 200, headers: { 'Content-Type': 'application/json' } }
                    )
                )
            )
        );
        await rigsState.load();
        // nil/absent inherits the rigdef default…
        expect(rigsState.ft8ModeFor(rigsState.rigs[0])).toBe('DATA-U');
        // …an explicit "" is NOT the default — it means leave the current mode…
        expect(rigsState.ft8ModeFor(rigsState.rigs[1])).toBe('leave current mode');
        // …and any other value is the override literal.
        expect(rigsState.ft8ModeFor(rigsState.rigs[2])).toBe('RTTY-U');
    });

    it('nameFor resolves the friendly rigdef name, falling back to the model id', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn((_u: RequestInfo | URL, _i?: RequestInit) =>
                Promise.resolve(
                    new Response(
                        JSON.stringify({
                            default_rig_id: 1,
                            rigs: [
                                { id: 1, model: 'yaesu-ftdx10', port: '/dev/a' },
                                { id: 2, model: 'unknown-rig', port: '/dev/b' },
                            ],
                            catalogue: [{ id: 'yaesu-ftdx10', name: 'Yaesu FTdx10' }],
                        }),
                        { status: 200, headers: { 'Content-Type': 'application/json' } }
                    )
                )
            )
        );
        await rigsState.load();
        expect(rigsState.nameFor(rigsState.rigs[0])).toBe('Yaesu FTdx10');
        // A model absent from the catalogue falls back to the raw id.
        expect(rigsState.nameFor(rigsState.rigs[1])).toBe('unknown-rig');
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

    it('editing the port marks dirty; cancel reverts to the pristine rig', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a', audio: { rx: 'A', tx: 'A' } }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        expect(rigsState.dirty).toBe(false);
        rigsState.setDraftPort('/dev/b');
        expect(rigsState.dirty).toBe(true);
        rigsState.resetDraft();
        expect(rigsState.draft?.port).toBe('/dev/a');
        expect(rigsState.dirty).toBe(false);
    });

    it('save round-trips EVERY rig field (unrendered ones too) in a whole-catalogue PUT', async () => {
        // rig 1 carries fields the panel never renders — they MUST ride back, or
        // the daemon's whole-replace of base.Rigs would zero them (config wipe).
        const puts = mockCluster(
            {
                default_rig_id: 1,
                rigs: [
                    {
                        id: 1,
                        model: 'ftdx10',
                        port: '/dev/a',
                        audio: { rx: 'A', tx: 'A' },
                        ft8_mode: 'DATA-U',
                        my_rig: 'FTDX10',
                        mode_mappings: { USB: { mode: 'SSB' } },
                        overrides: { baud_rate: 38400 },
                    },
                    { id: 2, model: 'ic7300', port: '/dev/b' },
                ],
                catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
            },
            {
                serial_ports: [{ id: '/dev/new', label: 'new port' }],
                audio: { available: true, capture: [{ name: 'A' }], playback: [{ name: 'A' }] },
            }
        );
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftPort('/dev/new');
        await rigsState.save();

        expect(puts).toHaveLength(1);
        const sent = JSON.parse(puts[0]) as {
            rigs: Array<Record<string, unknown>>;
            default_rig_id: number;
        };
        expect(sent.default_rig_id).toBe(1);
        const r1 = sent.rigs.find((r) => r.id === 1);
        expect(r1?.port).toBe('/dev/new'); // the edit
        // …and the unrendered fields rode back verbatim (the config-wipe guard):
        expect(r1?.mode_mappings).toEqual({ USB: { mode: 'SSB' } });
        expect(r1?.overrides).toEqual({ baud_rate: 38400 });
        expect(r1?.ft8_mode).toBe('DATA-U');
        expect(r1?.my_rig).toBe('FTDX10');
        // …and the OTHER rig is preserved whole.
        expect(sent.rigs.find((r) => r.id === 2)).toMatchObject({ id: 2, model: 'ic7300' });
        expect(rigsState.dirty).toBe(false); // cleared after a successful save
    });
});
