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
import { toastsState, dismissToast, _resetForTests as resetToasts } from '../ui/toasts.svelte';

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
        vi.useRealTimers();
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

    it('names a valid district beside the field, so a mis-hear shows', async () => {
        contest.on = true;
        render(LoggingCard);
        await fireEvent.input(screen.getByLabelText('District'), { target: { value: 'ox' } });
        flushSync();
        expect(screen.getByTestId('district-name')).toHaveTextContent('Oxford');
        // Out of the flow, so it never moves the row's other fields.
        expect(screen.getByTestId('district-name')).toHaveClass('absolute');
        await fireEvent.input(screen.getByLabelText('District'), { target: { value: 'QQ' } });
        flushSync();
        expect(screen.queryByTestId('district-name')).toBeNull();
        expect(screen.getByLabelText('District')).toHaveClass('input-error');
    });

    // Operator, 2026-10-31: a sticky toast (consistent with the app's other
    // warnings), not a box that pushes the form down.
    it('warns with a sticky toast while the rig is outside the rules, and clears it', () => {
        resetToasts();
        vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
        vi.setSystemTime(Date.UTC(2026, 9, 31, 13, 0)); // inside the event's hours
        contest.on = true;
        rig.mode = 'USB';
        rig.band = '20m';
        rig.freq = '14.100.000';
        const { unmount } = render(LoggingCard);
        flushSync();
        const warns = () => toastsState.items.filter((t) => t.level === 'warn');
        expect(screen.queryByTestId('contest-rule-warning')).toBeNull();
        expect(warns()).toHaveLength(1);
        expect(warns()[0].message).toContain('rule 9.2');
        expect(warns()[0].ttl).toBe(0); // sticky

        rig.band = '17m'; // another problem replaces it
        rig.freq = '18.150.000';
        flushSync();
        expect(warns()).toHaveLength(1);
        expect(warns()[0].message).toContain('17m');

        rig.band = '20m'; // fixed: it goes
        rig.freq = '14.200.000';
        flushSync();
        expect(warns()).toHaveLength(0);

        rig.freq = '14.100.000'; // and comes back; leaving the card removes it
        flushSync();
        expect(warns()).toHaveLength(1);
        unmount();
        expect(warns()).toHaveLength(0);
    });

    it('a dismissed warning stays dismissed until the problem changes', () => {
        resetToasts();
        vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
        vi.setSystemTime(Date.UTC(2026, 9, 31, 13, 0)); // inside the event's hours
        contest.on = true;
        rig.mode = 'USB';
        rig.band = '20m';
        rig.freq = '14.100.000';
        render(LoggingCard);
        flushSync();
        dismissToast(toastsState.items[0].id);
        rig.freq = '14.101.000'; // retuning within the same problem: no nag
        flushSync();
        expect(toastsState.items).toHaveLength(0);
        rig.band = '17m'; // a different problem: shown again
        rig.freq = '18.150.000';
        flushSync();
        expect(toastsState.items.filter((t) => t.level === 'warn')).toHaveLength(1);
    });

    it('the same warning stays the same toast as the clock ticks (no flicker)', async () => {
        resetToasts();
        vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
        vi.setSystemTime(Date.UTC(2026, 9, 31, 13, 0));
        contest.on = true;
        rig.mode = 'USB';
        rig.band = '20m';
        rig.freq = '14.100.000';
        render(LoggingCard);
        flushSync();
        const id = toastsState.items[0]?.id;
        expect(id).toBeDefined();
        await vi.advanceTimersByTimeAsync(5_000);
        flushSync();
        expect(toastsState.items.map((t) => t.id)).toEqual([id]);
    });

    it('while shown, the toast follows the frequency', () => {
        resetToasts();
        vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
        vi.setSystemTime(Date.UTC(2026, 9, 31, 13, 0)); // inside the event's hours
        contest.on = true;
        rig.mode = 'USB';
        rig.band = '20m';
        rig.freq = '14.100.000';
        render(LoggingCard);
        flushSync();
        rig.freq = '14.110.000';
        flushSync();
        const warns = toastsState.items.filter((t) => t.level === 'warn');
        expect(warns).toHaveLength(1);
        expect(warns[0].message).toContain('14.110');
    });

    // Operator, during the contest (2026-10-31): checking for a dupe needed a
    // Tab out of the callsign, which commits the contact and spends a serial.
    // The check now runs as the callsign is typed; only a commit allocates.
    it('checks for a dupe as the callsign is typed, without allocating a serial', async () => {
        vi.useFakeTimers({
            toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'],
        });
        vi.setSystemTime(Date.UTC(2026, 9, 31, 13, 0));
        const alloc = vi.fn(() => Promise.resolve({ ok: true as const, serial: 9 }));
        const check = vi.fn((call: string) => Promise.resolve(call === 'G4ABC'));
        setSerialAllocator(alloc);
        setContestDupeCheck(check);
        contest.on = true;
        rig.band = '20m';
        render(LoggingCard);
        const call = screen.getByLabelText('Callsign');
        await fireEvent.input(call, { target: { value: 'G4' } }); // not yet a valid call
        await vi.advanceTimersByTimeAsync(500);
        expect(check).not.toHaveBeenCalled();
        await fireEvent.input(call, { target: { value: 'G4AB' } });
        await vi.advanceTimersByTimeAsync(100);
        await fireEvent.input(call, { target: { value: 'G4ABC' } }); // still typing: one check, not two
        await vi.advanceTimersByTimeAsync(500);
        flushSync();
        expect(check).toHaveBeenCalledTimes(1);
        expect(check).toHaveBeenCalledWith('G4ABC', '20m', 'UKEIDXSSB', '20261031');
        expect(screen.getByTestId('contest-dupe')).toHaveTextContent('DUPE on 20m');
        expect(alloc).not.toHaveBeenCalled();
        expect(draft.stx).toBe('');
        setContestDupeCheck(null);
    });
});
