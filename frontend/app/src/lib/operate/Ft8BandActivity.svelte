<script lang="ts">
    // Band Activity — the FT8 view's front-and-centre anchor (ADR 0047). Pure
    // presentation over ft8State.decodes; enrichment (flag / worked-before) comes
    // from the ft8Enrich cache (lookup-once, fail-soft), and the per-CQ short-path
    // bearing is computed from the decode's grid + the operator's grid. Slot
    // dividers group the accumulated newest-first feed.
    import { ft8State, ft8OperatorCall, ft8MyGrid, type DecodeEntry } from './ft8.svelte';
    import { ft8EnrichState } from './ft8Enrich.svelte';
    import { rig } from './rig.svelte';
    import { parseCq, parseDirectedToMe } from '../utils/ft8Message';
    import { slotParity } from '../utils/ft8Parity';
    import { pathInfo } from '../utils/bearing';

    interface DecodeRow {
        d: DecodeEntry;
        kind: '' | 'cq' | 'call'; // cq = calling CQ · call = calling us
        call: string; // the other station (for enrichment / worked lookup)
        bearing: number | null; // short-path °, from the decode's grid
    }
    interface SlotGroup {
        key: string;
        time: string;
        parity: string;
        decodes: DecodeRow[];
    }

    function clock(utc: string): string {
        const t = Date.parse(utc);
        if (Number.isNaN(t)) return utc;
        const d = new Date(t);
        const p = (n: number) => String(n).padStart(2, '0');
        return `${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())}`;
    }

    function bearingFor(grid: string): number | null {
        const my = ft8MyGrid();
        if (grid === '' || my === '') return null;
        try {
            const p = pathInfo(my, grid);
            return p && Number.isFinite(p.shortPathBearing) ? p.shortPathBearing : null;
        } catch {
            return null; // malformed grid — no bearing, never an error
        }
    }

    // Group the flat newest-first feed by slot; classify + pre-parse each decode.
    const groups = $derived.by<SlotGroup[]>(() => {
        const out: SlotGroup[] = [];
        const me = ft8OperatorCall();
        let cur: SlotGroup | null = null;
        for (const d of ft8State.decodes) {
            const key = `d:${d.startUtc}`;
            if (cur === null || cur.key !== key) {
                cur = { key, time: clock(d.startUtc), parity: slotParity(d.startUtc), decodes: [] };
                out.push(cur);
            }
            const called = parseDirectedToMe(d.text, me);
            const cq = called ? null : parseCq(d.text);
            const kind: DecodeRow['kind'] = called ? 'call' : cq ? 'cq' : '';
            const call = called?.call ?? cq?.call ?? '';
            const grid = called?.grid ?? cq?.grid ?? '';
            cur.decodes.push({ d, kind, call, bearing: bearingFor(grid) });
        }
        return out;
    });

    // Kick the flag + worked-before lookups for every visible CQ call on the
    // current band (idempotent — cached/in-flight is a no-op). A side effect, so
    // it lives here, not in the pure derived above.
    $effect(() => {
        const band = rig.band;
        for (const g of groups) {
            for (const row of g.decodes) {
                if (row.kind === 'cq' && row.call !== '') ft8EnrichState.observe(row.call, band);
            }
        }
    });

    // Full literal class strings (not class: directives — those can't express a
    // `dark:` variant); Tailwind's scanner still finds these in-source.
    function rowClass(kind: DecodeRow['kind'], worked: boolean | undefined): string {
        if (kind === 'call') return 'bg-blue-50 dark:bg-blue-500/10';
        if (kind === 'cq') return worked ? 'text-muted' : 'bg-amber-50 dark:bg-amber-500/10';
        return '';
    }
</script>

<section class="flex h-full flex-col overflow-hidden rounded-xl border border-line bg-surface">
    <div class="flex items-center border-b border-line px-4 py-2">
        <h3 class="text-sm font-semibold text-ink">Band Activity</h3>
    </div>

    {#if groups.length === 0}
        <div class="flex flex-1 items-center justify-center text-sm text-muted">
            Decodes appear here as slots are received.
        </div>
    {:else}
        <div class="flex-1 overflow-auto">
            <table class="w-full font-mono text-sm tabular-nums">
                <thead>
                    <tr class="text-[10px] font-bold tracking-wide text-muted uppercase">
                        <th class="sticky top-0 z-10 border-b border-line bg-surface py-1.5 pr-2 pl-3"
                        ></th>
                        <th
                            class="sticky top-0 z-10 border-b border-line bg-surface px-2 py-1.5 text-left"
                            >Message</th
                        >
                        <th
                            class="sticky top-0 z-10 border-b border-line bg-surface px-2 py-1.5 text-right"
                            >Brg</th
                        >
                        <th
                            class="sticky top-0 z-10 border-b border-line bg-surface px-2 py-1.5 text-right"
                            >SNR</th
                        >
                        <th
                            class="sticky top-0 z-10 border-b border-line bg-surface px-2 py-1.5 text-right"
                            >Hz</th
                        >
                    </tr>
                </thead>
                <tbody>
                    {#each groups as g (g.key)}
                        <tr class="bg-surface-muted text-muted">
                            <td colspan="5" class="py-0.5 pl-3 text-xs font-semibold">
                                {g.time}{g.parity ? ` · ${g.parity}` : ''}
                            </td>
                        </tr>
                        {#each g.decodes as row (row.d.id)}
                            {@const info = row.kind === 'cq' ? ft8EnrichState.info(row.call, rig.band) : undefined}
                            <tr class="text-ink {rowClass(row.kind, info?.worked)}">
                                <td class="py-0.5 pr-1 pl-3 text-base leading-none" title={info?.country}>
                                    {info?.flag ?? ''}
                                </td>
                                <td
                                    class="overflow-hidden px-2 text-nowrap text-ellipsis {row.kind !==
                                    ''
                                        ? 'font-semibold'
                                        : ''}"
                                    >{row.d.text}{#if info?.isNewEntity}<span
                                            class="ml-1 text-focus"
                                            title="New DXCC entity">★</span
                                        >{/if}</td
                                >
                                <td class="px-2 text-right text-focus"
                                    >{row.bearing !== null ? `${Math.round(row.bearing)}°` : ''}</td
                                >
                                <td class="px-2 text-right text-muted">{row.d.snr}</td>
                                <td class="px-2 text-right">{Math.round(row.d.freqHz)}</td>
                            </tr>
                        {/each}
                    {/each}
                </tbody>
            </table>
        </div>
    {/if}
</section>
