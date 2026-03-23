<script lang="ts">
    import {qsoState} from "$lib/states/new-qso-state.svelte";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {OpenInBrowser} from "$lib/wailsjs/go/facade/Service"
    import {configState} from "$lib/states/config-state.svelte";

    let qslCardWanted = $state(false);

    /**
     * Handler function for the onClick event.
     * Toggles the QSL card wanted status by updating the QSL wanted value
     * in the QSO form state. When the QSL card is wanted, the value is set
     * to `adifNoString`. Otherwise, it is set to `adifYesString`.
     */
    const onClick = (): void => {
        if (qslCardWanted) {
            qsoState.qslWanted = 'Y';
        } else {
            qsoState.qslWanted = 'N';
        }
    }

    const viewWebSite = async ():Promise<void> => {
        if (qsoState.web === "") {
            return;
        }
        try {
            await OpenInBrowser(qsoState.web);
        } catch(e: unknown) {
            handleAsyncError(e, "Failed to open web site")
        }
    }

    const callsignLookup = async ():Promise<void> => {
        if (qsoState.call.length < 3) {
            return;
        }
        try {
            await OpenInBrowser(configState.qrz_view_url + qsoState.call.toUpperCase());
        } catch(e: unknown) {
            handleAsyncError(e, "Failed to open callsign lookup")
        }
    }
</script>

<div class="flex flex-row space-x-4 px-5">
    <div class="flex flex-col mt-3 w-1/2">
        <div class="flex flex-row space-x-4">
            <div>
                <label class="block text-sm/5 font-medium" for="rx_pwr">Power</label>
                <div class="mt-2 w-25">
                    <input
                            bind:value={qsoState.rx_pwr}
                            class="block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                            type="text"
                            id="rx_pwr"
                            placeholder="RX Power"
                            autocomplete="off"
                            title="Contacted Station's Power">
                </div>
            </div>
            <div>
                <label class="block text-sm/5 font-medium" for="rig">Rig</label>
                <div class="mt-2 w-90">
                    <textarea
                            bind:value={qsoState.rig}
                            class="resize-none w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                            id="rig"
                            placeholder="Working conditions"></textarea>
                </div>
            </div>
        </div>
        <div class="-mt-4">
            <label class="block text-sm/5 font-medium" for="notes">Notes</label>
            <textarea
                    bind:value={qsoState.notes}
                    class="mt-2 w-119 h-20 resize-none rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                    id="notes"
                    placeholder="My personal notes"></textarea>
        </div>
        <div class="flex flex-row mt-3 text-sm/5 font-medium space-x-4">
            <div class="flex flex-row">
                <div class="w-18">CQ Zone:</div>
                <div>{qsoState.cqz}</div>
            </div>
        </div>
        <div class="flex flex-row items-center mt-5">
            <label class="mr-2 text-sm/5 font-medium" for="qsl_wanted">Request QSL:</label>
            <div class="group grid size-5 grid-cols-1">
                <input
                        onclick={onClick}
                        checked={qslCardWanted}
                        id="qsl_wanted"
                        type="checkbox"
                        title="Request QSL from contacted station"
                        class="size-5 col-start-1 row-start-1 appearance-none rounded-sm border border-gray-400 bg-white checked:border-indigo-600 checked:bg-indigo-600 indeterminate:border-indigo-600 indeterminate:bg-indigo-600 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:border-gray-300 disabled:bg-gray-100 disabled:checked:bg-gray-100 forced-colors:appearance-auto"
                />
                <svg class="pointer-events-none col-start-1 row-start-1 size-5 self-center justify-self-center stroke-white group-has-disabled:stroke-gray-950/25" viewBox="0 0 14 14" fill="none">
                    <path class="opacity-0 group-has-checked:opacity-100" d="M3 8L6 11L11 3.5" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
                    <path class="opacity-0 group-has-indeterminate:opacity-100" d="M3 7H11" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
            </div>
        </div>
    </div>
    <div class="flex flex-col mt-3 w-1/2">
        <div>
            <label class="block text-sm/5 font-medium" for="email">Email</label>
            <div class="mt-2 w-70">
                <input
                        bind:value={qsoState.email}
                        class="w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300"
                        id="email"
                        type="email"
                        placeholder="Email" disabled/>
            </div>
        </div>
        <div class="mt-2">
            <label class="block text-sm/5 font-medium" for="web">Web Site</label>
            <div class="flex mt-2">
                <input
                        bind:value={qsoState.web}
                        class="w-min bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300"
                        id="web"
                        type="text"
                        placeholder="Web site" disabled/>
                <button
                        onclick={viewWebSite}
                        title="View web site"
                        class="ml-2 cursor-pointer hover:text-indigo-600 disabled:cursor-not-allowed"
                        aria-label="View web site"
                        disabled={qsoState.web === ""}>
                    <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5 ml-2">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
                    </svg>
                </button>
            </div>
        </div>
        <div class="flex mt-4">
            <div class="block text-sm/5 font-medium">Lookup on QRZ.com</div>
            <button
                    onclick={callsignLookup}
                    title="Callsign lookup"
                    class="ml-2 cursor-pointer hover:text-indigo-600 disabled:cursor-not-allowed"
                    aria-label="Callsign lookup"
                    disabled={qsoState.call.length < 3}>
                <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5 ml-2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
                </svg>
            </button>
        </div>
    </div>
</div>
