<script lang="ts">
    // The card-below the logging card. Shows whichever info panel the rail has
    // active (Worked/Session/Rig). All live (on stubbed data seams until the
    // /v1 wiring).
    import { operate, closePanel, openExport, openContact } from './state.svelte';
    import { rig, rigGate } from './rig.svelte';
    import { session } from './session.svelte';
    import { qsoClock } from './qso.svelte';
    import WorkedPanel from './WorkedPanel.svelte';
    import SessionPanel from './SessionPanel.svelte';
    import RigPanel from './RigPanel.svelte';

    const titles: Record<string, string> = {
        worked: 'Worked',
        session: 'Session',
        rig: 'Rig',
    };
</script>

{#if operate.panel}
    <!-- Centred on the logging card's axis: operate-center anchors children
         to the logging card's LEFT edge, so a wider card must overhang
         symmetrically — half the width difference as a negative left margin.
         (Falls back to 0 outside operate-center, e.g. the FT8 branch, via
         the var() default.) Keep the 42rem calc in step with w-2xl
         (= --container-2xl = 42rem). -->
    <div class="card mt-4 w-2xl ml-[calc((var(--card-w,42rem)-42rem)/2)]">
        <div class="flex items-center justify-between">
            <div class="flex items-center gap-x-3 -mt-1">
                <h3 class="text-sm font-semibold text-ink">{titles[operate.panel]}</h3>
                {#if operate.panel === 'rig'}
                    {#if rig.identity !== ''}
                        <span class="text-sm text-muted">{rig.identity}</span>
                    {/if}
                    <span
                        class="flex items-center gap-x-1.5 rounded-full bg-surface-muted px-2.5 py-1 text-xs font-medium text-ink"
                    >
                        <span
                            class="size-2 rounded-full"
                            class:bg-green-500={rigGate() === 'live'}
                            class:bg-gray-400={rigGate() === 'manual'}
                            class:bg-amber-500={rigGate() === 'unconfirmed'}
                            class:bg-red-500={rigGate() === 'lost'}
                        ></span>
                        {rigGate() === 'live'
                            ? 'CAT connected'
                            : rigGate() === 'manual'
                              ? 'Manual — confirmed'
                              : rigGate() === 'unconfirmed'
                                ? 'Manual — confirm to log'
                                : 'CAT link lost'}
                    </span>
                {/if}
            </div>
            <div class="flex items-center gap-x-2 -mt-1">
                <!-- Worked-panel header action: the contact-detail overlay
                     (everything we know + deliberate edit). Only meaningful once
                     a QSO is underway (a contact to view), same slot as the
                     Session panel's Export so header actions stay consistent. -->
                {#if operate.panel === 'worked'}
                    <button
                        class="btn text-xs"
                        disabled={!qsoClock.started}
                        title={qsoClock.started ? undefined : 'Start a QSO to view contact details'}
                        onclick={openContact}
                    >
                        View…
                    </button>
                {/if}
                <!-- Session-panel header action: the deliberate export/email
                     lives here (not on the card body, not a rail submenu) so
                     the log table stays clean. Disabled with an empty log. -->
                {#if operate.panel === 'session'}
                    <button
                        class="btn text-xs"
                        disabled={session.qsos.length === 0}
                        onclick={openExport}
                    >
                        Export…
                    </button>
                {/if}
                <button
                    class="cursor-pointer rounded-md text-muted hover:text-ink"
                    title="Close"
                    onclick={closePanel}
                >
                    <span class="sr-only">Close panel</span>
                    <svg
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        stroke-width="1.5"
                        aria-hidden="true"
                        class="size-5"
                    >
                        <path
                            d="M6 18 18 6M6 6l12 12"
                            stroke-linecap="round"
                            stroke-linejoin="round"
                        />
                    </svg>
                </button>
            </div>
        </div>
        <div class="mt-3">
            {#if operate.panel === 'worked'}
                <WorkedPanel />
            {:else if operate.panel === 'session'}
                <SessionPanel />
            {:else}
                <RigPanel />
            {/if}
        </div>
    </div>
{/if}
