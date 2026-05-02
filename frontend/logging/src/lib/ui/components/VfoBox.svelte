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
    // boxes suppress hover/cursor/title so the visual matches the
    // behaviour. See Vfos.svelte for the top-vs-bottom positioning.
    //
    // VfoBox is intentionally NOT in the keyboard tab order
    // (`tabindex={-1}` always) — settled session 28. Operators reach
    // the swap action via mouse click or the planned Ctrl+\ keyboard
    // shortcut (ADR 0007). Tab navigation skips the box entirely and
    // lands on the input below it.
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
    tabindex={-1}
    aria-label="Select {label}"
    aria-disabled={disabled}
    data-vfo={label}
    title={interactive ? 'Select VFO' : null}
    onclick={handleClick}
    onkeydown={handleKeydown}
    class="flex flex-col font-medium text-xs text-white outline-none focus-visible:ring-2 focus-visible:ring-focus-ring rounded {interactive ? 'cursor-pointer hover:brightness-110' : disabled ? 'cursor-not-allowed' : 'cursor-default'}"
>
    {#if isSplit}
        <div class="text-center w-vfo-w h-vfo-half rounded-t {action === 'RX' ? 'bg-vfo-rx/80' : 'bg-vfo-tx/80'}">{action}</div>
        <div class="text-center w-vfo-w h-vfo-half rounded-b bg-vfo-label/90">{label}</div>
    {:else}
        <div class="flex w-vfo-w h-vfo-full rounded {action === 'RX' ? 'bg-vfo-tx/80' : 'bg-vfo-inactive/80'} items-center pl-2">{label}</div>
    {/if}
</div>
