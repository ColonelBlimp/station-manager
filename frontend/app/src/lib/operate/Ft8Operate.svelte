<script lang="ts">
    // Operate — the FT8 view's most-watched co-primary anchor (ADR 0047): the
    // control center. This increment renders the LIVE exchange from ft8State
    // (working station · slot-timing pill · role-aware message ladder); the TX
    // control bar (Arm / Call CQ / Abandon / Next) + click-to-start is the next
    // increment (first RF from this SPA), kept a deliberate boundary.
    import RunSurface from './RunSurface.svelte';
    import {
        ft8State,
        ft8OperatorCall,
        ft8MyGrid,
        armTx,
        callCq,
        abandonQso,
        skipQso,
        nextAnswerer,
    } from './ft8.svelte';
    import { rig } from './rig.svelte';
    import { buildLadder } from './ft8Ladder';
    import { parseFrequency } from '../validators/frequency';
    import { toasts } from '../ui/toasts.svelte';
    import EnrichmentCard from './EnrichmentCard.svelte';

    const qso = $derived(ft8State.qso);
    const tx = $derived(ft8State.tx);

    const roleLabel = $derived(
        !qso.active
            ? 'Idle'
            : qso.role === 'answerer'
              ? qso.fd
                  ? 'Answering CQ FD'
                  : qso.type4
                    ? 'Answering (compound)'
                    : 'Answering a CQ'
              : qso.role === 'caller'
                ? 'Calling CQ'
                : qso.role === 'worker'
                  ? qso.fd
                      ? 'Working an FD caller'
                      : qso.type4
                        ? 'Working (compound)'
                        : 'Working a caller'
                  : 'Active'
    );

    const ladder = $derived(buildLadder(qso, tx.transmitting, ft8OperatorCall(), ft8MyGrid()));

    // ---- Slot-timing pill: a live countdown to the next 15 s UTC boundary. ----
    // FT8 slots are wall-clock aligned (:00/:15/:30/:45 UTC), so the countdown is a
    // pure function of NOW — NOT ft8State.slot, which lags (it only updates when a
    // decode lands ~13 s into the slot, so counting to that slot's end reads 0 almost
    // at once). Tick sub-second so the boundary flip is caught promptly.
    let now = $state(Date.now());
    $effect(() => {
        const t = setInterval(() => (now = Date.now()), 250);
        return () => clearInterval(t);
    });
    // Whole seconds until the next slot boundary (1–15), off the UTC-aligned 15 s grid.
    const secsLeft = $derived.by(() => {
        const boundary = Math.floor(now / 15_000) * 15_000;
        return Math.max(1, Math.min(15, Math.ceil((boundary + 15_000 - now) / 1000)));
    });
    // 'tx' = transmitting this slot; 'rx' = active but receiving; 'idle' = no session.
    const pillMode = $derived(tx.transmitting ? 'tx' : qso.active ? 'rx' : 'idle');
    // Parity of the slot currently in progress — same 15 s grid as slotParity / the
    // daemon (:00/:30 even, :15/:45 odd). Live off `now`, so it flips at the boundary.
    const nowParity = $derived(Math.floor(now / 15_000) % 2 === 0 ? 'even' : 'odd');
    const pillText = $derived(
        !ft8State.connected
            ? 'Waiting for slot…'
            : pillMode === 'tx'
              ? `Transmit slot (${nowParity}) · listen in ${secsLeft}s`
              : pillMode === 'rx'
                ? `Listen slot (${nowParity}) · transmit in ${secsLeft}s`
                : `Listen · ${nowParity} slot · next ${secsLeft}s`
    );
    const pillClass = $derived(
        pillMode === 'tx'
            ? 'bg-red-600 text-white'
            : 'border border-green-500/40 bg-green-50 text-green-700 dark:bg-green-500/10 dark:text-green-400'
    );

    // A "sent ×N" suffix on the current TX rung while a session is repeating it.
    const repeatSuffix = $derived(
        qso.active && qso.repeats > 0 && ladder.rungs[ladder.step]?.dir === 'tx'
            ? ` · sent ×${qso.repeats}`
            : ''
    );

    // ---- TX control bar (ADR 0029/0030/0031/0033) — first RF from this SPA -----
    // The daemon owns arming + the guaranteed stop + the CQ→73 sequencing; these
    // controls send only intent and reflect confirm-by-push state (ft8State.tx/qso).
    const myCall = $derived(ft8OperatorCall().trim().toUpperCase());
    // CAT connected ⇒ the daemon pushed real rig state, so band/freq are the rig's
    // (not the manual fallback) and the rig can key. All TX gates require it —
    // arming a disconnected rig just 503s.
    const catLive = $derived(rig.cat === 'connected');
    // Dial frequency (selected VFO) in Hz — the daemon logs the contact at the dial
    // (FT8 convention), passed as MHz. parseFrequency takes the rig's dot-grouped
    // form or a hand-typed decimal; parseFloat would misread the grouped form.
    const opFreqHz = $derived(parseFrequency(rig.freq));
    // Where TX will land: the operator's pick, else the daemon's top clear offset.
    const offset = $derived(ft8State.effectiveOffset);
    const offsetAuto = $derived(ft8State.selectedOffset === null);
    const offsetLabel = $derived(
        offset === null ? 'no clear channel yet' : `${offset} Hz${offsetAuto ? ' · auto' : ''}`
    );

    // Arm whenever CAT is live. Call CQ needs armed + idle + a known offset & dial
    // freq + our callsign. Abandon drops any active sequenced session.
    const canArm = $derived(catLive);
    const canSend = $derived(
        tx.armed &&
            !tx.transmitting &&
            !qso.active &&
            offset !== null &&
            opFreqHz !== null &&
            myCall !== '' &&
            catLive
    );
    // Abandon stops a CONTACT or a RUN, and the armed-and-idle state has no contact.
    // Gating on qso.active alone disabled the button precisely where the auto-work
    // indicator advertises it ("Abandon stops the run"), while the daemon accepts the
    // call there and clears the run. Invariant 7 inverted: not offering a stop that
    // cannot act, but withholding one that can.
    const canAbandon = $derived(tx.armed && (qso.active || qso.autoWorkArmed));
    // A Call-CQ run is in progress (we are the caller, looping the pile-up) — the
    // Call CQ button goes red so "I'm running CQ" is unmistakable.
    const callerActive = $derived(qso.active && qso.role === 'caller');
    // Next — move on. Its gate AND its action differ by mode:
    //   - Call-CQ run working an answerer: short-circuit the repeat cap on a stuck
    //     rung. Gated on there being a CONTACT, not on the curated queue: the daemon
    //     picks the replacement from the slot's own decodes, so requiring a queued
    //     station hid the control in exactly the case it is for (one stuck station,
    //     nothing curated). Merely calling CQ offers nothing to move on from.
    //   - working/answering a specific station: a DEFERRED "skip if no reply" (below),
    //     which hands over to the pile-up drain — so that one keeps the queue gate.
    const callerWorking = $derived(callerActive && qso.theirCall !== '');
    const canNext = $derived(
        tx.armed && qso.active && (callerActive ? callerWorking : qso.queue.length > 0)
    );

    // Deferred "skip if no reply" (the Next control while working a station) —
    // DAEMON-SIDE since 2026-07-13: Next arms skip_if_silent on the sequencer,
    // which ends the session INSTEAD of keying the repeat when the station stays
    // silent (the old SPA-side resolve could only abandon a repeat already on
    // the air — an audible PTT tick + wasted RF at a no-show). The armed state
    // renders from the ft8-qso SSE (confirm-by-push); the watcher effect below
    // turns its falling edge into the operator toasts + the drain hand-off.
    const skipArmed = $derived(qso.skipArmed);
    // The watcher's own memory (deliberately plain lets, not $state — nothing
    // renders from them; they exist to detect the falling edge).
    let prevSkipArmed = false;
    let prevSkipCall = '';

    // Per-action in-flight latches. The daemon single-flights competing starts and
    // 409s the loser, so these mainly stop a double-tap issuing a wasted second POST.
    let arming = $state(false);
    let sending = $state(false);
    let abandoning = $state(false);
    let nexting = $state(false);

    async function onArm(): Promise<void> {
        if (arming) return;
        arming = true;
        try {
            const r = await armTx(!tx.armed);
            if (!r.ok) toasts.error(r.message);
            // The armed state itself arrives via the ft8-tx SSE (confirm-by-push).
        } finally {
            arming = false;
        }
    }

    async function onCallCq(): Promise<void> {
        if (sending || offset === null || opFreqHz === null || myCall === '') return;
        sending = true;
        try {
            const r = await callCq(offset, opFreqHz / 1_000_000, ft8State.txParity);
            if (!r.ok) toasts.error(r.message);
        } finally {
            sending = false;
        }
    }

    // Abandon — the full stop. Drop the in-flight TX (the daemon cancels it + idles the
    // sequencer) AND pause the drain so the queue does NOT immediately take over: the
    // operator regains control, the queue is kept, and Resume (drawer) continues it.
    // Also clears any armed skip — Abandon supersedes it. Without the pause, abandoning
    // a pile-up contact just handed straight to the drain (the next caller's opening
    // fired in the same slot, within the sequencer's ~4.5s late window) — i.e. Abandon
    // behaved like Next.
    async function onAbandon(): Promise<void> {
        if (abandoning) return;
        abandoning = true;
        // Abandon supersedes an armed skip: the daemon clears the arm itself; the
        // watcher must NOT read that falling edge as "skip fired" (it would toast
        // and resume the drain that Abandon is about to pause).
        prevSkipArmed = false;
        try {
            const r = await abandonQso();
            if (!r.ok) toasts.error(r.message);
        } finally {
            abandoning = false;
        }
    }

    // Next — advance the pile-up (see canNext). A click while a skip is already armed
    // cancels it. During a Call-CQ run it's an immediate takeover; otherwise it arms a
    // deferred skip that the resolve effect below fires only once the RX outcome is
    // known — so Next never keys the next caller "blind" right after your TX.
    function onNext(): void {
        if (nexting) return;
        if (skipArmed) {
            void setSkip(false); // second click cancels the pending skip
            return;
        }
        if (!canNext) return;
        if (callerWorking) {
            void moveOn(); // Call-CQ: park this answerer, the run carries on
            return;
        }
        void setSkip(true);
    }

    // Call-CQ Next. Posts the park verb and nothing else: an ancestor of this
    // used to abandon the run and hand over to the SPA drain, which quietly
    // switched the operator from calling CQ to working their curated queue. The
    // queue is daemon state now (ADR 0067) and the daemon's own drain decides
    // what follows a parked answerer.
    async function moveOn(): Promise<void> {
        if (nexting) return;
        nexting = true;
        try {
            const r = await nextAnswerer();
            if (!r.ok) toasts.error(r.message);
            // The pending state arrives via the ft8-qso SSE (confirm-by-push).
        } finally {
            nexting = false;
        }
    }

    async function setSkip(armed: boolean): Promise<void> {
        const r = await skipQso(armed);
        if (!r.ok) toasts.error(r.message);
        // The armed state itself arrives via the ft8-qso SSE (confirm-by-push).
    }

    // Skip-outcome watcher (the resolve logic lives daemon-side now): a falling
    // skip_armed edge means either the skip FIRED (session ended without a repeat
    // — toast + hand the drain the next caller) or the station REPLIED (the
    // daemon disarmed; keep working them). Abandon suppresses the edge above.
    $effect(() => {
        const armed = qso.skipArmed;
        const active = qso.active;
        const call = qso.theirCall;
        if (prevSkipArmed && !armed) {
            if (!active) {
                toasts.info(`No reply from ${prevSkipCall} — next.`);
            } else if (call === prevSkipCall) {
                toasts.info(`${call} replied — continuing.`);
            }
        }
        prevSkipArmed = armed;
        if (armed) prevSkipCall = call;
    });

    // The pile-up drain is DAEMON state since ADR 0067 (slice B): bagged
    // stations are worked by the sequencer's own drain, in bag order, with
    // Stop-pauses/Resume semantics. The SPA-side curated stack and its drain
    // $effect retired with it.
