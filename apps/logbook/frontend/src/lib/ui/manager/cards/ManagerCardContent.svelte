<script lang="ts">
    import {types} from "$lib/wailsjs/go/models";
    import {configState} from "$lib/states/config-state.svelte";
    import {onMount} from "svelte";
    import {logbookState} from "$lib/states/logbook-state.svelte";
    import {DeleteLogbook, GetLogbookList, NewLogbook} from "$lib/wailsjs/go/facade/Service";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {showToast} from "$lib/utils/toast";
    import {logbookListState} from "$lib/states/logbook-list-state.svelte";

    const Action = {
        CREATE: "Create",
        UPDATE: "Update",
        DELETE: "Delete"
    } as const;

    type Action = typeof Action[keyof typeof Action];

    let nameInputDiv: HTMLDivElement;
    let nameInput: HTMLInputElement;
    let callsignInputDiv: HTMLDivElement;
    let callsignInput: HTMLInputElement;
    let descriptionInput: HTMLTextAreaElement;

    let inputDisabled: boolean = $state(true);
    let currentAction: Action = $state(Action.UPDATE);

    const newLogbookAction = (): void => {
        logbookState.clear();
        inputDisabled = false;
        currentAction = Action.CREATE;
    }

    const loadLogbookAction = (id: number): void => {
        currentAction = Action.UPDATE;
        const lb = logbookListState.list.find((lb) => lb.id === id);
        if (!lb) return;
        logbookState.fromLogbook(lb);
    }

    const submitLogbookAction = (a: Action): void => {
        if (logbookState.name.trim().length === 0) {
            nameInputDiv.classList.remove("focus-within:outline-indigo-600");
            nameInputDiv.classList.add("focus-within:outline-red-600");
            nameInput.focus();
            return;
        }
        if (logbookState.callsign.trim().length < 3) {
            callsignInputDiv.classList.remove("focus-within:outline-indigo-600");
            callsignInputDiv.classList.add("focus-within:outline-red-600");
            callsignInput.focus();
            return;
        }
        console.log("submitLogbookAction: ", a);
        switch (a) {
            case Action.CREATE: {
                const lb = logbookState.toLogbook();
                createNewLogbook(lb);
                break;
            }
            case Action.UPDATE: {
                break;
            }
            case Action.DELETE: {
                if (logbookState.id === 1) {
                    showToast.ERROR("You cannot delete the default logbook.");
                    return;
                }
                deleteLogbook(logbookState.id);
                break;
            }
            default:
                break;
        }
    }

    const cancelLogbookAction = (): void => {
        logbookState.clear();
        inputDisabled = true;
        currentAction = Action.UPDATE;
        logbookState.fromLogbook(configState.logbook);
    }

    const createNewLogbook = async (lb: types.Logbook): Promise<void> => {
        try {
            await NewLogbook(lb);
            logbookListState.list = await GetLogbookList();
            showToast.INFO("New Logbook created...");
        } catch (e: unknown) {
            handleAsyncError(e, 'ManagerCardContent.svelte->createNewLogbook()');
        }
    }

    const deleteLogbook = async (id: number): Promise<void> => {
        try {
            await DeleteLogbook(id);
            showToast.INFO("Logbook deleted...");
            logbookListState.list = await GetLogbookList();
            logbookState.fromLogbook(configState.logbook);
        } catch (e: unknown) {
            handleAsyncError(e, 'ManagerCardContent.svelte->deleteLogbook()');
        }
    }

    onMount((): void => {
        logbookState.fromLogbook(configState.logbook);
    });
</script>

