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
    // FT8 standard messages carry only the 4-char Maidenhead field, so trim a
    // longer configured locator (e.g. IO91wm → IO91) for the on-air message.
    const myGrid = $derived(
        configState.loggingStation.myGridsquare.trim().toUpperCase().slice(0, 4)
    );
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
    // Abandon stays disabled until there is a sequenced exchange to bail out of —
    // answering a CQ (qso.active), the only thing abandonFt8Qso can cancel today.
    // Always disabled when TX isn't armed. A single-shot Call CQ is deliberately
    // NOT covered (it has no abandonable state); calling-CQ sequencing is the
    // deferred call-CQ scope, and qso.active will extend to it when it lands.
    const canAbandon = $derived(tx.armed && qso.active);

    // ---- Call-CQ message ladder (presentational) ------------------------------
    // The full call-CQ exchange, one slot per row top to bottom: our TX messages
    // interleaved with the remote station's expected responses (rx). Unknowns are
    // placeholders — <DX> (their callsign), <GRID> (their locator), <RST> (a
    // signal report) — that resolve as the QSO progresses. The highlighted row is
    // our message for the current slot. NOTE: calling-CQ sequencing is the
    // deferred call-CQ scope (today Call CQ is single-shot), so for now the active
    // rung is borrowed from the answer-a-CQ qso.state machine
    // (calling→reporting→confirming) — that makes the highlight demoable via
    // ?__ft8demo=1..4; the real call-CQ driver replaces it when that backend lands.
    const dxCall = $derived(qso.theirCall || '<DX>');
    const callerLadder: { dir: 'tx' | 'rx'; text: string }[] = $derived([
        { dir: 'tx', text: cqMessage },
        { dir: 'rx', text: `${myCall} ${dxCall} <GRID>` },
        { dir: 'tx', text: `${dxCall} ${myCall} <RST>` },
        { dir: 'rx', text: `${myCall} ${dxCall} R<RST>` },
        { dir: 'tx', text: `${dxCall} ${myCall} RR73` },
        { dir: 'rx', text: `${myCall} ${dxCall} 73` },
    ]);
    // The highlighted row is the message for the CURRENT slot. qso.state names our
    // transmit rung (tx rows 0 / 2 / 4); while we're actually transmitting it, that
    // TX row is current — but between transmissions, when we're listening for the
    // remote's reply, the current row is the RX row just below (1 / 3 / 5).
    // Idle/armed (no exchange yet) sits on the CQ row.
    const callerStep = $derived.by(() => {
        const txRow = qso.state === 'reporting' ? 2 : qso.state === 'confirming' ? 4 : 0;
        if (!qso.active) return txRow;
        return tx.transmitting ? txRow : txRow + 1;
    });

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
        if (!tx.armed) return 'TX disabled.';
        if (tx.transmitting)
            return `Transmitting ${tx.message || cqMessage} @ ${tx.offsetHz || offset} Hz…`;
        if (tx.error) return `Last transmission failed (${tx.error}).`;
        return 'TX Enabled — ready.';
    });
</script>

<div class="flex flex-col text-sm text-gray-700 h-56">
    <h3 class="text-center my-1 font-semibold text-lg w-full">Next slot in {secondsToNextSlot}s</h3>
    <div class="flex flex-row h-46">
        <div class="w-full px-2">
            {#if tx.armed}
                <div
                    class="flex flex-col py-0 font-mono text-sm text-left border border-gray-300 rounded"
                >
                    {#each callerLadder as m, i (i)}
                        <div
                            class="items-center h-6 flex gap-x-2 rounded px-2 {i === callerStep
                                ? 'bg-indigo-100 font-semibold text-indigo-800'
                                : m.dir === 'rx'
                                  ? 'italic text-gray-400'
                                  : 'text-gray-600'}"
                        >
                            <span class="w-6 shrink-0 text-xs uppercase opacity-70">{m.dir}</span>
                            <span>{m.text}</span>
                        </div>
                    {/each}
                </div>
            {:else}
                <p class="text-xs text-gray-400">Enable TX to call CQ.</p>
            {/if}
        </div>
        <div class="flex flex-col gap-1 w-50 z-10 border">
            <div class="flex flex-col gap-y-2 h-80">
                <button
                    type="button"
                    class="btn btn-primary"
                    onclick={callCq}
                    disabled={!canSend || sending}
                >
                    {tx.transmitting && !qso.active ? 'Transmitting…' : 'Call CQ'}
                </button>
                <button
                    type="button"
                    class="btn btn-secondary"
                    onclick={onAbandon}
                    disabled={!canAbandon || abandoning}
                >
                    Abandon
                </button>
            </div>
            <div>
                <button
                    type="button"
                    onclick={toggleArm}
                    disabled={arming || !canArm}
                    class="h-8 w-41 rounded px-3 py-1.5 text-sm font-medium cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed {tx.armed
                        ? 'bg-red-600 text-white hover:bg-red-700'
                        : 'bg-focus text-surface hover:opacity-90'}"
                >
                    {tx.armed ? 'Disable TX' : 'Enable TX'}
                </button>
            </div>
        </div>
    </div>
    <div class="z-0 flex flex-col items-center -mt-7.5">{statusLine}</div>
</div>
