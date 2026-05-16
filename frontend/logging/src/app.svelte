<script lang="ts">
    import LoggingCard from './lib/ui/cards/LoggingCard.svelte';
    import Toasts from './lib/ui/Toasts.svelte';
    import { fetchConfig, putConfig } from './lib/api/config';
    import { configState } from './lib/states/config.svelte';
    import { startBridge } from './lib/states/bridge.svelte';
    import { toasts } from './lib/states/toasts.svelte';
    import { onMount } from 'svelte';
    import { isValidCallsign } from './lib/validators/callsign';
    import StackingDrawer from './lib/ui/cards/StackingDrawer.svelte';

    // Local form state for the setup card. Lives here (not in
    // configState) because configState mirrors the DAEMON's view —
    // stationCallsign there means "what the daemon has persisted,"
    // not "what the operator is typing." We push to configState only
    // after the daemon confirms the save.
    let callsign: string = $state('');
    let saving: boolean = $state(false);

    // Focus the welcome-page callsign input the moment it enters the
    // DOM. The setup snippet renders behind the
    // {#if !configState.setupComplete} gate, so a script-level
    // onMount focus call would fire before the input exists. A node
    // action runs at the right moment: when the {#if} branch becomes
    // truthy and the input mounts.
    function autofocus(node: HTMLInputElement) {
        node.focus();
    }

    const putCallsign = async (): Promise<void> => {
        // Normalise the same way the daemon does (TrimSpace +
        // ToUpper) so what the operator sees in the toast on
        // validation failure matches what they typed.
        const normalised = callsign.trim().toUpperCase();
        if (normalised === '' || saving) return;

        if (isValidCallsign(normalised) !== null) {
            toasts.error('Invalid callsign format');
            saving = false;
            return;
        }

        saving = true;
        try {
            const outcome = await putConfig({
                logging_station: { station_callsign: normalised },
            });
            switch (outcome.kind) {
                case 'ok':
                    // Hydrate from the response — flips
                    // configState.setupComplete=true reactively, the
                    // {#if !setupComplete} branch falls away, and
                    // main_app renders. No manual flag flip here.
                    configState.applyResponse(outcome.config);
                    break;
                case 'validation':
                    console.warn(`[config save] ${outcome.code}: ${outcome.message}`);
                    toasts.error(outcome.message);
                    break;
                case 'server':
                    console.error(`[config save] ${outcome.code}: ${outcome.message}`);
                    toasts.error('Could not save configuration. Try again.');
                    break;
                case 'network':
                    console.error(`[config save] daemon unreachable: ${outcome.message}`);
                    toasts.error('Cannot reach the daemon — check it is running.');
                    break;
            }
        } finally {
            saving = false;
        }
    };

    // Boot: GET /v1/config so the SPA knows whether to render the
    // setup dialog (setupComplete=false) or the QSO panel. Failure
    // paths (network, server) leave configState.loaded=true with the
    // stub defaults so the UI doesn't hang on a half-rendered shell;
    // the operator gets a toast telling them what's wrong.
    //
    // After config settles, startBridge() wires the SSE consumer for
    // /v1/rig/events. It tracks configState.station.enabled and
    // opens/closes the EventSource accordingly — no-op while CAT is
    // disabled, automatic open when the operator toggles CAT on.
    onMount(async (): Promise<void> => {
        const outcome = await fetchConfig();
        switch (outcome.kind) {
            case 'ok':
                configState.applyResponse(outcome.config);
                break;
            case 'validation':
                console.warn(`[config fetch] ${outcome.code}: ${outcome.message}`);
                toasts.error(outcome.message);
                configState.markLoaded();
                break;
            case 'server':
                console.error(`[config fetch] ${outcome.code}: ${outcome.message}`);
                toasts.error('Could not load configuration. Try restarting the daemon.');
                configState.markLoaded();
                break;
            case 'network':
                console.error(`[config fetch] daemon unreachable: ${outcome.message}`);
                toasts.error('Cannot reach the daemon — check it is running.');
                configState.markLoaded();
                break;
        }
        startBridge();
    });
</script>

<!--
    Render nothing while configState is still loading. The fetch
    settles in tens of ms on localhost so a spinner would only flash
    annoyingly. Failure paths (network/server unreachable) toast the
    operator AND flip configState.loaded=true with stub defaults, so
    the UI moves on to the setup branch rather than hanging here.
    Toasts always render so the failure message is visible.
-->
{#if configState.loaded}
    {#if !configState.setupComplete}
        {@render setup()}
    {:else}
        {@render main_app()}
    {/if}
{/if}
<Toasts />

<!--
    h-140 / w-fit | w-200 are the whole-app shell dimensions — single use,
    no design-token relationship. Adjust here when the operator decides
    they want a different overall card size; nothing else depends on
    these values.

    <Toasts/> is the single mount point for the notification queue per
    ADR 0008. Sits as a sibling of <main> because it's a fixed-position
    overlay; nesting it inside <main> would constrain its z-index stack
    and inherit unrelated layout rules.
-->
{#snippet main_app()}
    <main class="relative rounded-xl border border-line-soft h-166 w-fit mx-auto mt-12">
        <LoggingCard />
        <StackingDrawer />
    </main>
{/snippet}

{#snippet setup()}
    <main class="rounded-xl border border-line-soft h-120 w-200 mx-auto mt-12 p-8">
        <h1 class="text-center font-semibold text-2xl">Welcome to Station Manager</h1>
        <div class="py-4">
            <p>
                For Station Manager to work, the <i>default log book</i> needs to be initialised.
                All this requires is a callsign. Don't worry if you are not sure which callsign to
                enter; it is generally recommended that the <i>default log book</i> be associated with
                you personal callsign.
            </p>
            <p>
                If you use QRZ.com, then the callsign entered here should be the same as the
                callsign for the QRZ.com log book to which the QSOs will be forwarded (forwarding of
                QSOs is configurable and not enabled by default).
            </p>
        </div>
        <!--
            Wrapped in a <form> so Enter on the Callsign input submits
            via the same path as clicking Save. preventDefault keeps the
            browser from issuing the synthetic GET that the absence of
            a method/action would otherwise trigger.
        -->
        <form
            class="flex flex-col items-center"
            onsubmit={(e) => {
                e.preventDefault();
                void putCallsign();
            }}
        >
            <div class="flex flex-row items-center space-x-4">
                <label class="input-label" for="callsign">Callsign</label>
                <input
                    id="callsign"
                    type="text"
                    placeholder="Callsign"
                    class="input-base uppercase w-38"
                    title="The Default log book's callsign."
                    bind:value={callsign}
                    disabled={saving}
                    autocomplete="off"
                    spellcheck="false"
                    use:autofocus
                />
                <button
                    type="submit"
                    disabled={callsign.trim() === '' || saving}
                    class="h-9 cursor-pointer rounded-md bg-focus p-2.5 py-1.5 text-base font-semibold text-white shadow-sm hover:bg-focus-ring focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-50 disabled:cursor-not-allowed"
                    >{saving ? 'Saving…' : 'Save'}</button
                >
            </div>
        </form>
    </main>
{/snippet}
