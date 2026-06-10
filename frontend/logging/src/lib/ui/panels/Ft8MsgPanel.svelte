<script lang="ts">
    import { onMount, onDestroy } from 'svelte';
    import { ft8State } from '../../states/ft8.svelte';
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { armFt8Tx, sendFt8Tx } from '../../api/ft8tx';
    import { abandonFt8Qso } from '../../api/ft8qso';
    import { toasts } from '../../states/toasts.svelte';

    /*
        FT8 transmit (ADR 0029/0030/0031). The daemon owns the TX path (arm gate +
        guaranteed stop) and the sequencer; this panel is the SPA surface:
          - Arm/Disarm + a slot countdown (always).
          - When a sequenced answer-a-CQ contact is active (ft8State.qso.active —
            started by clicking a CQ row in Band Activity, step e3): the live rung,
            next message, and an Abandon button. The daemon auto-advances the
            CQ→73 ladder; the operator only watches + can bail.
          - When idle + armed: a manual "Call CQ" send on the picked offset (e1).
        Arm/send/abandon go through lib/api; the daemon confirms by push, so the UI
        reflects ft8State.tx / ft8State.qso rather than optimistic local state.
    */

    // The operator must have a callsign to call CQ; grid is optional (CQ <call>
    // is still an encodable standard message). Built from My Station identity.
    const myCall = $derived(configState.loggingStation.stationCallsign.trim().toUpperCase());
    const myGrid = $derived(configState.loggingStation.myGridsquare.trim().toUpperCase());
    const cqMessage = $derived(myCall ? `CQ ${myCall}${myGrid ? ` ${myGrid}` : ''}` : '');

    const tx = $derived(ft8State.tx);
    const qso = $derived(ft8State.qso);
    const offset = $derived(ft8State.selectedOffset);
    // Arming a disconnected rig just 503s; gate on the live-rig signal so the
    // control is only offered when it can work.
    const canArm = $derived(displayedState.isLive);
    // Manual Call CQ is only offered when idle (no sequenced contact in flight).
    const canSend = $derived(
        tx.armed && !tx.transmitting && !qso.active && offset !== null && cqMessage !== ''
    );

    let arming = $state(false);
    let sending = $state(false);
    let abandoning = $state(false);

    async function onAbandon(): Promise<void> {
        if (abandoning) return;
        abandoning = true;
        try {
            const out = await abandonFt8Qso();
            if (out.kind !== 'ok') toasts.error(out.message);
        } finally {
            abandoning = false;
        }
    }

    async function toggleArm(): Promise<void> {
        if (arming) return;
        arming = true;
        try {
            const out = await armFt8Tx(!tx.armed);
            if (out.kind !== 'ok') toasts.error(out.message);
            // armed state itself arrives via the ft8-tx SSE (confirm-by-push).
        } finally {
            arming = false;
        }
    }

    async function callCq(): Promise<void> {
        if (sending || offset === null || cqMessage === '') return;
        sending = true;
        try {
            const out = await sendFt8Tx(cqMessage, offset);
            if (out.kind !== 'ok') toasts.error(out.message);
        } finally {
            sending = false;
        }
    }

    // Slot countdown — SPA-derived (FT8 slots align to UTC :00/:15/:30/:45). A
    // light ticker; the tab only mounts this panel while it's the active tab.
    let nowSec = $state(Math.floor(Date.now() / 1000));
    let timer: ReturnType<typeof setInterval> | undefined;
    onMount(() => {
        timer = setInterval(() => (nowSec = Math.floor(Date.now() / 1000)), 500);
    });
    onDestroy(() => clearInterval(timer));
    // Epoch seconds align to the UTC :00/:15/:30/:45 slot boundaries (epoch 0 is
    // a boundary), so seconds-to-next is just 15 − (epoch % 15): a 15→1 countdown.
    const secondsToNextSlot = $derived(15 - (nowSec % 15));

    // Status line under the controls.
    const statusLine = $derived.by(() => {
        if (!tx.armed) return 'TX disarmed.';
        if (tx.transmitting)
            return `Transmitting ${tx.message || cqMessage} @ ${tx.offsetHz || offset} Hz…`;
        if (tx.error) return `Last transmission failed (${tx.error}).`;
        return 'Armed — ready.';
    });
</script>

<div class="flex flex-col gap-3 px-2 py-4 text-sm text-gray-700" style="max-width: 32rem">
    <div class="flex items-center gap-4">
        <button
            type="button"
            onclick={toggleArm}
            disabled={arming || !canArm}
            class="rounded px-3 py-1.5 text-sm font-medium cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed {tx.armed
                ? 'bg-red-600 text-white hover:bg-red-700'
                : 'bg-focus text-surface hover:opacity-90'}"
        >
            {tx.armed ? 'Disarm TX' : 'Arm TX'}
        </button>
        <span class="text-xs text-gray-500">next slot in {secondsToNextSlot}s</span>
    </div>

    {#if !canArm}
        <p class="text-xs text-amber-700">Rig not connected — connect a rig to transmit.</p>
    {/if}

    {#if qso.active}
        <!-- A sequenced answer-a-CQ contact is in progress; the daemon walks the
             ladder, the operator watches and can Abandon. -->
        <div class="flex flex-col gap-1 rounded border border-indigo-200 bg-indigo-50 p-2">
            <div>
                Working <span class="font-mono font-semibold">{qso.theirCall}</span>
                <span class="text-xs text-gray-500">· {qso.state}</span>
                {#if qso.repeats > 0}<span class="text-xs text-gray-500"
                        >· repeat {qso.repeats}</span
                    >{/if}
            </div>
            <div>
                Next: <span class="font-mono">{qso.nextMessage}</span>
            </div>
            <div class="mt-1">
                <button
                    type="button"
                    class="btn btn-secondary"
                    onclick={onAbandon}
                    disabled={abandoning}
                >
                    Abandon
                </button>
            </div>
        </div>
    {:else if tx.armed}
        <div class="flex flex-col gap-1">
            <div>
                TX offset:
                <span class="font-mono">{offset !== null ? `${offset} Hz` : '—'}</span>
            </div>
            {#if offset === null}
                <p class="text-xs text-gray-500">Pick a clear offset on the Occupancy tab first.</p>
            {/if}
            <div>
                Message:
                <span class="font-mono">{cqMessage || '— set your callsign in My Station —'}</span>
            </div>
            <div class="mt-1">
                <button type="button" class="btn btn-primary" onclick={callCq} disabled={!canSend || sending}>
                    {tx.transmitting ? 'Transmitting…' : 'Call CQ'}
                </button>
            </div>
            <p class="text-xs text-gray-500">
                Or click a CQ in Band Activity to answer it.
            </p>
        </div>
    {/if}

    <p class="text-xs {tx.error ? 'text-red-700' : 'text-gray-500'}">{statusLine}</p>
</div>
