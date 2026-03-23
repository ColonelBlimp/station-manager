<script lang="ts">
    import {inputBase, inputBaseUppercase, labelBase} from "@station-manager/shared-utils";
    import {types} from "$lib/wailsjs/go/models";
    import {FinaliseSetup} from "$lib/wailsjs/go/facade/Service";
    import {getFocusContext} from "@station-manager/shared-utils/svelte";
    import {onMount} from "svelte";

    interface Logbook {
        name: string;
        callsign: string;
        description: string;
    }

    const focusContext = getFocusContext();

    let logbook: Logbook = $state({
        name: "Default",
        callsign: "",
        description: "",
    });

    let showMsg = $state(false);

    const onClickSave = async (): Promise<void> => {
        if (showMsg) return;
        if (logbook.callsign.length === 0) {
            await focusContext.focus('callsignInput');
            return;
        }
        if (logbook.description.length === 0) {
            return;
        }

        try {
            const lb: types.Logbook = new types.Logbook();
            lb.name = logbook.name;
            lb.callsign = logbook.callsign;
            lb.description = logbook.description;
            await FinaliseSetup(lb);
            showMsg = true;
        } catch(e: unknown) {
            console.error(e);
        }
    }

    onMount(async (): Promise<void> => {
        await focusContext.focus('callsignInput');
    })

</script>

<div class="mx-20 flex flex-col gap-y-6">
    <div class="mt-4">
        <h2 class="text-center">Setting your Default Log Book</h2>
        <p>This page will create the <b>default log book</b> used by the <i>Station Manager</i> logging application. The callsign
            you provide here will identify the <i>Station Callsign</i> for all QSOs associated with this log book.
        </p>
        <p>If you use QRZ.com, then the callsign entered here should be the same as the callsign for the QRZ.com log book
            to which the QSOs will be forwarded (forwarding of QSOs is configurable and not enabled by default).
        </p>
    </div>
    <div class="flex flex-col gap-y-6 mx-68">
        <div>
            <div class="flex flex-col w-37.5">
                <label for="name" class={labelBase}>Name</label>
                <input
                        bind:value={logbook.name}
                        id="name"
                        type="text"
                        class="{inputBase} text-gray-400"
                        title="This cannot be changed."
                        disabled>
            </div>
        </div>
        <div>
            <div class="flex flex-col w-37.5">
                <label class={labelBase} for="callsign">Callsign</label>
                <input
                        bind:value={logbook.callsign}
                        bind:this={focusContext.refs.callsignInput}
                        id="callsign"
                        type="text"
                        placeholder="Callsign"
                        class={inputBaseUppercase}
                        title="The Default log book's callsign."
                />
            </div>
        </div>
        <div>
            <div class="flex flex-col w-80">
                <label class={labelBase} for="description">Description</label>
                <input
                        bind:value={logbook.description}
                        id="description"
                        type="text"
                        placeholder="Description"
                        class={inputBase}
                />
            </div>
        </div>
        <div class="flex justify-end">
            <button
                    onclick={onClickSave}
                    class="{showMsg ? 'cursor-not-allowed' : 'cursor-pointer'} h-9 rounded-md bg-indigo-600 p-2.5 py-1.5 text-base font-semibold text-white shadow-sm disabled:bg-gray-400 hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 w-20"
                    disabled={showMsg}
            >Save</button>
        </div>
    </div>
    <div class="mx-20 {showMsg ? 'block' : 'hidden'}">
        <p class="font-bold text-red-600">
        Please close this window and restart the application to start using the logging features. If you have any questions,
        please refer to the documentation or reach out to me for support.
        </p>
    </div>
</div>
