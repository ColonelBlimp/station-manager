<script lang="ts">
    import {
        DETAILS_TAB_TITLE,
        SESSION_TAB_TITLE,
        STATION_PANEL,
        WORKED_TAB_TITLE
    } from "$lib/ui/logging/panels/constants";
    import {qsoState} from "$lib/states/new-qso-state.svelte";
    import {sessionState} from "$lib/states/session-state.svelte";
    import {appState} from "$lib/states/app-state.svelte";
    import {configState} from "$lib/states/config-state.svelte";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {showToast} from "$lib/utils/toast";
    import {ForwardSessionQsosByEmail} from "$lib/wailsjs/go/facade/Service";
    import {getFocusContext} from "@station-manager/shared-utils/svelte";
    import {getTabButtonClass, inputCompact} from "@station-manager/shared-utils";

    const focusContext = getFocusContext();

    const emailPattern: RegExp = /^(([^<>()[\]\\.,;:\s@"]+(\.[^<>()[\]\\.,;:\s@"]+)*)|(".+"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$/;

    const btnClass = (value: string) => getTabButtonClass(selected === value);

    let selected = $derived(appState.activePanel);
    let disableFwdByEmail = $derived(sessionState.list.length === 0);
    let sending = $state(false);

    const tabSelectClickHandler = (value: string): void => {
        appState.activePanel = value;
    }
    const sendEmailClickHandler = async (): Promise<void> => {
        if (sending) return;

        const recipientAddress = configState.default_fwd_email;
        if (!recipientAddress || recipientAddress.length === 0) {
            await focusContext.focus('fwdSessionEmailInput');
            return;
        }

        if (emailPattern.test(recipientAddress) == false) {
            await focusContext.focus('fwdSessionEmailInput', true);
            return;
        }

        try {
            sending = true;
            showToast.INFOSTICKY("Sending "+sessionState.list.length+" QSOs by email...");
            await ForwardSessionQsosByEmail(sessionState.list, recipientAddress);
            showToast.SUCCESS("Email sent successfully.");
        } catch(e: unknown) {
            handleAsyncError(e, "Sending session QSOs by email failed.")
        }
        sending = false;
    }

</script>

<div class="flex flex-row items-center h-10 border-b border-gray-300 gap-x-10 px-6">
    <button type="button" onclick={() => tabSelectClickHandler(WORKED_TAB_TITLE)} value={WORKED_TAB_TITLE}
            class="{btnClass(WORKED_TAB_TITLE)} w-37.5">
        <svg class="size-6" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round"
                  d="M9.348 14.652a3.75 3.75 0 0 1 0-5.304m5.304 0a3.75 3.75 0 0 1 0 5.304m-7.425 2.121a6.75 6.75 0 0 1 0-9.546m9.546 0a6.75 6.75 0 0 1 0 9.546M5.106 18.894c-3.808-3.807-3.808-9.98 0-13.788m13.788 0c3.808 3.807 3.808 9.98 0 13.788M12 12h.008v.008H12V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"/>
        </svg>
        <span>{WORKED_TAB_TITLE} ({qsoState.contact_history.length})</span>
    </button>
    <button type="button" onclick={() => tabSelectClickHandler(DETAILS_TAB_TITLE)} value={DETAILS_TAB_TITLE}
            class="{btnClass(DETAILS_TAB_TITLE)} w-30">
        <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
            <path stroke-linecap="round" stroke-linejoin="round"
                  d="M15 9h3.75M15 12h3.75M15 15h3.75M4.5 19.5h15a2.25 2.25 0 0 0 2.25-2.25V6.75A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25v10.5A2.25 2.25 0 0 0 4.5 19.5Zm6-10.125a1.875 1.875 0 1 1-3.75 0 1.875 1.875 0 0 1 3.75 0Zm1.294 6.336a6.721 6.721 0 0 1-3.17.789 6.721 6.721 0 0 1-3.168-.789 3.376 3.376 0 0 1 6.338 0Z"/>
        </svg>
        <span>{DETAILS_TAB_TITLE}</span>
    </button>
    <button type="button" onclick={() => tabSelectClickHandler(STATION_PANEL)} value={STATION_PANEL}
            class="{btnClass(STATION_PANEL)} w-30">
        <svg class="size-6" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round"
                  d="M10.343 3.94c.09-.542.56-.94 1.11-.94h1.093c.55 0 1.02.398 1.11.94l.149.894c.07.424.384.764.78.93.398.164.855.142 1.205-.108l.737-.527a1.125 1.125 0 0 1 1.45.12l.773.774c.39.389.44 1.002.12 1.45l-.527.737c-.25.35-.272.806-.107 1.204.165.397.505.71.93.78l.893.15c.543.09.94.559.94 1.109v1.094c0 .55-.397 1.02-.94 1.11l-.894.149c-.424.07-.764.383-.929.78-.165.398-.143.854.107 1.204l.527.738c.32.447.269 1.06-.12 1.45l-.774.773a1.125 1.125 0 0 1-1.449.12l-.738-.527c-.35-.25-.806-.272-1.203-.107-.398.165-.71.505-.781.929l-.149.894c-.09.542-.56.94-1.11.94h-1.094c-.55 0-1.019-.398-1.11-.94l-.148-.894c-.071-.424-.384-.764-.781-.93-.398-.164-.854-.142-1.204.108l-.738.527c-.447.32-1.06.269-1.45-.12l-.773-.774a1.125 1.125 0 0 1-.12-1.45l.527-.737c.25-.35.272-.806.108-1.204-.165-.397-.506-.71-.93-.78l-.894-.15c-.542-.09-.94-.56-.94-1.109v-1.094c0-.55.398-1.02.94-1.11l.894-.149c.424-.07.765-.383.93-.78.165-.398.143-.854-.108-1.204l-.526-.738a1.125 1.125 0 0 1 .12-1.45l.773-.773a1.125 1.125 0 0 1 1.45-.12l.737.527c.35.25.807.272 1.204.107.397-.165.71-.505.78-.929l.15-.894Z"/>
            <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"/>
        </svg>
        <span>{STATION_PANEL}</span>
    </button>
    <button type="button" onclick={() => tabSelectClickHandler(SESSION_TAB_TITLE)} value={SESSION_TAB_TITLE}
            class="{btnClass(SESSION_TAB_TITLE)} w-44.5">
        <svg class="size-6" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round"
                  d="M8.25 6.75h12M8.25 12h12m-12 5.25h12M3.75 6.75h.007v.008H3.75V6.75Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0ZM3.75 12h.007v.008H3.75V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Zm-.375 5.25h.007v.008H3.75v-.008Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"/>
        </svg>
        <span>{SESSION_TAB_TITLE} ({sessionState.total})</span>
    </button>
    {#if selected === SESSION_TAB_TITLE}
        <div class="flex items-center gap-x-2">
            <div class="w-52.5">
                <input
                        bind:this={focusContext.refs.fwdSessionEmailInput}
                        bind:value={configState.default_fwd_email}
                        type="email"
                        id="fwd_session_by_email"
                        placeholder="Email address"
                        class={inputCompact}>
            </div>
            <div class="w-5">
                <button
                        onclick={sendEmailClickHandler}
                        disabled={disableFwdByEmail}
                        type="button"
                        aria-label="Send email"
                        class="cursor-pointer disabled:cursor-not-allowed">
                    <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor"
                         class="size-5 -rotate-30">
                        <path stroke-linecap="round" stroke-linejoin="round"
                              d="M6 12 3.269 3.125A59.769 59.769 0 0 1 21.485 12 59.768 59.768 0 0 1 3.27 20.875L5.999 12Zm0 0h7.5"/>
                    </svg>
                </button>
            </div>
        </div>
    {/if}
</div>
