<!--
    WorldMap — the reusable great-circle map surface (backlog "QSO contacts
    map", Phase 1; zoom/pan + hover tooltip added per the 2026-07-18 triage).
    Pure presentation: parent owns the data story (which QSOs, what window,
    live updates) and hands down origin + arcs; this component owns
    projection, drawing (lib/map/engine) and view interaction (lib/map/zoom).

    Sized by viewBox so the parent's CSS decides the on-screen size; the
    projection is fitted once to the fixed viewBox space, not re-fitted per
    resize (SVG scales, coordinates stay valid). Zoom/pan is a viewBox-space
    transform on one <g> — the projection never changes, so every cached
    path stays valid at any zoom. Stroke widths and marker radii divide by k
    to hold their on-screen size. Colours ride the app theme tokens so
    light/dark both work with no per-mode markup.
-->
<script lang="ts">
    import {
        createEngine,
        worldCountries,
        worldCountriesHi,
        arcPath,
        project,
        geometryPath,
        graticulePath,
        nightCap,
        SPHERE,
        TWILIGHT_RADII,
        type LatLon,
    } from './engine';
    import { IDENTITY, zoomAt, panBy, toContent, endpointsNear, type ZoomTransform } from './zoom';

    export interface MapArc {
        from: LatLon;
        to: LatLon;
        /** Stable identity for keyed rendering (e.g. the QSO uuid). */
        key: string;
        /** Tooltip line, e.g. "DL3YA · JO62 · 8,431 km · 12.3°". */
        label?: string;
        /** Stroke colour (e.g. the band colour); theme accent when absent. */
        color?: string;
        /** Lateral bow, so several QSOs with ONE station don't draw the same
         *  path on top of itself. 0 (the default) is the plain great circle —
         *  the parent decides, because only it can see the other arcs. */
        bow?: number;
    }

    interface Props {
        /** Operator QTH — drawn as the origin marker when set. */
        origin?: LatLon | null;
        arcs?: MapArc[];
        /** Draw the grey-line (day/night) overlay for this instant; null = off.
         *  The parent owns the clock (it's data, like the QSOs). */
        terminator?: Date | null;
    }

    let { origin = null, arcs = [], terminator = null }: Props = $props();

    // Fixed drawing space; 960×500 fits Natural Earth's ~1.92 aspect.
    const W = 960;
    const H = 500;
    const engine = createEngine(W, H);

    const spherePath = geometryPath(engine, SPHERE);
    const gratPath = graticulePath(engine);

    // Key by id + index: the 50m dataset carries duplicate feature ids
    // (e.g. `036` twice), which a bare-id keyed each rejects.
    function countryPaths(features: ReturnType<typeof worldCountries>['features']) {
        return features.map((f, i) => ({
            id: `${String(f.id ?? '')}·${i}`,
            d: geometryPath(engine, f),
        }));
    }
    const countries = countryPaths(worldCountries().features);

    // Twilight caps, outermost first; their translucent fills stack so the
    // night deepens through the grey line into full dark.
    const nightPaths = $derived(
        terminator === null
            ? []
            : TWILIGHT_RADII.flatMap((r) => {
                  const d = geometryPath(engine, nightCap(terminator, r));
                  return d === null ? [] : [{ r, d }];
              })
    );

    const originXY = $derived(origin === null ? null : project(engine, origin));
    const drawnArcs = $derived(
        arcs.flatMap((a) => {
            const d = arcPath(engine, a.from, a.to, a.bow ?? 0);
            const end = project(engine, a.to);
            return d === null || end === null
                ? []
                : [{ key: a.key, label: a.label, color: a.color, d, end }];
        })
    );
    const ends = $derived(drawnArcs.map((a) => ({ x: a.end[0], y: a.end[1] })));

    // ---- zoom / pan -------------------------------------------------------
    let transform = $state<ZoomTransform>({ ...IDENTITY });
    let svgEl = $state<SVGSVGElement | null>(null);
    let wrapEl = $state<HTMLDivElement | null>(null);
    let panLast = $state<[number, number] | null>(null);

    // Level of detail: past LOD_ZOOM the 110m basemap is visibly blocky, so
    // the 50m set is lazy-loaded once (bundled chunk, no network) and its
    // paths cached against the same never-changing projection. Until it
    // arrives — or if the import ever fails — 110m keeps drawing: fail-soft.
    const LOD_ZOOM = 3;
    let hiCountries = $state<{ id: string; d: string | null }[] | null>(null);
    let hiRequested = false;
    $effect(() => {
        if (transform.k < LOD_ZOOM || hiRequested) return;
        hiRequested = true;
        worldCountriesHi()
            .then((fc) => {
                hiCountries = countryPaths(fc.features);
            })
            .catch(() => {
                // keep drawing 110m; allow a later zoom to retry
                hiRequested = false;
            });
    });
    const drawCountries = $derived(
        transform.k >= LOD_ZOOM && hiCountries !== null ? hiCountries : countries
    );

    /**
     * Pointer position in viewBox coordinates, replicating the default
     * xMidYMid-meet fit by hand — jsdom-friendly (no getScreenCTM/DOMPoint)
     * and exactly right for this fixed 960×500 viewBox.
     */
    function viewPoint(e: { clientX: number; clientY: number }): [number, number] | null {
        if (svgEl === null) return null;
        const r = svgEl.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) return null;
        const scale = Math.min(r.width / W, r.height / H);
        const ox = (r.width - W * scale) / 2;
        const oy = (r.height - H * scale) / 2;
        return [(e.clientX - r.left - ox) / scale, (e.clientY - r.top - oy) / scale];
    }

    // Svelte 5 declares `onwheel` handlers passive, so the zoom handler is
    // attached manually — it must preventDefault or the page scrolls too.
    $effect(() => {
        const el = svgEl;
        if (el === null) return;
        const onWheel = (e: WheelEvent) => {
            e.preventDefault();
            const pt = viewPoint(e);
            if (pt === null) return;
            // deltaMode 1 = lines (Firefox wheel); pinch arrives as
            // ctrl+wheel in pixels and works through the same path.
            const factor = Math.exp(-e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.002));
            transform = zoomAt(transform, pt[0], pt[1], factor, W, H);
        };
        el.addEventListener('wheel', onWheel, { passive: false });
        return () => el.removeEventListener('wheel', onWheel);
    });

    function resetView(): void {
        transform = { ...IDENTITY };
    }

    function onPointerDown(e: PointerEvent): void {
        if (e.button !== 0 || transform.k === 1) return;
        const pt = viewPoint(e);
        if (pt === null) return;
        panLast = pt;
        hover = null;
        (e.currentTarget as Element).setPointerCapture(e.pointerId);
    }

    function onPointerUp(): void {
        panLast = null;
    }

    // ---- endpoint hover tooltip ------------------------------------------
    // Hit-test radii are on-screen sizes, so divide by k in content space.
    //
    // `hover` also drives the CURSOR (see the svg's class below). Endpoints are
    // hover-only — there is no click handler on a station — so the cursor becomes
    // `help` (info available here), NOT `pointer`, which would promise an activation
    // that does not exist. It takes precedence over the pan cursor so a station reads
    // as a target rather than more draggable map; the two can't conflict, since
    // pointerdown clears `hover` and the pan branch returns before re-hit-testing.
    // Without this the pointer stayed a grab-hand over a station when zoomed, and a
    // plain arrow when not — no feedback either way (dogfood 2026-07-26).
    const HIT_R = 10;
    const GROUP_R = 5;
    const TIP_MAX = 8;

    interface HoverInfo {
        keys: Set<string>;
        entries: { key: string; label: string; color: string }[];
    }
    let hover = $state<HoverInfo | null>(null);
    let tip = $state<{ left: number; top: number; flip: boolean } | null>(null);

    function onPointerMove(e: PointerEvent): void {
        const pt = viewPoint(e);
        if (pt === null) return;
        if (panLast !== null) {
            transform = panBy(transform, pt[0] - panLast[0], pt[1] - panLast[1], W, H);
            panLast = pt;
            return;
        }
        const [cx, cy] = toContent(transform, pt[0], pt[1]);
        const k = transform.k;
        const idxs = endpointsNear(ends, cx, cy, HIT_R / k, GROUP_R / k);
        if (idxs.length === 0) {
            hover = null;
            return;
        }
        // List the stack TOPMOST-FIRST. endpointsNear returns indices ascending,
        // and arcs paint in document order (highest index drawn last = on top), so
        // reversing gives topmost→bottommost. This matters at the tooltip's TIP_MAX
        // cap: the entries kept are the ones actually visible on top, and the
        // "+N more" drops the ones buried underneath — a pure paint-stacking rule,
        // independent of what the arcs mean. Because the parent paints newest last
        // (MapView), the tooltip therefore leads with the newest contact and never
        // truncates it away (778b343a review P2).
        const stack = [...idxs].reverse();
        hover = {
            keys: new Set(stack.map((i) => drawnArcs[i].key)),
            entries: stack.map((i) => ({
                key: drawnArcs[i].key,
                label: drawnArcs[i].label ?? drawnArcs[i].key,
                color: drawnArcs[i].color ?? 'var(--color-focus)',
            })),
        };
        if (wrapEl !== null) {
            const wr = wrapEl.getBoundingClientRect();
            const flip = e.clientX - wr.left > wr.width * 0.62;
            tip = {
                left: e.clientX - wr.left + (flip ? -14 : 14),
                top: e.clientY - wr.top + 14,
                flip,
            };
        }
    }

    function onPointerLeave(): void {
        hover = null;
        panLast = null;
    }
