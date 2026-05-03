<script lang="ts">
    import LoggingCard from "./lib/ui/cards/LoggingCard.svelte";
    import Toasts from "./lib/ui/Toasts.svelte";
    import {fetchConfig} from "./lib/api/config";
    import {configState} from "./lib/states/config.svelte";
    import {toasts} from "./lib/states/toasts.svelte";
    import {onMount} from "svelte";

    // Boot: GET /v1/config so the SPA knows whether to render the
    // setup dialog (setupComplete=false) or the QSO panel. Failure
    // paths (network, server) leave configState.loaded=true with the
    // stub defaults so the UI doesn't hang on a half-rendered shell;
    // the operator gets a toast telling them what's wrong.
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
    });
</script>

<!--
    h-120 / w-200 are the whole-app shell dimensions — single use,
    no design-token relationship. Adjust here when the operator decides
    they want a different overall card size; nothing else depends on
    these values.

    <Toasts/> is the single mount point for the notification queue per
    ADR 0008. Sits as a sibling of <main> because it's a fixed-position
    overlay; nesting it inside <main> would constrain its z-index stack
    and inherit unrelated layout rules.
-->
<main class="rounded-xl border border-line-soft h-120 w-fit mx-auto mt-12">
    <LoggingCard/>
</main>
<Toasts/>

