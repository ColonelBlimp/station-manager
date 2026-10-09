import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import DestinationsSection from './DestinationsSection.svelte';
import { bindingsState, _resetBindingsForTests } from './bindings.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    W-0021 5F.3 commit 4b3: the adoption status on Home's default SM Cloud row
    (ADR 0090 T4; rulings B1 and B2, 2026-10-09).

      R1  the daemon's message is shown under the row as served: the muted row
          text for adopted, adopted after a restart, checking and confirming;
          the warning text for every other state; nothing without a status.
      R2  while the row shows checking or confirming, the status is re-read
          every 5 s; once it changes the re-reads stop.
      R3  the attempt finishes while the operator has unsaved edits: the
          status updates and the typed value and switch stay as they were.
      R4  closing the tab stops the re-reads.
      R5  no re-reads while nothing is in progress.
      R6  a re-read still on the wire when the tab closes is ignored.
    Ruling B3 (a correction to B1, after the Codex review of 53a4c45e): a
    status the daemon retries on its own is re-read every 60 s.
      R7  a lost response: "uncertain" is re-read at 60 s until a later retry
          confirms it; then the re-reads stop.
      R8  an account replacement: "needs confirmation" likewise.
      R9  the cadence follows the state: 5 s while checking, 60 s while
          unreachable, none once adopted.
      R10 a failed re-read keeps the status and its cadence.
      R11 no timer for the terminal states or a final one.
*/

const TYPES = {
    types: [
        {
            type: 'smcloud',
            display_name: 'SM Cloud backup',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'Service URL', kind: 'text', scope: 'station' },
                {
                    key: 'logbook',
                    label: 'Cloud logbook name',
                    kind: 'text',
                    clearable: true,
                    scope: 'logbook',
                },
            ],
        },
    ],
};

const CHECKING = {
    state: 'checking',
    message: 'Checking whether the legacy cloud logbook can be adopted.',
};
const CONFIRMING = {
    state: 'confirming',
    message: 'Confirming the adoption for the current station account.',
};
const ADOPTED = { state: 'adopted_restart_required', message: 'Adopted; applies after a restart.' };
const DONE = { state: 'adopted', message: 'Adopted.' };
const REFUSED = { state: 'unauthorized', message: 'Not adopted: the token was refused.' };
const NEEDS = {
    state: 'needs_confirmation',
    message: 'Adoption needs confirmation for the current station account.',
};
const UNREACHABLE = {
    state: 'unreachable',
    message: 'Not yet: the server could not be reached (retrying).',
};
const UNCERTAIN = {
    state: 'uncertain',
    message: 'Adoption outcome uncertain: no confirmation was received from the server. Retrying.',
};

function view(adoption: unknown) {
    return {
        archive_id: 'A1',
        archive_label: 'Home',
        restart_required: false,
        destinations: [
            {
                type: 'smcloud',
                display_name: 'SM Cloud backup',
                account: { configured: true },
                state: 'mixed',
                reason: '',
                new_logbook_reason: '',
                logbooks: [
                    {
                        logbook_id: 1,
                        logbook_uuid: 'u1',
                        logbook_name: 'Default',
                        logbook_callsign: 'M0ABC',
                        bound: true,
                        enabled: true,
                        forwarder_name: 'smcloud.u1',
                        credentials_set: ['logbook'],
                        queue: { waiting: 0, failed: 0, in_flight: 0 },
                        locked_fields: ['logbook'],
                        adoption,
                    },
                    {
                        logbook_id: 2,
                        logbook_uuid: 'u2',
                        logbook_name: 'Portable',
                        logbook_callsign: 'M0ABC',
                        bound: false,
                        enabled: false,
                        forwarder_name: '',
                        credentials_set: [],
                        queue: { waiting: 0, failed: 0, in_flight: 0 },
                    },
                ],
            },
        ],
    };
}

function json(body: unknown): Response {
    return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
    });
}

let served: unknown;
let gets = 0;
let holdNext = false;
let failNext = false;
let held: ((r: Response) => void) | null = null;

