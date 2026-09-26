<script lang="ts">
    // Email section — the SMTP submission account the daemon sends from. Ported
    // from the standalone config SPA's Email tab (ADR 0044).
    import { onMount } from 'svelte';
    import { emailState } from './email.svelte';
    import StoredSecretField from './StoredSecretField.svelte';

    onMount(() => void emailState.load());

    // Numeric entry is restricted to digits at the keystroke rather than coerced
    // on save: a coerce turns "abc" into some number the operator never chose,
    // and blank has to survive as blank (it is what asks the daemon for its
    // default). Stripping keeps every state the operator can reach meaningful.
    function digitsOnly(e: Event, set: (v: string) => void): void {
        const el = e.currentTarget as HTMLInputElement;
        const cleaned = el.value.replace(/\D/g, '');
        el.value = cleaned;
        set(cleaned);
    }
</script>

<!-- Same shell as StationSection / ForwardingSection so the tabs read as one
     page: mx-auto max-w-3xl, the same loading / error-card / body branch order,
     the same space-y-8 rhythm and border-t save footer. -->
<div class="mx-auto max-w-3xl">
    {#if !emailState.loaded && emailState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !emailState.loaded && emailState.error}
        <div class="card">
            <p class="text-sm text-ink">Couldn’t load email settings: {emailState.error}</p>
            <button class="btn mt-3" onclick={() => emailState.load()}>Retry</button>
        </div>
    {:else}
        <div class="space-y-8">
            <p class="text-sm text-muted">
                The SMTP account Station Manager uses to email selected QSOs from Logbook and
                session logs from Export. Leave disabled if you don’t send logs by email. The
                password is stored on the daemon and never sent back to the browser, so leaving it
                blank keeps the saved one.
            </p>

            <section>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={emailState.draft.enabled}
                    />
                    Enabled
                </label>
            </section>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">Server</h2>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    <label class="flex w-72 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Host</span>
                        <input
                            class="input"
                            placeholder="smtp.example.org"
                            autocomplete="off"
                            spellcheck="false"
                            bind:value={emailState.draft.host}
                        />
                    </label>
                    <label class="flex w-28 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Port</span>
                        <!-- The placeholder is the daemon's default, shown so a
                             blank box says what saving will actually store
                             rather than leaving the operator to guess. It is a
                             DISPLAY duplicate of config.defaultSmtpPort; the
                             value itself is still resolved daemon-side, so a
                             drift here misinforms but cannot misconfigure. -->
                        <input
                            class="input"
                            inputmode="numeric"
                            placeholder="587"
                            value={emailState.draft.port}
                            oninput={(e) => digitsOnly(e, (v) => (emailState.draft.port = v))}
                        />
                    </label>
                </div>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={emailState.draft.starttls}
                    />
                    Use STARTTLS
                </label>
                <p class="text-xs text-muted">
                    STARTTLS upgrades the connection to TLS (port 587, the usual choice). Turn it
                    off only for a cleartext local relay you trust.
                </p>
            </section>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">Credentials</h2>
                <label class="flex w-72 flex-col gap-1">
                    <span class="text-sm font-medium text-ink">Username</span>
                    <input
                        class="input"
                        autocomplete="off"
                        spellcheck="false"
                        bind:value={emailState.draft.username}
                    />
                </label>
                <!-- A stored password is a status line with Replace and Remove
                     (ruling 2026-09-26). Remove is offered only when one is
                     stored: with nothing to remove the daemon treats the flag as
                     a no-op, and a control that appears to work and does nothing
                     teaches the operator it worked. Unauthenticated submission
                     is a legitimate setup — plenty of local relays want no
                     credentials — which is why removal exists at all. -->
                <div class="w-72">
                    <StoredSecretField
                        label="Password"
                        stored={emailState.draft.passwordSet}
                        value={emailState.draft.password}
                        cleared={emailState.draft.passwordCleared}
                        removable={true}
                        removedNote="Removed when you save — the account will then connect without authentication."
                        oninput={(v: string) => emailState.setPassword(v)}
                        onremove={() => emailState.clearPassword()}
                        onundo={() => emailState.keepPassword()}
                    />
                </div>
            </section>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">Addresses</h2>
                <label class="flex w-72 flex-col gap-1">
                    <span class="text-sm font-medium text-ink">From</span>
                    <input
                        class="input"
                        type="email"
                        placeholder="you@example.org"
                        autocomplete="off"
                        spellcheck="false"
                        bind:value={emailState.draft.from}
                    />
                </label>
                <label class="flex w-72 flex-col gap-1">
                    <span class="text-sm font-medium text-ink">Default recipient</span>
                    <input
                        class="input"
                        type="email"
                        placeholder="qsl-manager@example.org"
                        autocomplete="off"
                        spellcheck="false"
                        bind:value={emailState.draft.defaultRecipient}
                    />
                    <span class="text-xs text-muted">
                        Pre-fills the recipient when emailing a log. Optional.
                    </span>
                </label>
            </section>

            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">Timeout</h2>
                <label class="flex w-28 flex-col gap-1">
                    <span class="text-sm font-medium text-ink">Send timeout (s)</span>
                    <input
                        class="input"
                        inputmode="numeric"
                        placeholder="30"
                        value={emailState.draft.timeoutSec}
                        oninput={(e) => digitsOnly(e, (v) => (emailState.draft.timeoutSec = v))}
                    />
                </label>
                <p class="mt-2 text-xs text-muted">
                    Bounds the whole connect + send round-trip. Leave either number blank to use the
                    default shown.
                </p>
            </section>

            {#if emailState.dirty}
                <div
                    class="rounded-md border border-warning bg-surface-muted px-3 py-2 text-sm text-warning"
                >
                    ⚠ Email changes apply when the daemon restarts — the mailer binds at startup.
                </div>
            {/if}

            <div class="flex items-center gap-3 border-t border-line pt-4">
                <button
                    class="btn btn-primary"
                    disabled={!emailState.dirty || emailState.saving}
                    onclick={() => emailState.save()}
                >
                    {emailState.saving ? 'Saving…' : 'Save'}
                </button>
                <button
                    class="btn"
                    disabled={!emailState.dirty || emailState.saving}
                    onclick={() => emailState.reset()}
                >
                    Cancel
                </button>
                {#if emailState.dirty}
                    <span class="text-xs text-muted">Unsaved changes</span>
                {/if}
            </div>
        </div>
    {/if}
</div>
