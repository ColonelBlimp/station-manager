// Continue's list as a dialog (W-0029 part 6, criteria F; the operator's
// rulings 3 of 2026-11-25 and of 2026-11-27). The list's rules are proved in
// continueList.svelte.test.ts; this proves what the operator SEES of them. The
// nearest confusable outcomes: a list still being read, or one that could not
// be read, shown as "no earlier session"; and a line that can be pressed while
// the page would send nothing, or send for a list it no longer shows.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import ContinueDialog from './ContinueDialog.svelte';
import {
    continueList,
    continueStateDrawn,
    openContinue,
    setContinuePorts,
    _resetContinueForTests,
    type ClosedListReadLike,
    type ClosedSessionLike,
} from './continueList.svelte';
import { session, _resetSessionForTests } from './session.svelte';
import { archivesState } from '../config/archives.svelte';

const HOME = '01a16a00-0000-7000-8000-000000000001';
const id = (n: number): string => `0190${String(n).padStart(4, '0')}-0000-7000-8000-000000000000`;
const closed = (n: number, over: Partial<ClosedSessionLike> = {}): ClosedSessionLike => ({
    id: id(n),
    openedAt: `2026-11-${String(n).padStart(2, '0')}T09:15:00Z`,
    latestSittingStartedAt: `2026-11-${String(n).padStart(2, '0')}T09:15:00Z`,
    latestSittingEndedAt: `2026-11-${String(n).padStart(2, '0')}T11:42:00Z`,
    anEndUnconfirmed: false,
    contactCount: 37,
    ...over,
});

let held: ClosedSessionLike[];
let scripted: Array<ClosedListReadLike | Promise<ClosedListReadLike>>;
let sent: Array<[number, string]>;

const page = (from: number, limit: number): ClosedListReadLike => ({
    kind: 'ok',
    archiveId: HOME,
    generation: 4,
    logbookUuid: 'lb',
    sessions: held.slice(from, from + limit),
    more: from + limit < held.length,
});

async function flush(): Promise<void> {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    flushSync();
}

beforeEach(async () => {
    _resetContinueForTests();
    _resetSessionForTests();
    held = [closed(20), closed(19, { latestSittingEndedAt: null, anEndUnconfirmed: true })];
    scripted = [];
    sent = [];
    setContinuePorts({
        read: (limit, after) => {
            const next = scripted.shift();
            if (next !== undefined) return Promise.resolve(next);
            const from = after ? held.findIndex((s) => s.id === after.sessionId) + 1 : 0;
            return Promise.resolve(page(from, limit));
        },
        archive: () => HOME,
        readState: () => {},
        send: (generation, sessionId) => {
            sent.push([generation, sessionId]);
            return Promise.resolve('applied');
        },
        // As main.ts wires it: no guarded request can be sent while the
        // page's archive is being checked.
        sendable: () => !archivesState.verifying && !archivesState.switchUnresolved,
    });
    continueStateDrawn({ archiveId: HOME, generation: 4, state: 'open' });
    await flush();
});