beforeEach(() => {
    served = view(CHECKING);
    gets = 0;
    holdNext = false;
    failNext = false;
    held = null;
    // Only the interval is faked: testing-library's own waits use timeouts.
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    vi.stubGlobal(
        'fetch',
        vi.fn((url: string) => {
            if (url === '/v1/version') {
                return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
            }
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url === '/v1/qso-archives/A1/bindings') {
                gets++;
                if (failNext) {
                    failNext = false;
                    return Promise.resolve(new Response('{}', { status: 500 }));
                }
                if (holdNext) {
                    holdNext = false;
                    return new Promise<Response>((resolve) => (held = resolve));
                }
                return Promise.resolve(json(served));
            }
            return Promise.resolve(new Response('{}', { status: 404 }));
        })
    );
});

afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    _resetBindingsForTests();
    resetToasts();
});

// testing-library's wait runs on real time; vi.waitFor would advance the
// faked interval while it waits.
async function shown(text: string): Promise<HTMLElement> {
    await screen.findByText(text);
    const el = screen.getByTestId('row-adoption');
    expect(el.textContent?.trim()).toBe(text);
    return el;
}

async function wantGets(ms: number, more: number): Promise<void> {
    const before = gets;
    await vi.advanceTimersByTimeAsync(ms);
    expect(gets).toBe(before + more);
}

