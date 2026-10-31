// W-0023 weekend slice: the card's contest exchange. Contest mode and the
// contest itself are chosen in the shell header (Header.contest.svelte.test.ts);
// the card shows the exchange row only in contest mode, and committing the
// callsign fills Sent #.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import { draft, clearDraft, setSerialAllocator } from './qso.svelte';
import { contest } from './contest.svelte';
import { setContestDupeCheck } from './contestDupe.svelte';
import { rig } from './rig.svelte';

describe('LoggingCard: contest exchange', () => {
    beforeEach(() => {
        clearDraft();
        contest.on = false;
        contest.id = 'UKEIDXSSB';
        contest.start = '2026-10-31';
    });
    afterEach(() => {
        clearDraft();
        contest.on = false;
        setSerialAllocator(null);
    });

    it('shows no exchange fields while contest mode is off, and never the contest choice itself', () => {
        render(LoggingCard);
        expect(screen.queryByTestId('contest-exchange')).toBeNull();
        expect(screen.queryByLabelText('Sent #')).toBeNull();
        expect(screen.queryByTestId('contest-on')).toBeNull(); // it lives in the header
    });

    it('in contest mode shows Sent #, Rcvd # and District; Tab out of the callsign fills Sent #', async () => {
        const alloc = vi.fn(() => Promise.resolve({ ok: true as const, serial: 7 }));
        setSerialAllocator(alloc);
        contest.on = true;
        render(LoggingCard);
        flushSync();
        expect(screen.getByLabelText('Sent #')).toBeInTheDocument();
        expect(screen.getByLabelText('Rcvd #')).toBeInTheDocument();
        expect(screen.getByLabelText('District')).toBeInTheDocument();
        expect(screen.getByLabelText('Rcvd #')).toHaveAttribute('placeholder', '0');
        expect(screen.queryByTestId('contest-select')).toBeNull();

        const call = screen.getByLabelText('Callsign');
        await fireEvent.input(call, { target: { value: 'G4ABC' } });
        await fireEvent.keyDown(call, { key: 'Tab' });
        await new Promise((r) => setTimeout(r, 0));
        flushSync();
        expect(alloc).toHaveBeenCalledTimes(1);
        expect(draft.stx).toBe('7');
        expect(screen.getByLabelText('Sent #')).toHaveValue('7');
    });

    it('shows the DUPE warning for a station already worked on this band in this contest', async () => {
        setSerialAllocator(() => Promise.resolve({ ok: true as const, serial: 3 }));
        setContestDupeCheck(() => Promise.resolve(true));
        contest.on = true;
        rig.band = '20m';
        render(LoggingCard);
        const call = screen.getByLabelText('Callsign');
        await fireEvent.input(call, { target: { value: '7Q7EB' } });
        await fireEvent.keyDown(call, { key: 'Tab' });
        await new Promise((r) => setTimeout(r, 0));
        flushSync();
        const dupe = screen.getByTestId('contest-dupe');
        expect(dupe).toHaveTextContent(/^DUPE on 20m$/);
        // Under the enrichment panel, not in the exchange row.
        expect(screen.getByTestId('contest-exchange')).not.toContainElement(dupe);
        expect(screen.getByTestId('enrichment-column')).toContainElement(dupe);
        setContestDupeCheck(null);
    });

    // Operator, 2026-10-31: in a contest RST is always 59 and Sent # is
    // allocated, so the typing path is Callsign → Rcvd # → District. The other
    // fields stay clickable; outside contest mode the order is unchanged.
    function tabOrder(container: HTMLElement): string[] {
        return [...container.querySelectorAll<HTMLElement>('input, select, textarea, button')]
            .filter((el) => el.tabIndex >= 0 && !(el as HTMLInputElement).disabled)
            .map(
                (el) =>
                    el.id || el.getAttribute('aria-label') || el.textContent?.trim() || el.tagName
            );
    }

    it('in contest mode Tab goes Callsign → Rcvd # → District', () => {
        contest.on = true;
        const { container } = render(LoggingCard);
        flushSync();
        const order = tabOrder(container);
        const at = order.indexOf('lc-call');
        expect(order.slice(at, at + 3)).toEqual(['lc-call', 'lc-srx', 'lc-srx-string']);
        expect(screen.getByLabelText('RST Sent').tabIndex).toBe(-1);
        expect(screen.getByLabelText('RST Rcvd').tabIndex).toBe(-1);
    });

    it('outside contest mode Tab still goes Callsign → RST Sent → RST Rcvd', () => {
        const { container } = render(LoggingCard);
        const order = tabOrder(container);
        const at = order.indexOf('lc-call');
        expect(order.slice(at, at + 3)).toEqual(['lc-call', 'lc-rst-s', 'lc-rst-r']);
    });
});
