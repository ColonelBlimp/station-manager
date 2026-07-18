<script lang="ts">
    // Export-session dialog — the deliberate end-of-session actions moved off
    // the Session card (operator decision 2026-07-08): download the session as
    // an ADIF file (offline, client-side) and/or email it to a QSL manager
    // (daemon SMTP, marks rows). Same modal+backdrop pattern as
    // DuplicateDialog. Opened from the InfoPanel header's Export button when
    // the Session panel is active; driven by operate.exportOpen.
    //
    // Cabrillo is deliberately absent (no daemon support + needs contest
    // metadata) — a real option lands when the format does, not a greyed tease.
    import { fade } from 'svelte/transition';
    import { operate, closeExport } from './state.svelte';
    import { session } from './session.svelte';
    import { mailer } from './mailer.svelte';
    import { sendSessionEmail } from '../api/session-email';
    import { exportSessionAdif, downloadTextFile } from '../api/session-export';
    import { toasts } from '../ui/toasts.svelte';

    let recipient = $state('');
    let recipientSeeded = false;
    // Seed once from config when it first lands non-empty, then operator-owned
    // (a later config change won't clobber a typed value; the guard stops the
    // seed re-firing after a manual clear).
    $effect(() => {
        if (!recipientSeeded && mailer.defaultRecipient !== '') {
            recipient = mailer.defaultRecipient;
            recipientSeeded = true;
        }
    });

    let sending = $state(false);
    let downloading = $state(false);

    const count = $derived(session.qsos.length);
    const uuids = $derived(session.qsos.map((q) => q.uuid).filter((u): u is string => !!u));
    const canSend = $derived(
        mailer.enabled &&
            count > 0 &&
            recipient.trim() !== '' &&
            recipient.includes('@') &&
            !sending
    );

    // The daemon rebuilds the ADIF from the DB (enriched record + a backup in
    // exports/sent-adif/) and returns it as an attachment — the SPA never
    // composes ADIF itself. Fail-soft: an error toasts, the dialog stays open.
    async function downloadAdif(): Promise<void> {
        if (uuids.length === 0 || downloading) return;
        downloading = true;
        try {
            const outcome = await exportSessionAdif(uuids);
            if (outcome.kind === 'ok') {
                downloadTextFile(outcome.filename, outcome.body);
                toasts.info(`Exported ${count} QSO${count === 1 ? '' : 's'} to ADIF`);
                closeExport();
            } else if (outcome.kind !== 'aborted') {
                toasts.error(`Export failed: ${outcome.message}`);
            }
        } finally {
            downloading = false;
        }
    }

    async function send(): Promise<void> {
        if (!canSend) return;
        sending = true;
        const pending = toasts.info('Sending email…', 0);
        try {
            const outcome = await sendSessionEmail({ to: recipient.trim(), uuids });
            toasts.dismiss(pending);
            switch (outcome.kind) {
                case 'sent':
                    toasts.info(`Sent ${outcome.emailed.length} QSOs to ${recipient.trim()}`);
                    closeExport();
                    break;
                case 'mailer_disabled':
                    toasts.error('Email not configured (SMTP block missing)');
                    break;
                case 'invalid':
                    toasts.error(outcome.message);
                    break;
                case 'smtp_failure':
                    toasts.error('Email send failed; check daemon logs');
                    break;
                case 'server':
                    toasts.error(`Email send failed: ${outcome.message}`);
                    break;
                case 'aborted':
                    break;
                case 'network':
                    // EVERY transport failure on a send is ambiguous, not
                    // just a timeout — the daemon's 30 s SMTP/HTTP timeouts
                    // can drop the connection after SMTP accepted, and the
                    // browser can't tell that from connect-refused. A blind
                    // re-send risks a duplicate email in a real inbox.
                    toasts.error(
                        'Cannot confirm the send — the connection to the daemon failed. The email may still have gone out; check the inbox (or the Emailed markers) before re-sending.'
                    );
                    break;
            }
        } finally {
            sending = false;
        }
    }
</script>

{#if operate.exportOpen}
    <div
        class="fixed inset-0 z-50"
        role="dialog"
        aria-modal="true"
        aria-labelledby="export-title"
        transition:fade={{ duration: 150 }}
    >
        <button
            type="button"
            class="absolute inset-0 cursor-default bg-gray-500/75 dark:bg-gray-900/50"
            aria-label="Close"
            onclick={closeExport}
        ></button>

        <div class="pointer-events-none relative flex min-h-full items-center justify-center p-4">
            <div
                class="pointer-events-auto w-full max-w-md rounded-lg bg-surface p-6 shadow-xl outline-1 outline-line"
            >
                <div class="flex items-start justify-between">
                    <h3 id="export-title" class="text-base font-semibold text-ink">
                        Export session
                    </h3>
                    <button
                        class="cursor-pointer rounded-md text-muted hover:text-ink"
                        aria-label="Close"
                        onclick={closeExport}
                    >
                        <svg
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            stroke-width="1.5"
                            aria-hidden="true"
                            class="size-5"
                        >
                            <path
                                d="M6 18 18 6M6 6l12 12"
                                stroke-linecap="round"
                                stroke-linejoin="round"
                            />
                        </svg>
                    </button>
                </div>
                <p class="mt-1 text-sm text-muted">
                    {count} QSO{count === 1 ? '' : 's'} logged this session.
                </p>

                <!-- Download ADIF (offline, no daemon) -->
                <div class="mt-4 border-t border-line pt-4">
                    <h4 class="text-sm font-medium text-ink">Download</h4>
                    <div class="mt-2 flex items-center justify-between gap-x-4">
                        <p class="text-xs text-muted">Save this session as an ADIF file.</p>
                        <button
                            class="btn shrink-0"
                            disabled={count === 0 || downloading}
                            onclick={downloadAdif}
                        >
                            {downloading ? 'Exporting…' : 'ADIF'}
                        </button>
                    </div>
                </div>

                <!-- Email to QSL manager (daemon SMTP, marks rows sent) -->
                <div class="mt-4 border-t border-line pt-4">
                    <h4 class="text-sm font-medium text-ink">Send to QSL manager</h4>
                    {#if mailer.enabled}
                        <div class="mt-2 flex items-end gap-x-2">
                            <div class="flex-1">
                                <label for="export-to" class="block text-xs text-muted"
                                    >Recipient</label
                                >
                                <input
                                    id="export-to"
                                    class="input"
                                    type="email"
                                    autocomplete="off"
                                    placeholder="qsl@example.com"
                                    bind:value={recipient}
                                />
                            </div>
                            <button
                                class="btn btn-primary shrink-0"
                                disabled={!canSend}
                                onclick={send}
                            >
                                {sending ? 'Sending…' : 'Send'}
                            </button>
                        </div>
                    {:else}
                        <p class="mt-2 text-xs text-muted">
                            Email isn't configured — add an SMTP block in Settings.
                        </p>
                    {/if}
                </div>
            </div>
        </div>
    </div>
{/if}
