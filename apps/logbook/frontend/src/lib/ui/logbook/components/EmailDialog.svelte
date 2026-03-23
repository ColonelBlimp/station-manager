<script lang="ts">
    import {sendEmailFormState, sendEmailState} from "$lib/states/send-email-state.svelte";
    import {inputBase} from "@station-manager/shared-utils";
    import {getFocusContext} from "@station-manager/shared-utils/svelte";
    import {onMount} from "svelte";
    import {showToast} from "$lib/utils/toast";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {ForwardQsosViaEmail} from "$lib/wailsjs/go/facade/Service";
    import {configState} from "$lib/states/config-state.svelte";

    interface Props {
        selections: number[]
    }

    const emailPattern: RegExp = /^(([^<>()[\]\\.,;:\s@"]+(\.[^<>()[\]\\.,;:\s@"]+)*)|(".+"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$/;
    const focusContext = getFocusContext();

    let container: HTMLDivElement | null = null;

    let {selections}: Props = $props();
    let sending = $state(false);

    const cancelClickHandler = ():void => {
        sendEmailState.dialogOpen = false;
    }

    const sendEmail = async (): Promise<void> => {
        if (sending) return;

        const recipientAddress = sendEmailFormState.email;
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
            if (container) container.classList.add("cursor-not-allowed");
            showToast.INFOSTICKY("Sending "+selections.length+" QSOs by email...");
            await ForwardQsosViaEmail(selections, recipientAddress);
            showToast.SUCCESS("Email sent successfully.");
        } catch(e: unknown) {
            handleAsyncError(e, "Sending session QSOs by email failed.")
        } finally {
            if (container) container.classList.add("cursor-default");
            sending = false;
            cancelClickHandler();
        }
    }

    onMount(async (): Promise<void> => {
        sendEmailFormState.email = configState.defaultEmail;
        await focusContext.focus('fwdSessionEmailInput');
    })
</script>

<div
        bind:this={container}
        class="flex absolute top-23.25 w-full h-155 bg-gray-300/50 z-30 pt-32 justify-center">
    <div class="flex flex-col bg-white rounded-lg shadow-2xl w-100 h-42.5 z-40 p-6">
        <div>
            <div>
                <label class="flex flex-row text-sm/5 font-medium text-gray-900" for="email">Send selected ({selections.length}) QSOs to:</label>
                <div class="mt-2 w-[320px]">
                    <input
                            bind:this={focusContext.refs.fwdSessionEmailInput}
                            bind:value={sendEmailFormState.email}
                            id="email"
                            type="email"
                            placeholder="Enter email address"
                            class={inputBase}>
                </div>
            </div>
        </div>
        <div class="mt-6 flex items-center justify-end gap-x-6">
            <button
                    onclick={cancelClickHandler}
                    type="button"
                    class="cursor-pointer rounded-md bg-white px-2.5 py-1.5 text-sm font-semibold text-gray-900 shadow-xs inset-ring inset-ring-gray-300 hover:bg-gray-50 dark:bg-white/10 dark:text-white dark:shadow-none dark:inset-ring-white/5 dark:hover:bg-white/20">
                Cancel</button>
            <button
                    onclick={sendEmail}
                    type="submit"
                    class="cursor-pointer rounded-md bg-indigo-600 px-3 py-2 text-sm font-semibold text-white shadow-xs hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600">
                Send</button>
        </div>
    </div>
</div>
