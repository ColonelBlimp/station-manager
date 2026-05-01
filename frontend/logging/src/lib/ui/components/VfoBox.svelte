<script lang="ts">

    interface Props {
        label: string;
        isSplit?: boolean;
        action?: string;
        bgColor?: string;
    }

    let { label, isSplit = false, action = 'RX'}: Props = $props();
</script>

<div class="flex flex-col font-medium text-xs text-white">
    {#if isSplit}
        <div class="text-center w-13 h-4.25 rounded-t {action === 'RX' ? 'bg-green-600/80' : 'bg-rose-900/80'}">{action}</div>
        <div class="text-center w-13 h-4.25 rounded-b bg-blue-700/90">{label}</div>
    {:else}
        <div class="flex w-13 h-8.5 rounded {action === 'RX' ? 'bg-rose-900/80' : 'bg-gray-500/80'} items-center pl-2">{label}</div>
    {/if}
</div>
