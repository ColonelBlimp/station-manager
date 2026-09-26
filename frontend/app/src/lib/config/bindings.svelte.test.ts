import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { bindingsState, rowKey, _resetBindingsForTests } from './bindings.svelte';
import { toasts } from '../ui/toasts.svelte';

/*
    ADR 0082 part 9 (W-0021 5E): the Forwarding tab's "Destinations for this
    archive" state. Drafts are per logbook row; only changed rows ride a save;
    a row turned on without a required per-logbook field is refused BEFORE the
    wire (the field is marked, that switch goes back to what the daemon holds,
    sibling rows keep their drafts); a daemon refusal restores every switch
    (nothing was saved) and keeps typed values; a timed-out save is re-read,
    never reported as saved.
*/

const TYPES = {
    types: [
        {
            type: 'qrz',
            display_name: 'QRZ Logbook',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'api_key', label: 'API key', kind: 'password', scope: 'logbook' },
            ],
        },
        {
            type: 'smcloud',
            display_name: 'SM Cloud backup',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'Service URL', kind: 'text', scope: 'station' },
                { key: 'token', label: 'Bearer token', kind: 'password', scope: 'station' },
                {
                    key: 'logbook',
                    label: 'Cloud logbook name',
                    kind: 'text',
                    clearable: true,
                    scope: 'logbook',
                },
            ],
        },
        {
            type: 'club',
            display_name: 'Club-like',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'email', label: 'Account email', kind: 'text', scope: 'logbook' },
                {
                    key: 'callsign',
                    label: 'Callsign',
                    kind: 'text',
                    scope: 'logbook',
                    defaults_to: 'logbook_callsign',
                },
            ],
        },
    ],
};

const row = (id: number, name: string, over: Record<string, unknown> = {}) => ({
    logbook_id: id,
    logbook_uuid: `u${id}`,
    logbook_name: name,
    logbook_callsign: 'M0ABC',
    bound: false,
    enabled: false,
    forwarder_name: '',
    credentials_set: [],
    queue: { waiting: 0, failed: 0, in_flight: 0 },
    ...over,
});

function view(over: Record<string, unknown> = {}) {
    return {
        archive_id: 'A1',
        archive_label: 'Home',
        restart_required: false,
        destinations: [
            {
                type: 'qrz',
                display_name: 'QRZ Logbook',
                account: { configured: true },
                state: 'mixed',
                reason: '',
                logbooks: [
                    row(1, 'Main', {
                        bound: true,
                        enabled: true,
                        forwarder_name: 'qrz',
                        credentials_set: ['api_key'],
                    }),
                    row(2, 'Second'),
                ],
            },
            {
                type: 'smcloud',
                display_name: 'SM Cloud backup',
                account: { configured: false },
                state: 'off',
                reason: 'this destination has no station account in config.json',
                logbooks: [row(1, 'Main'), row(2, 'Second')],
            },
        ],
        ...over,
    };
}

type Call = { url: string; method: string; body: unknown };
let calls: Call[] = [];
let putAnswer: () => Promise<Response> = () => Promise.resolve(json(view()));
let getView: () => unknown = () => view();
let getAnswer: () => Promise<Response> = () => Promise.resolve(json(getView()));

function json(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
}

beforeEach(() => {
    calls = [];
    putAnswer = () => Promise.resolve(json(view()));
    getView = () => view();
    getAnswer = () => Promise.resolve(json(getView()));
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            const url = input;
            const method = init?.method ?? 'GET';
            calls.push({
                url,
                method,
                body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
            });
            if (url === '/v1/version')
                return Promise.resolve(json({ instance: 'i1', archive: { id: 'A1' } }));
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url === '/v1/qso-archives/A1/bindings') {
                return method === 'PUT' ? putAnswer() : getAnswer();
            }
            return Promise.resolve(json({}, 404));
        })
    );
});

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    _resetBindingsForTests();
});

const puts = () => calls.filter((c) => c.method === 'PUT');

