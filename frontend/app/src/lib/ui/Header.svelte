<script lang="ts">
    // Sticky top bar. Carries the ADR 0044 rig chip — the always-visible
    // freq/mode/band glance anchor AND the CAT gate's status light (green
    // live / grey confirmed-manual / amber confirm-needed / red lost).
    // Clicking it jumps to the Rig panel, where the gate is acted on.
    import { rig, rigGate } from '../operate/rig.svelte';
    import { operate } from '../operate/state.svelte';
    import { navigate } from '../router.svelte';

    const gateLabel = $derived(
        rigGate() === 'live'
            ? 'CAT'
            : rigGate() === 'manual'
              ? 'manual'
              : rigGate() === 'unconfirmed'
                ? 'confirm'
                : 'lost'
    );

    function openRigPanel(): void {
        navigate('operate');
        operate.panel = 'rig';
    }
</script>

<header
    class="sticky top-0 z-40 flex h-16 shrink-0 items-center gap-x-4 border-b border-line bg-surface px-4 sm:gap-x-6 sm:px-6 lg:px-8"
>
    <button
        class="ml-auto flex cursor-pointer items-center gap-x-2 rounded-full bg-surface-muted px-3 py-1.5 text-sm font-medium text-ink hover:bg-surface-muted/70"
        title="Rig panel"
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
