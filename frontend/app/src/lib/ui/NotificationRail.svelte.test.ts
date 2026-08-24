// The notification rail is the durable counterpart to the transient toasts
// (W-0001 / ADR 0076): a global slide-over that reads GET /v1/notifications so a
// failure survives its toast expiry and a page reload.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import NotificationRail from './NotificationRail.svelte';
import { ui, closeNotifications } from './state.svelte';
import { toastsState, _resetForTests as resetToasts } from './toasts.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));
const settle = async () => {
    await flush();
    await flush();
    flushSync();
};

const urlOf = (input: RequestInfo | URL): string =>
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

const browserEvent = {
    id: 1,
    category: 'notification',
    kind: 'export.adif_failed',
    severity: 'error',
    occurred_at: '2026-08-24T06:00:00Z',
    build: 'v-test',
    detail: { count: 3, outcome: 'server' },
};
const daemonEvent = {
    id: 2,
    category: 'notification',
    kind: 'forward.failed',
    severity: 'warn',
    occurred_at: '2026-08-24T06:05:00Z',
    build: 'v-test',
    detail: { qso_id: 7, forwarder: 'qrz', action: 'insert', attempts: 2 },
};

// stubNotifications routes GET /v1/notifications to `items` and counts the calls.
function stubNotifications(items: unknown[]): { calls: () => number } {
    let calls = 0;
    vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL) => {
            if (urlOf(input).startsWith('/v1/notifications')) {
                calls++;
                return Promise.resolve(
                    new Response(JSON.stringify({ items }), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            }
            return Promise.reject(new Error(`unexpected fetch: ${urlOf(input)}`));
        })
    );
    return { calls: () => calls };
}

beforeEach(() => {
    resetToasts();
    closeNotifications();
});

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    closeNotifications();
});

describe('NotificationRail', () => {
    it('renders both a browser- and a daemon-originated event, newest first', async () => {
        stubNotifications([daemonEvent, browserEvent]);
        ui.notificationsOpen = true;
        render(NotificationRail);
        await settle();

        expect(screen.getByText('Upload failed')).toBeInTheDocument();
        expect(screen.getByText('ADIF export failed')).toBeInTheDocument();
        expect(screen.getByText('qrz · insert · 2 attempts')).toBeInTheDocument();
        expect(screen.getByText('3 QSOs · server')).toBeInTheDocument();
    });

    it('shows the empty state when there is no history', async () => {
        stubNotifications([]);
        ui.notificationsOpen = true;
        render(NotificationRail);
        await settle();
        expect(screen.getByText(/No notifications yet/)).toBeInTheDocument();
    });

    it('shows an error with Retry, then recovers on retry', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new Error('connection refused')))
        );
        ui.notificationsOpen = true;
        render(NotificationRail);
        await settle();

        const retry = screen.getByRole('button', { name: /Retry/ });
        expect(retry).toBeInTheDocument();

        // Now the daemon is reachable; retry loads the history.
        stubNotifications([browserEvent]);
        await fireEvent.click(retry);
        await settle();
        expect(screen.getByText('ADIF export failed')).toBeInTheDocument();
    });

    it('degrades unknown/malformed detail to "Details unavailable" (never raw)', async () => {
        const weird = { ...browserEvent, id: 9, detail: { surprise: 'x</script>' } };
        stubNotifications([weird]);
        ui.notificationsOpen = true;
        render(NotificationRail);
        await settle();

        expect(screen.getByText('Details unavailable')).toBeInTheDocument();
        expect(screen.queryByText(/surprise/)).toBeNull();
    });

    it('closes on Escape, backdrop, and the close button — and only the rail', async () => {
        stubNotifications([browserEvent]);

        for (const shut of ['escape', 'backdrop', 'button'] as const) {
            ui.notificationsOpen = true;
            const view = render(NotificationRail);
            await settle();

            if (shut === 'escape') {
                await fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
            } else if (shut === 'backdrop') {
                await fireEvent.click(
                    screen.getByRole('button', { name: /Close notification history/ })
                );
            } else {
                await fireEvent.click(screen.getByRole('button', { name: /^Close$/ }));
            }
            flushSync();
            expect(ui.notificationsOpen).toBe(false);
            view.unmount();
        }
    });

    // Proof 1: reopening the SAME mounted component re-fetches (a second GET).
    it('re-fetches on every open (close then reopen → a second GET)', async () => {
        const h = stubNotifications([browserEvent]);
        ui.notificationsOpen = true;
        render(NotificationRail);
        await settle();
        expect(h.calls()).toBe(1);

        closeNotifications();
        flushSync();
        ui.notificationsOpen = true;
        await settle();
        expect(h.calls()).toBe(2);
    });

    // Proof 2: a FRESH mount (models a page reload) fetches the durable history
    // and renders it with NO toast in state — so retrievability does not depend
    // on the transient toast (the nearest-confusable "toast shown then forgotten").
    it('a fresh mount fetches and renders durable events with no toast present', async () => {
        resetToasts();
        expect(toastsState.items).toHaveLength(0);

        const h = stubNotifications([daemonEvent, browserEvent]);
        ui.notificationsOpen = true;
        render(NotificationRail); // fresh component instance = fresh local state
        await settle();

        expect(h.calls()).toBe(1);
        expect(screen.getByText('Upload failed')).toBeInTheDocument();
        expect(screen.getByText('ADIF export failed')).toBeInTheDocument();
        // The content came from the durable GET, not from any toast.
        expect(toastsState.items).toHaveLength(0);
    });
});
