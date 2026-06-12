<script lang="ts">
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

    // ---- Message ladder --------------------------------------------------------
    // One slot per row, top to bottom: our TX messages interleaved with the remote
    // station's expected responses (rx); the highlighted row is the current slot.
    // Unknowns are placeholders — <DX> (their call), <GRID> (locator), <RST> (report).
    // Two ladders, branched on qso.active:
    //   - answer-a-CQ (qso.active, e3): the REAL exchange we drive — our rungs are
    //     grid → R-report → 73, advancing on the daemon's qso.state.
    //   - call-CQ (idle + armed): still PRESENTATIONAL — calling-CQ sequencing is the
    //     deferred caller-side scope (Call CQ is single-shot today). Its highlight is
    //     borrowed from the qso.state machine so it stays demoable via ?__ft8demo=1..4
    //     until the caller-side driver lands.
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

    // Answer-a-CQ ladder (qso.active): we are the ANSWERING station — our rungs are
    // grid → R-report → 73 (tx rows 0/2/4), interleaved with the worked station's
    // replies. RST values aren't exposed to the SPA, so reports stay <RST>.
    const answerLadder: { dir: 'tx' | 'rx'; text: string }[] = $derived([
        { dir: 'tx', text: `${dxCall} ${myCall}${myGrid ? ` ${myGrid}` : ' <GRID>'}` },
        { dir: 'rx', text: `${myCall} ${dxCall} <RST>` },
        { dir: 'tx', text: `${dxCall} ${myCall} R<RST>` },
        { dir: 'rx', text: `${myCall} ${dxCall} RR73` },
        { dir: 'tx', text: `${dxCall} ${myCall} 73` },
    ]);
    const answerStep = $derived.by(() => {
        const txRow = qso.state === 'reporting' ? 2 : qso.state === 'confirming' ? 4 : 0;
        const next = tx.transmitting ? txRow : txRow + 1;
        return Math.min(next, answerLadder.length - 1);
    });

    // Rendered ladder + highlight: the real answer ladder while answering a CQ,
    // else the presentational caller ladder (armed + idle).
    const ladder = $derived(qso.active ? answerLadder : callerLadder);
    const ladderStep = $derived(qso.active ? answerStep : callerStep);

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

    // Status line under the controls.
    const statusLine = $derived.by(() => {
        if (!tx.armed) return 'Tx disabled.';
        if (tx.transmitting)
            return `Transmitting ${tx.message || cqMessage} @ ${tx.offsetHz || offset} Hz…`;
        if (tx.error) return `Last transmission failed (${tx.error}).`;
        return 'Tx Enabled — ready.';
    });
</script>

<div class="flex flex-col text-sm text-gray-700 h-44 mt-4">
    <div class="flex flex-row h-46">
        <div class="w-full px-2">
            {#if tx.armed}
                <div
                    class="flex flex-col py-0 font-mono text-sm text-left border border-gray-300 rounded"
                >
                    {#each ladder as m, i (i)}
                        <div
                            class="items-center h-6 flex gap-x-2 rounded px-2 {i === ladderStep
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
        <div class="flex flex-col gap-1 w-50 z-10">
            <div class="flex flex-col gap-y-2 h-34">
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
                    {tx.armed ? 'Disable Tx' : 'Enable Tx'}
                </button>
            </div>
        </div>
    </div>
    <div class="z-0 flex flex-col items-center -mt-7.5">{statusLine}</div>
</div>
