<script lang="ts">
    interface Props {
        id: string;
        label: string;
        value: string;
        onClickUpdate: (value: string) => void;
    }

    let {
        id,
        label,
        value = $bindable(),
        onClickUpdate = $bindable(),
    }: Props = $props();
</script>

<div class="flex flex-row items-center">
    <dt class="text-sm font-semibold w-56">{label}</dt>
    <dd class="flex text-sm text-gray-700">
        <span class="w-32 text-red-700 font-semibold">{value}</span>
        <span class="ml-4 shrink-0">
            <button
                    onclick={() => onClickUpdate(value)}
                    id="{id}-update"
                    type="button"
                    class="cursor-pointer bg-white font-semibold text-indigo-600 hover:text-indigo-500"
            >Update</button>
        </span>
    </dd>
</div>
