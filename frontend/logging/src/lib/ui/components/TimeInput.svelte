<script lang="ts">
    interface Props {
        id: string;
        label: string;
        value: string;
        disabled: boolean;
        widthClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        disabled,
        widthClass = 'w-28',
    }: Props = $props();
</script>

<div class="{widthClass}">
    <label for="{id}" class="input-label">{label}</label>
    <div class="relative mt-1">
        <input
            {id}
            bind:value
            type="time"
            disabled={disabled}
            class="block w-full rounded-md bg-white px-1.5 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600"
        />
        <span class="absolute top-2 right-2">
            <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5 text-gray-700">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"/>
            </svg>
        </span>
    </div>
</div>