<div class="flex flex-row gap-x-2 p-8">
    <div class="flex flex-col w-145 h-145 border-r border-r-gray-300 pr-10 gap-y-4">
        <button onclick={newLogbookAction} class="flex flex-col border border-gray-300 p-4 rounded-md hover:border-gray-400 hover:cursor-pointer">
            <span class="font-semibold">New Log Book</span>
        </button>
        {#each logbookListState.list as lb (lb.id)}
        <button
                onclick={() => loadLogbookAction(lb.id)}
                class="flex flex-col border border-gray-300 p-4 rounded-md text-left hover:border-gray-400 hover:cursor-pointer disabled:hover:border-gray-300 disabled:cursor-not-allowed">
            <span class="font-semibold">{lb.name}</span>
            <span class="text-sm text-gray-400">{lb.description}</span>
        </button>
        {/each}
    </div>
    <div class="flex flex-col w-full pl-10">
        <div class="w-90">
            <div class="flex flex-col gap-y-4">
                <div>
                    <label for="name" class="block text-sm/6 font-medium text-gray-900 dark:text-white">Name</label>
                    <div class="mt-2">
                        <div
                                bind:this={nameInputDiv}
                                class="flex items-center rounded-md bg-white pl-3 outline-1 -outline-offset-1 outline-gray-300 focus-within:outline-2 focus-within:-outline-offset-2 focus-within:outline-indigo-600">
                            <input
                                    bind:this={nameInput}
                                    bind:value={logbookState.name}
                                    disabled={inputDisabled}
                                    required
                                    id="name"
                                    type="text"
                                    placeholder="Log book name"
                                    class="block bg-white py-1.5 pr-3 pl-1 text-base text-gray-900 placeholder:text-gray-400 focus:outline-none"/>
                        </div>
                    </div>
                </div>
                <div>
                    <label for="callsign" class="block text-sm/6 font-medium text-gray-900 dark:text-white">Callsign</label>
                    <div class="mt-2">
                        <div
                                bind:this={callsignInputDiv}
                                class="flex items-center rounded-md bg-white pl-3 outline-1 -outline-offset-1 outline-gray-300 focus-within:outline-2 focus-within:-outline-offset-2 focus-within:outline-indigo-600 dark:bg-white/5 dark:outline-white/10 dark:focus-within:outline-indigo-500">
                            <input
                                    bind:this={callsignInput}
                                    bind:value={logbookState.callsign}
                                    disabled={inputDisabled}
                                    id="callsign"
                                    type="text"
                                    placeholder="Callsign"
                                    class="uppercase block bg-white py-1.5 pr-3 pl-1 text-base text-gray-900 placeholder:text-gray-400 focus:outline-none"/>
                        </div>
                    </div>
                </div>
                <div>
                    <label for="description" class="block text-sm/6 font-medium text-gray-900">Description</label>
                    <div class="mt-2">
                        <textarea
                                bind:this={descriptionInput}
                                bind:value={logbookState.description}
                                disabled={inputDisabled}
                                id="description"
                                rows="3"
                                class="resize-none block w-full rounded-md bg-white px-3 py-1.5 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"></textarea>
                    </div>
                </div>
            </div>
        </div>
        <div class="flex justify-between mt-4">
            <div>
                <button
                        onclick={() => submitLogbookAction(Action.DELETE)}
                        disabled={logbookState.id === 0 || logbookState.id === configState.logbook.id}
                        class="rounded-md bg-red-600 px-2.5 py-1.5 text-sm font-semibold text-white shadow-xs hover:bg-red-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-red-600 cursor-pointer disabled:bg-gray-400 disabled:cursor-not-allowed">Delete</button>
            </div>
            <div class="flex gap-x-4">
            <button
                    disabled={logbookState.id === 0 || logbookState.id === configState.logbook.id}
                    class="rounded-md bg-indigo-600 px-2.5 py-1.5 text-sm font-semibold text-white shadow-xs hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:bg-gray-400">Select</button>
            <button
                    onclick={() => submitLogbookAction(currentAction)}
                    class="rounded-md bg-indigo-600 w-19 py-1.5 text-sm font-semibold text-white shadow-xs hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 cursor-pointer">
                {currentAction}
            </button>
            <button
                    onclick={cancelLogbookAction}
                    class="rounded-md bg-white px-2.5 py-1.5 text-sm font-semibold text-gray-900 shadow-xs inset-ring inset-ring-gray-300 hover:bg-gray-50 cursor-pointer">Cancel</button>
            </div>
        </div>
    </div>
</div>
