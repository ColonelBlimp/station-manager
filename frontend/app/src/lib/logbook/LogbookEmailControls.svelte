<script lang="ts">
    /*
        Email-out controls for the logbook's selected rows (ported from the
        shipping logbook SPA, restyled onto the app tokens): a recipient input +
        send button that posts the selected QSOs' UUIDs to POST /v1/session/email.
        The daemon rebuilds the ADIF from the live DB rows (so the mail carries
        current data), sends it as an attachment, and durably stamps each row
        "forwarded by email". The send outcome stays inline next to the button
        (faithful port — the result reads in-place where the operator is looking,
        which beats a corner toast for a toolbar action).

        Selection is NOT cleared on success (matching the Operate session panel):
        the rows' callsign tint flips green via markEmailed and the result line
        confirms the send, so the operator sees what happened, then clears the
        selection themselves.

        recipient seeds once from logbookState.mailerDefaultRecipient when that
        first lands non-empty, then is operator-owned (a later config refresh or a
        manual clear isn't clobbered — the boolean guard stops the seed re-firing).
    */
    import { logbookState } from './logbook.svelte';
    import { sendSessionEmail } from '../api/session-email';

    let recipient = $state('');
    let recipientSeeded = false;
    $effect(() => {
        const def = logbookState.mailerDefaultRecipient;
        if (!recipientSeeded && def !== '') {
            recipient = def;
            recipientSeeded = true;
        }
    });

    let sending = $state(false);
    let result: { ok: boolean; text: string } | null = $state(null);

    // All clauses must hold: mailer enabled, at least one selected row with a UUID
    // (the email payload), a recipient that at least contains '@' (obvious-typo
    // guard so we don't round-trip a 400), and not already mid-send.
    const canSend = $derived(
        logbookState.mailerEnabled &&
            logbookState.selectedUuids.length > 0 &&
            recipient.trim() !== '' &&
            recipient.includes('@') &&
            !sending
    );

    async function handleSend(): Promise<void> {
        if (!canSend) return;
        sending = true;
        result = null;
        const to = recipient.trim();
        const uuids = logbookState.selectedUuids;
        const outcome = await sendSessionEmail({ to, uuids });
        sending = false;
        switch (outcome.kind) {
            case 'sent':
                logbookState.markEmailed(outcome.emailed);
                result = {
                    ok: true,
                    text: `Sent ${uuids.length} QSO${uuids.length === 1 ? '' : 's'} to ${to}`,
                };
                break;
            case 'mailer_disabled':
                result = { ok: false, text: 'Email not configured (SMTP block missing)' };
                break;
            case 'invalid':
                result = { ok: false, text: outcome.message };
                break;
            case 'smtp_failure':
                result = { ok: false, text: 'Email send failed; check daemon logs' };
                break;
            case 'server':
                result = { ok: false, text: `Email send failed: ${outcome.message}` };
                break;
            case 'network':
                // A timeout is ambiguous — SMTP may have accepted the
                // message; a blind re-send risks a duplicate email.
                result = {
                    ok: false,
                    text:
                        outcome.timedOut === true
                            ? 'Email outcome unknown — the send may have completed. Check the inbox (or the Emailed column) before re-sending.'
                            : 'Cannot reach the daemon',
                };
                break;
        }
    }
</script>

{#if logbookState.mailerEnabled}
    <input
        type="email"
        bind:value={recipient}
        placeholder="recipient@example.com"
        class="input mt-0 w-52"
        aria-label="Recipient email address"
    />
    <button
        type="button"
        onclick={handleSend}
        disabled={!canSend}
        class="btn btn-primary text-xs"
        title={logbookState.selectedUuids.length === 0
            ? 'The selected rows have no id and cannot be emailed'
            : 'Email the selected QSOs as an ADIF attachment'}
    >
        {sending ? 'Sending…' : 'Email'}
    </button>
{:else}
    <span class="text-xs text-muted" title="Enable SMTP in the Config app to email QSOs"
        >Email not configured</span
    >
{/if}

{#if result !== null}
    <span
        class="text-xs {result.ok
            ? 'text-green-700 dark:text-green-400'
            : 'text-red-700 dark:text-red-400'}">{result.text}</span
    >
{/if}
