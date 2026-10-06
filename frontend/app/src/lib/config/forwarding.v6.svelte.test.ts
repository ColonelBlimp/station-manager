import { afterEach, describe, expect, it, vi } from 'vitest';
import { forwardingState } from './forwarding.svelte';

/*
    Station accounts under config v6 (W-0021 5C, ADR 0082 parts 3 and 8; ruling
    R1 2026-10-06). GET /v1/config serves each destination's STATION ACCOUNT —
    type, label, action_filter and its station-scoped credentials_set — with no
    `name` and no `enabled`: both are binding facts, edited under Destinations.
    The daemon refuses a PUT that carries either at all, so the tab must never
    send them.

      W1  accounts load keyed by type, though the daemon sends no name.
      W2  a save identifies each account by type and sends no name or enabled.
      W3  per-account edits — the unsaved mark, a reset and its undo — key on type.
*/

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

const TYPES = {
    types: [
        {
            type: 'smcloud',
            display_name: 'SM Cloud',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'URL', kind: 'text', scope: 'station' },
                { key: 'token', label: 'Token', kind: 'password', scope: 'station' },
                { key: 'region', label: 'Region', kind: 'text', clearable: true, scope: 'station' },
                { key: 'logbook', label: 'Cloud logbook', kind: 'text', scope: 'logbook' },
            ],
        },
        {
            type: 'clublog',
            display_name: 'Club Log',
            supported_actions: ['insert'],
            credential_fields: [{ key: 'email', label: 'Email', kind: 'text', scope: 'logbook' }],
        },
    ],
};

const CONFIG = {
    forwarders: [
        {
            type: 'smcloud',
            label: 'Shack cloud',
            action_filter: ['insert'],
            credentials_set: ['token', 'url'],
        },
        { type: 'clublog', action_filter: ['insert'] },
    ],
};

function mockDaemon(): unknown[] {
    const puts: unknown[] = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
            const u = url instanceof URL ? url.href : typeof url === 'string' ? url : url.url;
            if (init?.method === 'PUT') {
                puts.push(JSON.parse(typeof init.body === 'string' ? init.body : ''));
            }
            const body = u.includes('forwarder-types') ? TYPES : CONFIG;
            return Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
    return puts;
}

describe('forwardingState — config v6 station accounts', () => {
    it('W1: accounts load keyed by type, with no name from the daemon', async () => {
        mockDaemon();
        await forwardingState.load();
        expect(forwardingState.drafts.map((d) => d.type)).toEqual(['smcloud', 'clublog']);
        expect(forwardingState.drafts[0].label).toBe('Shack cloud');
        expect(forwardingState.drafts[0].credentialsSet).toEqual(['token', 'url']);
    });

    it('W2: a save identifies accounts by type and never sends name or enabled', async () => {
        const puts = mockDaemon();
        await forwardingState.load();
        forwardingState.drafts[0].credentials.token = 'W2-NEW-TOKEN';
        expect(await forwardingState.save()).toBe(true);
        const sent = (puts[0] as { forwarders: Record<string, unknown>[] }).forwarders;
        expect(sent.map((f) => f.type)).toEqual(['smcloud', 'clublog']);
        for (const f of sent) {
            expect(Object.keys(f)).not.toContain('name');
            expect(Object.keys(f)).not.toContain('enabled');
        }
        expect(sent[0].credentials).toEqual({ token: 'W2-NEW-TOKEN' });
    });

    it('W3: the unsaved mark, a reset and its undo key on type', async () => {
        mockDaemon();
        await forwardingState.load();
        expect(forwardingState.hasEdits('smcloud')).toBe(false);
        forwardingState.clear('smcloud', 'region');
        expect(forwardingState.hasEdits('smcloud')).toBe(true);
        expect(forwardingState.drafts[0].cleared).toEqual(['region']);
        forwardingState.uncleared('smcloud', 'region');
        expect(forwardingState.drafts[0].cleared).toEqual([]);
        expect(forwardingState.hasEdits('smcloud')).toBe(false);
    });
});
