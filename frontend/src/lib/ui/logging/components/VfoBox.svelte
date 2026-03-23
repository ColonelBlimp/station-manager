<script lang="ts">
interface Props {
    label: string;
    action?: string;
    bgColorCss?: string;
    isSplit?: boolean;
    bgColorTopCss?: string;
    bgColorBottomCss?: string;
}

let {
    label,
    action = 'RX',
    isSplit = false,
    bgColorCss = 'bg-rose-900/80',
    bgColorTopCss = 'bg-green-600/80',
    bgColorBottomCss = 'bg-blue-700/90'
}: Props = $props();

</script>

<div class="text-xs font-medium">
    {#if isSplit}
        <div class="{bgColorTopCss} cursor-default rounded-t  px-1 text-white w-13 h-4 text-center">
            {action}
        </div>
        <div class="{bgColorBottomCss} cursor-default rounded-b px-1 text-white w-13 h-4 text-center">
            {label}
        </div>
    {:else}
        <div class="{bgColorCss} cursor-default rounded text-white w-13 h-8 text-center pt-2">
            {label}
        </div>
    {/if}
</div>
