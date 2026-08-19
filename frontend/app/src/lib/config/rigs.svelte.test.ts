import { afterEach, describe, expect, it, vi } from 'vitest';
import { rigsState } from './rigs.svelte';
import { toasts } from '../ui/toasts.svelte';

afterEach(() => {
    vi.restoreAllMocks();
    // Reset the singleton between cases (load() short-circuits if selectedId set).
    rigsState.rigs = [];
    rigsState.defaultRigId = 0;
    rigsState.selectedId = null;
    rigsState.loaded = false;
    rigsState.error = '';
    rigsState.catalogue = {};
    rigsState.drafts = {};
    rigsState.baselines = {};
    rigsState.saving = false;
    rigsState.settingDefault = false;
    rigsState.serialPorts = [];
    rigsState.audioAvailable = false;
    rigsState.capture = [];
    rigsState.playback = [];
});

// Route-aware fetch mock: /v1/rigs (GET), /v1/hardware (GET), /v1/config (PUT).
// rigsBody may be a function called per GET /v1/rigs (so a test can return a
// DIFFERENT catalogue on the save's re-fetch than on the initial load). Returns
// the captured PUT bodies for the save assertions.
function mockCluster(rigsBody: object | (() => unknown), hardwareBody?: unknown): string[] {
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
            return resp(typeof rigsBody === 'function' ? rigsBody() : rigsBody);
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

    it('a rig with no audio is NOT dirty on load (no injected empty audio block)', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }], // no audio field
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        expect(rigsState.dirty).toBe(false); // review Rigs-editor #2
        expect(rigsState.draft?.audio).toBeUndefined();
    });

    it('per-rig drafts survive switching rigs (edits are not discarded)', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [
                { id: 1, model: 'ftdx10', port: '/dev/a' },
                { id: 2, model: 'ic7300', port: '/dev/b' },
            ],
            catalogue: [],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftPort('/dev/edited-1');
        rigsState.select(2); // switch away — must NOT discard rig 1's edit
        expect(rigsState.dirty).toBe(false); // rig 2 is pristine
        rigsState.select(1); // back to rig 1
        expect(rigsState.draft?.port).toBe('/dev/edited-1'); // review Rigs-editor #3
        expect(rigsState.dirty).toBe(true);
    });

    it('save re-fetches + merges onto the FRESH catalogue and omits default_rig_id', async () => {
        // Load returns catalogue A; the save's re-fetch returns catalogue B where
        // rig 2 changed concurrently (new port) — the PUT must carry the FRESH
        // rig 2, not the stale mount snapshot, and must NOT send default_rig_id
        // (reviews Rigs-editor #1). rig 1 keeps its un-rendered fields.
        let get = 0;
        const rig1 = {
            id: 1,
            model: 'ftdx10',
            port: '/dev/a',
            audio: { rx: 'A', tx: 'A' },
            mode_mappings: { USB: { mode: 'SSB' } },
        };
        const puts = mockCluster(
            () => {
                get++;
                return {
                    default_rig_id: 1,
                    // rig 2's port differs between load (get 1) and re-fetch (get 2)
                    rigs: [
                        { ...rig1 },
                        { id: 2, model: 'ic7300', port: get === 1 ? '/dev/b' : '/dev/CONCURRENT' },
                    ],
                    catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
                };
            },
            { serial_ports: [{ id: '/dev/new', label: 'p' }], audio: { available: false } }
        );
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftPort('/dev/new');
        await rigsState.save();

        expect(puts).toHaveLength(1);
        const sent = JSON.parse(puts[0]) as {
            rigs: Array<Record<string, unknown>>;
            default_rig_id?: number;
        };
        expect('default_rig_id' in sent).toBe(false); // active rig untouched
        const s1 = sent.rigs.find((r) => r.id === 1);
        expect(s1?.port).toBe('/dev/new'); // our edit
        expect(s1?.mode_mappings).toEqual({ USB: { mode: 'SSB' } }); // un-rendered field preserved
        // the concurrent change to rig 2 is preserved, not clobbered by the snapshot
        expect(sent.rigs.find((r) => r.id === 2)?.port).toBe('/dev/CONCURRENT');
        expect(rigsState.dirty).toBe(false);
    });

    it('changing the model REPLACES the draft object and PUTs the new model', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ic7300', port: '/dev/a' }],
            catalogue: [
                { id: 'ic7300', name: 'IC-7300' },
                { id: 'ftdx10', name: 'FTdx10' },
            ],
        });
        await rigsState.load();
        rigsState.select(1);
        const before = rigsState.draft;
        rigsState.setDraftModel('ftdx10');
        expect(rigsState.draft).not.toBe(before); // object replaced ⇒ {#key draft} sub-editors remount
        expect(rigsState.draft?.model).toBe('ftdx10');
        expect(rigsState.dirty).toBe(true);

        await rigsState.save();
        const sent = JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> };
        expect(sent.rigs.find((r) => r.id === 1)?.model).toBe('ftdx10');
        expect(rigsState.dirty).toBe(false);
    });

    it('editing ft8_mode and MY_RIG PUTs them as per-rig overrides', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }], // no overrides ⇒ inherit
            catalogue: [{ id: 'ftdx10', name: 'FTdx10', ft8_mode: 'DATA-U' }],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftFt8Mode('DATA-U');
        rigsState.setDraftMyRig('FTDX10 #2');
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.ft8_mode).toBe('DATA-U');
        expect(s1?.my_rig).toBe('FTDX10 #2');
    });

    it('a pure MY_RIG edit is NOT restart-relevant (resolved live); other fields are', async () => {
        // MY_RIG is resolved per QSO at submit (qsoservice ResolveMyRigFor), so a
        // MY_RIG-only change applies live — no daemon restart. Everything else
        // (model / port / audio / ft8_mode / serial) binds at startup. Mirrors the
        // config SPA's restartRelevant (canonRig sans my_rig). (clean-room 8c42755e P3)
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);

        rigsState.setDraftMyRig('FTDX10 #2');
        expect(rigsState.dirty).toBe(true); // it IS an unsaved change…
        expect(rigsState.restartDirty).toBe(false); // …but no restart is owed

        rigsState.setDraftFt8Mode('DATA-U'); // ft8_mode binds at startup
        expect(rigsState.restartDirty).toBe(true);
    });

    it('a MY_RIG-only save reports a live change, not a daemon restart', async () => {
        const info = vi.spyOn(toasts, 'info');
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftMyRig('FTDX10 #2'); // the ONLY edit
        await rigsState.save();

        expect(info).toHaveBeenCalledTimes(1);
        expect(info.mock.calls[0][0]).not.toMatch(/restart/i); // MY_RIG applies per QSO
    });

    it('a connection/ft8_mode save reports that a daemon restart is needed', async () => {
        const info = vi.spyOn(toasts, 'info');
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10', ft8_mode: 'DATA-U' }],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftFt8Mode('RTTY-U'); // binds at startup ⇒ restart owed
        await rigsState.save();

        expect(info.mock.calls[0][0]).toMatch(/restart/i);
    });

    it('typing then clearing an inherited override returns to not-dirty (delete-key)', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }], // ft8_mode absent (inherit)
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftFt8Mode('X');
        expect(rigsState.dirty).toBe(true);
        rigsState.setDraftFt8Mode(''); // cleared back to inherit
        expect(rigsState.dirty).toBe(false); // matches the loaded-absent form — no spurious dirty
    });

    it('clearing a SET override drops the key on save (inherit) and is not blank/null', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a', ft8_mode: 'DATA-U' }], // SET
            catalogue: [{ id: 'ftdx10', name: 'FTdx10', ft8_mode: 'DATA-U' }],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftFt8Mode('');
        expect(rigsState.dirty).toBe(true); // clearing a SET value IS a change
        await rigsState.save();

        const s1 =
            (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
                (r) => r.id === 1
            ) ?? {};
        expect('ft8_mode' in s1).toBe(false); // dropped ⇒ inherit (not '' or null)
    });

    it('editing only ft8_mode preserves a concurrent port change on the same rig', async () => {
        let get = 0;
        const puts = mockCluster(() => {
            get++;
            return {
                default_rig_id: 1,
                rigs: [{ id: 1, model: 'ftdx10', port: get === 1 ? '/dev/a' : '/dev/CONCURRENT' }],
                catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
            };
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftFt8Mode('DATA-U'); // ONLY ft8_mode
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.ft8_mode).toBe('DATA-U'); // our edit
        expect(s1?.port).toBe('/dev/CONCURRENT'); // concurrent change preserved, not clobbered
    });

    it('editing only the port preserves a concurrent audio change on the SAME rig', async () => {
        // review Rigs-editor #1: the merge is FIELD-level. The operator changes
        // only the port; between load and the save re-fetch, another writer sets
        // this rig's audio. The PUT must carry the fresh audio (not the draft's
        // stale value) alongside the operator's new port.
        let get = 0;
        const puts = mockCluster(
            () => {
                get++;
                return {
                    default_rig_id: 1,
                    rigs: [
                        {
                            id: 1,
                            model: 'ftdx10',
                            port: '/dev/a',
                            // audio changed concurrently between load (get 1) and re-fetch (get 2)
                            audio:
                                get === 1
                                    ? { rx: 'OLD', tx: 'OLD' }
                                    : { rx: 'CONCURRENT', tx: 'CONCURRENT' },
                        },
                    ],
                    catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
                };
            },
            { serial_ports: [{ id: '/dev/new', label: 'p' }], audio: { available: false } }
        );
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftPort('/dev/new'); // ONLY the port is edited
        await rigsState.save();

        const sent = JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> };
        const s1 = sent.rigs.find((r) => r.id === 1);
        expect(s1?.port).toBe('/dev/new'); // our edit
        // the concurrent audio survives — NOT overwritten by the draft's stale "OLD"
        expect(s1?.audio).toEqual({ rx: 'CONCURRENT', tx: 'CONCURRENT' });
    });

    it('editing only audio RX preserves a concurrent audio TX change (rx/tx are independent)', async () => {
        // review Rigs-editor #5: audio RX and TX are independent fields. Editing
        // only RX must not write the draft's stale TX back over a concurrent TX
        // change picked up by the save re-fetch.
        let get = 0;
        const puts = mockCluster(
            () => {
                get++;
                return {
                    default_rig_id: 1,
                    rigs: [
                        {
                            id: 1,
                            model: 'ftdx10',
                            port: '/dev/a',
                            audio:
                                get === 1
                                    ? { rx: 'RX-OLD', tx: 'TX-OLD' }
                                    : { rx: 'RX-OLD', tx: 'TX-CONCURRENT' },
                        },
                    ],
                    catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
                };
            },
            { serial_ports: [], audio: { available: false } }
        );
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftAudio('rx', 'RX-NEW'); // ONLY RX edited
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        // our RX edit AND the concurrent TX change both survive
        expect(s1?.audio).toEqual({ rx: 'RX-NEW', tx: 'TX-CONCURRENT' });
    });

    it('a pristine retained draft is re-baselined on refresh (no false dirty, no revert)', async () => {
        // review Rigs-editor #6: visit rig 1 (pristine), edit + save rig 2 while
        // rig 1's port changes concurrently. Rig 1's retained draft must rebase to
        // the fresh value — not read falsely dirty, and not revert the concurrent
        // change if later saved.
        let get = 0;
        const puts = mockCluster(
            () => {
                get++;
                return {
                    default_rig_id: 1,
                    rigs: [
                        {
                            id: 1,
                            model: 'ftdx10',
                            port: get === 1 ? '/dev/a1' : '/dev/a1-CONCURRENT',
                        },
                        { id: 2, model: 'ic7300', port: '/dev/b2' },
                    ],
                    catalogue: [],
                };
            },
            { serial_ports: [{ id: '/dev/new', label: 'p' }], audio: { available: false } }
        );
        await rigsState.load();
        rigsState.select(1); // visit rig 1 — a pristine draft + baseline are created
        rigsState.select(2); // switch to rig 2 and edit it
        rigsState.setDraftPort('/dev/new');
        await rigsState.save(); // the save's re-fetch observes rig 1's concurrent change

        // rig 1's pristine draft rebased to the concurrent value → not falsely dirty
        rigsState.select(1);
        expect(rigsState.draft?.port).toBe('/dev/a1-CONCURRENT');
        expect(rigsState.dirty).toBe(false);
        // and the rig 2 PUT carried rig 1's FRESH port, never the stale mount value
        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.port).toBe('/dev/a1-CONCURRENT');
    });

    it('Cancel on a concurrently-changed rig adopts the fresh server value, not the stale baseline', async () => {
        // review Rigs-editor #7: a dirty draft keeps its old baseline across a
        // refresh (by design). If the rig ALSO changed concurrently, Cancel must
        // surface the current server value — not revert to the stale original.
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftPort('/dev/edited'); // dirty
        // Simulate #applyFetched adopting a concurrent server change while this
        // draft stayed dirty (a dirty draft's baseline is deliberately not rebased).
        rigsState.rigs = [{ id: 1, model: 'ftdx10', port: '/dev/CONCURRENT' }];
        rigsState.resetDraft(); // Cancel
        expect(rigsState.draft?.port).toBe('/dev/CONCURRENT'); // adopts server truth
        expect(rigsState.dirty).toBe(false); // re-baselined to it
    });

    it('editing mode_mappings persists the override on save', async () => {
        // The ModeMappingsEditor mutates draft.mode_mappings; save() must diff it
        // as one field and carry it in the PUT (the connection-only patch would
        // otherwise drop it — the reason the editor alone wasn't enough).
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        const d = rigsState.draft;
        if (!d) throw new Error('expected a draft after select');
        d.mode_mappings = { 'DATA-U': { mode: 'MFSK', submode: 'FT4' } };
        expect(rigsState.dirty).toBe(true);
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.mode_mappings).toEqual({ 'DATA-U': { mode: 'MFSK', submode: 'FT4' } });
    });

    it('editing only mode_mappings preserves a concurrent port change (field independence)', async () => {
        // mode_mappings and the connection fields are independent. Editing only the
        // mappings must keep the FRESH server port picked up by the save re-fetch,
        // not write the draft's stale port back.
        let get = 0;
        const puts = mockCluster(() => {
            get++;
            return {
                default_rig_id: 1,
                rigs: [{ id: 1, model: 'ftdx10', port: get === 1 ? '/dev/a' : '/dev/CONCURRENT' }],
                catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
            };
        });
        await rigsState.load();
        rigsState.select(1);
        const d = rigsState.draft;
        if (!d) throw new Error('expected a draft after select');
        d.mode_mappings = { 'CW-U': { mode: 'CW' } }; // ONLY the mappings edited
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.port).toBe('/dev/CONCURRENT'); // concurrent port survives
        expect(s1?.mode_mappings).toEqual({ 'CW-U': { mode: 'CW' } }); // our override applied
    });

    it('clearing mode_mappings removes the stored override on save', async () => {
        // Reverting all rows to their rigdef defaults sets draft.mode_mappings to
        // undefined; save() must DROP the override on the PUT (the fresh rig still
        // carries it), not send an empty object or the stale map.
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [
                {
                    id: 1,
                    model: 'ftdx10',
                    port: '/dev/a',
                    mode_mappings: { 'DATA-U': { mode: 'RTTY' } },
                },
            ],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        const d = rigsState.draft;
        if (!d) throw new Error('expected a draft after select');
        d.mode_mappings = undefined; // editor's "all rows back to default" state
        expect(rigsState.dirty).toBe(true);
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.mode_mappings).toBeUndefined();
    });

    it('editing serial overrides persists them on save', async () => {
        // SerialOverridesEditor mutates draft.overrides; save() diffs it as one
        // field and carries it in the PUT (the connection-only patch would drop it).
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        const d = rigsState.draft;
        if (!d) throw new Error('expected a draft after select');
        d.overrides = { baud_rate: 4800, parity: 'even' };
        expect(rigsState.dirty).toBe(true);
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.overrides).toEqual({ baud_rate: 4800, parity: 'even' });
    });

    it('editing only serial overrides preserves a concurrent port change (field independence)', async () => {
        // overrides and the connection fields are independent. Editing only the
        // overrides must keep the FRESH server port, not the draft's stale one.
        let get = 0;
        const puts = mockCluster(() => {
            get++;
            return {
                default_rig_id: 1,
                rigs: [{ id: 1, model: 'ftdx10', port: get === 1 ? '/dev/a' : '/dev/CONCURRENT' }],
                catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
            };
        });
        await rigsState.load();
        rigsState.select(1);
        const d = rigsState.draft;
        if (!d) throw new Error('expected a draft after select');
        d.overrides = { baud_rate: 9600 }; // ONLY the overrides edited
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.port).toBe('/dev/CONCURRENT'); // concurrent port survives
        expect(s1?.overrides).toEqual({ baud_rate: 9600 }); // our override applied
    });

    it('clearing serial overrides removes them on save', async () => {
        // Blanking every field sets draft.overrides to undefined; save() must DROP
        // the override on the PUT (the fresh rig still carries it), inheriting the
        // rigdef serial defaults.
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a', overrides: { baud_rate: 4800 } }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        const d = rigsState.draft;
        if (!d) throw new Error('expected a draft after select');
        d.overrides = undefined;
        expect(rigsState.dirty).toBe(true);
        await rigsState.save();

        const s1 = (JSON.parse(puts[0]) as { rigs: Array<Record<string, unknown>> }).rigs.find(
            (r) => r.id === 1
        );
        expect(s1?.overrides).toBeUndefined();
    });

    it('setDefault PUTs only default_rig_id (no catalogue) and moves the active flag', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [
                { id: 1, model: 'ftdx10', port: '/dev/a' },
                { id: 2, model: 'ic7300', port: '/dev/b' },
            ],
            catalogue: [
                { id: 'ftdx10', name: 'FTdx10' },
                { id: 'ic7300', name: 'IC-7300' },
            ],
        });
        await rigsState.load();
        expect(rigsState.defaultRigId).toBe(1);
        await rigsState.setDefault(2);

        // The PUT carries ONLY default_rig_id — the catalogue is left untouched.
        const body = JSON.parse(puts[0]) as { default_rig_id?: number; rigs?: unknown };
        expect(body.default_rig_id).toBe(2);
        expect(body.rigs).toBeUndefined();
        expect(rigsState.defaultRigId).toBe(2); // optimistic badge move
    });

    it('setDefault is a no-op when the rig is already the default', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        await rigsState.setDefault(1);
        expect(puts.length).toBe(0);
    });

    it('save() is blocked while a set-default write is in flight (no overlap)', async () => {
        // codex e539a080 P2: a connection save that overlaps a set-default could
        // apply the stale (pre-change) default via #applyFetched and revert the
        // badge. The two write paths are now mutually exclusive.
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        rigsState.select(1);
        rigsState.setDraftPort('/dev/new'); // dirty
        rigsState.settingDefault = true; // a set-default is mid-flight
        await rigsState.save();
        expect(puts.length).toBe(0); // save refused — no overlapping connection PUT
    });

    it('nextRigModel picks the first unused catalogue model, falling back to the first', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ic7300', port: '/dev/a' }],
            catalogue: [
                { id: 'ic7300', name: 'IC-7300' },
                { id: 'ftdx10', name: 'FTdx10' },
            ],
        });
        await rigsState.load();
        expect(rigsState.nextRigModel()).toBe('ftdx10'); // ic7300 in use ⇒ first unused

        // every model in use ⇒ fall back to the first catalogue entry
        rigsState.rigs = [
            { id: 1, model: 'ic7300', port: '/dev/a' },
            { id: 2, model: 'ftdx10', port: '/dev/b' },
        ];
        expect(rigsState.nextRigModel()).toBe('ic7300');
    });

    it('addRig appends a rig with id = max(fresh)+1, blank port, and PUTs the whole list', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [
                { id: 1, model: 'ic7300', port: '/dev/a' },
                { id: 3, model: 'ftdx10', port: '/dev/b' }, // sparse ids — max is 3, not length
            ],
            catalogue: [
                { id: 'ic7300', name: 'IC-7300' },
                { id: 'ftdx10', name: 'FTdx10' },
            ],
        });
        await rigsState.load();
        await rigsState.addRig('ic7300');

        expect(puts).toHaveLength(1);
        const sent = JSON.parse(puts[0]) as {
            rigs: Array<Record<string, unknown>>;
            default_rig_id?: number;
        };
        expect(sent.rigs.map((r) => r.id)).toEqual([1, 3, 4]); // max(1,3)+1 = 4
        expect(sent.rigs.find((r) => r.id === 4)).toEqual({ id: 4, model: 'ic7300', port: '' });
        expect('default_rig_id' in sent).toBe(false); // existing default resolves ⇒ untouched
        expect(rigsState.selectedId).toBe(4); // new rig focused for configuration
        expect(rigsState.dirty).toBe(false); // fresh draft == baseline (not spuriously dirty)
    });

    it('adding the FIRST rig makes it the active default (default_rig_id sent)', async () => {
        const puts = mockCluster({
            default_rig_id: 0,
            rigs: [],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        expect(rigsState.rigs).toHaveLength(0);
        await rigsState.addRig('ftdx10');

        const sent = JSON.parse(puts[0]) as {
            rigs: Array<{ id: number }>;
            default_rig_id?: number;
        };
        expect(sent.rigs.map((r) => r.id)).toEqual([1]);
        expect(sent.default_rig_id).toBe(1); // daemon 400s on an unresolvable default_rig_id
        expect(rigsState.defaultRigId).toBe(1);
    });

    it('addRig appends onto the FRESH list, preserving a concurrent add', async () => {
        let get = 0;
        const puts = mockCluster(() => {
            get++;
            const rigs: Array<Record<string, unknown>> = [
                { id: 1, model: 'ic7300', port: '/dev/a' },
            ];
            // between load (get 1) and the add re-fetch (get 2) another client added rig 5
            if (get >= 2) rigs.push({ id: 5, model: 'ftdx10', port: '/dev/CONCURRENT' });
            return {
                default_rig_id: 1,
                rigs,
                catalogue: [
                    { id: 'ic7300', name: 'IC-7300' },
                    { id: 'ftdx10', name: 'FTdx10' },
                ],
            };
        });
        await rigsState.load();
        await rigsState.addRig('ic7300');

        const sent = JSON.parse(puts[0]) as { rigs: Array<{ id: number; port?: string }> };
        // the concurrent rig 5 survives, and the new id is max(1,5)+1 = 6
        expect(sent.rigs.map((r) => r.id)).toEqual([1, 5, 6]);
        expect(sent.rigs.find((r) => r.id === 5)?.port).toBe('/dev/CONCURRENT');
    });

    it('addRig surfaces a daemon rejection and does not add the rig locally', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn((url: string, init?: RequestInit) => {
                if (init?.method === 'PUT') {
                    return Promise.resolve(
                        new Response(JSON.stringify({ message: 'rig invalid' }), {
                            status: 400,
                            headers: { 'Content-Type': 'application/json' },
                        })
                    );
                }
                const body = url.includes('/v1/hardware')
                    ? { serial_ports: [], audio: { available: false } }
                    : {
                          default_rig_id: 1,
                          rigs: [{ id: 1, model: 'ic7300', port: '/dev/a' }],
                          catalogue: [{ id: 'ic7300', name: 'IC-7300' }],
                      };
                return Promise.resolve(
                    new Response(JSON.stringify(body), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );
        await rigsState.load();
        await rigsState.addRig('ic7300');
        expect(rigsState.rigs.map((r) => r.id)).toEqual([1]); // unchanged — the add was rejected
        expect(rigsState.saving).toBe(false); // and the section isn't stuck saving
    });

    it('addRig is a no-op when there is no model to add (empty catalogue)', async () => {
        const puts = mockCluster({ default_rig_id: 0, rigs: [], catalogue: [] });
        await rigsState.load();
        await rigsState.addRig(rigsState.nextRigModel()); // '' ⇒ no-op
        expect(puts).toHaveLength(0);
    });

    // The immediate add is NON-idempotent (each call assigns a new id), so an
    // AMBIGUOUS timeout — the PUT committed but its response was lost — must NOT be
    // treated as a plain failure: a blind retry would append a SECOND rig of the
    // same model. addRig re-reads and reconciles, mirroring the CAT switch's
    // #reconcileAfterTimeout (clean-room review 7b5ed1d2 P2).
    it('addRig adopts a timed-out PUT that COMMITTED (reconcile, no double-add)', async () => {
        let get = 0;
        const ok = (body: unknown) =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        vi.stubGlobal(
            'fetch',
            vi.fn((url: string, init?: RequestInit) => {
                if (init?.method === 'PUT') {
                    const e = new Error('timed out');
                    e.name = 'TimeoutError';
                    return Promise.reject(e);
                }
                if (url.includes('/v1/hardware')) {
                    return ok({ serial_ports: [], audio: { available: false } });
                }
                get++;
                // GET 1 = load, GET 2 = the add's fresh re-fetch → both [1];
                // GET 3 = the reconcile re-read → [1,2] (the add DID commit).
                const rigs =
                    get <= 2
                        ? [{ id: 1, model: 'ic7300', port: '/dev/a' }]
                        : [
                              { id: 1, model: 'ic7300', port: '/dev/a' },
                              { id: 2, model: 'ic7300', port: '' },
                          ];
                return ok({
                    default_rig_id: 1,
                    rigs,
                    catalogue: [{ id: 'ic7300', name: 'IC-7300' }],
                });
            })
        );
        await rigsState.load();
        await rigsState.addRig('ic7300'); // PUT times out; reconcile sees rig 2 landed

        expect(rigsState.rigs.map((r) => r.id)).toEqual([1, 2]); // adopted the committed rig
        expect(rigsState.selectedId).toBe(2); // focused for configuration
        expect(rigsState.saving).toBe(false);
    });

    it('addRig reports a timed-out PUT that did NOT commit (state unchanged, safe retry)', async () => {
        const ok = (body: unknown) =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        vi.stubGlobal(
            'fetch',
            vi.fn((url: string, init?: RequestInit) => {
                if (init?.method === 'PUT') {
                    const e = new Error('timed out');
                    e.name = 'TimeoutError';
                    return Promise.reject(e);
                }
                if (url.includes('/v1/hardware')) {
                    return ok({ serial_ports: [], audio: { available: false } });
                }
                // Every GET returns [1] — the add never landed.
                return ok({
                    default_rig_id: 1,
                    rigs: [{ id: 1, model: 'ic7300', port: '/dev/a' }],
                    catalogue: [{ id: 'ic7300', name: 'IC-7300' }],
                });
            })
        );
        await rigsState.load();
        await rigsState.addRig('ic7300'); // times out; reconcile shows no new rig

        expect(rigsState.rigs.map((r) => r.id)).toEqual([1]); // no phantom rig appended
        expect(rigsState.saving).toBe(false);
    });

    it('addRig with a timed-out PUT AND an unreadable reconcile keeps state + reports unknown', async () => {
        // The formal residual (a delayed PUT + a failed reconcile) must NOT invite a
        // blind retry: local state is preserved, saving clears, and the message tells
        // the operator to reload rather than retry (clean-room review 0c3abf9f P1,
        // accept + harden). The reconcile GET failing is the safe fallback for a
        // genuinely stuck daemon.
        const err = vi.spyOn(toasts, 'error');
        let get = 0;
        const ok = (body: unknown) =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        vi.stubGlobal(
            'fetch',
            vi.fn((url: string, init?: RequestInit) => {
                if (init?.method === 'PUT') {
                    const e = new Error('timed out');
                    e.name = 'TimeoutError';
                    return Promise.reject(e);
                }
                if (url.includes('/v1/hardware')) {
                    return ok({ serial_ports: [], audio: { available: false } });
                }
                get++;
                // GET 1 = load, GET 2 = the add's fresh re-fetch → ok [1];
                // GET 3 = the reconcile re-read → FAILS (daemon unreadable).
                if (get >= 3) return Promise.resolve(new Response('{}', { status: 503 }));
                return ok({
                    default_rig_id: 1,
                    rigs: [{ id: 1, model: 'ic7300', port: '/dev/a' }],
                    catalogue: [{ id: 'ic7300', name: 'IC-7300' }],
                });
            })
        );
        await rigsState.load();
        await rigsState.addRig('ic7300'); // PUT times out; reconcile GET fails ⇒ unknown

        expect(rigsState.rigs.map((r) => r.id)).toEqual([1]); // local state preserved (no phantom)
        expect(rigsState.saving).toBe(false); // not stuck saving
        expect(rigsState.loaded).toBe(true); // still the last good load — not torn down
        expect(err).toHaveBeenCalledTimes(1);
        expect(err.mock.calls[0][0]).toMatch(/state unknown/i); // "unknown", not "did not commit"
        expect(err.mock.calls[0][0]).toMatch(/reload/i); // reload, NOT a blind retry
    });

    // A failed RELOAD marks the section unloaded, not just errored. Settings is
    // mounted behind a router branch (App.svelte:100), so leaving unmounts it
    // while this module — a singleton — survives; returning re-fires onMount →
    // load(). Leaving `loaded` true there renders the previous catalogue as
    // though current, and a rigs save PUTs the WHOLE catalogue, so a stale one
    // can revert a rig added or re-ported meanwhile. Full reasoning in
    // email.svelte.test.ts (clean-room review dcb0316e69b9).
    it('a failed reload after a successful one clears loaded', async () => {
        mockJSON(200, {
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        await rigsState.load();
        expect(rigsState.loaded).toBe(true);

        mockJSON(503, {});
        await rigsState.load();

        expect(rigsState.loaded).toBe(false);
        expect(rigsState.error).not.toBe('');
    });

    it('a pending reload immediately marks the section unloaded', async () => {
        const body = rigsBody(1, [1]);
        mockJSON(200, body);
        await rigsState.load();

        const releases: { url: string; resolve: (response: Response) => void }[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn(
                (url: string) =>
                    new Promise<Response>((resolve) => {
                        releases.push({ url, resolve });
                    })
            )
        );
        const reload = rigsState.load();

        expect(rigsState.loading).toBe(true);
        expect(rigsState.loaded).toBe(false);

        for (const pending of releases) {
            const responseBody = pending.url.includes('/v1/hardware')
                ? { serial_ports: [], audio: { available: false } }
                : body;
            pending.resolve(
                new Response(JSON.stringify(responseBody), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        }
        await reload;
        expect(rigsState.loaded).toBe(true);
    });

    it('save is refused while the section is not loaded', async () => {
        const puts = mockCluster(rigsBody(1, [1]));
        await rigsState.load();
        rigsState.setDraftPort('/dev/changed');
        rigsState.loaded = false;

        await rigsState.save();

        expect(puts).toHaveLength(0);
    });
});