</script>

<div bind:this={wrapEl} class="relative">
    <svg
        bind:this={svgEl}
        viewBox="0 0 {W} {H}"
        role="img"
        aria-label="World map of contacts"
        class="block h-auto w-full touch-none select-none
               {hover !== null
            ? 'cursor-help'
            : transform.k > 1
              ? panLast !== null
                  ? 'cursor-grabbing'
                  : 'cursor-grab'
              : ''}"
        onpointerdown={onPointerDown}
        onpointermove={onPointerMove}
        onpointerup={onPointerUp}
        onpointercancel={onPointerUp}
        onpointerleave={onPointerLeave}
        ondblclick={resetView}
    >
        <g
            data-testid="viewport"
            transform="translate({transform.x} {transform.y}) scale({transform.k})"
        >
            {#if spherePath !== null}
                <path
                    d={spherePath}
                    class="fill-map-water stroke-line"
                    stroke-width={1 / transform.k}
                />
            {/if}
            {#if gratPath !== null}
                <path
                    d={gratPath}
                    class="fill-none stroke-line-soft"
                    stroke-width={0.5 / transform.k}
                />
            {/if}
            {#each drawCountries as c (c.id)}
                {#if c.d !== null}
                    <path
                        d={c.d}
                        data-testid="country"
                        class="fill-map-land stroke-map-border"
                        stroke-width={0.5 / transform.k}
                    />
                {/if}
            {/each}
            {#each nightPaths as n (n.r)}
                <path
                    d={n.d}
                    data-testid="night"
                    class="pointer-events-none fill-black"
                    opacity="0.1"
                />
            {/each}
            {#each drawnArcs as a (a.key)}
                <g data-testid="arc">
                    <path
                        d={a.d}
                        class="fill-none"
                        stroke={a.color ?? 'var(--color-focus)'}
                        stroke-width={1.5 / transform.k}
                        stroke-linecap="round"
                        opacity="0.85"
                    >
                        {#if a.label}<title>{a.label}</title>{/if}
                    </path>
                    <circle
                        cx={a.end[0]}
                        cy={a.end[1]}
                        r={(hover?.keys.has(a.key) ? 4.5 : 3) / transform.k}
                        fill={a.color ?? 'var(--color-focus)'}
                        stroke={hover?.keys.has(a.key) ? 'var(--color-ink)' : 'none'}
                        stroke-width={hover?.keys.has(a.key) ? 1 / transform.k : 0}
                    />
                </g>
            {/each}
            {#if originXY !== null}
                <circle
                    cx={originXY[0]}
                    cy={originXY[1]}
                    r={4 / transform.k}
                    data-testid="origin"
                    class="fill-logged stroke-surface"
                    stroke-width={1.5 / transform.k}
                />
            {/if}
        </g>
    </svg>

    {#if transform.k > 1}
        <button
            type="button"
            data-testid="reset-view"
            class="absolute top-2 right-2 rounded-md border border-line bg-surface px-2 py-1
                   text-xs text-muted shadow-sm hover:text-ink"
            title="Back to the whole world (or double-click the map)"
            onclick={resetView}
        >
            Reset view
        </button>
    {/if}

    {#if hover !== null && tip !== null}
        <div
            data-testid="map-tooltip"
            class="pointer-events-none absolute z-10 max-w-72 rounded-md border border-line
                   bg-surface px-2.5 py-1.5 shadow-lg"
            style="left: {tip.left}px; top: {tip.top}px;{tip.flip
                ? ' transform: translateX(-100%);'
                : ''}"
        >
            {#each hover.entries.slice(0, TIP_MAX) as en (en.key)}
                <p class="flex items-center gap-x-1.5 text-xs whitespace-nowrap text-ink">
                    <span
                        class="inline-block size-2 shrink-0 rounded-full"
                        style="background-color: {en.color}"
                    ></span>
                    {en.label}
                </p>
            {/each}
            {#if hover.entries.length > TIP_MAX}
                <p class="text-[0.65rem] text-muted">
                    +{hover.entries.length - TIP_MAX} more here — zoom in to separate
                </p>
            {/if}
        </div>
    {/if}
</div>
