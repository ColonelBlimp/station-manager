// W-0023 weekend slice: the card's contest mode. Off, the card is unchanged;
// on, the exchange row appears and committing the callsign fills Sent #.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import { draft, clearDraft, setSerialAllocator } from './qso.svelte';
import { contest } from './contest.svelte';

describe('LoggingCard: contest mode', () => {
    beforeEach(() => {
        clearDraft();
        contest.on = false;
        contest.start = '2026-10-31';
    });
    afterEach(() => {
        clearDraft();
        contest.on = false;
        setSerialAllocator(null);
    });

    it('shows no exchange fields while contest mode is off', () => {
        render(LoggingCard);
        expect(screen.queryByTestId('contest-exchange')).toBeNull();
        expect(screen.queryByLabelText('Sent #')).toBeNull();
    });

    it('turning it on shows Sent #, Rcvd # and District; Tab out of the callsign fills Sent #', async () => {
        const alloc = vi.fn(() => Promise.resolve({ ok: true as const, serial: 7 }));
        setSerialAllocator(alloc);
        render(LoggingCard);
        await fireEvent.click(screen.getByTestId('contest-on'));
        expect(screen.getByLabelText('Sent #')).toBeInTheDocument();
        expect(screen.getByLabelText('Rcvd #')).toBeInTheDocument();
        expect(screen.getByLabelText('District')).toBeInTheDocument();

        const call = screen.getByLabelText('Callsign');
        await fireEvent.input(call, { target: { value: 'G4ABC' } });
        await fireEvent.keyDown(call, { key: 'Tab' });
        await new Promise((r) => setTimeout(r, 0));
        flushSync();
        expect(alloc).toHaveBeenCalledTimes(1);
        expect(draft.stx).toBe('7');
        expect(screen.getByLabelText('Sent #')).toHaveValue('7');
    });

    it('keeps the contest controls editable until a serial is allocated', async () => {
        setSerialAllocator(() => Promise.resolve({ ok: true as const, serial: 1 }));
        contest.start = '';
        render(LoggingCard);
        await fireEvent.click(screen.getByTestId('contest-on'));
        const call = screen.getByLabelText('Callsign');
        await fireEvent.input(call, { target: { value: 'G4ABC' } });
        await fireEvent.keyDown(call, { key: 'Tab' });
        await new Promise((r) => setTimeout(r, 0));
        flushSync();
        expect(screen.getByTestId('contest-start')).toBeEnabled(); // fix the date, then re-Tab
        await fireEvent.input(screen.getByTestId('contest-start'), {
            target: { value: '2026-10-31' },
        });
        await fireEvent.keyDown(call, { key: 'Tab' });
        await new Promise((r) => setTimeout(r, 0));
        flushSync();
        expect(draft.stx).toBe('1');
        expect(screen.getByTestId('contest-start')).toBeDisabled();
    });

    it('locks the toggle and contest ID once the QSO has its serial', async () => {
        setSerialAllocator(() => Promise.resolve({ ok: true as const, serial: 1 }));
        render(LoggingCard);
        await fireEvent.click(screen.getByTestId('contest-on'));
        expect(screen.getByTestId('contest-id')).toBeEnabled();
        expect(screen.getByTestId('contest-start')).toBeEnabled();
        const call = screen.getByLabelText('Callsign');
        await fireEvent.input(call, { target: { value: 'G4ABC' } });
        await fireEvent.keyDown(call, { key: 'Tab' });
        await new Promise((r) => setTimeout(r, 0));
        flushSync();
        expect(screen.getByTestId('contest-on')).toBeDisabled();
        expect(screen.getByTestId('contest-id')).toBeDisabled();
        expect(screen.getByTestId('contest-start')).toBeDisabled();
        clearDraft();
        flushSync();
        expect(screen.getByTestId('contest-id')).toBeEnabled();
    });
});
