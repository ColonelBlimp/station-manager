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

    async function wantGets(ms: number, more: number): Promise<void> {
        const before = gets;
        await vi.advanceTimersByTimeAsync(ms);
        expect(gets).toBe(before + more);
    }

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
