import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import EmailSection from './EmailSection.svelte';
import { emailState } from './email.svelte';

/*
    EMAIL SECTION — WHAT THE OPERATOR SEES.

    email.svelte.test.ts pins what goes ON THE WIRE. These rules pin the half of
    the criteria that is only observable in the browser:

        E1  I can tell "a password is stored" from "none is stored" without
            either showing me a value.
        E8  I can tell "this save will REMOVE the stored password" from "this
            save will KEEP it" — the two otherwise render as the same empty box.
        Q2  A blank port or timeout tells me which default it will resolve to.

    U2 IS THE LOAD-BEARING ONE. Remove must not be offered when there is nothing
    stored: a control that appears to work and does nothing teaches the operator
    that the password WAS removed. The state module would send the flag anyway
    (the daemon treats it as a no-op, pinned by S3), so no wire rule can fail if
    the button is offered universally — only this one can.

    U3 IS THE OTHER HALF AND IS EASY TO MISS. Once pressed, the pending removal
    has to be VISIBLE, because the box it applies to is empty either way. A
    Remove button that merely records the intent and changes nothing on screen
    is indistinguishable from a button that does nothing at all — the same
    defect U2 guards against, one step later.
*/

afterEach(() => {
    vi.restoreAllMocks();
    emailState.loaded = false;
    emailState.loading = false;
    emailState.saving = false;
    emailState.error = '';
});

function mockConfig(passwordSet: boolean, port = 2525, timeout = 15) {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(
                    JSON.stringify({
                        smtp: {
                            enabled: true,
                            host: 'smtp.example.org',
                            port,
                            username: 'tx@example.org',
                            from: 'tx@example.org',
                            default_recipient: '',
                            starttls: true,
                            timeout_sec: timeout,
                            password_set: passwordSet,
                        },
                    }),
                    { status: 200, headers: { 'Content-Type': 'application/json' } }
                )
            )
        )
    );
}

async function renderLoaded(passwordSet: boolean, port?: number, timeout?: number) {
    mockConfig(passwordSet, port, timeout);
    render(EmailSection);
    await vi.waitFor(() => expect(emailState.loaded).toBe(true));
}

describe('EmailSection', () => {
    // U1 — E1. The stored-or-not distinction is carried by the placeholder, and
    // it is the ONLY thing the daemon tells us about the value.
    it('U1: says a password is stored without showing one', async () => {
        await renderLoaded(true);
        const box = screen.getByPlaceholderText(/set — leave blank to keep/i);
        expect((box as HTMLInputElement).value).toBe('');
    });

    // U1b — and says nothing when none is stored, so the hint cannot be read as
    // decoration that is always there.
    it('U1b: shows no "set" hint when no password is stored', async () => {
        await renderLoaded(false);
        expect(screen.queryByPlaceholderText(/set — leave blank to keep/i)).toBeNull();
    });

    // U2 — E8. Offered only when there is something to remove.
    it('U2: offers Remove only when a password is stored', async () => {
        await renderLoaded(false);
        expect(screen.queryByRole('button', { name: /remove stored password/i })).toBeNull();
    });

    it('U2b: offers Remove when one is stored', async () => {
        await renderLoaded(true);
        expect(screen.getByRole('button', { name: /remove stored password/i })).toBeTruthy();
    });

    // U3 — E8's discriminator. After pressing Remove the operator must be able
    // to SEE that this save deletes the password, and be able to back out.
    it('U3: shows a pending removal, and can undo it', async () => {
        await renderLoaded(true);
        await fireEvent.click(screen.getByRole('button', { name: /remove stored password/i }));

        expect(screen.getByText(/will be removed when you save/i)).toBeTruthy();
        expect(emailState.draft.passwordCleared).toBe(true);

        await fireEvent.click(screen.getByRole('button', { name: /keep stored password/i }));
        expect(emailState.draft.passwordCleared).toBe(false);
        expect(screen.queryByText(/will be removed when you save/i)).toBeNull();
    });

    // U4 — Q2. A blank number says which default it will take. Without this the
    // operator sees an empty box and no way to know what saving will store.
    it('U4: blank port and timeout show the daemon defaults as placeholders', async () => {
        await renderLoaded(true, 0, 0);
        expect(screen.getByPlaceholderText('587')).toBeTruthy();
        expect(screen.getByPlaceholderText('30')).toBeTruthy();
    });

    // U5 — E6. Staged changes announce that they are restart-only, so "saved"
    // is not mistaken for "in effect".
    it('U5: warns that changes apply at daemon restart', async () => {
        await renderLoaded(true);
        expect(screen.queryByText(/apply when the daemon restarts/i)).toBeNull();
        await fireEvent.input(screen.getByPlaceholderText('smtp.example.org'), {
            target: { value: 'new.example.org' },
        });
        expect(screen.getByText(/apply when the daemon restarts/i)).toBeTruthy();
    });

    // U6 — the digit rule. A pasted "25a25" must not become a port the operator
    // never chose, and must not wipe the field either.
    it('U6: keeps only digits in the port field', async () => {
        await renderLoaded(true);
        const port = screen.getByDisplayValue('2525');
        await fireEvent.input(port, { target: { value: '5a8b7' } });
        expect(emailState.draft.port).toBe('587');
    });
});