describe('ContinueDialog', () => {
    it('shows nothing while it is closed', () => {
        render(ContinueDialog);
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    // The operator's ruling of 2026-11-28: the line under the title. It is true
    // with a session open and with none, which the first wording ("… closes
    // the current one") was not.
    it('says, under its title, what choosing does, in the ruled words', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();
        expect(
            screen.getByText('Choosing a session reopens it. Any open session will close.')
        ).toBeInTheDocument();
    });

    it('while the list is being read it says so, and does not say there is nothing to continue', async () => {
        let release!: (v: ClosedListReadLike) => void;
        scripted.push(new Promise<ClosedListReadLike>((r) => (release = r)));
        render(ContinueDialog);
        openContinue();
        await flush();

        expect(screen.getByRole('dialog', { name: 'Continue an earlier session' })).toBeVisible();
        expect(screen.getByText('Reading the earlier sessions…')).toBeInTheDocument();
        expect(screen.queryByText(/no earlier session/i)).toBeNull();

        release(page(0, 20));
        await flush();
        expect(screen.queryByText('Reading the earlier sessions…')).toBeNull();
    });

    it('gives each session as the ruled line, with its dates, its count and an end that was not confirmed', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();

        const lines = screen
            .getAllByRole('button', { name: /UTC/ })
            .map((b) => b.textContent.trim());
        expect(lines).toEqual([
            '2026-11-20 09:15 to 11:42 UTC · 37 contacts',
            '2026-11-19 09:15 UTC · 37 contacts · an end not confirmed',
        ]);
    });

    it('choosing a line sends that session at the generation of the list, once, and closes the dialog', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();

        await fireEvent.click(screen.getByRole('button', { name: /2026-11-19 09:15 UTC/ }));
        await flush();

        expect(sent).toEqual([[4, id(19)]]);
        // (The element itself fades out; what is closed is the list's state.)
        expect(continueList.open).toBe(false);
    });

    it('a list that could not be read says so with what went wrong, never that there is nothing to continue', async () => {
        scripted.push({ kind: 'error', message: 'Cannot reach the daemon.' });
        render(ContinueDialog);
        openContinue();
        await flush();

        expect(screen.getByRole('alert').textContent).toMatch(
            /The earlier sessions could not be read\. Cannot reach the daemon\./
        );
        expect(screen.queryByText(/no earlier session/i)).toBeNull();

        await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
        await flush();
        expect(screen.queryByRole('alert')).toBeNull();
        expect(screen.getAllByRole('button', { name: /UTC/ })).toHaveLength(2);
    });

    it('says there is no earlier session only when the daemon answered an empty list', async () => {
        held = [];
        render(ContinueDialog);
        openContinue();
        await flush();
        expect(screen.getByText('There is no earlier session to continue.')).toBeInTheDocument();
    });

    // codex-295: an empty list that is known to be an old one is not "nothing
    // to continue". The dialog says the session has changed, and offers to read.
    it('an empty list known to be older than the session is not said to be nothing to continue', async () => {
        held = [];
        render(ContinueDialog);
        openContinue();
        await flush();
        expect(screen.getByText('There is no earlier session to continue.')).toBeInTheDocument();

        // The state moves to another generation, and the list read for it fails
        // to be current: the daemon answers from the generation before.
        scripted.push(page(0, 20), page(0, 20), page(0, 20));
        continueStateDrawn({ archiveId: HOME, generation: 6, state: 'open' });
        await flush();

        expect(screen.queryByText('There is no earlier session to continue.')).toBeNull();
        expect(
            screen.getByText('The session has changed since this list was read.')
        ).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Read it again' })).toBeEnabled();
    });

    it('"Show more" is offered only while there are more, and adds them', async () => {
        held = Array.from({ length: 25 }, (_, i) => closed(25 - i));
        render(ContinueDialog);
        openContinue();
        await flush();
        expect(screen.getAllByRole('button', { name: /UTC/ })).toHaveLength(20);

        await fireEvent.click(screen.getByRole('button', { name: 'Show more' }));
        await flush();

        expect(screen.getAllByRole('button', { name: /UTC/ })).toHaveLength(25);
        expect(screen.queryByRole('button', { name: 'Show more' })).toBeNull();
    });

    it('a read again that fails keeps the lines and says they may be out of date', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();
        scripted.push({ kind: 'error', message: 'Cannot reach the daemon.' });
        const { continueContactDeleted } = await import('./continueList.svelte');
        continueContactDeleted();
        await flush();

        expect(screen.getAllByRole('button', { name: /UTC/ })).toHaveLength(2);
        expect(screen.getByRole('alert').textContent.replace(/\s+/g, ' ')).toMatch(
            /could not be read again; this list may be out of date\. Cannot reach the daemon\./
        );
    });

    it('no line can be pressed while a control is out, or its outcome is not known', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();
        for (const standing of ['out', 'unknown'] as const) {
            session.control = standing;
            flushSync();
            for (const b of screen.getAllByRole('button', { name: /UTC/ }))
                expect(b).toBeDisabled();
        }
        session.control = '';
        flushSync();
        for (const b of screen.getAllByRole('button', { name: /UTC/ })) expect(b).toBeEnabled();
    });

    // codex-297: while the page's archive is being checked a guarded request
    // is refused before it is sent, so no line may be pressable; and they come
    // back by themselves when the check has confirmed the archive.
    it('no line can be pressed while the archive is being checked, and all can again once it is confirmed', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();
        try {
            archivesState.verifying = true;
            flushSync();
            for (const b of screen.getAllByRole('button', { name: /UTC/ })) {
                expect(b).toBeDisabled();
            }
            await fireEvent.click(screen.getAllByRole('button', { name: /UTC/ })[0]);
            expect(sent).toEqual([]);
            expect(continueList.open).toBe(true);

            archivesState.verifying = false;
            flushSync();
            for (const b of screen.getAllByRole('button', { name: /UTC/ })) {
                expect(b).toBeEnabled();
            }
        } finally {
            archivesState.verifying = false;
        }
    });

    it('no line can be pressed once the session has moved on from the list, and the dialog says so', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();
        // The state moves to another generation, and the list read for it fails.
        scripted.push(
            { kind: 'error', message: 'Cannot reach the daemon.' },
            { kind: 'error', message: 'Cannot reach the daemon.' }
        );
        continueStateDrawn({ archiveId: HOME, generation: 6, state: 'open' });
        await flush();

        for (const b of screen.getAllByRole('button', { name: /UTC/ })) expect(b).toBeDisabled();
        await fireEvent.click(screen.getAllByRole('button', { name: /UTC/ })[0]);
        expect(sent).toEqual([]);
    });

    it('Escape closes it', async () => {
        render(ContinueDialog);
        openContinue();
        await flush();
        await fireEvent.keyDown(window, { key: 'Escape' });
        flushSync();
        expect(continueList.open).toBe(false);
    });

    // codex-290: the dialog owns the window while it is open. The FT8 view
    // stays mounted under it; nothing under it can be reached or acted on.
    describe('owns the pointer and the keyboard across the window', () => {
        let appRoot: HTMLElement;
        let opener: HTMLButtonElement;
        const dialog = (): HTMLElement => screen.getByRole('dialog');
        const tab = (shiftKey = false): boolean =>
            document.activeElement!.dispatchEvent(
                new KeyboardEvent('keydown', {
                    key: 'Tab',
                    shiftKey,
                    bubbles: true,
                    cancelable: true,
                })
            );

        beforeEach(() => {
            document.body.innerHTML = '';
            appRoot = document.createElement('div');
            appRoot.id = 'app';
            opener = document.createElement('button');
            opener.textContent = 'opener';
            appRoot.appendChild(opener);
            document.body.appendChild(appRoot);
        });

        it('is drawn over the whole window, outside the app it covers, which is inert meanwhile', async () => {
            render(ContinueDialog, { target: appRoot });
            expect(appRoot.hasAttribute('inert')).toBe(false);
            openContinue();
            await flush();

            expect(dialog().parentElement).toBe(document.body);
            expect(appRoot.contains(dialog())).toBe(false);
            expect(appRoot.hasAttribute('inert')).toBe(true);

            await fireEvent.keyDown(document.body, { key: 'Escape' });
            flushSync();
            expect(appRoot.hasAttribute('inert')).toBe(false);
        });

        it('takes the focus when it opens, and gives it back to where it was when it closes', async () => {
            render(ContinueDialog, { target: appRoot });
            opener.focus();
            expect(document.activeElement).toBe(opener);

            openContinue();
            await flush();
            await flush();
            expect(dialog().contains(document.activeElement)).toBe(true);

            await fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
            flushSync();
            expect(continueList.open).toBe(false);
            expect(document.activeElement).toBe(opener);
        });

        it('Tab and Shift+Tab stay inside it, wrapping at both ends', async () => {
            render(ContinueDialog, { target: appRoot });
            opener.focus();
            openContinue();
            await flush();
            await flush();
            const inside = [
                ...dialog().querySelectorAll<HTMLElement>('button:not([tabindex="-1"])'),
            ];
            expect(inside.length).toBeGreaterThanOrEqual(3); // close, and the two lines
            const first = inside[0];
            const last = inside[inside.length - 1];

            last.focus();
            expect(tab()).toBe(false); // the page's own move was prevented
            expect(document.activeElement).toBe(first);

            expect(tab(true)).toBe(false);
            expect(document.activeElement).toBe(last);

            // In the middle the browser moves the focus itself: nothing is prevented.
            inside[1].focus();
            expect(tab()).toBe(true);
        });

        // codex-291: the archive gate is the one screen that must never be
        // covered. It lives inside the app (so that the app under IT is
        // inert), which this dialog makes inert and draws over: its "Reload
        // now", "Stop tune" and "Disable FT8 TX" would be out of reach.
        it('gives way to the archive gate at once: the gate takes the focus, and its stops can be reached', async () => {
            const { default: ArchiveSwitchGate } = await import('../ui/ArchiveSwitchGate.svelte');
            const { archivesState } = await import('../config/archives.svelte');
            const { rig } = await import('./rig.svelte');
            render(ArchiveSwitchGate, { target: appRoot });
            render(ContinueDialog, { target: appRoot });
            opener.focus();
            openContinue();
            await flush();
            await flush();
            expect(appRoot.hasAttribute('inert')).toBe(true);
            try {
                rig.tuneActive = true;
                archivesState.switchUnresolved = true;
                await flush();
                await flush();

                expect(continueList.open).toBe(false);
                expect(appRoot.hasAttribute('inert')).toBe(false);
                const reload = screen.getByRole('button', { name: 'Reload now' });
                expect(document.activeElement).toBe(reload);
                expect(screen.getByRole('button', { name: 'Stop tune' })).toBeEnabled();
                // Tab is the browser's again: nothing holds it inside a dialog.
                expect(tab()).toBe(true);
                // And the dialog cannot be opened over the gate.
                openContinue();
                await flush();
                expect(continueList.open).toBe(false);
            } finally {
                archivesState.switchUnresolved = false;
                rig.tuneActive = false;
            }
        });

        it('no key reaches a handler under it, and every key does again once it is closed', async () => {
            const heard: string[] = [];
            const under = (e: KeyboardEvent): void => void heard.push(e.key);
            window.addEventListener('keydown', under);
            try {
                render(ContinueDialog, { target: appRoot });
                openContinue();
                await flush();

                await fireEvent.keyDown(document.body, { key: 'Enter', ctrlKey: true });
                await fireEvent.keyDown(document.body, {
                    key: 'ArrowUp',
                    ctrlKey: true,
                    shiftKey: true,
                });
                await fireEvent.keyDown(document.body, { key: 'Escape' });
                expect(heard).toEqual([]);
                expect(continueList.open).toBe(false);

                await fireEvent.keyDown(document.body, { key: 'Escape' });
                expect(heard).toEqual(['Escape']);
            } finally {
                window.removeEventListener('keydown', under);
            }
        });
    });
});
