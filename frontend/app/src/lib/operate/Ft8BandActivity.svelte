<script lang="ts">
    // Band Activity — the FT8 view's front-and-centre anchor (ADR 0047). Pure
    // presentation over ft8State.decodes; enrichment (flag / worked-before) comes
    // from the ft8Enrich cache (lookup-once, fail-soft), and the per-CQ short-path
    // bearing is computed from the decode's grid + the operator's grid. Slot
    // dividers group the accumulated newest-first feed.
    import {
        ft8State,
        ft8OperatorCall,
        ft8MyGrid,
        answerCq,
        workCaller,
        type DecodeEntry,
    } from './ft8.svelte';
    import { ft8EnrichState } from './ft8Enrich.svelte';
    import { rig } from './rig.svelte';
    import { session } from './session.svelte';
    import { parseCq, parseDirectedToMe, parseDirectedToMeFd, isCqFd } from '../utils/ft8Message';
    import { slotParity } from '../utils/ft8Parity';
    import { pathInfo } from '../utils/bearing';
    import { parseFrequency } from '../validators/frequency';
    import { toasts } from '../ui/toasts.svelte';

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

    // ---- Click to work (ADR 0033) — first RF-initiating clicks from this SPA -----
    // A CQ row answers the CQ; a directed-at-me row works the caller. Both go through
    // the daemon (armed gate + guaranteed stop + sequencing); this only sends intent.
    const me = $derived(ft8OperatorCall().trim().toUpperCase());
    const catLive = $derived(rig.cat === 'connected');
    // A row is workable only when armed + CAT live + no session already in flight; a
    // plain row (kind '') is never clickable.
    const canStart = $derived(ft8State.tx.armed && catLive && !ft8State.qso.active);

    // In-flight latch for the click-start POST→SSE window: set synchronously so a
    // second click can't fire a second start. Released when the daemon confirms the
    // session is active (below) or the start fails.
    let starting = $state(false);
    $effect(() => {
        if (ft8State.qso.active) starting = false;
    });

    // Same-session dupe (band-scoped): a call already logged this session on this
    // band is skipped. A prior-session/durable-log contact is fine — only a repeat
    // WITHIN this sitting is blocked. Cross-band the same call is not a dupe.
    function workedThisSession(call: string): boolean {
        return session.qsos.some((q) => q.callsign === call && q.band === rig.band);
    }

    async function onRowClick(row: DecodeRow): Promise<void> {
        if (row.kind === '' || starting) return;
        // Explain why nothing happened rather than silently no-op'ing on a click.
        if (!ft8State.tx.armed) {
            toasts.info('Enable TX first (Operate panel).');
            return;
        }
        if (!catLive) {
            toasts.info('Rig not connected — cannot transmit.');
            return;
        }
        if (ft8State.qso.active) {
            toasts.info('Finish or Abandon the current contact first.');
            return;
        }
        const offset = ft8State.effectiveOffset;
        if (offset === null) {
            toasts.info('No clear TX offset yet — pick one in Occupancy.');
            return;
        }
        const opHz = parseFrequency(rig.freq);
        if (opHz === null) {
            toasts.info('Rig frequency is not known yet.');
            return;
        }
        if (workedThisSession(row.call)) {
            toasts.info(`Already worked ${row.call} this session.`);
            return;
        }
        const opMHz = opHz / 1_000_000;
        starting = true;
        let r;
        if (row.kind === 'cq') {
            const cq = parseCq(row.d.text);
            if (!cq) {
                starting = false;
                return;
            }
            // A CQ FD answers with the operator's Field Day exchange (daemon config);
            // theirSnr (our SNR of their CQ) is logged as RST_SENT for FD.
            r = await answerCq({
                theirCall: cq.call,
                theirGrid: cq.grid,
                slotUtc: row.d.startUtc,
                offsetHz: offset,
                opFreqMHz: opMHz,
                fd: isCqFd(row.d.text),
                theirSnr: row.d.snr,
            });
        } else {
            // Work a caller: try the FD shape first (more specific), else standard.
            const fd = parseDirectedToMeFd(row.d.text, me);
            const toMe = fd ?? parseDirectedToMe(row.d.text, me);
            if (!toMe) {
                starting = false;
                return;
            }
            r = await workCaller({
                theirCall: toMe.call,
                theirGrid: toMe.grid,
                theirSnr: row.d.snr, // our SNR of their call → RST_SENT
                slotUtc: row.d.startUtc,
                offsetHz: offset,
                opFreqMHz: opMHz,
                fd: fd ? { class: fd.class, section: fd.section } : undefined,
            });
        }
        if (!r.ok) {
            starting = false;
            toasts.error(r.message);
        }
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
        <!-- Margin-inset scroll box: its OWN edges sit inside the card (margin all
             round), so scrolled rows clip at this box's bottom — leaving a gap to the
             card edge — instead of bleeding to the border. Padding on a scroll box is
             dropped at scroll end, hence the margin holds the inset. -->
        <div class="mx-3 mb-3 min-h-0 flex-1 overflow-auto">
            <table class="w-full font-mono text-sm tabular-nums">
                <thead>
                    <tr class="text-[10px] font-bold tracking-wide text-muted uppercase">
                        <th
                            class="sticky top-0 z-10 border-b border-line bg-surface py-1.5 pr-2 pl-3"
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
                            {@const info =
                                row.kind === 'cq'
                                    ? ft8EnrichState.info(row.call, rig.band)
                                    : undefined}
                            <tr class="text-ink {rowClass(row.kind, info?.worked)}">
                                <td
                                    class="py-0.5 pr-1 pl-3 text-base leading-none"
                                    title={info?.country}
                                >
                                    {info?.flag ?? ''}
                                </td>
                                <td
                                    class="overflow-hidden px-2 text-nowrap text-ellipsis {row.kind !==
                                    ''
                                        ? 'font-semibold'
                                        : ''}"
                                    >{#if row.kind !== ''}<button
                                            type="button"
                                            class="text-left {canStart
                                                ? 'cursor-pointer hover:underline'
                                                : 'cursor-default'}"
                                            title={row.kind === 'cq'
                                                ? 'Answer this CQ'
                                                : 'Work this station calling you'}
                                            onclick={() => onRowClick(row)}>{row.d.text}</button
                                        >{:else}{row.d.text}{/if}{#if info?.isNewEntity}<span
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
