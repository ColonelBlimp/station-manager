<script lang="ts">
    interface Props {
        label: string;
        isSplit?: boolean;
        action?: string;
        disabled?: boolean;
        isSelected?: boolean;
        onSelect?: () => void;
    }
    let {
        label,
        isSplit = false,
        action = 'RX',
        disabled = false,
        isSelected = false,
        onSelect,
    }: Props = $props();

    // Interactive only when neither disabled (CAT operating) nor already
    // selected (clicking the active VFO is a no-op). Non-interactive
    // boxes suppress hover/cursor/tabindex/title so the visual matches
    // the behaviour. See Vfos.svelte for the top-vs-bottom positioning.
    const interactive = $derived(!disabled && !isSelected);

    function handleClick(): void {
        if (!interactive) return;
        onSelect?.();
    }

    function handleKeydown(e: KeyboardEvent): void {
        if (!interactive) return;
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onSelect?.();
        }
    }
</script>

<div
    role="button"
    tabindex={interactive ? 0 : -1}
    aria-label="Select {label}"
    aria-disabled={disabled}
    data-vfo={label}
    title={interactive ? 'Select' : null}
    onclick={handleClick}
    onkeydown={handleKeydown}
    class="flex flex-col font-medium text-xs text-white outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 rounded {interactive ? 'cursor-pointer hover:brightness-110' : disabled ? 'cursor-not-allowed' : 'cursor-default'}"
>
    {#if isSplit}
        <div class="text-center w-vfo-w h-vfo-half rounded-t {action === 'RX' ? 'bg-green-600/80' : 'bg-rose-900/80'}">{action}</div>
        <div class="text-center w-vfo-w h-vfo-half rounded-b bg-blue-700/90">{label}</div>
    {:else}
        <div class="flex w-vfo-w h-vfo-full rounded {action === 'RX' ? 'bg-rose-900/80' : 'bg-gray-500/80'} items-center pl-2">{label}</div>
    {/if}
</div>
