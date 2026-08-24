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
    import { toggleNotifications } from './state.svelte';
    import SessionTimer from './SessionTimer.svelte';

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
                ? 'FT8 needs a live CAT connection'
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

    <!-- Station identity: the logbook this session writes to + the configured rig.
         Config-sourced (not CAT), so it's the operator's always-on "am I logging
         into the right book, on the right radio?" check — across Phone/CW AND FT8.
         Hidden on the narrowest widths to keep the chip + timer readable. -->
    <div
        class="ml-auto hidden flex-col items-end text-xs leading-tight sm:flex"
        title="Logbook + rig this session logs to (from config)"
    >
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

    <!-- Notification history — durable counterpart to the transient toasts
         (W-0001). Plain icon, no unread badge. -->
    <button
        class="flex cursor-pointer items-center rounded-md p-1.5 text-muted hover:bg-surface-muted hover:text-ink"
        title="Notification history"
        aria-label="Notification history"
        onclick={toggleNotifications}
    >
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
            class="size-5"
        >
            <path
                d="M8.25 6.75h12M8.25 12h12m-12 5.25h12M3.75 6.75h.007v.008H3.75V6.75Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0ZM3.75 12h.007v.008H3.75V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Zm-.375 5.25h.007v.008H3.75v-.008Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"
                stroke-linecap="round"
                stroke-linejoin="round"
            />
        </svg>
    </button>
</header>
