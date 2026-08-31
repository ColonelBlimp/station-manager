// The logbook email controls must surface send outcomes as toasts (the app's
// standard signalling) IN ADDITION TO the inline result line — and, critically,
// the ambiguous-network warning must STAY inline: a transient toast can't be the
// only record of "check before re-sending". Pre-fix, handleSend pushed no toasts
// at all, so every assertion here fails against the old component.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LogbookEmailControls from './LogbookEmailControls.svelte';
import { logbookState } from './logbook.svelte';
import type { LogbookQso } from '../api/logbooks';
import { toastsState, _resetForTests } from '../ui/toasts.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));

function qso(id: number, uuid: string): LogbookQso {
    return { id, uuid, call: `C${id}` };
}

// Enable the mailer, seed a recipient (the $effect copies mailerDefaultRecipient
// into the input on mount), and select one row so canSend holds.
function armOneRow(): void {
    logbookState.mailerEnabled = true;
    logbookState.mailerDefaultRecipient = 'op@example.com';
    logbookState.toggleRow(qso(1, 'u-1'));
}

function stubJson(status: number, body: unknown): void {
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

async function renderAndSend(): Promise<void> {
    render(LogbookEmailControls);
    flushSync();
    await fireEvent.click(screen.getByRole('button', { name: 'Email' }));
    await flush();
    await flush();
    flushSync();
}

beforeEach(() => {
    _resetForTests();
    logbookState.clearSelection();
    logbookState.mailerEnabled = false;
    logbookState.mailerDefaultRecipient = '';
});

afterEach(() => {
    vi.unstubAllGlobals();
    _resetForTests();
    logbookState.clearSelection();
    logbookState.mailerEnabled = false;
    logbookState.mailerDefaultRecipient = '';
});

describe('LogbookEmailControls toasts', () => {
    it('a successful send pushes an info toast and keeps the inline result', async () => {
        armOneRow();
        stubJson(200, { status: 'sent', emailed: ['u-1'], date: '20260724' });
        await renderAndSend();

        // Outcome toast present…
        expect(
            toastsState.items.some(
                (t) => t.level === 'info' && /Emailed 1 QSO to op@example\.com/.test(t.message)
            )
        ).toBe(true);
        // …and the sticky "sending" toast was superseded (dismissed on outcome).
        expect(toastsState.items.some((t) => /Emailing/.test(t.message))).toBe(false);
        // The inline result line is retained alongside the toast.
        expect(screen.getByText(/Sent 1 QSO to op@example\.com/)).toBeInTheDocument();
    });

    it('a network failure warns via toast AND keeps the verbose inline warning', async () => {
        armOneRow();
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('connection refused')))
        );
        await renderAndSend();

        expect(
            toastsState.items.some((t) => t.level === 'warn' && /unconfirmed/.test(t.message))
        ).toBe(true);
        // The persistent, verbose warning MUST remain inline — not replaced by a
        // transient toast (a re-send guard the operator must still see later).
        expect(screen.getByText(/may still have gone out/)).toBeInTheDocument();
    });

    // F-04b (ADR 0078): a FIRED timeout is the ambiguous case — SMTP may have
    // accepted before the response was lost. It gets the shared "outcome unknown"
    // lead inline (persistent, the primary record) plus a companion warn toast;
    // the generic (non-timeout) network wording above is preserved unchanged.
    it('a timed-out send shows the shared outcome-unknown lead inline and warns', async () => {
        armOneRow();
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        await renderAndSend();

        expect(toastsState.items.some((t) => t.level === 'warn')).toBe(true);
        expect(screen.getByText(/the outcome is unknown/)).toBeInTheDocument();
        expect(screen.getByText(/check before retrying/)).toBeInTheDocument();
    });

    it('an SMTP failure pushes an error toast', async () => {
        armOneRow();
        stubJson(502, { code: 'smtp_failure', message: 'relay refused' });
        await renderAndSend();

        expect(
            toastsState.items.some(
                (t) => t.level === 'error' && /check daemon logs/.test(t.message)
            )
        ).toBe(true);
    });
});
