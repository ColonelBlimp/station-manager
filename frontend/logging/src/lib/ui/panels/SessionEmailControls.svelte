<script lang="ts">
    /*
        Session-tab email controls: a recipient input + paper-plane send button
        that emails the current session's QSOs to a QSL manager. The session list
        (sessionQsosState) is shared across operating modes, so this is used by
        both InfoPanel (Phone/CW) and Ft8Panel (FT8) — each drops it into its own
        positioned Session-tab strip, gated on (Session tab active && mailer
        enabled); this component owns the recipient + send logic.

        recipient seeds once from configState.mailer.defaultRecipient when that
        first lands non-empty, then is operator-owned (later config changes don't
        clobber a typed value; the boolean guard stops the seed re-firing after a
        manual clear).
    */
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { configState } from '../../states/config.svelte';
    import { sendSessionEmail } from '../../api/session-email';
    import { toasts } from '../../states/toasts.svelte';

    let recipient: string = $state('');
    let recipientSeeded = false;
    $effect(() => {
        const def = configState.mailer.defaultRecipient;
        if (!recipientSeeded && def !== '') {
            recipient = def;
            recipientSeeded = true;
        }
    });

    let sending: boolean = $state(false);

    /*
        All four clauses must hold: mailer enabled, at least one logged QSO in the
        session, recipient non-empty and contains '@' (obvious-typo guard so we
        don't round-trip a 400), and not already mid-send.
    */
    const canSend: boolean = $derived(
        configState.mailer.enabled &&
            sessionQsosState.count > 0 &&
            recipient.trim() !== '' &&
            recipient.includes('@') &&
            !sending
    );

    /*
        Send the session QSOs' UUIDs (submit order); the daemon rebuilds the ADIF
        from the live DB rows so the mail carries current data, stamps each row
        "forwarded by email", and reports which UUIDs it marked — mirror that onto
        the session rows so the Sent column updates immediately. Sticky "Sending…"
        toast (ttl=0) survives a slow handshake; dismissed before the result toast.
    */
    async function handleSend(): Promise<void> {
        if (!canSend) return;
        sending = true;
        const sendingToastId = toasts.info('Sending…', 0);
        try {
            const uuids = sessionQsosState.items.map((q) => q.uuid);
            const outcome = await sendSessionEmail({ to: recipient.trim(), uuids });
            toasts.dismiss(sendingToastId);
            switch (outcome.kind) {
                case 'sent':
                    sessionQsosState.markEmailed(outcome.emailed, outcome.date);
                    toasts.info(`Sent to ${recipient.trim()}`);
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
                case 'network':
                    toasts.error('Cannot reach daemon');
                    break;
            }
        } finally {
            sending = false;
        }
    }
</script>

<!--
    Heroicon "paper-airplane" (outline) — stroke-width matches the tab icons so it
    sits visually flush with them.
-->
{#snippet paperPlaneIcon()}
    <svg
        class="size-5 -rotate-30"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M6 12 3.269 3.125A59.769 59.769 0 0 1 21.485 12 59.768 59.768 0 0 1 3.27 20.875L5.999 12Zm0 0h7.5"
        />
    </svg>
{/snippet}

<input
    type="email"
    bind:value={recipient}
    placeholder="recipient@example.com"
    class="text-sm border border-gray-300 rounded px-2 py-1 w-47 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
    aria-label="Recipient email address"
/>
<button
    type="button"
    onclick={handleSend}
    disabled={!canSend}
    class="p-1.5 rounded text-indigo-600 hover:text-indigo-800 hover:bg-indigo-50 disabled:text-gray-400 disabled:hover:bg-transparent disabled:cursor-not-allowed cursor-pointer"
    aria-label="Send session ADIF"
    title={sending ? 'Sending…' : 'Send session ADIF'}
>
    {@render paperPlaneIcon()}
</button>
