<script lang="ts">
    // The Operate tile board (ADR 0046). Renders columns of tiles from the
    // layout state; hosts the pointer-drag engine. No overlap: tiles pack into
    // columns and REFLOW live as you drag (the dragged tile moves in the keyed
    // {#each}, so Svelte repositions the node rather than rebuilding it), while a
    // lightweight ghost tracks the pointer. Pointer Events, not HTML5 DnD
    // (cross-browser reliable — the POC established this).
    import {
        layout,
        TILES,
        setColumnsLive,
        commitLayout,
        type TileId,
    } from './layout.svelte';
    import CardFrame from './CardFrame.svelte';

    let boardEl: HTMLDivElement;

    // Drag state: the id being dragged + the ghost's geometry. Null when idle.
    let drag = $state<null | {
        id: TileId;
        w: number;
        h: number;
        x: number;
        y: number;
        offX: number;
        offY: number;
    }>(null);

    function onGrip(id: TileId, e: PointerEvent): void {
        if (e.button !== 0) return;
        e.preventDefault();
        const tileEl = (e.currentTarget as HTMLElement).closest('.tile') as HTMLElement;
        const r = tileEl.getBoundingClientRect();
        drag = {
            id,
            w: r.width,
            h: r.height,
            x: r.left,
            y: r.top,
            offX: e.clientX - r.left,
            offY: e.clientY - r.top,
        };
        document.body.style.userSelect = 'none';
        window.addEventListener('pointermove', onMove);
        window.addEventListener('pointerup', onUp, { once: true });
    }

    function onMove(e: PointerEvent): void {
        if (!drag) return;
        drag.x = e.clientX - drag.offX;
        drag.y = e.clientY - drag.offY;
        reorderTo(e.clientX, e.clientY);
    }

    function onUp(): void {
        window.removeEventListener('pointermove', onMove);
        document.body.style.userSelect = '';
        drag = null;
        commitLayout(); // persist once (if pinned); the reorder already happened live
    }

    // Nearest column to x (inside wins; else closest centre).
    function nearestCol(px: number): number {
        const cols = [...boardEl.querySelectorAll('.tile-col')] as HTMLElement[];
        let best = 0,
            bestDist = Infinity;
        cols.forEach((el, i) => {
            const r = el.getBoundingClientRect();
            const d = px >= r.left && px <= r.right ? -1 : Math.abs(px - (r.left + r.width / 2));
            if (d < bestDist) {
                bestDist = d;
                best = i;
            }
        });
        return best;
    }

    // Insertion index within a column by pointer-y, measured against the tiles
    // that AREN'T the one being dragged.
    function insertionIndex(colEl: HTMLElement, py: number): number {
        const tiles = [...colEl.querySelectorAll('.tile:not([data-dragging])')] as HTMLElement[];
        for (let i = 0; i < tiles.length; i++) {
            const r = tiles[i].getBoundingClientRect();
            if (py < r.top + r.height / 2) return i;
        }
        return tiles.length;
    }

    function eqCols(a: TileId[][], b: TileId[][]): boolean {
        if (a.length !== b.length) return false;
        for (let i = 0; i < a.length; i++) {
            if (a[i].length !== b[i].length) return false;
            for (let j = 0; j < a[i].length; j++) if (a[i][j] !== b[i][j]) return false;
        }
        return true;
    }

    function reorderTo(px: number, py: number): void {
        if (!drag) return;
        const ci = nearestCol(px);
        const colEls = [...boardEl.querySelectorAll('.tile-col')] as HTMLElement[];
        const idx = insertionIndex(colEls[ci], py);
        const next = layout.current.columns.map((c) => c.filter((x) => x !== drag!.id));
        next[ci].splice(idx, 0, drag.id);
        if (!eqCols(next, layout.current.columns)) setColumnsLive(next);
    }
</script>

<div class="tile-board" class:arranging={layout.arranging} bind:this={boardEl}>
    {#each layout.current.columns as col, ci (ci)}
        <div class="tile-col" data-col={ci}>
            {#each col as id (id)}
                <CardFrame {id} dragging={drag?.id === id} {onGrip} />
            {/each}
        </div>
    {/each}
</div>

{#if drag}
    <!-- Lightweight ghost tracking the pointer (a name chip, not a live card —
         no double-mount of a stateful card like the logging form). -->
    <div
        class="tile-ghost"
        style="left:{drag.x}px; top:{drag.y}px; width:{drag.w}px; height:{drag.h}px"
    >
        <div class="tile-frame">
            <span class="tile-grip">⠿</span>
            <span class="tile-fname">{TILES[drag.id].name}</span>
        </div>
    </div>
{/if}
