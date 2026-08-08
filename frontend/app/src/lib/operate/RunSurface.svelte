<script lang="ts">
    /*
        The RUN SURFACE (ADR 0067, operator-ratified UI) — ONE home for the
        run lifecycle, replacing the checkbox/chip morph: fixed three-row
        structure (the audio card's V6 lesson — layouts must not swap), only
        text and the Stop/Resume button's presence vary.

        Row 1: the Answer mode selector (relocated from the TX control bar —
               the bar keeps the genuinely CQ-scoped CQ-slot parity).
               Locked while a run is active OR armed (the parity precedent:
               changes apply to the next run; d7fbf935 P1 pinned the armed
               half — an editable selector under an armed run lets the UI
               claim "I pick" while the run auto-works with its pinned mode).
        Row 2: state dot + the ratified state line. Click-to-open the drawer
               when there is a caller list (operator-initiated — the 0065
               badge-only rule stays for unprompted surfacing).
        Row 3: Stop run (auto run: stops outright · pick run: PAUSES the
               drain, queue kept) / Resume when paused.
    */
    import { ft8State, stopAutoWork, resumeDrain } from './ft8.svelte';
    import { setPileup } from './state.svelte';
    import { toasts } from '../ui/toasts.svelte';

    const qso = $derived(ft8State.qso);
    const mode = $derived(ft8State.answerMode);
    const pick = $derived(mode === 'operator_pick');
    const orderWord = $derived(mode === 'auto_strongest' ? 'strongest first' : 'first come');
    // A pick context is live when the daemon says so on the frame — the
    // listing run (idle) or a pick session (active) both carry the mode.
    const pickLive = $derived(qso.answerMode === 'operator_pick');
    const runLive = $derived(qso.autoWorkArmed || pickLive);

    const dot: () => string = $derived(() => {
        if (pick && pickLive && qso.drainPaused) return 'bg-zinc-500';
        if (pick && pickLive && qso.queue.length > 0) return 'bg-amber-500';
        if (pick && pickLive) return 'bg-sky-500';
        if (qso.autoWorkArmed) return 'bg-amber-500';
        return 'bg-zinc-400';
    });

    const line: () => string = $derived(() => {
        if (pick) {
            if (pickLive && qso.drainPaused)
                return `Drain paused — ${qso.queue.length} bagged waiting`;
            if (pickLive && qso.queue.length > 0)
                return `Working your queue — ${qso.queue.length} bagged left`;
            if (pickLive && qso.answerers.length > 0)
                return `${qso.answerers.length} calling you — open the drawer to work or bag`;
            if (pickLive) return 'Listing callers — nobody calling yet';
            return 'Manual — callers will be listed; nothing transmits until you choose';
        }
        if (qso.autoWorkArmed && qso.active)
            return `Run live — working ${qso.theirCall} (${orderWord})`;
        if (qso.autoWorkArmed) return `Run live — waiting for callers (${orderWord})`;
        return `Your next contact starts a run — callers worked ${orderWord}`;
    });

    async function onStop(): Promise<void> {
        const r = await stopAutoWork();
        if (!r.ok) toasts.error(r.message);
    }
    async function onResume(): Promise<void> {
        const r = await resumeDrain();
        if (!r.ok) toasts.error(r.message);
    }
</script>

<div data-run-surface class="mt-1.5 text-[11px]">
    <label class="flex items-center gap-x-1 text-muted">
        <span>Answer mode</span>
        <select
            class="rounded border border-line bg-surface px-1 py-0.5 text-xs text-ink disabled:opacity-50"
            bind:value={ft8State.answerMode}
            disabled={qso.active || qso.autoWorkArmed}
            data-testid="answer-mode"
            aria-label="Answer mode — how any run treats callers"
            title={qso.autoWorkArmed && !qso.active
                ? 'A run is armed with this mode — stop it to change'
                : "How any run treats callers — config.json holds the default; this is the session's choice"}
        >
            <option value="auto_first">First answerer</option>
            <option value="auto_strongest">Strongest</option>
            <option value="operator_pick">I pick</option>
        </select>
    </label>
    <button
        type="button"
        data-run-state
        class="mt-1 flex items-center gap-x-1.5 text-left text-muted {pickLive
            ? 'cursor-pointer hover:text-ink'
            : 'cursor-default'}"
        title={pickLive ? 'Open the pile-up drawer' : ''}
        onclick={() => {
            if (pickLive) setPileup(true);
        }}
    >
        <span class="size-1.5 shrink-0 rounded-full {dot()}"></span>
        <span>{line()}</span>
    </button>
    <div class="mt-1 h-5">
        {#if pickLive && qso.drainPaused}
            <button
                type="button"
                data-run-resume
                class="cursor-pointer rounded-full bg-focus/15 px-2 py-0.5 text-[11px] font-bold text-focus uppercase hover:bg-focus/25"
                onclick={() => void onResume()}
            >
                Resume queue
            </button>
        {:else if runLive && (qso.autoWorkArmed || qso.queue.length > 0)}
            <button
                type="button"
                data-run-stop
                class="cursor-pointer rounded-full bg-amber-500/15 px-2 py-0.5 text-[11px] font-bold text-amber-600 uppercase hover:bg-amber-500/25"
                title="Stop the run (an auto run stops; a pick queue pauses — any active contact continues)"
                onclick={() => void onStop()}
            >
                Stop run
            </button>
        {/if}
    </div>
</div>
