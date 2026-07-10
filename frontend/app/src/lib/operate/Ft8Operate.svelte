<script lang="ts">
    // Operate — the FT8 view's most-watched co-primary anchor (ADR 0047): the
    // control center. This increment renders the LIVE exchange from ft8State
    // (working station · slot-timing pill · role-aware message ladder); the TX
    // control bar (Arm / Call CQ / Abandon / Next) + click-to-start is the next
    // increment (first RF from this SPA), kept a deliberate boundary.
    import { ft8State, ft8OperatorCall, ft8MyGrid, armTx, callCq, abandonQso } from './ft8.svelte';
    import { rig } from './rig.svelte';
    import { buildLadder } from './ft8Ladder';
    import { pathInfo } from '../utils/bearing';
    import { parseFrequency } from '../validators/frequency';
    import { toasts } from '../ui/toasts.svelte';

    const qso = $derived(ft8State.qso);
    const tx = $derived(ft8State.tx);

    const roleLabel = $derived(
        !qso.active
            ? 'Idle'
            : qso.role === 'answerer'
              ? qso.fd
                  ? 'Answering CQ FD'
                  : 'Answering a CQ'
              : qso.role === 'caller'
                ? 'Calling CQ'
                : qso.role === 'worker'
                  ? qso.fd
                      ? 'Working an FD caller'
                      : 'Working a caller'
                  : 'Active'
    );

    const ladder = $derived(buildLadder(qso, tx.transmitting, ft8OperatorCall(), ft8MyGrid()));

    // Short-path bearing to the worked station (their grid → our grid).
    const bearing = $derived.by(() => {
        const my = ft8MyGrid();
        if (qso.theirGrid === '' || my === '') return null;
        try {
            const p = pathInfo(my, qso.theirGrid);
            return p && Number.isFinite(p.shortPathBearing) ? Math.round(p.shortPathBearing) : null;
        } catch {
            return null;
        }
    });

    // ---- Slot-timing pill: a live 1 s ticker off the latest slot's start. ----
    let now = $state(Date.now());
    $effect(() => {
        const t = setInterval(() => (now = Date.now()), 1000);
        return () => clearInterval(t);
    });
    // Seconds remaining in the current 15 s slot (0–15), or null with no slot yet.
    const secsLeft = $derived.by(() => {
        if (ft8State.slot === null) return null;
        const start = Date.parse(ft8State.slot.start_utc);
        if (Number.isNaN(start)) return null;
        const left = Math.ceil((start + 15_000 - now) / 1000);
        return Math.max(0, Math.min(15, left));
    });
    // 'tx' = transmitting this slot; 'rx' = active but receiving; 'idle' = no session.
    const pillMode = $derived(tx.transmitting ? 'tx' : qso.active ? 'rx' : 'idle');
    const pillText = $derived(
        secsLeft === null
            ? 'Waiting for slot…'
            : pillMode === 'tx'
              ? `Transmit slot · listen in ${secsLeft}s`
              : pillMode === 'rx'
                ? `Listen slot · transmit in ${secsLeft}s`
                : `Listen · next slot ${secsLeft}s`
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
    const canAbandon = $derived(tx.armed && qso.active);
    // A Call-CQ run is in progress (we are the caller, looping the pile-up) — the
    // Call CQ button goes red so "I'm running CQ" is unmistakable.
    const callerActive = $derived(qso.active && qso.role === 'caller');

    // Per-action in-flight latches. The daemon single-flights competing starts and
    // 409s the loser, so these mainly stop a double-tap issuing a wasted second POST.
    let arming = $state(false);
    let sending = $state(false);
    let abandoning = $state(false);

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

    async function onAbandon(): Promise<void> {
        if (abandoning) return;
        abandoning = true;
        try {
            const r = await abandonQso();
            if (!r.ok) toasts.error(r.message);
        } finally {
            abandoning = false;
        }
    }
</script>

<section class="flex h-full flex-col overflow-hidden rounded-xl border border-line bg-surface">
    <div class="flex items-center gap-x-3 border-b border-line px-4 py-2">
        <h3 class="text-sm font-semibold text-ink">Operate</h3>
        <span class="ml-auto text-xs font-semibold text-muted">{roleLabel}</span>
    </div>

    <div class="flex-1 overflow-auto p-4">
        <!-- Worked station -->
        {#if qso.active && qso.theirCall !== ''}
            <div class="font-mono text-lg font-extrabold tracking-wide text-ink">
                {qso.theirCall}
            </div>
            <div class="mt-0.5 text-xs text-muted">
                {roleLabel}{bearing !== null ? ` · ${bearing}° short-path` : ''}
            </div>
        {:else}
            <div class="text-sm text-muted">
                No active contact — answer a CQ from Band Activity, or Call CQ.
            </div>
        {/if}

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
         Abandon drive the sequenced session; Arm sits alone at the bottom (its own
         divider) as the operator's explicit consent to key. The pile-up "Next"
         button lands with the operator_pick stack (a later increment). -->
    <div class="border-t border-line px-4 py-3">
        <div class="mb-2 flex items-center justify-between gap-x-2 text-xs">
            <span class="text-muted"
                >TX offset <span class="font-mono text-ink">{offsetLabel}</span></span
            >
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
        </div>
        <hr class="my-3 border-line" />
        <button
            type="button"
            class="w-full rounded-md px-3 py-2 text-sm font-bold disabled:cursor-not-allowed disabled:opacity-50 {tx.armed
                ? 'bg-red-600 text-white hover:bg-red-700'
                : 'border border-focus text-focus hover:bg-nav-accent-bg'}"
            onclick={onArm}
            disabled={!canArm || arming}
        >
            {tx.armed ? 'Armed — click to disable TX' : 'Enable TX'}
        </button>
    </div>
</section>
