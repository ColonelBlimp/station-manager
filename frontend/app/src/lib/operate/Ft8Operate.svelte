<script lang="ts">
    // Operate — the FT8 view's most-watched co-primary anchor (ADR 0047): the
    // control center. This increment renders the LIVE exchange from ft8State
    // (working station · slot-timing pill · role-aware message ladder); the TX
    // control bar (Arm / Call CQ / Abandon / Next) + click-to-start is the next
    // increment (first RF from this SPA), kept a deliberate boundary.
    import { ft8State, ft8OperatorCall, ft8MyGrid } from './ft8.svelte';
    import { buildLadder } from './ft8Ladder';
    import { pathInfo } from '../utils/bearing';

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

    <!-- TX control bar (Arm / Call CQ / Abandon / Next) lands in the next
         increment — the first RF path from this SPA, deliberately separate. -->
    <div class="border-t border-line px-4 py-2 text-center text-[11px] text-muted">
        TX controls — next increment
    </div>
</section>
