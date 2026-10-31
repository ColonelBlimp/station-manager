<script lang="ts">
    // Sticky top bar. Carries the ADR 0044 rig chip — the always-visible
    // freq/mode/band glance anchor AND the CAT gate's status light (green
    // live / grey confirmed-manual / amber confirm-needed / red lost).
    // On Operate, clicking it TOGGLES the Rig Control panel. Everywhere else
    // it is a READOUT ONLY (operator, 2026-08-03): it used to jump to Operate
    // and reveal the panel, which put "leave this page" one stray click away
    // from a glance at the frequency — costly from Settings, where navigating
    // away silently discards unsaved edits (no navigation guard exists; see
    // Header.svelte.test.ts for the full reasoning). Leading (left) is the
    // operating-session timer — the other always-visible ambient readout, at
    // the opposite end so the eye finds each.
    import { rig, rigGate } from '../operate/rig.svelte';
    import { station } from '../operate/station.svelte';
    import { toggleTile } from '../operate/layout.svelte';
    import { router } from '../router.svelte';
    import SessionTimer from './SessionTimer.svelte';
    import { archivesState, activeArchive } from '../config/archives.svelte';
    import {
        contest,
        CONTESTS,
        contestId,
        contestStart,
        contestWindow,
    } from '../operate/contest.svelte';
    import { contestHistory, loadContestHistory } from '../operate/contestHistory.svelte';
    import { computeTimers } from '../operate/contestTimers';
    import { contestBound } from '../operate/qso.svelte';

    // Contest mode (W-0023 weekend slice; operator, 2026-10-31): ticked, the
    // Phone / CW card shows the exchange and allocates serials for the contest
    // chosen here. Each entry is one occurrence, so choosing it sets the
    // contest ID and start date together. Both lock once the QSO in progress
    // has its serial: the number belongs to the occurrence it came from.
    const contestValue = $derived(`${contest.id}|${contest.start}`);
    // Contest timers (operator, 2026-10-31): "Last" — full minutes since the
    // last contest QSO, green once the gap is an off period (UK/EI rule 4.4b:
    // 60 minutes) — and "Op", operating time so far against the limited-time
    // category's limit (amber from an hour before it, red past it). Counted
    // the scorers' way (contestTimers.ts). The occurrence's contacts load from
    // the daemon when it is chosen; the clock ticks each second.
    let now = $state(Date.now());
    $effect(() => {
        if (!contest.on) return;
        const t = setInterval(() => (now = Date.now()), 1_000);
        return () => clearInterval(t);
    });
    $effect(() => {
        if (!contest.on) return;
        const id = contestId();
        const start = contestStart();
        if (id !== '' && start !== '' && contestHistory.key !== `${id}|${start}`) {
            void loadContestHistory(id, start);
        }
    });
    const timers = $derived.by(() => {
        const w = contestWindow();
        if (!contest.on || w === null) return null;
        const r = computeTimers(contestHistory.completions, w.startMs, w.endMs, now, w.offMin);
        return { ...r, maxOpMin: w.maxOpMin, offMin: w.offMin };
    });
    const hm = (min: number): string =>
        `${Math.floor(min / 60)}:${String(min % 60).padStart(2, '0')}`;

    function chooseContest(value: string): void {
        const c = CONTESTS.find((o) => `${o.id}|${o.start}` === value);
        if (c) Object.assign(contest, { id: c.id, start: c.start });
    }

    // Thousands-grouped QSO count (e.g. "1,234") beside the logbook name.
    const countFmt = new Intl.NumberFormat();

    // FT8 can't operate without CAT, so the Rig panel shows "CAT required"
    // there instead of the manual/confirm states (its requiresCat lockout).
    // Mirror that here — the header light must never claim a usable manual
    // state the FT8 view rejects. 'lost' keeps its own label in both places.
    const catRequired = $derived(
        router.mode !== 'phone' && (rigGate() === 'manual' || rigGate() === 'unconfirmed')
    );

    const gateLabel = $derived(
        rigGate() === 'live'
            ? 'CAT'
            : rigGate() === 'lost'
              ? 'lost'
              : catRequired
                ? 'CAT required'
                : rigGate() === 'manual'
                  ? 'manual'
                  : 'confirm'
    );

    // Short hover label, keyed to the gate state.
    const chipTitle = $derived(
        rigGate() === 'live'
            ? 'CAT active'
            : rigGate() === 'lost'
              ? 'CAT link lost'
              : catRequired
                ? `${router.mode === 'ft4' ? 'FT4' : 'FT8'} needs a live CAT connection`
                : rigGate() === 'manual'
                  ? 'Manual — confirmed'
                  : 'Waiting for confirmation'
    );

    // The chip only acts on Operate, where the Rig Control panel it toggles
    // actually lives. Off Operate it renders as a plain readout rather than a
    // disabled button: an inert control the operator can still click, hover and
    // focus is indistinguishable from a broken one.
    const interactive = $derived(router.view === 'operate');
</script>

<header
    class="sticky top-0 z-40 flex h-16 shrink-0 items-center gap-x-4 border-b border-line bg-surface px-4 sm:gap-x-6 sm:px-6 lg:px-8"
