<!--
    WorldMap — the reusable great-circle map surface (backlog "QSO contacts
    map", Phase 1). Pure presentation: parent owns the data story (which QSOs,
    what window, live updates) and hands down origin + arcs; this component
    owns projection and drawing via lib/map/engine.

    Sized by viewBox so the parent's CSS decides the on-screen size; the
    projection is fitted once to the fixed viewBox space, not re-fitted per
    resize (SVG scales, coordinates stay valid). Colours ride the app theme
    tokens so light/dark both work with no per-mode markup.
-->
<script lang="ts">
    import {
        createEngine,
        worldCountries,
        arcPath,
        project,
        geometryPath,
        graticulePath,
        nightCap,
        SPHERE,
        TWILIGHT_RADII,
        type LatLon,
    } from './engine';

    export interface MapArc {
        from: LatLon;
        to: LatLon;
        /** Stable identity for keyed rendering (e.g. the QSO uuid). */
        key: string;
        /** Tooltip line, e.g. "DL3YA · JO62 · 8,431 km · 12.3°". */
        label?: string;
        /** Stroke colour (e.g. the band colour); theme accent when absent. */
        color?: string;
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
    const countries = worldCountries().features.map((f, i) => ({
        id: f.id ?? i,
        d: geometryPath(engine, f),
    }));

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
            const d = arcPath(engine, a.from, a.to);
            const end = project(engine, a.to);
            return d === null || end === null
                ? []
                : [{ key: a.key, label: a.label, color: a.color, d, end }];
        })
    );
</script>

<svg
    viewBox="0 0 {W} {H}"
    role="img"
    aria-label="World map of contacts"
    class="block h-auto w-full select-none"
>
    {#if spherePath !== null}
        <path d={spherePath} class="fill-map-water stroke-line" stroke-width="1" />
    {/if}
    {#if gratPath !== null}
        <path d={gratPath} class="fill-none stroke-line-soft" stroke-width="0.5" />
    {/if}
    {#each countries as c (c.id)}
        {#if c.d !== null}
            <path
                d={c.d}
                data-testid="country"
                class="fill-map-land stroke-map-border"
                stroke-width="0.5"
            />
        {/if}
    {/each}
    {#each nightPaths as n (n.r)}
        <path d={n.d} data-testid="night" class="pointer-events-none fill-black" opacity="0.1" />
    {/each}
    {#each drawnArcs as a (a.key)}
        <g data-testid="arc">
            <path
                d={a.d}
                class="fill-none"
                stroke={a.color ?? 'var(--color-focus)'}
                stroke-width="1.5"
                stroke-linecap="round"
                opacity="0.85"
            >
                {#if a.label}<title>{a.label}</title>{/if}
            </path>
            <circle cx={a.end[0]} cy={a.end[1]} r="3" fill={a.color ?? 'var(--color-focus)'}>
                {#if a.label}<title>{a.label}</title>{/if}
            </circle>
        </g>
    {/each}
    {#if originXY !== null}
        <circle
            cx={originXY[0]}
            cy={originXY[1]}
            r="4"
            data-testid="origin"
            class="fill-logged stroke-surface"
            stroke-width="1.5"
        />
    {/if}
</svg>
