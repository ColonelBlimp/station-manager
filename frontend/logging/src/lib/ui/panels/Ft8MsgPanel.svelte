<script lang="ts">
    import { ft8State } from '../../states/ft8.svelte';
    import { ft8PileupStack } from '../../states/ft8PileupStack.svelte';
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { catState } from '../../states/cat.svelte';
    import { armFt8Tx } from '../../api/ft8tx';
    import { abandonFt8Qso, startFt8Cq } from '../../api/ft8qso';
    import { toasts } from '../../states/toasts.svelte';

    /*
        FT8 transmit (ADR 0029/0030/0031/0033). The daemon owns the TX path (arm gate
        + guaranteed stop) and the sequencer; this panel is the SPA surface:
          - Arm/Disarm (always).
          - A sequenced session is driven by the daemon and shown live via
            ft8State.qso (role + rung); the operator only watches + can Abandon:
              · answer-a-CQ (role "answerer", started by clicking a CQ in Band
                Activity, step e3): our rungs grid → R-report → 73.
              · call-CQ (role "caller", started by the Call CQ button, ADR 0033):
                we call CQ and work the stations that answer, looping the pile-up.
          - When idle + armed: Call CQ starts a caller session on the picked offset.
        Arm/start/abandon go through lib/api; the daemon confirms by push, so the UI
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
    // The rig dial frequency (selected VFO), in Hz; the daemon logs each contact at
    // the dial frequency (FT8 convention — not dial+offset), so Call CQ passes
    // opFreq / 1e6 MHz.
    const opFreq = $derived(
        displayedState.selectedVfo === 'B' ? displayedState.vfoB : displayedState.vfoA
    );
    // Arming a disconnected rig just 503s; gate on the live-rig signal so the
    // control is only offered when it can work.
    const canArm = $derived(displayedState.isLive);
    // A real, rig-reported dial frequency must be known before we start anything that
    // logs — otherwise the QSO records the placeholder band (the 14.250 bug). FT8 keys
    // through a live rig, but it can be "responding" before its frequency poll lands.
    const freqKnown = $derived(displayedState.isLive && catState.freqKnown);
    // Call CQ starts a caller session — offered only when armed, idle (no session in
    // flight), an offset is picked, we have a callsign, and the dial frequency is known.
    const canSend = $derived(
        tx.armed &&
            !tx.transmitting &&
            !qso.active &&
            offset !== null &&
            cqMessage !== '' &&
            freqKnown
    );
    // Abandon is enabled whenever a sequenced session is active (answer-a-CQ or
    // call-CQ) and TX is armed — abandonFt8Qso drops either.
    const canAbandon = $derived(tx.armed && qso.active);
    // A Call-CQ session is in progress (we are the caller, looping the pile-up). The
    // Call CQ button turns red while this holds so "I'm running CQ" is unmistakable —
    // distinct from the per-slot ON AIR indicator (Ft8Panel) that pulses during TX.
    const callerActive = $derived(qso.active && qso.role === 'caller');
    // "Next" is offered only mid-contact AND with stations still queued — it drops
    // the current exchange and lets the drain advance to the next caller. NOT in a
    // Call-CQ run (!callerActive): the daemon's auto-pick is the sole controller
    // there, so a leftover pile-up queue must not light Next — clicking it abandons
    // the whole CQ run and hands the rig to a drain that never resumes CQ. Enqueueing
    // is already blocked in caller mode; this guards a pre-existing queue.
    const canNext = $derived(canAbandon && ft8PileupStack.count > 0 && !callerActive);

    // ---- Message ladder --------------------------------------------------------
    // One slot per row, top to bottom: our TX messages interleaved with the remote
    // station's expected responses (rx); the highlighted row is the current slot.
    // Unknowns are placeholders — <DX> (their call), <GRID> (locator), <RST> (report).
    // Three ladders, branched on the session role (ft8State.qso.role):
    //   - answer-a-CQ ("answerer", e3): our rungs are grid → R-report → 73.
    //   - call-CQ ("caller", ADR 0033): CQ → report → RR73, advancing on the daemon's
    //     qso.state (calling-cq → reporting → rogering).
    //   - work-a-caller ("worker", ADR 0033): a station called US, so there is NO CQ
    //     row — the opening is THEIR call to us; then report → RR73. Same exchange as
    //     the caller side from the report rung on, just without the CQ phase.
    // All are LIVE — driven by the daemon's qso.state. When idle the caller ladder is
    // a static preview (the CQ row highlighted).
    const dxCall = $derived(qso.theirCall || '<DX>');
    // The worked station's grid (daemon-supplied), for the ladder's opening row; the
    // <GRID> placeholder only while it's still unknown (the idle caller preview).
    const dxGrid = $derived(qso.theirGrid || '<GRID>');
    // Real reports once the daemon knows them (qso.our_report / their_report),
    // else the <RST> placeholder. ourRst = the report WE send; theirRst = theirs.
    const ourRst = $derived(qso.ourReport || '<RST>');
    const theirRst = $derived(qso.theirReport || '<RST>');
    const callerLadder: { dir: 'tx' | 'rx'; text: string }[] = $derived([
        { dir: 'tx', text: cqMessage },
        { dir: 'rx', text: `${myCall} ${dxCall} ${dxGrid}` },
        { dir: 'tx', text: `${dxCall} ${myCall} ${ourRst}` },
        { dir: 'rx', text: `${myCall} ${dxCall} R${theirRst}` },
        { dir: 'tx', text: `${dxCall} ${myCall} RR73` },
        { dir: 'rx', text: `${myCall} ${dxCall} 73` },
    ]);

    // The highlighted ("current") ladder row for a TX rung at index txRow. While
    // transmitting — OR before we've sent this rung at all (repeats === 0, e.g. the
    // instant an exchange opens, before our first key) — the TX rung is current. Only
    // once it's been sent and we're waiting for the reply (repeats > 0 and not
    // transmitting) does the RX row just below become current. Without the repeats
    // guard the reply row flashes as current at the very start of an exchange (the
    // daemon publishes the opening rung with repeats=0 + transmitting=false before the
    // first transmit fires).
    const rowFor = (txRow: number, len: number): number =>
        Math.min(!tx.transmitting && qso.repeats > 0 ? txRow + 1 : txRow, len - 1);

    // The caller's TX rungs are rows 0 (calling-cq) / 2 (reporting) / 4 (rogering).
    // Idle (no session) sits on the CQ row.
    const callerStep = $derived.by(() => {
        const txRow = qso.state === 'reporting' ? 2 : qso.state === 'rogering' ? 4 : 0;
        if (!qso.active) return txRow;
        return rowFor(txRow, callerLadder.length);
    });

    // Work-a-caller ladder (role "worker"): the caller ladder with the CQ row dropped —
    // the opening is the station's call to US (their grid), then our report → RR73. No
    // calling-cq state here; the exchange starts at "reporting".
    const workLadder: { dir: 'tx' | 'rx'; text: string }[] = $derived([
        { dir: 'rx', text: `${myCall} ${dxCall} ${dxGrid}` },
        { dir: 'tx', text: `${dxCall} ${myCall} ${ourRst}` },
        { dir: 'rx', text: `${myCall} ${dxCall} R${theirRst}` },
        { dir: 'tx', text: `${dxCall} ${myCall} RR73` },
        { dir: 'rx', text: `${myCall} ${dxCall} 73` },
    ]);
    // Our TX rungs are rows 1 (reporting) / 3 (rogering). Opening defaults to the
    // report rung — and rowFor keeps row 1 current until our report actually keys,
    // rather than jumping to the reply row (row 2) the instant the exchange opens.
    const workStep = $derived.by(() => {
        const txRow = qso.state === 'rogering' ? 3 : 1;
        return rowFor(txRow, workLadder.length);
    });

    // Answer-a-CQ ladder (qso.active): we are the ANSWERING station — our rungs are
    // grid → R-report → 73 (tx rows 0/2/4), interleaved with the worked station's
    // replies. Reports fill from qso.our_report / their_report once known.
    const answerLadder: { dir: 'tx' | 'rx'; text: string }[] = $derived([
        { dir: 'tx', text: `${dxCall} ${myCall}${myGrid ? ` ${myGrid}` : ' <GRID>'}` },
        { dir: 'rx', text: `${myCall} ${dxCall} ${theirRst}` },
        { dir: 'tx', text: `${dxCall} ${myCall} R${ourRst}` },
        { dir: 'rx', text: `${myCall} ${dxCall} RR73` },
        { dir: 'tx', text: `${dxCall} ${myCall} 73` },
    ]);
    const answerStep = $derived.by(() => {
        const txRow = qso.state === 'reporting' ? 2 : qso.state === 'confirming' ? 4 : 0;
        return rowFor(txRow, answerLadder.length);
    });

    // ---- ARRL Field Day ladders (qso.fd) --------------------------------------
    // FD carries class + section instead of grid + report, so the rungs differ from the
    // standard ladders above. Our class/section are known from config at start; theirs
    // fill from qso.theirClass/theirSection once known (the answerer learns them in the
    // R-exchange; the worker has them from the picked call). role is still
    // answerer (answer a CQ FD) / worker (work an FD caller).
    const ourCls = $derived(qso.ourClass || '<CLS>');
    const ourSec = $derived(qso.ourSection || '<SEC>');
    const theirCls = $derived(qso.theirClass || '<CLS>');
    const theirSec = $derived(qso.theirSection || '<SEC>');
    // Answer-a-CQ-FD: our exchange → their R+exchange → our RR73 (tx rows 0/2; state
    // calling → rogering).
    const fdAnswerLadder: { dir: 'tx' | 'rx'; text: string }[] = $derived([
        { dir: 'tx', text: `${dxCall} ${myCall} ${ourCls} ${ourSec}` },
        { dir: 'rx', text: `${myCall} ${dxCall} R ${theirCls} ${theirSec}` },
        { dir: 'tx', text: `${dxCall} ${myCall} RR73` },
    ]);
    const fdAnswerStep = $derived.by(() =>
        rowFor(qso.state === 'rogering' ? 2 : 0, fdAnswerLadder.length)
    );
    // Work-a-caller-FD: their call to us (their exchange) → our R+exchange → their RR73
    // → our RR73 (tx rows 1/3; state reporting → rogering).
    const fdWorkLadder: { dir: 'tx' | 'rx'; text: string }[] = $derived([
        { dir: 'rx', text: `${myCall} ${dxCall} ${theirCls} ${theirSec}` },
        { dir: 'tx', text: `${dxCall} ${myCall} R ${ourCls} ${ourSec}` },
        { dir: 'rx', text: `${myCall} ${dxCall} RR73` },
        { dir: 'tx', text: `${dxCall} ${myCall} RR73` },
    ]);
    const fdWorkStep = $derived.by(() =>
        rowFor(qso.state === 'rogering' ? 3 : 1, fdWorkLadder.length)
    );

    // Rendered ladder + highlight, by role: a Field Day session (qso.fd) uses the FD
    // answer/work ladder; otherwise answerer → answer ladder, worker → the no-CQ
    // work-a-caller ladder, else the caller ladder (live while calling CQ, or a static
    // preview when idle).
    const answering = $derived(qso.active && qso.role === 'answerer');
    const working = $derived(qso.active && qso.role === 'worker');
    const ladder = $derived(
        qso.fd && answering
            ? fdAnswerLadder
            : qso.fd && working
              ? fdWorkLadder
              : answering
                ? answerLadder
                : working
                  ? workLadder
                  : callerLadder
    );
    const ladderStep = $derived(
        qso.fd && answering
            ? fdAnswerStep
            : qso.fd && working
              ? fdWorkStep
              : answering
                ? answerStep
                : working
                  ? workStep
                  : callerStep
    );

    let arming = $state(false);
    let sending = $state(false);
    let abandoning = $state(false);

    async function onAbandon(): Promise<void> {
        if (abandoning) return;
        abandoning = true;
        // Pause pile-up auto-drain too: Abandon should stop the run, not silently jump
        // to the next queued caller. The queue is kept (Resume on the drawer restarts).
        ft8PileupStack.pause();
        try {
            const out = await abandonFt8Qso();
            if (out.kind !== 'ok') toasts.error(out.message);
        } finally {
            abandoning = false;
        }
    }

    let nexting = $state(false);

    // "Next" — abort the in-flight pile-up contact and move straight to the next
    // queued station. Unlike Abandon it does NOT pause the drain, so the Ft8Panel
    // drain effect picks up the next head as soon as this contact goes idle. Lets
    // the operator ditch a no-show after a rung or two instead of burning the full
    // max_repeats backstop — without touching that config (it governs CQ /
    // answer-a-CQ). The current station was dequeued when it started being worked,
    // so aborting just drops it; nothing extra to expel.
    //
    // Only acts BETWEEN transmissions (gated on !tx.transmitting, both here and on the
    // button): clicking Next mid-slot used to abandon the contact while the current
    // slot played out AND then immediately key the next caller — so the tail of one
    // station's message ran straight into a transmission for a different callsign.
    // Advancing only during the RX half means the drop lands cleanly and the next
    // caller opens on the following TX slot.
    async function onNext(): Promise<void> {
        if (nexting || tx.transmitting) return;
        nexting = true;
        try {
            const out = await abandonFt8Qso();
            if (out.kind !== 'ok') toasts.error(out.message);
        } finally {
            nexting = false;
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
            // Start a sequenced Call-CQ session (ADR 0033). The daemon resolves our
            // callsign/grid from config; we pass the offset + dial freq for logging,
            // and the chosen CQ slot parity (next/even/odd, WSJT-X "Tx even/1st").
            const out = await startFt8Cq(offset, opFreq / 1_000_000, ft8State.txParity);
            if (out.kind !== 'ok') toasts.error(out.message);
        } finally {
            sending = false;
        }
    }

    // Status line under the controls. Commented out alongside its render (search
    // for the matching `{statusLine}` block in the markup) while the layout is being
    // reworked — re-enable both together.
    // const statusLine = $derived.by(() => {
    //     if (!tx.armed) return 'Tx disabled.';
    //     if (tx.transmitting)
    //         return `Transmitting ${tx.message || cqMessage} @ ${tx.offsetHz || offset} Hz…`;
    //     if (tx.error) return `Last transmission failed (${tx.error}).`;
    //     return 'Tx Enabled — ready.';
    // });
</script>

<div class="flex flex-col text-sm text-gray-700 ft8-info-panel-height pt-4">
    <div class="flex flex-row h-46">
        <div class="flex w-100">
            {#if tx.armed}
                <div
                    class="w-100 h-31 flex flex-col py-0 font-mono text-xs text-left border border-gray-300 rounded"
                >
                    {#each ladder as m, i (i)}
                        <div
                            class="items-center h-5 flex gap-x-2 rounded px-2 {i === ladderStep
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
        <div class="flex flex-col gap-1 px-5 w-50 z-10">
            <div class="flex flex-col gap-y-2 h-29">
                <button
                    type="button"
                    class="btn {callerActive
                        ? 'bg-red-600 text-white hover:bg-red-700'
                        : 'btn-primary'}"
                    onclick={callCq}
                    disabled={!canSend || sending}
                >
                    {callerActive ? 'Calling CQ…' : 'Call CQ'}
                </button>
                <button
                    type="button"
                    class="btn btn-secondary"
                    onclick={onAbandon}
                    disabled={!canAbandon || abandoning}
                >
                    Abandon
                </button>
                {#if canNext}
                    <button
                        type="button"
                        class="btn btn-secondary"
                        title={tx.transmitting
                            ? 'Wait for receive — Next advances between transmissions'
                            : 'Drop this station and work the next in the pile-up'}
                        onclick={onNext}
                        disabled={nexting || tx.transmitting}
                    >
                        Next
                    </button>
                {/if}
            </div>
        </div>
        <div class="flex flex-col w-36 mt-1">
            <!-- Call-CQ slot parity (WSJT-X "Tx even/1st"). 'Next' fires on the next
                 slot (fastest); Even/Odd wait for that parity. Applies to the next
                 Call CQ — locked while a session is active (parity is fixed at start).
                 Caller-side only; answering a CQ forces the opposite parity. -->
            <label class="flex items-center justify-between gap-1 text-xs text-gray-600">
                <span title="Which 15-second slot parity to call CQ in">CQ slot</span>
                <select
                    class="rounded border border-gray-300 px-1 py-0.5 text-xs focus:border-indigo-500 focus:outline-none disabled:opacity-50"
                    bind:value={ft8State.txParity}
                    onchange={(e) =>
                        ft8State.setTxParity(e.currentTarget.value as 'next' | 'even' | 'odd')}
                    disabled={qso.active}
                    aria-label="Call CQ slot parity"
                >
                    <option value="next">Next slot</option>
                    <option value="even">Even :00/:30</option>
                    <option value="odd">Odd :15/:45</option>
                </select>
            </label>
        </div>
        <div class="flex flex-col w-48 items-center mt-4.5">
            <button
                type="button"
                onclick={toggleArm}
                disabled={arming || !canArm}
                class="h-23 w-38 rounded text-base font-semibold cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed {tx.armed
                    ? 'bg-red-600 text-white hover:bg-red-700'
                    : 'bg-focus text-surface hover:opacity-90'}"
            >
                {tx.armed ? 'Disable Tx' : 'Enable Tx'}
            </button>
        </div>
    </div>
    <!-- div class="z-0 flex flex-col items-center -mt-7.5">{statusLine}</div-->
</div>