>
    <!-- Leading: operating-session timer (ambient, always visible). -->
    <span class="flex items-center gap-x-1.5 text-sm font-medium text-ink" title="Session length">
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
            class="size-5 text-muted"
        >
            <path
                d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"
                stroke-linecap="round"
                stroke-linejoin="round"
            />
        </svg>
        <SessionTimer />
    </span>

    <div class="flex items-center gap-x-2 text-sm text-ink">
        <label class="flex items-center gap-x-1">
            <input
                type="checkbox"
                bind:checked={contest.on}
                disabled={contestBound()}
                data-testid="contest-on"
            />
            Contest
        </label>
        <select
            aria-label="Contest"
            class="input py-1 text-sm"
            value={contestValue}
            disabled={!contest.on || contestBound()}
            onchange={(e) => chooseContest(e.currentTarget.value)}
            data-testid="contest-select"
        >
            {#each CONTESTS as c (`${c.id}|${c.start}`)}
                <option value={`${c.id}|${c.start}`}>{c.label}</option>
            {/each}
        </select>
        {#if contest.on && contestHistory.failed}
            <span class="text-xs text-muted" data-testid="timers-unavailable">
                Timers unavailable —
                <button
                    type="button"
                    class="cursor-pointer underline"
                    onclick={() => void loadContestHistory(contestId(), contestStart())}
                    >retry</button
                >
            </span>
        {:else if timers !== null}
            <!-- Labelled like the Archive / Logbook block (operator, 2026-10-31:
                 say what each timer means; never wrap). -->
            <div
                class="flex flex-col text-xs leading-tight whitespace-nowrap"
                title="Full minutes since your last contest QSO, counted from the clock minute after it. Green once the gap is long enough to count as an off period ({timers.offMin} min)."
                data-testid="timer-last"
            >
                <span class="text-muted">Since last QSO</span>
                <span
                    class="text-sm font-medium text-ink tabular-nums"
                    class:text-green-500={timers.offQualifies}
                    data-testid="timer-last-value"
                    >{timers.sinceLastMin === null
                        ? '–'
                        : hm(timers.sinceLastMin)}{timers.offQualifies ? ' off period ✓' : ''}</span
                >
            </div>
            <div
                class="flex flex-col text-xs leading-tight whitespace-nowrap"
                title="Operating time so far, counted as the contest scorers do: off periods of {timers.offMin} minutes or more are excluded, shorter gaps count. Against the limited-time category's {hm(
                    timers.maxOpMin
                )}."
                data-testid="timer-op"
            >
                <span class="text-muted">Operating time</span>
                <span
                    class="text-sm font-medium text-ink tabular-nums"
                    class:text-amber-500={timers.opMin >= timers.maxOpMin - 60 &&
                        timers.opMin <= timers.maxOpMin}
                    class:text-red-500={timers.opMin > timers.maxOpMin}
                    data-testid="timer-op-value">{hm(timers.opMin)} of {hm(timers.maxOpMin)}</span
                >
            </div>
        {/if}
    </div>

    <!-- Station identity: the logbook this session writes to + the configured rig.
         Config-sourced (not CAT), so it's the operator's always-on "am I logging
         into the right book, on the right radio?" check — across Phone/CW AND FT8.
         Hidden on the narrowest widths to keep the chip + timer readable. -->
    <div
        class="ml-auto hidden flex-col items-end text-xs leading-tight sm:flex"
        title="Archive + logbook + rig this session logs to (from config)"
    >
        <!-- The archive name (ADR 0071) sits above the logbook: the DAEMON's
         active archive, display only — switching happens only from Settings →
         Archives (ADR 0087). A pending candidate is named while the restart is
         awaited. -->
        {#if archivesState.list.length > 0}
            {@const active = activeArchive()}
            {@const pending = archivesState.list.find((a) => a.state === 'pending')}
            <span data-testid="header-archive">
                <span class="text-muted">Archive</span>
                <span class="font-medium text-ink">{active?.label ?? '—'}</span>
                {#if pending}<span class="text-muted">(switching to {pending.label})</span>{/if}
            </span>
        {/if}
        <span>
            <span class="text-muted">Logbook</span>
            <span class="font-medium text-ink">{station.logbookName || '—'}</span>
            {#if station.logbookName}<span class="text-muted"
                    >({countFmt.format(station.logbookQsoCount)})</span
                >{/if}
        </span>
        <span>
            <span class="text-muted">Rig</span>
            <span class={station.rigName ? 'font-medium text-ink' : 'text-muted italic'}
                >{station.rigName || 'not set'}</span
            >
        </span>
    </div>

    <!-- One snippet, two wrappers: the chip looks identical either way, so the
         content must not be duplicated between the branches where it could
         drift. Only the affordances differ — cursor, hover, and whether it is a
         button at all. -->
    {#snippet chipBody()}
        <span
            class="size-2 shrink-0 rounded-full"
            class:bg-green-500={rigGate() === 'live'}
            class:bg-gray-400={rigGate() === 'manual' && !catRequired}
            class:bg-amber-500={rigGate() === 'unconfirmed' && !catRequired}
            class:bg-red-500={rigGate() === 'lost' || catRequired}
        ></span>
        <span class="tabular-nums">{rig.freq}</span>
        <span class="text-muted">·</span>
        <span>{rig.mode}</span>
        <span class="text-muted">·</span>
        <span>{rig.band}</span>
        <span class="text-xs text-muted">{gateLabel}</span>
    {/snippet}

    {#if interactive}
        <button
            class="ml-auto flex cursor-pointer items-center gap-x-2 rounded-full bg-surface-muted px-3 py-1.5 text-sm font-medium text-ink hover:bg-surface-muted/70 sm:ml-4"
            title={chipTitle}
            onclick={() => toggleTile('rig')}
        >
            {@render chipBody()}
        </button>
    {:else}
        <div
            class="ml-auto flex items-center gap-x-2 rounded-full bg-surface-muted px-3 py-1.5 text-sm font-medium text-ink sm:ml-4"
            title={chipTitle}
        >
            {@render chipBody()}
        </div>
    {/if}
</header>
