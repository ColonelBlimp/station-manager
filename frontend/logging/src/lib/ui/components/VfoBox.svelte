<script lang="ts">
    interface Props {
        label: string;
        isSplit?: boolean;
        action?: string;
        disabled?: boolean;
        onSelect?: () => void;
    }
    let {
        label,
        isSplit = false,
        action = 'RX',
        disabled = false,
        onSelect,
    }: Props = $props();

    function handleClick(): void {
        if (disabled) return;
        onSelect?.();
    }

    function handleKeydown(e: KeyboardEvent): void {
        if (disabled) return;
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onSelect?.();
        }
    }
</script>

<div
    role="button"
    tabindex={disabled ? -1 : 0}
    aria-label="Select {label}"
    aria-disabled={disabled}
    data-vfo={label}
    onclick={handleClick}
    onkeydown={handleKeydown}
    class="flex flex-col font-medium text-xs text-white outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 rounded {disabled ? 'cursor-not-allowed' : 'cursor-pointer hover:brightness-110'}"
>
    {#if isSplit}
        <div class="text-center w-13 h-4.25 rounded-t {action === 'RX' ? 'bg-green-600/80' : 'bg-rose-900/80'}">{action}</div>
        <div class="text-center w-13 h-4.25 rounded-b bg-blue-700/90">{label}</div>
    {:else}
        <div class="flex w-13 h-8.5 rounded {action === 'RX' ? 'bg-rose-900/80' : 'bg-gray-500/80'} items-center pl-2">{label}</div>
    {/if}
</div>
