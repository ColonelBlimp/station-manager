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
        ft8CqToTop,
        ft8HideHashed,
        answerCq,
        workCaller,
        type DecodeEntry,
    } from './ft8.svelte';
    import { ft8EnrichState, type Ft8CallInfo } from './ft8Enrich.svelte';
    import { ft8PileupStack } from './ft8Pileup.svelte';
    import { rig } from './rig.svelte';
    import { session } from './session.svelte';
    import {
        parseCq,
        parseDirectedToMe,
        parseDirectedToMeFd,
        parseSender,
        isCqFd,
        isCqType4,
        isNonstandardCall,
    } from '../utils/ft8Message';
    import { slotParity } from '../utils/ft8Parity';
    import { pathInfo } from '../utils/bearing';
    import { parseFrequency } from '../validators/frequency';
    import { toasts } from '../ui/toasts.svelte';
    import Ft8BandFilter from './Ft8BandFilter.svelte';

    interface DecodeRow {
        d: DecodeEntry;
        kind: '' | 'cq' | 'call'; // cq = calling CQ · call = calling us
        call: string; // the other station (for enrichment / worked lookup)
        bearing: number | null; // short-path °, from the decode's grid
        // Directed-call target for plain (kind '') rows: the decode's SENDER —
        // double-click starts calling them without waiting for their CQ (a DX
        // running a pile-up can go many minutes between CQs). null when the
        // sender isn't callable (hashed, or no parseable call).
        dx: { call: string; grid: string } | null;
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

    // Classify + pre-parse one decode into a display row.
    function classify(d: DecodeEntry, me: string): DecodeRow {
        const called = parseDirectedToMe(d.text, me);
        const cq = called ? null : parseCq(d.text);
        const kind: DecodeRow['kind'] = called ? 'call' : cq ? 'cq' : '';
        const call = called?.call ?? cq?.call ?? '';
        const grid = called?.grid ?? cq?.grid ?? '';
        const dx = kind === '' ? parseSender(d.text) : null;
        return { d, kind, call, bearing: bearingFor(grid), dx };
    }

    // The Band Activity feed. Normally the newest-first decode list grouped by slot
    // (a header row per slot). With config ft8.display.cq_to_top on, CQ rows are
    // instead floated above the rest (stable within each partition) so the answerable
    // stations sit together at the top — the feed is no longer slot-ordered, so it
    // renders as ONE header-less group (a group with time === '' skips its divider).
    //
    // Funnel filters (both HIDE rows) run first: the typed token-prefix filter
    // (ft8State.bandFilter — "show calls starting with VK") and hide-hashed
    // (ft8.display.hide_hashed_calls — drop unidentifiable "<...>" calls). A station
    // CALLING US always shows through — missing a caller is costly.
    const groups = $derived.by<SlotGroup[]>(() => {
        const me = ft8OperatorCall();
        const filter = ft8State.bandFilter.trim().toUpperCase();
        const hideHashed = ft8HideHashed();
        const rows = ft8State.decodes
            .map((d) => classify(d, me))
            .filter((r) => {
                if (r.kind === 'call') return true; // calling us — always show
                if (hideHashed && r.d.text.includes('<...>')) return false;
                return !(
                    filter !== '' &&
                    !r.d.text
                        .toUpperCase()
                        .split(/\s+/)
                        .some((t) => t.startsWith(filter))
                );
            });
        if (ft8CqToTop()) {
            const cq = rows.filter((r) => r.kind === 'cq');
            const rest = rows.filter((r) => r.kind !== 'cq');
            return [{ key: 'cq-top', time: '', parity: '', decodes: [...cq, ...rest] }];
        }
        const out: SlotGroup[] = [];
        let cur: SlotGroup | null = null;
        for (const row of rows) {
            const key = `d:${row.d.startUtc}`;
            if (cur === null || cur.key !== key) {
                cur = {
                    key,
                    time: clock(row.d.startUtc),
                    parity: slotParity(row.d.startUtc),
                    decodes: [],
                };
                out.push(cur);
            }
            cur.decodes.push(row);
        }
        return out;
    });

    // Kick the flag + worked-before lookups for every visible CQ AND calling-you
    // row on the current band (idempotent — cached/in-flight is a no-op), so a
    // station calling us also gets a flag + country + DXCC. A side effect, so it
    // lives here, not in the pure derived above.
    $effect(() => {
        const band = rig.band;
        for (const g of groups) {
            for (const row of g.decodes) {
                if (row.kind !== '' && row.call !== '') ft8EnrichState.observe(row.call, band);
            }
        }
    });

    // Country (+ DXCC entity, once resolved) for the hover tooltip on the flag and
    // the work/answer button — so a station calling you shows where it is and its
    // DXCC on hover. '' when the country isn't known yet.
    function enrichHover(info: Ft8CallInfo | undefined): string {
        if (!info?.country) return '';
        return info.dxcc ? `${info.country} · DXCC ${info.dxcc}` : info.country;
    }

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

    // Pile-up queue anchors. workableParity = the run's locked slot parity, or (before
    // anything's queued) the live contact's caller parity — a wrong-parity add is
    // rejected in enqueueCaller. callerActive = WE'RE running Call CQ (queue disabled
    // then: the caller sequencer owns the rig, a competing queue would fight it).
    const workableParity = $derived(
        ft8PileupStack.lockedParity || (ft8State.qso.active ? ft8State.qso.theirPeriod : '')
    );
    const callerActive = $derived(ft8State.qso.active && ft8State.qso.role === 'caller');

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

    // Ctrl/Cmd+click a calling-you row → PILE-UP queue (pure capture, works in ANY TX
    // state, so callers spotted mid-QSO aren't lost). The Operate view drains it FIFO
    // (increment 3). Single-parity run: the first add locks the parity; a wrong-parity
    // add is rejected with an explain-toast.
    function enqueueCaller(row: DecodeRow): void {
        const toMe = parseDirectedToMe(row.d.text, me);
        if (!toMe) return;
        const call = toMe.call;
        if (callerActive) {
            toasts.info('Calling CQ — pile-up queue disabled. Abandon to work stations by hand.');
            return;
        }
        // Never re-queue the station in flight (its grid re-calls still decode).
        if (ft8State.qso.active && call === ft8State.qso.theirCall) {
            toasts.info(`Already working ${call}.`);
            return;
        }
        if (workedThisSession(call)) {
            toasts.info(`Already worked ${call} this session.`);
            return;
        }
        if (ft8PileupStack.items.some((x) => x.call === call)) {
            toasts.info(`${call} is already in the pile-up.`);
            return;
        }
        // Fresh run? Empty queue + no contact → the previous run is over, so unlock; the
        // first add sets a new parity. A live run holds the lock across the drain.
        if (ft8PileupStack.items.length === 0 && !ft8State.qso.active) {
            ft8PileupStack.clearLock();
        }
        const parity = slotParity(row.d.startUtc);
        if (workableParity !== '' && parity !== '' && parity !== workableParity) {
            toasts.info(
                `${parity} slot — can't add to this ${workableParity} run. Finish or Abandon first.`
            );
            return;
        }
        const added = ft8PileupStack.push({
            call,
            grid: toMe.grid,
            snr: row.d.snr,
            slotUtc: row.d.startUtc,
        });
        // Only a genuinely NEW caller resumes a paused drain (Abandon stays in control).
        if (added) ft8PileupStack.resume();
    }

    // The guard chain every TX-starting row interaction runs — each miss explains
    // itself (toast) rather than silently no-op'ing on a click.
    function txPreflight(call: string): { offset: number; opMHz: number } | null {
        if (!ft8State.tx.armed) {
            toasts.info('Enable TX first (Operate panel).');
            return null;
        }
        if (!catLive) {
            toasts.info('Rig not connected — cannot transmit.');
            return null;
        }
        if (ft8State.qso.active) {
            toasts.info('Finish or Abandon the current contact first.');
            return null;
        }
        const offset = ft8State.effectiveOffset;
        if (offset === null) {
            toasts.info('No clear TX offset yet — pick one in Occupancy.');
            return null;
        }
        const opHz = parseFrequency(rig.freq);
        if (opHz === null) {
            toasts.info('Rig frequency is not known yet.');
            return null;
        }
        if (workedThisSession(call)) {
            toasts.info(`Already worked ${call} this session.`);
            return null;
        }
        return { offset, opMHz: opHz / 1_000_000 };
    }

    // Directed call (WSJT-X double-click semantic): call the SENDER of a plain
    // decode row — no CQ needed. A DX running a pile-up can go many minutes
    // between CQs; waiting for one costs the contact (the T22TT case,
    // 2026-07-13). The decode's slot fixes their parity (we TX opposite, i.e.
    // their RX slot); the opening message is identical to answering a CQ, so
    // the daemon sequencer (StartQso) is reused unchanged. Double-click, not
    // single: plain rows are dense non-actionable text, and starting a
    // transmission from them must be a deliberate gesture.
    async function onRowDblClick(row: DecodeRow): Promise<void> {
        if (row.kind !== '' || row.dx === null || starting) return;
        const pre = txPreflight(row.dx.call);
        if (!pre) return;
        starting = true;
        // A nonstandard/compound sender (PJ4/NA2AA, K1ABC/D) can't walk the standard
        // grid/report ladder — answer it with the reduced type-4 ladder (ADR 0048),
        // whose opening carries no grid.
        const type4 = isNonstandardCall(row.dx.call);
        const r = await answerCq({
            theirCall: row.dx.call,
            theirGrid: type4 ? '' : row.dx.grid,
            slotUtc: row.d.startUtc,
            offsetHz: pre.offset,
            opFreqMHz: pre.opMHz,
            fd: false,
            type4,
            theirSnr: row.d.snr,
        });
        if (!r.ok) {
            starting = false;
            toasts.error(r.message);
        }
    }

    async function onRowClick(e: MouseEvent, row: DecodeRow): Promise<void> {
        // Ctrl/Cmd+click a calling-you row queues it (capture only). A modifier click
        // NEVER triggers a work-now TX.
        if (e.ctrlKey || e.metaKey) {
            if (row.kind === 'call') enqueueCaller(row);
            return;
        }
        if (row.kind === '' || starting) return;
        const pre = txPreflight(row.call);
        if (!pre) return;
        const { offset, opMHz } = pre;
        starting = true;
        let r;
        if (row.kind === 'cq') {
            const cq = parseCq(row.d.text);
            if (!cq) {
                starting = false;
                return;
            }
            // A CQ FD answers with the operator's Field Day exchange (daemon config); a
            // CQ from a nonstandard/compound call routes to the reduced type-4 ladder
            // (ADR 0048). theirSnr (our SNR of their CQ) is logged as RST_SENT for both
            // (neither exchanges a report). The three modes are mutually exclusive.
            const type4 = isCqType4(row.d.text);
            r = await answerCq({
                theirCall: cq.call,
                theirGrid: type4 ? '' : cq.grid,
                slotUtc: row.d.startUtc,
                offsetHz: offset,
                opFreqMHz: opMHz,
                fd: isCqFd(row.d.text),
                type4,
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
    <!-- h-10 (not py-2): a fixed header height shared with the Operate panel, so
         the two cards' header rules align regardless of contents (the filter
         button here is taller than Operate's text-only row). -->
    <div class="flex h-10 shrink-0 items-center gap-1.5 border-b border-line px-4">
        <h3 class="text-sm font-semibold text-ink">Band Activity</h3>
        <Ft8BandFilter />
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
            <!-- table-fixed + explicit column widths: FT8 slots alternate parity and
                 their content differs (SNR sign, freq digits, flag presence), so an
                 auto layout re-sized the columns each slot and the table jittered
                 left↔right. Fixed widths pin every column; Message takes the rest. -->
            <table class="w-full table-fixed font-mono text-sm tabular-nums">
                <thead>
                    <tr class="text-[10px] font-bold tracking-wide text-muted uppercase">
                        <th
                            class="sticky top-0 z-10 w-16 border-b border-line bg-surface py-1.5 pr-2 pl-3 text-right"
                            >Hz</th
                        >
                        <th
                            class="sticky top-0 z-10 w-8 border-b border-line bg-surface px-2 py-1.5"
                        ></th>
                        <th
                            class="sticky top-0 z-10 w-14 border-b border-line bg-surface px-2 py-1.5 text-right"
                            >Brg</th
                        >
                        <th
                            class="sticky top-0 z-10 w-12 border-b border-line bg-surface px-2 py-1.5 text-right"
                            >SNR</th
                        >
                        <th
                            class="sticky top-0 z-10 border-b border-line bg-surface px-2 py-1.5 text-left"
                            >Message</th
                        >
                    </tr>
                </thead>
                <tbody>
                    {#each groups as g (g.key)}
                        {#if g.time !== ''}
                            <tr class="bg-surface-muted text-muted">
                                <td colspan="5" class="py-0.5 pl-3 text-xs font-semibold">
                                    {g.time}{g.parity ? ` · ${g.parity}` : ''}
                                </td>
                            </tr>
                        {/if}
                        {#each g.decodes as row (row.d.id)}
                            {@const info =
                                row.kind !== ''
                                    ? ft8EnrichState.info(row.call, rig.band)
                                    : undefined}
                            {@const hover = enrichHover(info)}
                            <tr class="text-ink {rowClass(row.kind, info?.worked)}">
                                <td class="py-0.5 pr-2 pl-3 text-right"
                                    >{Math.round(row.d.freqHz)}</td
                                >
                                <td
                                    class="px-2 py-0.5 text-base leading-none"
                                    title={hover || undefined}
                                >
                                    {info?.flag ?? ''}
                                </td>
                                <td class="px-2 text-right text-focus"
                                    >{row.bearing !== null ? `${Math.round(row.bearing)}°` : ''}</td
                                >
                                <td class="px-2 text-right text-muted">{row.d.snr}</td>
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
                                            title={(row.kind === 'cq'
                                                ? 'Answer this CQ'
                                                : 'Work this station calling you (Ctrl+click to queue)') +
                                                (hover ? ` — ${hover}` : '')}
                                            onclick={(e) => onRowClick(e, row)}>{row.d.text}</button
                                        >{:else if row.dx !== null}<button
                                            type="button"
                                            class="text-left {canStart
                                                ? 'cursor-pointer hover:underline decoration-dotted'
                                                : 'cursor-default'}"
                                            title="Double-click to call {row.dx
                                                .call} (directed call — no CQ needed)"
                                            ondblclick={() => onRowDblClick(row)}
                                            >{row.d.text}</button
                                        >{:else}{row.d.text}{/if}{#if info?.isNewEntity}<span
                                            class="ml-1 text-focus"
                                            title="New DXCC entity">★</span
                                        >{/if}{#if ft8State.qso.active && ft8State.qso.theirCall !== '' && (row.call === ft8State.qso.theirCall || row.dx?.call === ft8State.qso.theirCall)}<span
                                            class="ml-1 rounded bg-green-600 px-1 text-[10px] font-bold text-white"
                                            title="Working now">●</span
                                        >{:else if row.kind === 'call' && ft8PileupStack.items.some((x) => x.call === row.call)}<span
                                            class="ml-1 rounded bg-focus px-1 text-[10px] font-bold text-white"
                                            title="In the pile-up queue">Q</span
                                        >{/if}</td
                                >
                            </tr>
                        {/each}
                    {/each}
                </tbody>
            </table>
        </div>
    {/if}
</section>