describe('DestinationsSection — the adoption status (4b3)', () => {
    it('R1: the message as served, muted while fine or in progress, warning otherwise', async () => {
        for (const [adoption, warning] of [
            [CHECKING, false],
            [CONFIRMING, false],
            [ADOPTED, false],
            [DONE, false],
            [REFUSED, true],
            [UNCERTAIN, true],
        ] as const) {
            served = view(adoption);
            const { unmount } = render(DestinationsSection);
            const el = await shown(adoption.message);
            expect(el.classList.contains('text-warning')).toBe(warning);
            expect(el.classList.contains('text-muted')).toBe(!warning);
            // No alert container: the sentence carries the meaning.
            expect(el.getAttribute('role')).toBeNull();
            unmount();
            _resetBindingsForTests();
        }
        served = view(null);
        render(DestinationsSection);
        await screen.findByText('Default');
        expect(screen.queryByTestId('row-adoption')).toBeNull();
    });

    it('R2: re-read every 5 s while in progress; the re-reads stop once it changes', async () => {
        render(DestinationsSection);
        await shown(CHECKING.message);
        const sent = gets;
        await vi.advanceTimersByTimeAsync(4999);
        expect(gets).toBe(sent);
        await vi.advanceTimersByTimeAsync(1);
        expect(gets).toBe(sent + 1);
        served = view(ADOPTED);
        await vi.advanceTimersByTimeAsync(5000);
        await shown(ADOPTED.message);
        const after = gets;
        await vi.advanceTimersByTimeAsync(20000);
        expect(gets).toBe(after);
    });

    it('R3: the attempt finishes during unsaved edits; the edits stay', async () => {
        render(DestinationsSection);
        await shown(CHECKING.message);
        const toggle = screen.getByRole('checkbox', { name: 'SM Cloud backup for Portable' });
        await fireEvent.click(toggle);
        const field = screen.getByLabelText('Cloud logbook name');
        await fireEvent.input(field, { target: { value: 'portable' } });
        expect(field).toHaveValue('portable');

        served = view(ADOPTED);
        await vi.advanceTimersByTimeAsync(5000);
        await shown(ADOPTED.message);

        expect(screen.getByLabelText('Cloud logbook name')).toHaveValue('portable');
        expect(
            screen.getByRole('checkbox', { name: 'SM Cloud backup for Portable' })
        ).toBeChecked();
    });

    it('R4: closing the tab stops the re-reads', async () => {
        const { unmount } = render(DestinationsSection);
        await shown(CHECKING.message);
        unmount();
        const sent = gets;
        await vi.advanceTimersByTimeAsync(20000);
        expect(gets).toBe(sent);
    });

    it('R5: no re-reads while nothing is in progress', async () => {
        served = view(REFUSED);
        render(DestinationsSection);
        await shown(REFUSED.message);
        const sent = gets;
        await vi.advanceTimersByTimeAsync(20000);
        expect(gets).toBe(sent);
    });

    it('R6: a re-read still on the wire when the tab closes is ignored', async () => {
        const { unmount } = render(DestinationsSection);
        await shown(CHECKING.message);
        holdNext = true;
        await vi.advanceTimersByTimeAsync(5000);
        expect(held).not.toBeNull();
        unmount();
        held!(json(view(ADOPTED)));
        // Let the answer and its handling run (setTimeout is real here).
        await new Promise((r) => setTimeout(r, 20));
        expect(bindingsState.view!.destinations[0].logbooks[0].adoption).toEqual(CHECKING);
    });

    it('R7: a lost response, re-read at 60 s until a retry confirms it', async () => {
        served = view(UNCERTAIN);
        render(DestinationsSection);
        await shown(UNCERTAIN.message);
        await wantGets(59_999, 0);
        await wantGets(1, 1);
        served = view(ADOPTED); // a later hourly retry confirmed it
        await wantGets(60_000, 1);
        await shown(ADOPTED.message);
        await wantGets(180_000, 0);
    });

    it('R8: an account replacement, re-read at 60 s until confirmed', async () => {
        served = view(NEEDS);
        render(DestinationsSection);
        await shown(NEEDS.message);
        await wantGets(60_000, 1);
        served = view(ADOPTED);
        await wantGets(60_000, 1);
        await shown(ADOPTED.message);
        await wantGets(180_000, 0);
    });

    it('R9: the cadence follows the state', async () => {
        render(DestinationsSection);
        await shown(CHECKING.message);
        served = view(UNREACHABLE);
        await wantGets(5_000, 1);
        await shown(UNREACHABLE.message);
        await wantGets(55_000, 0);
        await wantGets(5_000, 1);
        served = view(CHECKING);
        await wantGets(60_000, 1);
        await shown(CHECKING.message);
        await wantGets(5_000, 1);
        served = view(DONE);
        await wantGets(5_000, 1);
        await shown(DONE.message);
        await wantGets(180_000, 0);
    });

    it('R10: a failed re-read keeps the status and its cadence', async () => {
        served = view(UNREACHABLE);
        render(DestinationsSection);
        await shown(UNREACHABLE.message);
        failNext = true;
        await wantGets(60_000, 1);
        await shown(UNREACHABLE.message);
        await wantGets(59_999, 0);
        await wantGets(1, 1);
    });

    it('R11: no timer for the terminal states or a final one', async () => {
        for (const state of [
            'unauthorized',
            'unreadable',
            'refused',
            'conflict',
            'unsupported',
            'adopted',
        ]) {
            served = view({ state, message: `Status ${state}.` });
            const { unmount } = render(DestinationsSection);
            await shown(`Status ${state}.`);
            await wantGets(180_000, 0);
            unmount();
            _resetBindingsForTests();
        }
    });
});

/*
    W-0021 5F.3 commit 5b, ruling C2 (2026-10-09): a binding the daemon
    started held shows the daemon's uploads_held line.

      U1  the line as served, in the warning text, in a plain paragraph, on
          every held row (the default or not), apart from the adoption line.
      U2  the sequence: waiting for confirmation, re-read at 60 s; confirmed,
          the line says a restart is required and the re-reads stop; after
          the restart, no line.
      U3  disabled while held: its line, and no re-reads by itself.
      U4  a held row with no adoption status of its own is re-read at 60 s.
      U5  a held row's Retry failed is disabled whatever the held line says
          (a confirmation alone creates no worker), with failed uploads
          present; Clear stays available; an unheld row's Retry is not.
*/

const WAITING = {
    state: 'waiting_for_confirmation',
    message: 'Uploads are held until adoption is confirmed for the current station account.',
};
const RESTART = { state: 'restart_required', message: 'Uploads resume after a restart.' };
const DISABLED = {
    state: 'disabled',
    message:
        'Uploads are held; this binding is off, so its queued uploads are discarded at the next restart.',
};

