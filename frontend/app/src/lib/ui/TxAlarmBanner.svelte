<script lang="ts">
    // Stuck-TX safety banner (ADR 0051). Rendered at shell level so it is
    // visible on EVERY view: the daemon raised tx-alarm because it cannot
    // confirm the transmitter is unkeyed — the rig may be transmitting. It
    // stays until the daemon confirms RX (clears the alarm) or the operator
    // dismisses; a dismiss hides the banner but does not claim safety, and a
    // NEW alarm re-shows it.
    import { rig, dismissTxAlarm } from '../operate/rig.svelte';

    const CODE_TEXT: Record<string, string> = {
        tx_unconfirmed: 'The stop-transmit command was sent but the rig has not confirmed it.',
        tx_still_keyed: 'The rig reports it is STILL transmitting after the stop command.',
        tx_liveness_lost: 'The CAT link died while a transmission was keyed.',
        tx_teardown_unconfirmed: 'The daemon shut down while keyed and could not confirm the stop.',
        tx_key_write_failed:
            'A transmit command failed in a way that may still have keyed the rig.',
    };

    const detail = $derived(CODE_TEXT[rig.txAlarmCode] ?? '');
    const show = $derived(rig.txAlarmActive && !rig.txAlarmDismissed);
</script>

{#if show}
    <div
        class="flex items-center gap-x-3 border-b border-red-800 bg-red-600 px-4 py-2 text-sm font-medium text-white"
        role="alert"
    >
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            class="size-5 shrink-0"
            aria-hidden="true"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"
            />
        </svg>
        <span>
            <strong>CHECK YOUR RADIO</strong> — it may still be transmitting.
            {detail}
        </span>
        <button
            type="button"
            class="ml-auto shrink-0 cursor-pointer rounded border border-white/40 px-2 py-0.5 text-xs hover:bg-red-700"
            onclick={dismissTxAlarm}
        >
            Dismiss
        </button>
    </div>
{/if}
