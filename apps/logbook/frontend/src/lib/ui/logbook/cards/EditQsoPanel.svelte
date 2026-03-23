<script lang="ts">
    import {onMount} from "svelte";
    import {type QsoEditState, qsoEditState} from "$lib/states/qso-edit-state.svelte";
    import FormControls from "$lib/ui/logbook/components/FormControls.svelte";
    import {
        inputBase,
        inputBaseUppercase, labelBase, selectBase, selectWrapper,
    } from "@station-manager/shared-utils";
    import {GetQsoById} from "$lib/wailsjs/go/facade/Service";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {getFocusContext} from "@station-manager/shared-utils/svelte";

    interface Props {
        qsoId: number
    }

    const focusContext = getFocusContext();
    const labelCss = labelBase + " mb-2";
    const cellWidth = "w-34";

    let {qsoId}: Props = $props();
    let qso: QsoEditState = $state(qsoEditState);

    const onCancel = (): void => {
        if (qsoEditState.panelOpen) {
            qsoEditState.panelOpen = false;
        }
    }

    const onSubmit = (): void => {}

    onMount(async (): Promise<void> => {
       try {
           await getFocusContext().focus('callsignInput');
           qso.fromQso(await GetQsoById(qsoId));
       } catch (e: unknown) {
           handleAsyncError(e, 'EditQsoPanel.svelte:onMount')
       }
    });
</script>

<div>
    <div class="flex flex-col h-126">
        <div class="flex flex-row gap-x-4 h-20 w-full">
            <div class={cellWidth}>
                <label for="call" class={labelCss}>Callsign</label>
                <input
                        bind:value={qso.call}
                        bind:this={focusContext.refs.callsignInput}
                        id="call"
                        autocomplete="off"
                        spellcheck="false"
                        class={inputBaseUppercase}/>
            </div>
            <div class={cellWidth}>
                <label for="band" class={labelCss}>Band</label>
                <input
                        id="band"
                        type="text"
                        bind:value={qso.band}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"
                        disabled/>
            </div>
            <div class={cellWidth}>
                <label for="freq" class={labelCss}>Frequency</label>
                <input
                        id="freq"
                        type="text"
                        bind:value={qso.freq}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"
                        />
            </div>
            <div class={cellWidth}>
                <label for="freq_rx" class={labelCss}>Frequency RX</label>
                <input
                        id="freq_rx"
                        type="text"
                        bind:value={qso.freq_rx}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"
                        disabled/>
            </div>
            <div class={cellWidth}>
                <label for="main_mode" class={labelCss}>Main Mode</label>
                <div class="{selectWrapper}">
                    <select id="main_mode" class={selectBase} bind:value={qso.mode}>
                        <option value="SSB">SSB</option>
                        <option value="CW">CW</option>
                    </select>
                    <svg class="pointer-events-none col-start-1 row-start-1 mr-2 size-5 self-center justify-self-end text-gray-500 sm:size-4" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" data-slot="icon">
                        <path fill-rule="evenodd" d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z" clip-rule="evenodd" />
                    </svg>
                </div>
            </div>
            <div class={cellWidth}>
                <label for="sub_mode" class={labelCss}>Sub Mode</label>
                <div class="{selectWrapper}">
                    <select id="sub_mode" class={selectBase} bind:value={qso.submode}>
                        <option value="USB">USB</option>
                        <option value="LSB">LSB</option>
                    </select>
                    <svg class="pointer-events-none col-start-1 row-start-1 mr-2 size-5 self-center justify-self-end text-gray-500 sm:size-4" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" data-slot="icon">
                        <path fill-rule="evenodd" d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z" clip-rule="evenodd" />
                    </svg>
                </div>
            </div>
        </div>
        <div class="flex flex-row gap-x-4 h-32 w-full">
            <div class={cellWidth}>
                <label for="date_on" class={labelCss}>Date On</label>
                <input
                        id="date_on"
                        type="date"
                        bind:value={qso.qso_date}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"/>
            </div>
            <div class={cellWidth}>
                <label for="date_off" class={labelCss}>Date Off</label>
                <input
                        id="date_off"
                        type="date"
                        bind:value={qso.qso_date_off}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"/>
            </div>
            <div class={cellWidth}>
                <label for="time_on" class={labelCss}>Time On</label>
                <input
                        id="time_on"
                        type="time"
                        bind:value={qso.time_on}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"/>
            </div>
            <div class={cellWidth}>
                <label for="time_off" class={labelCss}>Time Off</label>
                <input
                        id="time_off"
                        type="time"
                        bind:value={qso.time_off}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"/>
            </div>
            <div class={cellWidth}>
                <label for="rst_sent" class={labelCss}>RST Sent</label>
                <input
                        id="rst_sent"
                        type="text"
                        bind:value={qso.rst_sent}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"/>
            </div>
            <div class={cellWidth}>
                <label for="rst_rcvd" class={labelCss}>RST Rcvd</label>
                <input
                        id="rst_rcvd"
                        type="text"
                        bind:value={qso.rst_rcvd}
                        class={inputBase}
                        autocomplete="off"
                        spellcheck="false"/>
            </div>
        </div>
    </div>
    <div class="flex justify-end mt-2">
        <FormControls submitFunc={onSubmit} cancelFunc={onCancel}/>
    </div>
</div>
