<script lang="ts">
    import "./layout.css";
    import {SvelteToast} from "@zerodevx/svelte-toast";
    import MainNav from "$lib/ui/MainNav.svelte";
    import {onDestroy, onMount} from "svelte";
    import {goto} from "$app/navigation"
    import {sessionState} from "$lib/states/session-state.svelte";
    import {EventsOn} from "$lib/wailsjs/runtime/runtime";
    import {events} from "$lib/wailsjs/go/models";
    import {catState} from "$lib/states/cat-state.svelte";
    import {qsoState, type CatForQsoPayload} from "$lib/states/new-qso-state.svelte";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {Ready, HasDefaultLogbook} from "$lib/wailsjs/go/facade/Service";
    import {configState} from "$lib/states/config-state.svelte";
    import {setFocusContext} from "@station-manager/shared-utils/svelte";
    import {resolve} from "$app/paths";

    let {children} = $props();

    let catStateEventsCancel: () => void = (): void => {}
    let setupComplete: boolean = $state(false);

    // Initialize focus context for cross-component focus management
    const focusContext = setFocusContext();

    const registerForCatStateEvents = (): () => void => {
        return EventsOn(events.EventName.STATUS, (status: Record<string, string>) => {
            if (!status || Object.keys(status).length === 0) return;

            // First update our CAT snapshot
            catState.update(status);

            // Then map the latest CAT state into the subset of QSO fields driven by CAT.
            const payload: CatForQsoPayload = {
                cat_vfoa_freq: catState.vfoaFreq,
                cat_vfob_freq: catState.vfobFreq,
                cat_main_mode: catState.mainMode,
            };
            qsoState.updateFromCAT(payload);
        });
    }

    onMount(async (): Promise<void> => {
        // Here we check if the default logbook is set up. If it isn't, we redirect to the setup page.
        // We do this check before starting the session.
        try {
            setupComplete = await HasDefaultLogbook();
        } catch (e: unknown) {
            console.error("Error checking default logbook: ", e);
        }

        if (!setupComplete) {
            await goto(resolve('/setup'));
            return;
        }

        sessionState.start();
        // We set the operator's callsign to be the same as the logbook's callsign. However, the operator's callsign
        // can be set to a different callsign in contest mode. The station_callsign will always reflect the logbook's callsign.
        // The owner's callsign is the owner of the physical station (if there is one), which may be different from the
        // operator and the station's callsign.
        sessionState.operator = configState.logbook.callsign;
        catStateEventsCancel = registerForCatStateEvents();
        try {
            await Ready();
        } catch (e: unknown) {
            handleAsyncError(e, '+layout.svelte->onMount')
        }

        // Set initial focus to the callsign input field
        await focusContext.focus('callsignInput');
    });

    onDestroy((): void => {
        catStateEventsCancel();
        catStateEventsCancel = () => {};
        sessionState.stop();
    });
</script>

<header>
    <SvelteToast/>
    <MainNav/>
</header>
<main>
    {@render children()}
</main>