describe('bindingsState', () => {
    it('B1: loads the ACTIVE archive the daemon names, with one clean draft per row', async () => {
        await bindingsState.load();
        expect(bindingsState.loaded).toBe(true);
        expect(bindingsState.archiveId).toBe('A1');
        expect(bindingsState.drafts[rowKey('qrz', 1)].enabled).toBe(true);
        expect(bindingsState.drafts[rowKey('qrz', 2)].enabled).toBe(false);
        expect(bindingsState.dirty).toBe(false);
        expect(bindingsState.logbookFields('smcloud').map((f) => f.key)).toEqual(['logbook']);
    });

    it('B2: the aggregate switch sets every row; the draft aggregate follows', async () => {
        await bindingsState.load();
        const qrz = bindingsState.view!.destinations[0];
        expect(bindingsState.draftState(qrz)).toBe('mixed');
        bindingsState.setAll('qrz', false);
        expect(bindingsState.draftState(qrz)).toBe('off');
        bindingsState.setAll('qrz', true);
        expect(bindingsState.draftState(qrz)).toBe('on');
        expect(bindingsState.dirty).toBe(true);
    });

    it('B3: a destination that cannot be turned on here refuses the switch', async () => {
        await bindingsState.load();
        bindingsState.setAll('smcloud', true);
        bindingsState.setRow('smcloud', 1, true);
        expect(bindingsState.drafts[rowKey('smcloud', 1)].enabled).toBe(false);
        expect(bindingsState.dirty).toBe(false);
    });

    it('B4: a row turned on without its required field is refused before the wire', async () => {
        await bindingsState.load();
        bindingsState.setField('qrz', 1, 'api_key', 'NEW-FOR-MAIN'); // a valid sibling edit
        bindingsState.setRow('qrz', 2, true); // Second has no API key set
        await bindingsState.save();
        expect(puts()).toHaveLength(0);
        expect(bindingsState.missing[rowKey('qrz', 2)]).toEqual(['api_key']);
        expect(bindingsState.refusal).toMatch(/QRZ Logbook for Second: API key is required/);
        // The offending switch shows what the daemon holds; the sibling keeps its draft.
        expect(bindingsState.drafts[rowKey('qrz', 2)].enabled).toBe(false);
        expect(bindingsState.drafts[rowKey('qrz', 1)].credentials.api_key).toBe('NEW-FOR-MAIN');
        // Typing the missing field drops its mark.
        bindingsState.setField('qrz', 2, 'api_key', 'K2');
        expect(bindingsState.missing[rowKey('qrz', 2)] ?? []).toEqual([]);
    });

    it('B5: a stored key satisfies the requirement; only changed rows and typed values ride the save', async () => {
        await bindingsState.load();
        putAnswer = () => Promise.resolve(json(view({ restart_required: true })));
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        bindingsState.setRow('qrz', 1, false);
        bindingsState.setRow('qrz', 2, true);
        bindingsState.setField('qrz', 2, 'api_key', 'K2');
        bindingsState.setField('qrz', 2, 'bogus', '   '); // blank: not an edit
        await bindingsState.save();
        expect(puts()).toHaveLength(1);
        expect(puts()[0].body).toEqual({
            destinations: [
                {
                    type: 'qrz',
                    logbooks: [
                        { logbook_id: 1, enabled: false },
                        { logbook_id: 2, enabled: true, credentials: { api_key: 'K2' } },
                    ],
                },
            ],
        });
        expect(info).toHaveBeenCalledWith(expect.stringMatching(/apply when the daemon restarts/));
        expect(bindingsState.dirty).toBe(false); // rebaselined to the daemon's answer
        expect(bindingsState.drafts[rowKey('qrz', 2)].credentials).toEqual({});
    });

    it('B6: a daemon refusal restores every switch and keeps typed values', async () => {
        await bindingsState.load();
        putAnswer = () =>
            Promise.resolve(
                json(
                    {
                        code: 'binding_unusable',
                        message: 'QRZ Logbook for logbook "Second" cannot be turned on',
                    },
                    400
                )
            );
        bindingsState.setRow('qrz', 1, false);
        bindingsState.setRow('qrz', 2, true);
        bindingsState.setField('qrz', 2, 'api_key', 'BAD');
        await bindingsState.save();
        expect(puts()).toHaveLength(1);
        expect(bindingsState.refusal).toMatch(/cannot be turned on/);
        expect(bindingsState.drafts[rowKey('qrz', 1)].enabled).toBe(true);
        expect(bindingsState.drafts[rowKey('qrz', 2)].enabled).toBe(false);
        expect(bindingsState.drafts[rowKey('qrz', 2)].credentials.api_key).toBe('BAD');
    });

    it('B7: a timed-out save is re-read, never reported as saved', async () => {
        await bindingsState.load();
        putAnswer = () => Promise.reject(Object.assign(new Error('t'), { name: 'TimeoutError' }));
        getView = () => view({ restart_required: true });
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        bindingsState.setRow('qrz', 2, true);
        bindingsState.setField('qrz', 2, 'api_key', 'K2');
        await bindingsState.save();
        expect(info).not.toHaveBeenCalled();
        expect(warn).toHaveBeenCalledWith(expect.stringMatching(/outcome is unknown/));
        expect(bindingsState.view!.restart_required).toBe(true); // re-read
        expect(bindingsState.drafts[rowKey('qrz', 2)].enabled).toBe(false); // the daemon's state
        expect(bindingsState.drafts[rowKey('qrz', 2)].credentials.api_key).toBe('K2'); // kept
    });

    it('B8: a stored field can be removed only from a row that ends off', async () => {
        await bindingsState.load();
        bindingsState.clear('qrz', 1, 'api_key'); // row 1 is on: refused
        expect(bindingsState.drafts[rowKey('qrz', 1)].cleared).toEqual([]);
        bindingsState.setRow('qrz', 1, false);
        bindingsState.clear('qrz', 1, 'api_key');
        bindingsState.clear('qrz', 1, 'not-a-field');
        expect(bindingsState.drafts[rowKey('qrz', 1)].cleared).toEqual(['api_key']);
        await bindingsState.save();
        expect(puts()[0].body).toEqual({
            destinations: [
                {
                    type: 'qrz',
                    logbooks: [{ logbook_id: 1, enabled: false, credentials_clear: ['api_key'] }],
                },
            ],
        });
        // Turning a row back on drops its pending removal.
        bindingsState.setRow('qrz', 1, false);
        bindingsState.clear('qrz', 1, 'api_key');
        bindingsState.setRow('qrz', 1, true);
        expect(bindingsState.drafts[rowKey('qrz', 1)].cleared).toEqual([]);
    });

    it('B8b: a refusal that restores a row to ON drops its removal marks', async () => {
        // Row 1 is on with a stored key: switched off and marked for removal,
        // then the daemon refuses the save (another row's fault). The switch
        // goes back on, so the removal — only possible from a row that ends
        // off — cannot survive with it: kept, it would leave the key's field
        // disabled with no Undo shown, and the next save refused.
        await bindingsState.load();
        putAnswer = () =>
            Promise.resolve(json({ code: 'binding_unusable', message: 'refused' }, 400));
        bindingsState.setRow('qrz', 1, false);
        bindingsState.clear('qrz', 1, 'api_key');
        bindingsState.setRow('qrz', 2, true);
        bindingsState.setField('qrz', 2, 'api_key', 'K2');
        await bindingsState.save();
        const d = bindingsState.drafts[rowKey('qrz', 1)];
        expect(d.enabled).toBe(true);
        expect(d.cleared).toEqual([]);
        expect(bindingsState.rowEdited('qrz', view().destinations[0].logbooks[0])).toBe(false);
    });

    it('B8c: after a timed-out save, removal marks survive only on a row still off with the key stored', async () => {
        await bindingsState.load();
        putAnswer = () => Promise.reject(Object.assign(new Error('t'), { name: 'TimeoutError' }));
        vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        bindingsState.setRow('qrz', 1, false);
        bindingsState.clear('qrz', 1, 'api_key');
        // Not committed: the daemon still holds row 1 ON → the mark goes.
        await bindingsState.save();
        expect(bindingsState.drafts[rowKey('qrz', 1)].enabled).toBe(true);
        expect(bindingsState.drafts[rowKey('qrz', 1)].cleared).toEqual([]);

        // Committed: row 1 is now OFF and its key is gone → nothing left to remove.
        bindingsState.setRow('qrz', 1, false);
        bindingsState.clear('qrz', 1, 'api_key');
        const base = view();
        getView = () => ({
            ...base,
            destinations: base.destinations.map((dest, i) =>
                i !== 0
                    ? dest
                    : {
                          ...dest,
                          logbooks: dest.logbooks.map((r) =>
                              r.logbook_id === 1 ? { ...r, enabled: false, credentials_set: [] } : r
                          ),
                      }
            ),
        });
        await bindingsState.save();
        expect(bindingsState.drafts[rowKey('qrz', 1)].enabled).toBe(false);
        expect(bindingsState.drafts[rowKey('qrz', 1)].cleared).toEqual([]);

        // Still off with the key stored (a stored-off row whose removal did not
        // commit) → the mark is kept, and the operator can save it again.
        getView = () => ({
            ...base,
            destinations: base.destinations.map((dest, i) =>
                i !== 0
                    ? dest
                    : {
                          ...dest,
                          logbooks: dest.logbooks.map((r) =>
                              r.logbook_id === 1 ? { ...r, enabled: false } : r
                          ),
                      }
            ),
        });
        await bindingsState.load();
        bindingsState.clear('qrz', 1, 'api_key');
        await bindingsState.save();
        expect(bindingsState.drafts[rowKey('qrz', 1)].cleared).toEqual(['api_key']);
    });

    it('B12: a field that defaults to the logbook callsign is satisfied, and left for the daemon', async () => {
        // ADR 0082 part 3 (ruled 2026-09-26): the DAEMON fills it on save, so
        // the store must neither refuse the enable nor send a value of its own.
        // Only the descriptor marker decides — no destination is named here.
        const base = view();
        getView = () => ({
            ...base,
            destinations: [
                ...base.destinations,
                {
                    type: 'club',
                    display_name: 'Club-like',
                    account: { configured: true },
                    state: 'off',
                    reason: '',
                    logbooks: [row(1, 'Main'), row(2, 'Second', { logbook_callsign: '' })],
                },
            ],
        });
        await bindingsState.load();
        bindingsState.setRow('club', 1, true);
        bindingsState.setField('club', 1, 'email', 'a@example.org');
        expect(bindingsState.validate()).toEqual({});
        expect(bindingsState.buildRequest()).toEqual({
            destinations: [
                {
                    type: 'club',
                    logbooks: [
                        { logbook_id: 1, enabled: true, credentials: { email: 'a@example.org' } },
                    ],
                },
            ],
        });
        // A logbook with no callsign has nothing to default from: required.
        bindingsState.setRow('club', 2, true);
        bindingsState.setField('club', 2, 'email', 'b@example.org');
        expect(bindingsState.validate()).toEqual({ [rowKey('club', 2)]: ['callsign'] });
    });

    it('B9: a count refresh keeps the operator’s drafts', async () => {
        await bindingsState.load();
        bindingsState.setField('qrz', 2, 'api_key', 'TYPED');
        getView = () => ({
            ...view(),
            destinations: [
                {
                    ...view().destinations[0],
                    logbooks: [
                        row(1, 'Main', {
                            bound: true,
                            enabled: true,
                            forwarder_name: 'qrz',
                            credentials_set: ['api_key'],
                            queue: { waiting: 0, failed: 0, in_flight: 0 },
                        }),
                        row(2, 'Second'),
                    ],
                },
                view().destinations[1],
            ],
        });
        expect(await bindingsState.refresh()).toBe(true);
        expect(bindingsState.drafts[rowKey('qrz', 2)].credentials.api_key).toBe('TYPED');
    });

    it('B11: saving holds for the whole PUT, and a second save meanwhile sends nothing', async () => {
        // The Settings leave guard refuses to leave while this flag is up
        // (unsaved.ts): it must cover every moment the PUT is on the wire.
        await bindingsState.load();
        let release!: () => void;
        const gate = new Promise<void>((r) => (release = r));
        putAnswer = async () => {
            await gate;
            return json(view());
        };
        vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        bindingsState.setRow('qrz', 1, false);
        const first = bindingsState.save();
        await vi.waitFor(() => expect(puts()).toHaveLength(1));
        expect(bindingsState.saving).toBe(true);
        await bindingsState.save();
        expect(puts()).toHaveLength(1);
        release();
        await first;
        expect(bindingsState.saving).toBe(false);
    });

    it('B13: binding drafts cannot change while their save is pending', async () => {
        await bindingsState.load();
        let release!: () => void;
        const gate = new Promise<void>((r) => (release = r));
        putAnswer = async () => {
            await gate;
            return json(view());
        };
        vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        bindingsState.setRow('qrz', 1, false);
        const saving = bindingsState.save();
        await vi.waitFor(() => expect(bindingsState.saving).toBe(true));

        // These controls remain mounted while the PUT is in flight. Their
        // state boundary must refuse a late event rather than accept work that
        // the successful response will then erase.
        bindingsState.setRow('qrz', 2, true);
        bindingsState.setField('qrz', 2, 'api_key', 'typed-after-dispatch');
        bindingsState.clear('qrz', 1, 'api_key');
        expect(bindingsState.drafts[rowKey('qrz', 2)].enabled).toBe(false);
        expect(bindingsState.drafts[rowKey('qrz', 2)].credentials.api_key).toBeUndefined();
        expect(bindingsState.drafts[rowKey('qrz', 1)].cleared).toEqual([]);

        release();
        await saving;
    });

    it('B14: a late queue refresh cannot replace a newer saved binding baseline', async () => {
        await bindingsState.load();
        let releaseRefresh!: (response: Response) => void;
        getAnswer = () => new Promise<Response>((resolve) => (releaseRefresh = resolve));

        // The queue refresh samples the old ON row and remains in flight.
        const old = view();
        const refreshing = bindingsState.refresh();

        // A binding save then commits OFF and its response becomes the current
        // baseline, including the restart warning.
        const saved = view({
            restart_required: true,
            destinations: old.destinations.map((dest, i) =>
                i === 0
                    ? {
                          ...dest,
                          state: 'off',
                          logbooks: dest.logbooks.map((r) =>
                              r.logbook_id === 1 ? { ...r, enabled: false } : r
                          ),
                      }
                    : dest
            ),
        });
        putAnswer = () => Promise.resolve(json(saved));
        vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        bindingsState.setRow('qrz', 1, false);
        await bindingsState.save();
        expect(bindingsState.dirty).toBe(false);

        // The older GET arrives last. It may refresh queue counts, but must not
        // restore ON, remove restart_required, or invent a dirty draft.
        releaseRefresh(json(old));
        await refreshing;
        const main = bindingsState.view!.destinations[0].logbooks[0];
        expect(main.enabled).toBe(false);
        expect(bindingsState.view!.restart_required).toBe(true);
        expect(bindingsState.drafts[rowKey('qrz', 1)].enabled).toBe(false);
        expect(bindingsState.dirty).toBe(false);
    });

    it('B10: a refused load says why and is not loaded', async () => {
        getView = () => ({});
        vi.stubGlobal(
            'fetch',
            vi.fn((input: string) => {
                const url = input;
                if (url === '/v1/version')
                    return Promise.resolve(json({ instance: 'i1', archive: { id: 'A1' } }));
                if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
                return Promise.resolve(
                    json({ code: 'archive_not_active', message: 'not active' }, 409)
                );
            })
        );
        await bindingsState.load();
        expect(bindingsState.loaded).toBe(false);
        expect(bindingsState.error).toBe('not active');
    });
});
