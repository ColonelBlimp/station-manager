<script lang="ts">
    // Sticky top bar. Carries the ADR 0044 rig chip — the always-visible
    // freq/mode/band glance anchor AND the CAT gate's status light (green
    // live / grey confirmed-manual / amber confirm-needed / red lost).
    // Clicking it jumps to the Rig panel, where the gate is acted on. Leading
    // (left) is the operating-session timer — the other always-visible ambient
    // readout, at the opposite end so the eye finds each.
    import { rig, rigGate } from '../operate/rig.svelte';
    import { showTile } from '../operate/layout.svelte';
    import { navigate } from '../router.svelte';
    import SessionTimer from './SessionTimer.svelte';

    const gateLabel = $derived(
        rigGate() === 'live'
            ? 'CAT'
            : rigGate() === 'manual'
              ? 'manual'
              : rigGate() === 'unconfirmed'
                ? 'confirm'
                : 'lost'
    );

    // Short hover label, keyed to the gate state.
    const chipTitle = $derived(
        rigGate() === 'live'
            ? 'CAT active'
            : rigGate() === 'manual'
              ? 'Manual — confirmed'
              : rigGate() === 'unconfirmed'
                ? 'Waiting for confirmation'
                : 'CAT link lost'
    );

    function openRigPanel(): void {
        navigate('operate');
        showTile('rig'); // reveal the Rig tile if it's hidden
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

    <button
        class="ml-auto flex cursor-pointer items-center gap-x-2 rounded-full bg-surface-muted px-3 py-1.5 text-sm font-medium text-ink hover:bg-surface-muted/70"
        title={chipTitle}
        onclick={openRigPanel}
    >
        <span
            class="size-2 shrink-0 rounded-full"
            class:bg-green-500={rigGate() === 'live'}
            class:bg-gray-400={rigGate() === 'manual'}
            class:bg-amber-500={rigGate() === 'unconfirmed'}
            class:bg-red-500={rigGate() === 'lost'}
        ></span>
        <span class="tabular-nums">{rig.freq}</span>
        <span class="text-muted">·</span>
        <span>{rig.mode}</span>
        <span class="text-muted">·</span>
        <span>{rig.band}</span>
        <span class="text-xs text-muted">{gateLabel}</span>
    </button>
</header>
