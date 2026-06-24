<script lang="ts">
    // Per-tab save footer. Each config tab owns its own dirty/saving state and
    // save+cancel handlers (the Rigs tab will write via rig CRUD, the rest via
    // PUT /v1/config), so the footer is a shared component the tab renders at the
    // bottom of its body rather than global shell chrome. Save/Cancel disable
    // when there's nothing to save or a save is in flight.
    let {
        dirty,
        saving,
        status,
        onsave,
        oncancel,
    }: {
        dirty: boolean;
        saving: boolean;
        /** 'ok' (saved), an error message to show, or null (idle). */
        status: string | null;
        onsave: () => void;
        oncancel: () => void;
    } = $props();
</script>

<div class="mt-8 flex items-center justify-end gap-3 border-t border-gray-200 pt-4">
    {#if status === 'ok'}
        <span class="text-sm text-green-700">Saved.</span>
    {:else if status}
        <span class="text-sm text-red-700">{status}</span>
    {/if}
    <button
        type="button"
        onclick={oncancel}
        disabled={!dirty || saving}
        class="cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium text-gray-600 hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-40"
    >
        Cancel
    </button>
    <button
        type="button"
        onclick={onsave}
        disabled={!dirty || saving}
        class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-40"
    >
        {saving ? 'Saving…' : 'Save'}
    </button>
</div>
