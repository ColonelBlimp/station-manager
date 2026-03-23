<script lang="ts">
    import {configState} from '$lib/states/config-state.svelte';
    import {qsoState} from "$lib/states/new-qso-state.svelte";
    import {sessionState} from "$lib/states/session-state.svelte";
    import {catState} from "$lib/states/cat-state.svelte";
    import {getFocusContext} from "@station-manager/shared-utils/svelte";

    const {refs} = getFocusContext();

    let multiplierOn: boolean = $state(configState.use_power_multiplier);

    let txPower = $derived.by(() => {
        let pwr = parseInt(catState.txPower);
        if (isNaN(pwr)) {
            pwr = configState.default_power;
        }
        if (multiplierOn) {
            pwr = pwr * configState.power_multiplier;
        }
        return pwr.toString();
    });

    let isValid = $state(true);
    let isRandomQso: boolean = $derived.by((): boolean => {
        return qsoState.qso_random === 'Y';
    });

    const toggleRandonQso = (): void => {
        qsoState.qso_random = isRandomQso ? 'N' : 'Y';
    }
</script>

<div class="cursor-default flex flex-row px-5">
    <div class="flex flex-col w-1/5">
        <div class="mt-3">
            <label for="station_callsign" class="flex flex-row text-sm/5 font-medium">
                <span>Station's callsign</span>
            </label>
            <div class="mt-2 w-35">
                <input
                        bind:value={configState.logbook.callsign}
                        type="text"
                        id="station_callsign"
                        class="uppercase block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 {isValid ? '' : 'outline-red-600 focus:outline-red-600'}"
                        autocomplete="off"
                        spellcheck="false"
                        title="Logging Station's Callsign"
                        disabled
                />
            </div>
        </div>
        <div class="mt-3">
            <label for="owner_callsign" class="flex flex-row text-sm/5 font-medium">
                <span>Owner's callsign</span>
            </label>
            <div class="mt-2 w-35">
                <input
                        bind:value={configState.owners_callsign}
                        type="text"
                        id="owner_callsign"
                        class="uppercase block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 {isValid ? '' : 'outline-red-600 focus:outline-red-600'}"
                        autocomplete="off"
                        spellcheck="false"
                        title="Owner of the logging Station"
                        disabled
                />
            </div>
        </div>
        <div class="mt-3">
            <label for="operator_callsign" class="flex flex-row text-sm/5 font-medium">
                <span>Operator's callsign</span>
            </label>
            <div class="mt-2 w-35">
                <input
                        bind:this={refs.operatorCallsignInput}
                        bind:value={sessionState.operator}
                        type="text"
                        id="operator_callsign"
                        class="uppercase block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 {isValid ? '' : 'outline-red-600 focus:outline-red-600'}"
                        autocomplete="off"
                        spellcheck="false"
                        title="Guest Operator's Callsign\nChanged in the config file."
                />
            </div>
        </div>
    </div>
    <div class="flex flex-col w-1/5">
        <div class="flex flex-row mt-3">
            <label class="mr-2 mt-0.5 text-sm font-medium" for="random_qso">Random QSO</label>
            <div class="group relative inline-flex h-6 w-11 shrink-0 rounded-full bg-gray-300 p-0.5 outline-offset-2 outline-indigo-600 transition-colors duration-200 ease-in-out has-checked:bg-indigo-600 has-focus-visible:outline-2 dark:bg-white/5 dark:inset-ring-white/10 dark:outline-indigo-500 dark:has-checked:bg-indigo-500">
                <span class="size-5 rounded-full bg-white shadow-xs transition-transform duration-200 ease-in-out group-has-checked:translate-x-5"></span>
                <input
                        bind:checked={isRandomQso}
                        onclick={toggleRandonQso}
                        type="checkbox"
                        name="random_qso"
                        aria-label="Random QSO"
                        class="cursor-pointer absolute inset-0 appearance-none focus:outline-hidden"/>
            </div>
        </div>
        <div class="flex flex-col space-y-1.5 mt-3">
            <label class="block text-sm/5 font-medium" for="tx_pwr">TX Power</label>
            <div class="flex items-center w-35.5">
                <input
                        bind:value={txPower}
                        class="mr-2 block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                        type="text"
                        id="tx_pwr"
                        maxlength="4"
                        minlength="1"
                        autocomplete="off"
                        title="Logging Station's Power">
                <div class="group relative inline-flex h-6 w-11 shrink-0 rounded-full bg-gray-200 p-0.5 inset-ring inset-ring-gray-900/5 outline-offset-2 outline-indigo-600 transition-colors duration-200 ease-in-out has-checked:bg-indigo-600 has-focus-visible:outline-2 dark:bg-white/5 dark:inset-ring-white/10 dark:outline-indigo-500 dark:has-checked:bg-indigo-500">
                    <span class="size-5 rounded-full bg-white shadow-xs ring-1 ring-gray-900/5 transition-transform duration-200 ease-in-out group-has-checked:translate-x-5"></span>
                    <input
                            bind:checked={multiplierOn}
                            type="checkbox" name="setting" aria-label="Use setting" class="absolute inset-0 appearance-none focus:outline-hidden" />
                </div>

            </div>
        </div>
    </div>
</div>
