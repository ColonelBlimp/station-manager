<script lang="ts">
    import {sessionState} from "$lib/states/session-state.svelte";
    import {
        formatTime,
        frequencyToBandFromDottedMHz,
        parseDatabaseFreqToDottedKhz
    } from "@station-manager/shared-utils";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {types} from "$lib/wailsjs/go/models";
    import {GetQsoById, CurrentSessionQsoSlice} from "$lib/wailsjs/go/facade/Service";
    import {qsoEditState} from "$lib/states/qso-edit-state.svelte";
    import Callsign from "$lib/ui/logging/components/Callsign.svelte";
    import Mode from "$lib/ui/logging/components/Mode.svelte";
    import {isContestMode} from "$lib/stores/logging-mode-store";
    import {catStateValues} from "$lib/stores/cat-state-store";
    import Rst from "$lib/ui/logging/components/Rst.svelte";
    import TextInput from "$lib/ui/logging/components/TextInput.svelte";
    import DateInput from "$lib/ui/logging/components/DateInput.svelte";
    import TimeInput from "$lib/ui/logging/components/TimeInput.svelte";
    import {isValidCallsignForLog} from "$lib/constants/callsign";
    import {UpdateQso} from "$lib/wailsjs/go/facade/Service";
    import {showToast} from "$lib/utils/toast";
    import {getFocusContext} from "@station-manager/shared-utils/svelte";
    import {sessionTable} from "@station-manager/shared-utils";

    const focusContext = getFocusContext();
    const SHORT_PATH = 'S';
    const LONG_PATH = 'L';

    let showEditPanel = $state(false);
    let isUpdating: boolean = $state(false);

    let freq: string = $derived.by(() => {
        return parseDatabaseFreqToDottedKhz(qsoEditState.freq);
    });

    let freqRx: string = $derived.by(() => {
        return parseDatabaseFreqToDottedKhz(qsoEditState.freq_rx);
    })

    let shortPathRadio: HTMLInputElement | undefined = $state();
    let longPathRadio: HTMLInputElement | undefined = $state();

    const toggleAntPath = (event: Event):void => {
        const target = event.currentTarget as HTMLInputElement;
        if (!target) return;
        if (target.value === SHORT_PATH) {
            qsoEditState.ant_path = SHORT_PATH;
            if (longPathRadio) longPathRadio.checked = false;

        } else if (target.value === LONG_PATH) {
            qsoEditState.ant_path = LONG_PATH;
            if (shortPathRadio) shortPathRadio.checked = false;
        }
    };

    const editSessonQso = async (event: MouseEvent): Promise<void> => {
        const target = event.currentTarget as HTMLButtonElement | null;
        if (!target) {
            return;
        }
        const qsoId: number = Number(target.id);
        try {
            const qso: types.Qso = await GetQsoById(qsoId);
            qsoEditState.fromQso(qso);
            showEditPanel = true;
        } catch(e: unknown) {
            handleAsyncError(e, 'SessionPanel.svelte->editSessonQso')
        }
    }

    const cancelAction = (): void => {
        showEditPanel = false;
    }

    const updateAction = async (): Promise<void> => {
        if (!canLog()) {
            await focusContext.focus('editCallsignInput', true);
            return;
        }

        if (isUpdating) return; // Prevent double-clicks
        isUpdating = true;

        const qso: types.Qso = qsoEditState.toQso();
        try {
            await UpdateQso(qso);
            showToast.SUCCESS("QSO updated...");
        } catch(e: unknown) {
            handleAsyncError(e, 'SessionPanel.svelte->updateAction')
        } finally {
            isUpdating = false;
            showEditPanel = false;
            sessionState.update(await CurrentSessionQsoSlice());
        }
    }

    const canLog = (): boolean => {
        return isValidCallsignForLog(qsoEditState.call)
    };

</script>

