<script lang="ts">
    // Operate — the FT8 view's most-watched co-primary anchor (ADR 0047): the
    // control center. This increment renders the LIVE exchange from ft8State
    // (working station · slot-timing pill · role-aware message ladder); the TX
    // control bar (Arm / Call CQ / Abandon / Next) + click-to-start is the next
    // increment (first RF from this SPA), kept a deliberate boundary.
    import {
        ft8State,
        ft8OperatorCall,
        ft8MyGrid,
        armTx,
        callCq,
        abandonQso,
        skipQso,
        nextAnswerer,
        ft8EngagedThisSession,
        workCaller,
        stopAutoWork,
        ft8AutoWorkIntent,
    } from './ft8.svelte';
    import { rig } from './rig.svelte';
    import { session } from './session.svelte';
    import { ft8PileupStack } from './ft8Pileup.svelte';
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
        tx.armed && qso.active && (callerActive ? callerWorking : ft8PileupStack.count > 0)
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
            else ft8PileupStack.pause();
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

    // Call-CQ Next. Deliberately does NOT touch ft8PileupStack: this used to abandon
    // the run and resume the drain, which quietly switched the operator from calling
    // CQ to working their curated queue. The drawer is for a pile-up that did not come
    // from a CQ call, and the drain cannot run during a live session anyway.
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
                ft8PileupStack.resume();
            } else if (call === prevSkipCall) {
                toasts.info(`${call} replied — continuing.`);
            }
        }
        prevSkipArmed = armed;
        if (armed) prevSkipCall = call;
    });

    // ---- Pile-up drain (SPA-only) ---------------------------------------------
    // The operator curates a FIFO of stations calling them (Ctrl+click in Band
    // Activity → ft8PileupStack). Whenever the rig is armed + CAT-live + idle + an
    // offset & dial freq are known + auto-drain is enabled, work the head via the
    // work-a-caller path, advancing as each contact completes. The queue lives
    // wholly in the SPA; the daemon is untouched (the same workCaller start the
    // click-to-work path uses). `draining` latches from the workCaller call until
    // qso.active confirms, so the effect can't double-fire in the window before the
    // daemon pushes the active state.
    let draining = false;
    // Re-fire trigger + per-head retry counter for transient start failures. Right
    // after a contact completes (or Abandon/Next), the rig is briefly not ready —
    // the TX→RX settle — so an immediate work gets a rig_not_ready. That is
    // transient: keep the head and re-attempt rather than dropping the caller AND
    // stalling the drain (nothing reactive would otherwise re-fire this effect).
    // retryTick is reactive so a scheduled bump re-runs the drain. The TX seam
    // flattens the daemon's {kind,code} to {ok,message}, so we can't tell a
    // transient rig settle from a hard reject here — treat every failure as
    // retryable up to the cap, then pause (keeping the queue) so no caller is lost.
    const WORK_RETRY_MS = 1500;
    const MAX_WORK_RETRIES = 6; // ~9s of settle before concluding the rig is really down
    let retryTick = $state(0);
    let workRetries = 0;

    // Same-session dupe (band-scoped). NOTE the deliberate asymmetry with
    // Ft8BandActivity, where this is advisory only: there the operator is clicking
    // and may work a station as often as they like. HERE nobody is present at the
    // moment of transmission — the drain keys automatically off a queue entry made
    // earlier — so an already-satisfied entry is dropped rather than re-worked. That
    // is queue hygiene, not a veto on the operator: their intent (one contact with
    // this station) has already been met, and the automatic path is exactly where an
    // unintended duplicate has no human to catch it.
    // Two sources, because neither alone is timely AND durable: `session.qsos` only
    // learns of a contact after the daemon's asynchronous enrich+submit finishes
    // (the terminal idle is published first), while the engaged-call set knows the
    // instant the sequencer touches a station but is forgotten on reload. Together
    // they cover the immediate-repair window this whole feature exists for
    // (codex 0f08d2b2 P1).
    function workedThisSession(call: string): boolean {
        return (
            ft8EngagedThisSession(call, rig.band) ||
            session.qsos.some((q) => q.callsign === call && q.band === rig.band)
        );
    }

    $effect(() => {
        // Touch retryTick so a scheduled bump re-runs this effect (registers the dep).
        // eslint-disable-next-line @typescript-eslint/no-unused-expressions
        retryTick;
        // A contact is active → never drain, and clear the latch: our start landed.
        if (qso.active) {
            draining = false;
            return;
        }
        if (draining) return;
        if (!ft8PileupStack.enabled || ft8PileupStack.items.length === 0) return;
        if (!tx.armed || !catLive || opFreqHz === null || offset === null) return;
        const head = ft8PileupStack.peek();
        if (!head) return;
        // Don't re-work a station already logged this session — a manual work during
        // an Abandon pause can leave a now-duplicate entry. Drop it; the dequeue
        // re-fires this effect for the next head.
        // `repeat` marks an entry the operator queued KNOWING the station was worked
        // — a deliberate repair/sked, not a stale entry — so it is honoured. Without
        // that carve-out the panel accepts the add and this silently discards it
        // (codex 0f9aa672 P1).
        if (!head.repeat && workedThisSession(head.call)) {
            ft8PileupStack.dequeue();
            return;
        }
        draining = true;
        const off = offset;
        const opMHz = opFreqHz / 1_000_000;
        void workCaller({
            theirCall: head.call,
            theirGrid: head.grid,
            theirSnr: head.snr,
            slotUtc: head.slotUtc,
            offsetHz: off,
            opFreqMHz: opMHz,
            // The entry's deliberate-repeat marker must reach persistence, not just
            // this drain's skip check: honouring the operator's intent on air and then
            // letting the dedupe key discard the result would be the same silent loss
            // in a different place (codex c2a8bea6 P1).
            allowDuplicate: head.repeat,
        }).then((r) => {
            if (r.ok) {
                // Now being worked — remove from the queue. The latch clears when
                // qso.active flips true (above).
                ft8PileupStack.dequeue();
                workRetries = 0;
                return;
            }
            draining = false;
            if (workRetries < MAX_WORK_RETRIES) {
                // Keep the head; re-attempt shortly (TX→RX settle after a contact).
                workRetries++;
                setTimeout(() => retryTick++, WORK_RETRY_MS);
                return;
            }
            workRetries = 0;
            toasts.error(r.message || 'Could not work the pile-up head.');
            // Still failing after several tries → pause (keep the queue) so the
            // operator can fix the rig / remove the stuck entry and Resume, rather
            // than losing callers.
            ft8PileupStack.pause();
        });
    });
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
                <!-- An armed auto-work run keys the rig at whoever calls next, and
                     between contacts it renders the same "No active contact" view as a
                     stopped station. Shown while ACTIVE too: the run is live
                     throughout, and a badge that vanished during each contact would
                     read as the run having ended. -->
                {#if qso.autoWorkArmed}
                    <!-- The pill is a CONTROL, not just status (ADR 0065): clicking it
                         stops the RUN without touching an active contact — the one stop
                         Abandon can't provide. -->
                    <button
                        type="button"
                        class="mt-1.5 inline-flex cursor-pointer items-center gap-x-1.5 rounded-full bg-amber-500/15 px-2 py-0.5 text-[11px] font-bold text-amber-600 uppercase hover:bg-amber-500/25"
                        data-testid="auto-work-armed"
                        title="Stop the auto-work run (any active contact continues)"
                        onclick={() => void stopAutoWork()}
                    >
                        <span class="size-1.5 rounded-full bg-amber-500"></span>
                        Auto-work armed — stop
                    </button>
                    <div class="mt-1 text-[11px] text-muted">
                        The next station calling you is worked without a click. Abandon stops the
                        run.
                    </div>
                {:else}
                    <!-- Standing intent (ADR 0065): the mouse-only arming path — the
                         next contact started also starts a run. One-shot: consumed by
                         the start that carries it. ctrl+shift+click on a CQ is the
                         keyboard-fast equivalent. -->
                    <!-- Under "I pick" the control explains itself instead of a
                         silent gate refusal (ADR 0066 fork 6): a pick run cannot
                         auto-work — the operator IS the selector. -->
                    <label
                        class="mt-1.5 inline-flex items-center gap-x-1.5 text-[11px] text-muted select-none {ft8State.answerMode ===
                        'operator_pick'
                            ? 'cursor-not-allowed opacity-50'
                            : 'cursor-pointer'}"
                        title={ft8State.answerMode === 'operator_pick'
                            ? 'Auto-work needs an auto Answer mode — under “I pick” you choose every station'
                            : 'Arm auto-work on the next contact you start (ctrl+shift+click a CQ does the same)'}
                    >
                        <input
                            type="checkbox"
                            bind:checked={ft8AutoWorkIntent.on}
                            disabled={ft8State.answerMode === 'operator_pick'}
                            data-testid="auto-work-intent"
                            class="size-3 accent-amber-500"
                        />
                        Auto-work the next contact
                    </label>
                {/if}
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
        <!-- The run's start parameters (ADR 0066): one centred row — little
             horizontal space, operator-specified. The TX offset readout that
             used to live here is duplicated in the Occupancy panel; its one
             unique job (explaining a no-offset disabled Call CQ) moved into
             the button's title. Both selectors lock while a run is active
             (the parity precedent): changes apply to the NEXT run. -->
        <div class="mb-2 flex items-center justify-between gap-x-4 text-xs">
            <label class="flex items-center gap-x-1 text-muted">
                <span>Answer mode</span>
                <!-- Locked while a run is ARMED too, not just active (codex
                     d7fbf935 P1): an armed auto-work run holds its pinned mode
                     past each contact, so an editable selector there would let
                     the UI claim "I pick" while the run auto-works with the
                     old mode. Stop the run (the pill) to change it. -->
                <select
                    class="rounded border border-line bg-surface px-1 py-0.5 text-xs text-ink disabled:opacity-50"
                    bind:value={ft8State.answerMode}
                    disabled={qso.active || qso.autoWorkArmed}
                    data-testid="answer-mode"
                    aria-label="Call CQ answerer selection mode"
                    title={qso.autoWorkArmed && !qso.active
                        ? 'A run is armed with this mode — stop it (click the pill) to change'
                        : "How a CQ run answers callers — config.json holds the default; this is the session's choice"}
                >
                    <option value="auto_first">First answerer</option>
                    <option value="auto_strongest">Strongest</option>
                    <option value="operator_pick">I pick</option>
                </select>
            </label>
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
