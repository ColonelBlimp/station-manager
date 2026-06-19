<script lang="ts">
    /*
        Tune-carrier toggle (ADR 0027) — the operator's one-button external-amp
        tune. Keys a steady reduced-power RTTY carrier the daemon owns and is
        guaranteed to drop (hard auto-off + release-on-disconnect); clicking
        again (or the auto-off) stops it and restores the pre-tune mode + power.

        Daemon-authoritative, confirm-by-push: the button reflects
        bridgeState.tuneActive (set from the `tune-state` SSE event), not an
        optimistic local flip — so an auto-off the operator didn't trigger
        updates the button too.

        Visibility: rendered only when the configured rig can tune
        (configState.bridge.tune — rigdef-derived), so a rig with no tune
        support shows no dangling TX affordance. Disabled unless the rig is
        live; tuning a non-live rig is impossible (the daemon refuses).

        Click-only by design — no keyboard shortcut. Starting a transmission
        should be a deliberate, visible action, not a stray keypress.
    */
    import { displayedState } from '../../states/displayed.svelte';
    import { configState } from '../../states/config.svelte';
    import { bridgeState } from '../../states/bridge.svelte';
    import { toggleTune } from '../../actions/rigControl';

    const supported = $derived(configState.bridge.tune);
    const enabled = $derived(displayedState.isLive);
    const active = $derived(bridgeState.tuneActive);
</script>

{#if supported}
    <!--
        Bottom-aligned in the form row (justify-end) so the button sits on the
        input-box baseline rather than the label line above it. The active
        (transmitting) state goes solid red and pulses so a keyed carrier is
        unmistakable.
    -->
    <div class="flex flex-col justify-end -mt-1">
        <button
            type="button"
            onclick={toggleTune}
            disabled={!enabled}
            aria-pressed={active}
            class="h-6 w-20 pb-0.5 rounded text-sm font-medium border cursor-pointer transition-colors
                disabled:text-gray-400 disabled:border-gray-200 disabled:cursor-default disabled:hover:bg-transparent
                {active
                ? 'border-red-600 bg-red-600 text-white hover:bg-red-700 animate-pulse'
                : 'border-indigo-600 text-indigo-700 hover:bg-indigo-50'}"
            title={active
                ? 'Transmitting tune carrier — click to stop'
                : 'Key a reduced-power tune carrier to tune the amp'}
        >
            {active ? 'Tuning…' : 'Tune'}
        </button>
    </div>
{/if}