<div class="cursor-default flex flex-col">
    <div class="flex flex-row border-b border-b-gray-300 font-semibold h-8 items-center px-4">
        <div class={sessionTable.callsign}>Callsign</div>
        <div class={sessionTable.name}>Name</div>
        <div class={sessionTable.freq}>Freq</div>
        <div class={sessionTable.band}>Band</div>
        <div class={sessionTable.rst}>Send</div>
        <div class={sessionTable.rst}>Rcvd</div>
        <div class={sessionTable.mode}>Mode</div>
        <div class={sessionTable.time}>Time On</div>
        <div class={sessionTable.country}>Country</div>
        <div class="w-32.5">Distance</div>
    </div>
    <div class="flex flex-col overflow-y-scroll h-74.5 px-4">
        {#each sessionState.list as entry (entry.id)}
            <div class="flex flex-row even:bg-gray-300 text-sm h-5.5 p-0.5 rounded-xs">
                <div class={sessionTable.callsign}>{entry.call}</div>
                <div class={sessionTable.name} title="{entry.name}">{entry.name}</div>
                <div class={sessionTable.freq}>{parseDatabaseFreqToDottedKhz(entry.freq)}</div>
                <div class={sessionTable.band}>{entry.band}</div>
                <div class={sessionTable.rst}>{entry.rst_sent}</div>
                <div class={sessionTable.rst}>{entry.rst_rcvd}</div>
                <div class={sessionTable.mode}>{entry.mode}</div>
                <div class={sessionTable.time}>{formatTime(entry.time_on)}</div>
                <div class={sessionTable.country} title="{entry.country}">{entry.country}</div>
                <div class={sessionTable.distance}>{entry.distance} km ({entry.ant_path})</div>
                <div><button onclick={editSessonQso} id={entry.id.toString()} class="cursor-pointer font-semibold hover:text-indigo-700">Edit</button></div>
            </div>
        {/each}
    </div>
</div>
{#if showEditPanel}
<div class="absolute top-12.5 w-full h-175.25 z-40 bg-gray-400/70">
    <div class="bg-white rounded-lg py-8 px-14 h-132.5 w-214 mt-21 mx-auto">
        <div class="flex flex-col gap-y-3 w-186 h-105 px-6">
            <div class="flex flex-row gap-x-4 items-center h-25">
                <Callsign
                        id="call"
                        label="Callsign"
                        value={qsoEditState.call}
                        focusRefKey="editCallsignInput"
                        focusRefs={focusContext.refs}
                />
                {#if $isContestMode}
                    {@render contestLogging()}
                {:else}
                    {@render normalLogging()}
                {/if}
                <Mode
                        id="mode"
                        label="Mode"
                        bind:value={qsoEditState.submode}
                        list={$catStateValues.getMainModes()}
                />
                {@render vfos()}
            </div>
            <div class="flex flex-row gap-x-4 -mt-2">
                <TextInput
                        id="name"
                        label="Name"
                        bind:value={qsoEditState.name}
                />
                <TextInput
                        id="qth"
                        label="Qth"
                        bind:value={qsoEditState.qth}
                        overallWidthCss="w-[170px]"
                />
                <div class="w-70">
                    <label for="comment" class="block text-sm/5 font-medium w-17.5">Comments</label>
                    <div class="mt-2">
                    <textarea
                            bind:value={qsoEditState.comment}
                            id="comment"
                            spellcheck="false"
                            class="h-16 resize-none w-full rounded-md bg-white px-2 py-1.5 text-sm outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                            autocomplete="off"></textarea>
                    </div>
                </div>
            </div>
            <div class="flex flex-row gap-x-4 items-top -mt-2">
                <DateInput
                        id="qso_date"
                        label="Date"
                        bind:value={qsoEditState.qso_date}
                />
                <TimeInput
                        id="time_on"
                        label="Time On (UTC)"
                        bind:value={qsoEditState.time_on}
                        disabled={false}
                />
                <TimeInput
                        id="time_off"
                        label="Time Off (UTC)"
                        bind:value={qsoEditState.time_off}
                        disabled={false}
                />
                <div class="w-70">
                    <label for="notes" class="block text-sm/5 font-medium w-17.5">Notes</label>
                    <div class="mt-2">
                    <textarea
                            bind:value={qsoEditState.notes}
                            id="notes"
                            spellcheck="false"
                            class="h-16 resize-none w-full rounded-md bg-white px-2 py-1.5 text-sm outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                            autocomplete="off"></textarea>
                    </div>
                </div>
            </div>
            <div class="flex flex-row space-x-4 -mt-7">
                <div>
                    <label class="block text-sm/5 font-medium" for="rx_pwr">Power</label>
                    <div class="mt-2 w-25">
                        <input
                                bind:value={qsoEditState.rx_pwr}
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
                            bind:value={qsoEditState.rig}
                            class="resize-none w-full rounded-md bg-white px-3 py-1.5 text-sm outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                            id="rig"
                            placeholder="Working conditions"></textarea>
                    </div>
                </div>
            </div>
            <div class="flex flex-row gap-x-4 -mt-2">
                <div>Ant Path:</div>
                <div class="flex items-center">
                    <input onclick={toggleAntPath} bind:this={shortPathRadio} value={SHORT_PATH} id="short_path" type="radio" checked class="relative size-4 appearance-none rounded-full border border-gray-400 bg-white before:absolute before:inset-1 before:rounded-full before:bg-white not-checked:before:hidden checked:border-indigo-600 checked:bg-indigo-600 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:border-gray-300 disabled:bg-gray-100 disabled:before:bg-gray-400 forced-colors:appearance-auto forced-colors:before:hidden">
                    <label for="short_path" class="ml-1 block text-sm font-medium">Short path</label>
                </div>
                <div class="flex items-center">
                    <input onclick={toggleAntPath} bind:this={longPathRadio} value={LONG_PATH} id="long_path" type="radio" class="relative size-4 appearance-none rounded-full border border-gray-400 bg-white before:absolute before:inset-1 before:rounded-full before:bg-white not-checked:before:hidden checked:border-indigo-600 checked:bg-indigo-600 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:border-gray-300 disabled:bg-gray-100 disabled:before:bg-gray-400 forced-colors:appearance-auto forced-colors:before:hidden">
                    <label for="long_path" class="ml-1 block text-sm font-medium">Long path</label>
                </div>
            </div>
        </div>
        <div class="flex w-full gap-x-3 justify-end">
            <button
                    onclick={updateAction}
                    id="update-contact-btn"
                    type="button"
                    class="disabled:bg-gray-400 disabled:cursor-not-allowed h-9 cursor-pointer rounded-md bg-indigo-600 px-2.5 py-1.5 text-base font-semibold text-white shadow-sm hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
                    title="Ctrl-s">Update QSO
            </button>
            <button
                    onclick={cancelAction}
                    type="button"
                    class="h-9 w-18.5 cursor-pointer rounded-md bg-white px-2.5 py-1.5 text-base font-semibold ring-1 shadow-sm ring-gray-300 ring-inset hover:bg-gray-100"
                    title="ESC">Cancel
            </button>
        </div>
    </div>
</div>
{/if}

{#snippet normalLogging()}
    <Rst
            id="rst_sent"
            label="RST Sent"
            bind:value={qsoEditState.rst_sent}
    />
    <Rst
            id="rst_rcvd"
            label="RST Rcvd"
            bind:value={qsoEditState.rst_rcvd}
    />
{/snippet}

{#snippet contestLogging()}
    <div></div>
{/snippet}

{#snippet vfos()}
    <div class="flex flex-col w-62.5 h-20 mt-6 gap-y-2">
        <div class="flex flex-row items-center">
            <label for="freq_rx" class="text-sm/5 font-medium w-17.5">Freq (TX)</label>
            <div class="w-29">
                <input
                        type="text"
                        autocomplete="off"
                        spellcheck="false"
                        id="freq_rx"
                        bind:value={freq}
                        title="Format: ?#.###.###"
                        class="block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                />
            </div>
            <div class="cursor-default w-8 font-semibold text-base ml-2">{frequencyToBandFromDottedMHz(parseDatabaseFreqToDottedKhz(qsoEditState.freq))}</div>
        </div>
        <div class="flex flex-row items-center">
            <label for="freq" class="text-sm/5 font-medium w-17.5">Freq (RX)</label>
            <div class="w-29">
                <input
                        type="text"
                        autocomplete="off"
                        spellcheck="false"
                        id="freq"
                        bind:value={freqRx}
                        title="Format: ?#.###.###"
                        class="block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
                />
            </div>
            <div class="cursor-default w-8 font-semibold text-base ml-2">{frequencyToBandFromDottedMHz(parseDatabaseFreqToDottedKhz(qsoEditState.freq_rx))}</div>
        </div>
    </div>
{/snippet}