</script>

<section class="flex h-full flex-col overflow-hidden rounded-xl border border-line bg-surface">
    <!-- h-10: fixed header height shared with Band Activity (see its header
         comment) so the two cards' header rules align. -->
    <div class="flex h-10 shrink-0 items-center gap-x-3 border-b border-line px-4">
        <h3 class="text-sm font-semibold text-ink">Operate</h3>
        <span class="ml-auto text-xs font-semibold text-muted">{roleLabel}</span>
    </div>

    <div class="flex-1 overflow-auto p-4">
        <!-- Enrichment zone — reserved at the enrichment-panel height (h-45 / 180px)
             so the slot pill + ladder below never reflow. The reused EnrichmentCard
             (same w-56 h-45 frame the logging card gives it) sits on the left,
             observing the worked station (blank when idle, exactly like Phone/CW);
             the worked call + role sit in the column beside it. -->
        <div class="flex h-45 gap-x-3">
            <div class="w-56 shrink-0">
                <EnrichmentCard call={qso.theirCall} />
            </div>
            <div class="min-w-0 flex-1">
                {#if qso.active && qso.theirCall !== ''}
                    <div class="font-mono text-lg font-extrabold tracking-wide text-ink">
                        {qso.theirCall}
                    </div>
                    <div class="mt-0.5 text-xs text-muted">{roleLabel}</div>
                {:else}
                    <div class="text-sm text-muted">No active contact</div>
                {/if}
                <!-- The RUN SURFACE (ADR 0067) — one home for the run lifecycle,
                     replacing the checkbox/chip morph that used to live here. -->
                <RunSurface />
            </div>
        </div>

        <hr class="my-3 border-line" />

        <!-- Slot-timing pill, directly above the rungs (the rungs are the slots) -->
        <div class="mb-3 text-center">
            <span
                class="inline-flex items-center gap-x-2 rounded-full px-3.5 py-1 text-xs font-bold tabular-nums {pillClass}"
            >
                {pillText}
            </span>
        </div>

        <!-- Message ladder -->
        <ul class="space-y-0.5 font-mono text-sm">
            {#each ladder.rungs as rung, i (i)}
                <li
                    class="flex items-center justify-between rounded-md border border-transparent px-2.5 py-1.5 {i ===
                    ladder.step
                        ? 'border-focus bg-nav-accent-bg font-bold text-nav-accent-fg'
                        : i < ladder.step
                          ? 'text-muted'
                          : 'text-ink'}"
                >
                    <span class="overflow-hidden text-nowrap text-ellipsis">
                        <span class="mr-1.5 text-[10px] font-bold text-muted uppercase"
                            >{rung.dir}</span
                        >{rung.text}
                    </span>
                    {#if i < ladder.step}
                        <span class="text-logged">✓</span>
                    {:else if i === ladder.step}
                        <span class="text-xs font-bold text-nav-accent-fg">
                            ◀ {tx.transmitting ? 'transmitting' : 'this slot'}{repeatSuffix}
                        </span>
                    {/if}
                </li>
            {/each}
        </ul>
    </div>

    <!-- TX control bar (ADR 0029/0030/0031/0033) — first RF from this SPA. Call CQ /
         Abandon drive the sequenced session; Next advances the pile-up (shown only
         when a caller is queued); Arm sits alone at the bottom (its own divider) as
         the operator's explicit consent to key. -->
    <div class="border-t border-line px-4 py-3">
        <!-- The CQ-slot parity — genuinely CQ-scoped, so it stays beside Call
             CQ (ADR 0067: the Answer mode moved into the run surface; the TX
             offset readout lives in the Occupancy panel, its no-offset
             explanation in the Call CQ button title). -->
        <div class="mb-2 flex items-center justify-end gap-x-4 text-xs">
            <label class="flex items-center gap-x-1 text-muted">
                <span>CQ slot</span>
                <select
                    class="rounded border border-line bg-surface px-1 py-0.5 text-xs text-ink disabled:opacity-50"
                    bind:value={ft8State.txParity}
                    disabled={qso.active}
                    aria-label="Call CQ slot parity"
                >
                    <option value="next">Next</option>
                    <option value="even">Even</option>
                    <option value="odd">Odd</option>
                </select>
            </label>
        </div>
        <div class="flex gap-x-2">
            <button
                type="button"
                class="flex-1 rounded-md px-3 py-1.5 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50 {callerActive
                    ? 'bg-red-600 text-white hover:bg-red-700'
                    : 'bg-focus text-surface hover:opacity-90'}"
                onclick={onCallCq}
                disabled={!canSend || sending}
                title={offset === null
                    ? 'No clear channel yet — the occupancy scan picks the TX offset'
                    : `TX offset ${offsetLabel}`}
            >
                {callerActive ? 'Calling CQ…' : 'Call CQ'}
            </button>
            <button
                type="button"
                class="flex-1 rounded-md border border-line px-3 py-1.5 text-sm font-semibold text-ink hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-50"
                onclick={onAbandon}
                disabled={!canAbandon || abandoning}
            >
                Abandon
            </button>
            {#if canNext || skipArmed || qso.nextArmed}
                <button
                    type="button"
                    class="flex-1 rounded-md border px-3 py-1.5 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50 {skipArmed
                        ? 'border-amber-500 bg-amber-500/15 text-amber-700 dark:text-amber-400'
                        : 'border-line text-ink hover:bg-surface-muted'}"
                    title={skipArmed
                        ? 'Skip armed — advances to the next caller if this station is silent this slot; click to cancel'
                        : callerActive
                          ? 'Move on from this station — the CQ run carries on'
                          : "Skip this station if it doesn't reply this slot"}
                    onclick={onNext}
                    disabled={nexting}
                >
                    {qso.nextArmed ? 'Moving on…' : skipArmed ? 'Skip if silent…' : 'Next'}
                </button>
            {/if}
        </div>
        <hr class="my-3 border-line" />
        <button
            type="button"
            class="w-full rounded-md border px-3 py-2 text-sm font-bold disabled:cursor-not-allowed disabled:opacity-50 {tx.armed
                ? 'border-red-600 bg-red-600 text-white hover:bg-red-700'
                : 'border-focus text-focus hover:bg-nav-accent-bg'}"
            onclick={onArm}
            disabled={!canArm || arming}
        >
            {tx.armed ? 'Disable TX' : 'Enable TX'}
        </button>
    </div>
</section>
