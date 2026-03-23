<script lang="ts">

    interface Props {
        submitFunc: () => void;
        cancelFunc: () => void;
    }

    let {submitFunc = $bindable(), cancelFunc = $bindable()}: Props = $props();

</script>

<div class="flex w-57.5 justify-end gap-x-3">
    <button
            id="log-contact-btn"
            onclick={submitFunc}
            type="button"
            class="disabled:bg-gray-400 disabled:cursor-not-allowed h-9 cursor-pointer rounded-md bg-indigo-600 p-2.5 py-1.5 text-base font-semibold text-white shadow-sm hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
            >Update
    </button>
    <button
            onclick={cancelFunc}
            type="button"
            class="h-9 w-18.5 cursor-pointer rounded-md bg-white px-2.5 py-1.5 text-base font-semibold ring-1 shadow-sm ring-gray-300 ring-inset hover:bg-gray-100"
            >Cancel
    </button>
</div>