/** view(adoption) with uploads_held on the default row and, when given, on a
 *  bound Portable row. */
function heldView(adoption: unknown, held: unknown, portableHeld?: unknown) {
    const v = view(adoption);
    const rows = v.destinations[0].logbooks as Record<string, unknown>[];
    rows[0].uploads_held = held;
    if (portableHeld !== undefined) {
        Object.assign(rows[1], {
            bound: true,
            enabled: true,
            forwarder_name: 'smcloud.u2',
            uploads_held: portableHeld,
        });
    }
    return v;
}

async function heldShown(...texts: string[]): Promise<HTMLElement[]> {
    await screen.findAllByText(texts[0]);
    const els = screen.getAllByTestId('row-uploads-held');
    expect(els.map((e) => e.textContent?.trim())).toEqual(texts);
    return els;
}

describe('DestinationsSection — uploads_held (5b)', () => {
    it('U1: the line as served, warning text, on every held row', async () => {
        served = heldView(NEEDS, WAITING, WAITING);
        render(DestinationsSection);
        const els = await heldShown(WAITING.message, WAITING.message);
        for (const el of els) {
            expect(el.classList.contains('text-warning')).toBe(true);
            expect(el.tagName).toBe('P');
            expect(el.getAttribute('role')).toBeNull();
        }
        // The adoption line stays its own.
        expect(screen.getByTestId('row-adoption').textContent?.trim()).toBe(NEEDS.message);
    });

    it('U2: waiting, confirmed, restarted', async () => {
        served = heldView(NEEDS, WAITING);
        const { unmount } = render(DestinationsSection);
        await heldShown(WAITING.message);
        await wantGets(59_999, 0);
        served = heldView(ADOPTED, RESTART); // confirmed during the run
        await wantGets(1, 1);
        await heldShown(RESTART.message);
        await shown(ADOPTED.message);
        await wantGets(180_000, 0);

        // The restart reloads the page.
        unmount();
        _resetBindingsForTests();
        served = heldView(DONE, undefined);
        render(DestinationsSection);
        await shown(DONE.message);
        expect(screen.queryByTestId('row-uploads-held')).toBeNull();
        await wantGets(180_000, 0);
    });

    it('U3: disabled while held: its line, no re-reads', async () => {
        served = heldView(ADOPTED, DISABLED);
        render(DestinationsSection);
        await heldShown(DISABLED.message);
        await wantGets(180_000, 0);
    });

    it('U4: a held row without an adoption status is re-read at 60 s', async () => {
        served = heldView(DONE, undefined, WAITING);
        render(DestinationsSection);
        await heldShown(WAITING.message);
        await wantGets(59_999, 0);
        served = heldView(DONE, undefined, RESTART);
        await wantGets(1, 1);
        await heldShown(RESTART.message);
        await wantGets(180_000, 0);
    });
});

describe('DestinationsSection — Retry while held (5b, operator review)', () => {
    it('U5: a held row cannot retry; it can clear; an unheld row can retry', async () => {
        for (const line of [WAITING, RESTART, DISABLED]) {
            const v = heldView(NEEDS, line, null);
            for (const row of v.destinations[0].logbooks as Record<string, unknown>[]) {
                row.queue = { waiting: 1, failed: 2, in_flight: 0 };
            }
            served = v;
            const { unmount } = render(DestinationsSection);
            await heldShown(line.message);
            const retry = (logbook: string) =>
                screen.getByRole('button', {
                    name: `Retry failed uploads for SM Cloud backup for ${logbook}`,
                });
            const clear = (logbook: string) =>
                screen.getByRole('button', {
                    name: `Clear the queue for SM Cloud backup for ${logbook}`,
                });
            expect(retry('Default'), line.state).toBeDisabled();
            expect(clear('Default'), line.state).toBeEnabled();
            expect(retry('Portable'), line.state).toBeEnabled();
            expect(clear('Portable'), line.state).toBeEnabled();
            unmount();
            _resetBindingsForTests();
        }
    });
});
