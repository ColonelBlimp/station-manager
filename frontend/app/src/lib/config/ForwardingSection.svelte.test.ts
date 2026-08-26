import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import ForwardingSection from './ForwardingSection.svelte';
import { forwardingState } from './forwarding.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    FORWARDING SECTION — WHAT THE OPERATOR SEES.

    forwarding.svelte.test.ts pins what goes ON THE WIRE. These rules pin the
    half of the criterion that is only observable in the browser:

        …for a field the daemon marks clearable, I can also reset it to its
        default and see which default I got.

    The load-bearing one is U2. "Reset" must not be offered for a credential the
    daemon will refuse to clear — a control that silently does nothing is worse
    than no control, because the operator concludes the value WAS cleared. The
    state module refuses such a reset anyway (F5b/F5c), so without U2 the UI
    could sprout the button on every field and every wire rule would stay green.
*/

afterEach(() => {
    vi.restoreAllMocks();
    resetToasts();
    forwardingState.drafts = [];
    forwardingState.types = [];
    forwardingState.loaded = false;
    forwardingState.loading = false;
    forwardingState.saving = false;
    forwardingState.error = '';
});

const TYPES = {
    types: [
        {
            type: 'qrz',
            display_name: 'QRZ.com',
            supported_actions: ['insert'],
            credential_fields: [{ key: 'api_key', label: 'API key', kind: 'password' }],
        },
        {
            type: 'smcloud',
            display_name: 'SM Cloud',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'URL', kind: 'text' },
                {
                    key: 'logbook',
                    label: 'Cloud logbook',
                    kind: 'text',
                    clearable: true,
                    help: 'Defaults to "main".',
                },
            ],
        },
    ],
};

const CONFIG = {
    forwarders: [
        { name: 'qrz', type: 'qrz', enabled: true, credentials_set: ['api_key'] },
        { name: 'smcloud', type: 'smcloud', enabled: true, credentials_set: ['url', 'logbook'] },
        { name: 'mystery', type: 'mystery', enabled: false, credentials_set: ['token'] },
    ],
};

