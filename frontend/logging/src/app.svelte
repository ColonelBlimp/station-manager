<script lang="ts">
    import LoggingCard from './lib/ui/cards/LoggingCard.svelte';
    import Toasts from './lib/ui/Toasts.svelte';
    import ManualLink from './lib/ui/ManualLink.svelte';
    import { fetchConfig, putConfig } from './lib/api/config';
    import { configState } from './lib/states/config.svelte';
    import { startBridge, bridgeState } from './lib/states/bridge.svelte';
    import { toasts } from './lib/states/toasts.svelte';
    import { onMount } from 'svelte';
    import { isValidCallsign } from './lib/validators/callsign';
    import StackingDrawer from './lib/ui/cards/StackingDrawer.svelte';
    import Ft8PileupDrawer from './lib/ui/panels/Ft8PileupDrawer.svelte';
    import Ft8EnrichDemo from './lib/ui/demo/Ft8EnrichDemo.svelte';
    import Ft8PileupDemo from './lib/ui/demo/Ft8PileupDemo.svelte';

    // ?ft8demo / ?pileupdemo render standalone layout playgrounds (the FT8
    // enrichment box, the FT8 pile-up drawer) and short-circuit the whole app
    // (no config fetch, no daemon needed). Dev affordance only — the gate is a
    // static URL check at module init.
    const params =
        typeof window !== 'undefined'
            ? new URLSearchParams(window.location.search)
            : new URLSearchParams();
    const ft8Demo = params.has('ft8demo');
    const pileupDemo = params.has('pileupdemo');
    const anyDemo = ft8Demo || pileupDemo;

    // Local form state for the setup card. Lives here (not in
    // configState) because configState mirrors the DAEMON's view —
    // stationCallsign there means "what the daemon has persisted,"
    // not "what the operator is typing." We push to configState only
    // after the daemon confirms the save.
    let callsign: string = $state('');
    let saving: boolean = $state(false);

    // First-run hand-off interstitial. A successful callsign save flips
    // configState.setupComplete=true, which would otherwise fall straight
    // through to the logging UI. We hold on a "setup complete" screen
    // instead — offering the Config app to finish station setup — until
    // the operator picks a path. Purely local + session-scoped: it's only
    // ever true right after THIS session's save, so a returning operator
    // (setupComplete already true on load) never sees it.
    let justCompleted: boolean = $state(false);

    // Focus the welcome-page callsign input the moment it enters the
    // DOM. The setup snippet renders behind the
    // {#if !configState.setupComplete} gate, so a script-level
    // onMount focus call would fire before the input exists. A node
    // action runs at the right moment: when the {#if} branch becomes
    // truthy and the input mounts.
    function autofocus(node: HTMLElement) {
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
                    // configState.setupComplete=true reactively. Rather
                    // than fall straight to main_app, hold the hand-off
                    // interstitial (justCompleted) so the operator is
                    // offered the Config app before logging starts.
                    configState.applyResponse(outcome.config);
                    justCompleted = true;
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
        if (anyDemo) return; // playground: no daemon, no config fetch
        const outcome = await fetchConfig();
        switch (outcome.kind) {
            case 'ok':
                configState.applyResponse(outcome.config);
                void configState.refreshLogbookCount();
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
{#if ft8Demo}
    <Ft8EnrichDemo />
{:else if pileupDemo}
    <Ft8PileupDemo />
{:else if configState.loaded}
    {#if !configState.setupComplete}
        {@render setup()}
    {:else if justCompleted}
        {@render setup_done()}
    {:else}
        {@render main_app()}
    {/if}
{/if}
<Toasts />
<ManualLink />

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
    <!-- Multi-tab awareness (advisory, not a lock): the daemon reports >1 rig-event
         subscriber, so another browser tab can move this same radio. Full operating-lock
         is future work; the dangerous cases (double-key, mic-steal, TX) are already
         prevented daemon-side. -->
    {#if bridgeState.tabCount > 1}
        <div
            class="mx-auto mt-6 w-fit max-w-200 rounded-md border border-amber-300 bg-amber-50 px-3 py-1.5 text-center text-sm text-amber-800"
            role="status"
        >
            ⚠ Another tab is controlling this rig ({bridgeState.tabCount} tabs open) — a
            frequency, band, or mode change here moves the same radio.
        </div>
    {/if}
    <main class="relative rounded-xl border border-line-soft h-166 w-fit mx-auto mt-12">
        <LoggingCard />
        <StackingDrawer />
        <!-- FT8 pile-up rail — twin of the Phone/CW Call Stack, hung off the same right
             edge. Self-guards on a non-empty ft8PileupStack, so it shows only in FT8 mode
             with queued callers (the two never coincide in practice — one operating mode
             is used at a time). -->
        <div class="absolute top-0 -right-40 z-10">
            <Ft8PileupDrawer />
        </div>
    </main>
{/snippet}

{#snippet setup()}
    <main class="rounded-xl border border-line-soft h-120 w-200 mx-auto mt-12 p-8">
        <h1 class="text-center font-semibold text-2xl">Welcome to Station Manager</h1>
        <div class="py-4">
            <p>
                Before you can use Station Manager, the <i>default log book</i> needs to be
                initialised. All this requires is a callsign. it is generally recommended that the
                <i>default log book</i>
                be associated with you personal callsign.
            </p>
            <p>
                If you use QRZ.com and plan to forward QSOs to it, then the callsign entered here
                should be the same as the callsign used for your target logbook at QRZ.com. If you
                are not sure what callsign to enter, just use your personal callsign - it can easily
                be changed later.
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

<!--
    First-run hand-off interstitial. Shown once, immediately after the
    callsign save completes (justCompleted), before the logging UI. The
    Config-app action is a plain full-page <a href="/config/"> — the
    config SPA is a separate mount, not an in-app route, so a dumb link
    is correct (no router). "Start logging" stays in the SPA: it just
    clears justCompleted so the main_app branch renders.
-->
{#snippet setup_done()}
    <main class="rounded-xl border border-line-soft h-120 w-200 mx-auto mt-12 p-8">
        <h1 class="text-center font-semibold text-2xl">
            <span class="text-green-500">✓</span> Setup complete{configState.loggingStation.stationCallsign
                ? ` — ${configState.loggingStation.stationCallsign}`
                : ''}
        </h1>
        <div class="py-4 text-center">
            <p>Your default logbook is ready and you can start logging right away.</p>
            <p class="mt-3">Want to finish setting up your station first?</p>
            <p>The <i>Config</i> app is where you set up your rig (CAT),
                QSO forwarding (QRZ, ClubLog…), session email, FT8, and the rest of your
                    station details.
            </p>
        </div>
        <div class="flex flex-col items-center space-y-3 pt-2">
            <a
                href="/config/"
                class="inline-flex h-9 items-center justify-center rounded-md bg-focus px-4 py-1.5 text-base font-semibold text-white shadow-sm hover:bg-focus-ring focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
                use:autofocus>Open the Config app →</a
            >
            <button
                type="button"
                onclick={() => (justCompleted = false)}
                class="cursor-pointer text-sm text-ink opacity-70 hover:opacity-100 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
                >Start logging →</button
            >
        </div>
    </main>
{/snippet}
