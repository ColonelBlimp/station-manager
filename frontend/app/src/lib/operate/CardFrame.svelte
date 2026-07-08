<script lang="ts">
    // Uniform tile wrapper (ADR 0046). Supplies the DRAG grip, shown ONLY in
    // arrange mode, so during operation the tile is clean (zero fast-path
    // pixels). The card body renders untouched (size/content owned by the card,
    // not the frame). Hiding an info card is its own always-visible header X
    // (the logging card is deliberately not hideable); the frame is drag only.
    import { layout, TILES, type TileId } from './layout.svelte';

    let {
        id,
        dragging = false,
        onGrip,
    }: { id: TileId; dragging?: boolean; onGrip: (id: TileId, e: PointerEvent) => void } = $props();

    const Comp = $derived(TILES[id].component);
</script>

<div class="tile" class:tile-dragging={dragging} data-dragging={dragging ? 'true' : null}>
    {#if layout.arranging}
        <div class="tile-frame">
            <button
                class="tile-grip"
                title="Drag to move"
                aria-label="Move {TILES[id].name}"
                onpointerdown={(e) => onGrip(id, e)}
            >
                ⠿
            </button>
            <span class="tile-fname">{TILES[id].name}</span>
        </div>
    {/if}
    <Comp />
</div>