function mockDaemon() {
    vi.stubGlobal(
        'fetch',
        vi.fn((url: RequestInfo | URL) => {
            const u = url instanceof URL ? url.href : typeof url === 'string' ? url : url.url;
            return Promise.resolve(
                new Response(JSON.stringify(u.includes('forwarder-types') ? TYPES : CONFIG), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
}

async function renderLoaded() {
    mockDaemon();
    render(ForwardingSection);
    await vi.waitFor(() => expect(forwardingState.loaded).toBe(true));
}

// A daemon mock that also answers the W-0005 queue endpoints. The queue GET
// reports `initialQueues`; a SUCCESSFUL clear drops that forwarder's CLEARABLE to
// zero on the next GET — in_flight is PRESERVED, because a clear must never touch
// the batch a worker is mid-upload on. `clearResult` sets the POST outcome. With
// `deferRefresh`, the post-clear GET hangs until `releaseRefresh()` is called, so
// a test can observe the state while the refetch is in flight.
function mockForwardingWithQueues(
    initialQueues: Array<{ name: string; clearable: number; in_flight: number }>,
    opts: { clearResult?: { status: number; body: unknown }; deferRefresh?: boolean } = {}
) {
    const clearResult = opts.clearResult ?? { status: 200, body: { discarded: 0 } };
    const cleared = new Set<string>();
    const calls = { clear: [] as string[] };
    let queueGets = 0;
    let releaseRefresh: (() => void) | null = null;
    const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), {
            status,
            headers: { 'Content-Type': 'application/json' },
        });
    const queueBody = () => ({
        // Clear zeroes ONLY clearable; in_flight is preserved.
        forwarders: initialQueues.map((q) => (cleared.has(q.name) ? { ...q, clearable: 0 } : q)),
    });
    vi.stubGlobal(
        'fetch',
        vi.fn((url: RequestInfo | URL) => {
            const u = url instanceof URL ? url.href : typeof url === 'string' ? url : url.url;
            if (u.includes('forwarder-types')) return Promise.resolve(json(TYPES));
            if (u.includes('/queue/clear')) {
                const name = decodeURIComponent(
                    u.split('/v1/forwarder/')[1].split('/queue/clear')[0]
                );
                calls.clear.push(name);
                if (clearResult.status >= 200 && clearResult.status < 300) cleared.add(name);
                return Promise.resolve(json(clearResult.body, clearResult.status));
            }
            if (u.includes('forwarder-queues')) {
                queueGets++;
                if (opts.deferRefresh && queueGets > 1) {
                    return new Promise<Response>((resolve) => {
                        releaseRefresh = () => resolve(json(queueBody()));
                    });
                }
                return Promise.resolve(json(queueBody()));
            }
            return Promise.resolve(json(CONFIG));
        })
    );
    return { calls, releaseRefresh: () => releaseRefresh?.() };
}

describe('ForwardingSection', () => {
    // U1 — EVERY DESTINATION IS LISTED, including one this build cannot edit.
    it('U1: lists all destinations and names the unsupported one', async () => {
        await renderLoaded();
        expect(screen.getByText('QRZ.com')).toBeTruthy();
        expect(screen.getByText('SM Cloud')).toBeTruthy();
        // No descriptor, so the heading falls back to the raw type — which for
        // this entry equals its name, so "mystery" legitimately appears twice
        // (heading + the mono name). Asserting a single match would be
        // asserting a coincidence of the fixture, not the behaviour.
        expect(screen.getAllByText('mystery').length).toBeGreaterThanOrEqual(1);
        expect(screen.getByText(/can't be edited here/)).toBeTruthy();
        // The real rule: it is present AND flagged as uneditable, so the
        // operator is not left wondering why it has no fields.
        expect(forwardingState.drafts.map((d) => d.name)).toContain('mystery');
    });

    // U2 — RESET IS OFFERED ONLY WHERE THE DAEMON WILL HONOUR IT. Exactly one
    // clearable field exists in this fixture, so exactly one control may appear.
    it('U2: the reset control appears only for a clearable field', async () => {
        await renderLoaded();
        const resets = screen.queryAllByRole('button', { name: /reset to default/i });
        expect(resets).toHaveLength(1);

        // …and it belongs to the clearable field, not merely to the same card.
        const label = screen.getByText('Cloud logbook').closest('label');
        expect(label?.textContent).toContain('Reset to default');
        expect(screen.getByText('URL').closest('label')?.textContent).not.toContain(
            'Reset to default'
        );
    });

    // U3 — A PENDING RESET IS VISIBLE BEFORE SAVING, and reversible. Otherwise
    // "cleared" and "left blank" look identical right up until the save lands,
    // which is the confusion this whole feature exists to remove.
    it('U3: clicking reset shows a pending state that can be undone', async () => {
        await renderLoaded();
        await fireEvent.click(screen.getByRole('button', { name: /reset to default/i }));

        expect(screen.getByText(/will reset to the default on save/i)).toBeTruthy();
        const smcloud = forwardingState.drafts.find((d) => d.name === 'smcloud');
        expect(smcloud?.cleared).toContain('logbook');

        await fireEvent.click(screen.getByRole('button', { name: /undo/i }));
        expect(screen.queryByText(/will reset to the default on save/i)).toBeNull();
        expect(forwardingState.drafts.find((d) => d.name === 'smcloud')?.cleared).not.toContain(
            'logbook'
        );
    });

    // U4 — A SET CREDENTIAL SAYS SO WITHOUT REVEALING ANYTHING. The placeholder
    // is the ONLY signal the daemon gives, and it must say "keep", because a
    // blank box is what preserves the stored value.
    it('U4: a stored credential is advertised as set, with keep-on-blank stated', async () => {
        await renderLoaded();
        const apiKey = screen.getByText('API key').closest('label');
        const input = apiKey?.querySelector('input');
        expect(input?.getAttribute('placeholder')).toMatch(/set — leave blank to keep/);
        // Masked, and never pre-filled with anything resembling the secret.
        expect(input?.getAttribute('type')).toBe('password');
        expect(input?.getAttribute('value') ?? '').toBe('');
    });

    // U8 — THE OPERATOR'S config.json LABEL WINS OVER THE BUILT-IN NAME, and an
    // unlabelled destination still shows the built-in rather than a blank. The
    // whole point is that the built-in lives in the binary, so changing it is a
    // release the operator cannot perform.
    it('U8: a config.json label overrides the built-in display name', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn((url: RequestInfo | URL) => {
                const u = url instanceof URL ? url.href : typeof url === 'string' ? url : url.url;
                const labelled = {
                    forwarders: [
                        { name: 'smcloud', type: 'smcloud', enabled: true, label: 'Shack cloud' },
                        { name: 'qrz', type: 'qrz', enabled: true },
                    ],
                };
                return Promise.resolve(
                    new Response(JSON.stringify(u.includes('forwarder-types') ? TYPES : labelled), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );
        render(ForwardingSection);
        await vi.waitFor(() => expect(forwardingState.loaded).toBe(true));

        expect(screen.getByText('Shack cloud')).toBeTruthy();
        // The built-in it replaced must be gone, or the label is merely extra.
        expect(screen.queryByText('SM Cloud')).toBeNull();
        // …while the unlabelled one still shows its built-in name.
        expect(screen.getByText('QRZ.com')).toBeTruthy();
    });

    // U6 — DESTINATIONS COLLAPSE, AND THE SUMMARY STILL ANSWERS THE SCAN. The
    // list is fixed and grows with every new online service, so the page must
    // not grow with it. Collapsing is only safe if the closed row still says
    // which service it is and whether it is on.
    it('U6: each destination is a collapsed disclosure showing name and state', async () => {
        await renderLoaded();
        const cards = document.querySelectorAll('details');
        expect(cards).toHaveLength(3);
        for (const c of cards) expect(c.open).toBe(false);

        const qrzSummary = screen.getByText('QRZ.com').closest('summary');
        // The summary carries the SERVICE name and its state, and deliberately
        // NOT the raw `name` key: with one entry per type it always equals the
        // type, so it added a second rendering of the same word.
        expect(qrzSummary?.textContent).not.toMatch(/\bqrz\b/);
        // The pill's text is lower-case and CSS upper-cases it, matching the
        // active-rig badge — so assert the DOM text, not the rendered casing.
        // Both states are asserted: a pill that read the same either way would
        // tell the operator nothing while still containing the word.
        expect(qrzSummary?.textContent).toContain('enabled');
        const mysterySummary = screen
            .getByText('mystery', { selector: 'span.font-semibold' })
            .closest('summary');
        expect(mysterySummary?.textContent).toContain('disabled');
        expect(
            screen.getByText('mystery', { selector: 'span.font-semibold' }).closest('summary')
                ?.textContent
        ).toContain('unsupported');
    });

    // U7 — AN EDITED CARD IS MARKED AND CANNOT BE COLLAPSED. This is the state
    // collapsing CREATES: an edit the operator cannot see, while the footer
    // reports only that something somewhere is unsaved. The marker names which
    // card; refusing to close means it cannot be hidden again by accident.
    it('U7: an edited destination is starred and refuses to collapse', async () => {
        await renderLoaded();
        await fireEvent.click(screen.getByRole('button', { name: /reset to default/i }));

        const smcloudSummary = screen.getByText(/SM Cloud/).closest('summary')!;
        expect(smcloudSummary.textContent).toContain('*');
        const card = smcloudSummary.parentElement as HTMLDetailsElement;
        expect(card.open).toBe(true);

        // Clicking the summary must NOT close it while edits are pending.
        await fireEvent.click(smcloudSummary);
        expect(card.open).toBe(true);

        // …and Cancel is the way out. It clears the edit, which releases the
        // forced-open — the card collapses on its own, no second click needed.
        // (jsdom really does toggle <details> on summary click, verified
        // separately, so the assertion above is not passing vacuously.)
        await fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }));
        expect(smcloudSummary.textContent).not.toContain('*');
        expect(card.open).toBe(false);

        // And normal disclosure behaviour is restored, not left disabled.
        await fireEvent.click(smcloudSummary);
        expect(card.open).toBe(true);
        await fireEvent.click(smcloudSummary);
        expect(card.open).toBe(false);
    });

    // U7b — AND AN UNEDITED CARD IS NOT STARRED, or the marker means nothing.
    it('U7b: an untouched destination carries no marker', async () => {
        await renderLoaded();
        expect(screen.getByText('QRZ.com').closest('summary')?.textContent).not.toContain('*');
    });

    // U5 — THE RESTART CAVEAT APPEARS ONLY WHEN THERE IS SOMETHING TO APPLY.
    // The worker binds destinations at startup, so a saved change that is not
    // yet live is a state the operator has to be told about.
    it('U5: the restart notice is tied to unsaved changes', async () => {
        await renderLoaded();
        expect(screen.queryByText(/apply when the daemon restarts/i)).toBeNull();

        await fireEvent.click(screen.getByRole('button', { name: /reset to default/i }));
        expect(screen.getByText(/apply when the daemon restarts/i)).toBeTruthy();
    });

    // U11 — THE LIVE QUEUE DEPTH IS SHOWN PER DESTINATION (W-0005). Distinguishes
    // the clearable backlog from the in-flight batch so "clear" is never confused
    // with "nothing is still sending."
    it('U11: shows each destination’s clearable/in-flight queue depth', async () => {
        mockForwardingWithQueues([
            { name: 'qrz', clearable: 12, in_flight: 3 },
            { name: 'smcloud', clearable: 0, in_flight: 0 },
        ]);
        render(ForwardingSection);
        await vi.waitFor(() =>
            expect(screen.getByText('12 queued · 3 in flight')).toBeInTheDocument()
        );
        expect(screen.getByText('0 queued · 0 in flight')).toBeInTheDocument();
    });

    // U12 — CLEAR QUEUE: confirm, POST, report the count, stay DISABLED through the
    // refetch (no stale-count re-enable / double clear), and PRESERVE in-flight.
    it('U12: clears after confirm, reports, stays disabled through refresh, keeps in-flight', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        const { calls, releaseRefresh } = mockForwardingWithQueues(
            [{ name: 'qrz', clearable: 5, in_flight: 2 }],
            { clearResult: { status: 200, body: { discarded: 5 } }, deferRefresh: true }
        );
        render(ForwardingSection);

        const btn = await vi.waitFor(() =>
            screen.getByRole('button', { name: /clear queue \(5\)/i })
        );
        await fireEvent.click(btn);

        // Posted once and reported.
        await vi.waitFor(() => expect(calls.clear).toEqual(['qrz']));
        await vi.waitFor(() =>
            expect(toastsState.items.some((t) => t.message.includes('Cleared 5'))).toBe(true)
        );
        // Refresh still in flight: the control is DISABLED (shows "Clearing…"), not
        // a re-enabled stale "Clear queue (5)" a second click could fire.
        expect(screen.getByRole('button', { name: /clearing/i })).toBeDisabled();
        expect(screen.queryByRole('button', { name: /clear queue \(5\)/i })).toBeNull();

        // Release the refetch: queued drops to 0, in-flight is untouched, one clear.
        releaseRefresh();
        await vi.waitFor(() =>
            expect(screen.getByText('0 queued · 2 in flight')).toBeInTheDocument()
        );
        expect(screen.getByRole('button', { name: /clear queue \(0\)/i })).toBeDisabled();
        expect(calls.clear).toEqual(['qrz']);
    });

    // U12b — CANCELLING THE CONFIRM MAKES NO REQUEST.
    it('U12b: cancelling the confirm does not clear', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(false);
        const { calls } = mockForwardingWithQueues([{ name: 'qrz', clearable: 5, in_flight: 0 }]);
        render(ForwardingSection);

        const btn = await vi.waitFor(() =>
            screen.getByRole('button', { name: /clear queue \(5\)/i })
        );
        await fireEvent.click(btn);
        expect(calls.clear).toEqual([]);
    });

    // U13 — A FAILED CLEAR SURFACES THE DAEMON'S ACTUAL MESSAGE (proving
    // daemonErrorMessage survives end-to-end), and changes nothing.
    it('U13: a failed clear surfaces the daemon error message', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        mockForwardingWithQueues([{ name: 'qrz', clearable: 3, in_flight: 0 }], {
            clearResult: {
                status: 404,
                body: { code: 'unknown_forwarder', message: 'no such forwarder' },
            },
        });
        render(ForwardingSection);

        const btn = await vi.waitFor(() =>
            screen.getByRole('button', { name: /clear queue \(3\)/i })
        );
        await fireEvent.click(btn);
        await vi.waitFor(() =>
            expect(
                toastsState.items.some(
                    (t) => t.level === 'error' && t.message === 'no such forwarder'
                )
            ).toBe(true)
        );
    });
});
