<script lang="ts">
    // Sticky top bar. Carries the ADR 0044 rig chip — the always-visible
    // freq/mode/band glance anchor AND the CAT gate's status light (green
    // live / grey confirmed-manual / amber confirm-needed / red lost).
    // Clicking it TOGGLES the Rig Control panel (from another view it first
    // jumps to Operate and reveals it — never a blind toggle-off of a panel
    // the operator can't see). Leading (left) is the operating-session
    // timer — the other always-visible ambient readout, at the opposite end
    // so the eye finds each.
    import { rig, rigGate } from '../operate/rig.svelte';
    import { station } from '../operate/station.svelte';
    import { showTile, toggleTile } from '../operate/layout.svelte';
    import { navigate, router } from '../router.svelte';
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

    // Chip click: already on Operate → toggle the Rig Control panel (a second
    // click dismisses what the first revealed); anywhere else → navigate there
    // and reveal it (show, not toggle, so the arrival state is deterministic).
    function toggleRigPanel(): void {
        if (router.view !== 'operate') {
            navigate('operate');
            showTile('rig');
            return;
        }
        toggleTile('rig');
    }
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

    <button
        class="ml-auto flex cursor-pointer items-center gap-x-2 rounded-full bg-surface-muted px-3 py-1.5 text-sm font-medium text-ink hover:bg-surface-muted/70 sm:ml-4"
        title={chipTitle}
        onclick={toggleRigPanel}
    >
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
    </button>
</header>
