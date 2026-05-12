<script lang="ts">
    interface Props {
        id: string;
        label: string;
        value: string;
        list: string[];
        disabled?: boolean;
        widthClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        list,
        disabled = false,
        widthClass = 'w-34',
    }: Props = $props();
</script>

<div class="{widthClass} input-row">
    <label for={id} class="input-label">{label}</label>
    <div class="grid grid-cols-1 mt-1">
        <select
            bind:value
            {id}
            {disabled}
            tabindex={disabled ? -1 : 0}
            class="col-start-1 row-start-1 w-full appearance-none rounded-md bg-surface py-1.5 pr-8 pl-3 text-base outline-1 -outline-offset-1 outline-line focus:outline-2 focus:-outline-offset-2 focus:outline-focus disabled:bg-surface-disabled disabled:cursor-not-allowed"
        >
            {#each list as mode (mode)}
                <option value={mode}>{mode}</option>
            {/each}
        </select>
        <svg
            class="pointer-events-none col-start-1 row-start-1 mr-2 size-5 self-center justify-self-end text-gray-500 sm:size-4"
            viewBox="0 0 16 16"
            fill="currentColor"
            aria-hidden="true"
            data-slot="icon"
        >
            <path
                fill-rule="evenodd"
                d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z"
                clip-rule="evenodd"
            />
        </svg>
    </div>
</div>
