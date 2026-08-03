/*
    Email settings state (app Settings view, ADR 0044) — the port of the
    standalone config SPA's Email tab.

    The SMTP account is a SINGLE block, not a list, so this is much simpler than
    the Forwarding section next door. What it shares is the masked-credential
    contract: the daemon never sends the password back, so a blank box means
    "not retyped" and the payload must be able to say three different things
    about a field it can only ever see as empty:

        never touched     → `password` omitted        → daemon keeps it
        typed             → `password: "…"`           → daemon replaces it
        explicitly removed→ `password_clear: true`    → daemon deletes it

    Forwarding expresses its third state by sending "" for fields the Go side
    declares Clearable. That idiom is deliberately NOT reused here (operator's
    ruling, 2026-08-03): blank must go on meaning KEEP, because it is what an
    operator editing the host sends on every single save. Overloading it for a
    password — the one credential whose loss is silent until the next send fails
    — trades a clear signal for an ambiguous one. Hence the explicit flag.

    The two intents are mutually exclusive and the LAST one expressed wins:
    setPassword cancels a pending removal, clearPassword discards a half-typed
    value. Neither can reach the wire together (the daemon has its own
    clear-wins rule for foreign clients, but ours must never need it).
*/
import { fetchEmail, saveEmail, type SmtpEntry, type SmtpPayload } from '../api/email';
import { toasts } from '../ui/toasts.svelte';

/** The form's view of the SMTP block, plus the two transient intents. */
export interface EmailDraft {
    enabled: boolean;
    host: string;
    /**
     * Port and timeout are STRINGS so that "blank" is representable. As numbers
     * there is no value distinct from a real 0, and blank has to survive as
     * blank all the way to buildPayload — that is what lets the daemon apply
     * its default rather than the SPA duplicating 587/30.
     */
    port: string;
    username: string;
    from: string;
    defaultRecipient: string;
    starttls: boolean;
    timeoutSec: string;
    /** What the operator typed. Blank = not retyped, never "erase it". */
    password: string;
    /** Whether the daemon holds a password. Read-only; from the masked GET. */
    passwordSet: boolean;
    /** Operator pressed Remove — the stored password goes on the next save. */
    passwordCleared: boolean;
}

/** The empty block the form binds to before the first load resolves. */
const BLANK: SmtpEntry = {
    enabled: false,
    host: '',
    port: 0,
    username: '',
    from: '',
    default_recipient: '',
    starttls: true,
    timeout_sec: 0,
    password_set: false,
};

function draftFrom(s: SmtpEntry): EmailDraft {
    return {
        enabled: s.enabled,
        host: s.host,
        // 0 renders as BLANK, not "0": the daemon resolves an unset number to
        // its default, so an empty box with a 587 placeholder is the honest
        // display. "0" would read as a port the operator had chosen.
        port: s.port ? String(s.port) : '',
        username: s.username,
        from: s.from,
        defaultRecipient: s.default_recipient,
        starttls: s.starttls,
        timeoutSec: s.timeout_sec ? String(s.timeout_sec) : '',
        password: '',
        passwordSet: s.password_set,
        passwordCleared: false,
    };
}

class EmailState {
    loading = $state(false);
    saving = $state(false);
    loaded = $state(false);
    error = $state('');
    draft = $state<EmailDraft>(draftFrom(BLANK));

    // The full draft doubles as the dirty-compare projection: every field on it
    // is either an editable value or one of the two transient intents, and BOTH
    // intents have to count. A projection that omitted them would let an
    // operator press Remove, see no "unsaved changes", and leave believing the
    // password was gone (W7).
    #pristine = $state(JSON.stringify(draftFrom(BLANK)));

    dirty = $derived(JSON.stringify(this.draft) !== this.#pristine);

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        this.error = '';
        const out = await fetchEmail();
        this.loading = false;
        if (out.kind === 'error') {
            this.error = out.message;
            return;
        }
        this.#apply(out.smtp);
        this.loaded = true;
    }

    async save(): Promise<void> {
        if (this.saving || !this.dirty) return;
        this.saving = true;
        try {
            const res = await saveEmail(this.buildPayload());
            if (res.kind === 'error') {
                // The draft is left exactly as typed: a refused save is the
                // operator's cue to fix one field, not to re-enter the form.
                toasts.error(`Save failed: ${res.message}`);
                return;
            }
            this.#apply(res.smtp);
            toasts.info('Email settings saved. Restart the daemon to apply.');
        } finally {
            this.saving = false;
        }
    }

    /** Record a typed password; supersedes a pending removal. */
    setPassword(v: string): void {
        this.draft.password = v;
        if (v !== '') this.draft.passwordCleared = false;
    }

    /**
     * Mark the stored password for removal.
     *
     * Drops any half-typed value: "set it to this" and "delete it" are opposite
     * intentions, and the last one expressed wins. Sending both would leave the
     * daemon arbitrating a contradiction we created.
     */
    clearPassword(): void {
        this.draft.password = '';
        this.draft.passwordCleared = true;
    }

    /** Undo a pending removal. */
    keepPassword(): void {
        this.draft.passwordCleared = false;
    }

    reset(): void {
        this.draft = JSON.parse(this.#pristine) as EmailDraft;
    }

    /**
     * Build the PUT payload.
     *
     * `password` is OMITTED unless freshly typed — never sent as "". The daemon
     * treats blank as keep either way, but an omitted key states the intent, and
     * a "" sitting in the body is one refactor away from being read as a value.
     *
     * `password_clear` rides only on an explicit removal, and suppresses the
     * password outright so the two can never appear together.
     *
     * Port and timeout go out as 0 when blank. That is not a magic number — it
     * is the absent value, which config.Normalize resolves to 587 / 30 before
     * validateSmtp sees it, and the response then carries the resolved figure
     * back into the form. Duplicating the defaults here would put a second copy
     * of them in a second language, to drift against the first.
     */
    buildPayload(): SmtpPayload {
        const d = this.draft;
        const out: SmtpPayload = {
            enabled: d.enabled,
            host: d.host.trim(),
            port: Number(d.port) || 0,
            username: d.username.trim(),
            from: d.from.trim(),
            default_recipient: d.defaultRecipient.trim(),
            starttls: d.starttls,
            timeout_sec: Number(d.timeoutSec) || 0,
        };
        if (d.passwordCleared) out.password_clear = true;
        else if (d.password !== '') out.password = d.password;
        return out;
    }

    #apply(s: SmtpEntry): void {
        this.draft = draftFrom(s);
        this.#pristine = JSON.stringify(this.draft);
    }
}

export const emailState = new EmailState();
