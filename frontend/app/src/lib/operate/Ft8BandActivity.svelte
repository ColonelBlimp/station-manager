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
        ft8EngagedThisSession,
        answerCq,
        workCaller,
        bagAnswerer,
        type DecodeEntry,
        type Ft8TxResult,
    } from './ft8.svelte';
    import { ft8EnrichState, type Ft8CallInfo } from './ft8Enrich.svelte';
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
    import { daemonNowMs as readDaemonNowMs, daemonClockTrusted } from '../api/daemonClock.svelte';
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
    function rowClass(
        kind: DecodeRow['kind'],
        worked: boolean | undefined,
        stale: boolean
    ): string {
        // Faded rather than removed (operator's call): deleting rows would make a
        // quiet band look identical to a dead decoder, which is exactly the state
        // the operator has to be able to tell it apart from.
        const age = stale ? ' opacity-40' : '';
        if (kind === 'call') return 'bg-blue-50 dark:bg-blue-500/10' + age;
        if (kind === 'cq') {
            return (worked ? 'text-muted' : 'bg-amber-50 dark:bg-amber-500/10') + age;
        }
        return age.trim();
    }

    // A stale row keys nothing. The daemon refuses it too (ErrStaleDecode -> 409), so
    // this is not the guarantee — it is what turns a rejection into a reason, and
    // stops the operator learning that clicking greyed rows is worth a try.
    function staleBlocked(row: DecodeRow): boolean {
        if (!isStale(row)) return false;
        toasts.info(
            `That decode is too old to work — ${row.call || 'the station'} may have left the air. Wait for a fresh decode.`
        );
        return true;
    }

    // ---- Click to work (ADR 0033) — first RF-initiating clicks from this SPA -----
    // A CQ row answers the CQ; a directed-at-me row works the caller. Both go through
    // the daemon (armed gate + guaranteed stop + sequencing); this only sends intent.
    const me = $derived(ft8OperatorCall().trim().toUpperCase());
    const catLive = $derived(rig.cat === 'connected');
    // A row is workable only when armed + CAT live + no session already in flight; a
    // plain row (kind '') is never clickable.
    const canStart = $derived(ft8State.tx.armed && catLive && !ft8State.qso.active);

    // DECODE STALENESS (operator's number, 2026-07-31: three minutes). Band Activity
    // retains by COUNT (historyMax), never by age, so on a quiet band a station that
    // left the air minutes ago keeps a clickable row — UA4FKT's did for 5m31s, and
    // clicking it transmitted six rungs at nobody.
    //
    // MUST MATCH staleDecodeLimit in internal/ft8/sequencer.go. The daemon is the
    // guarantee (it refuses the start with ErrStaleDecode -> 409 ft8_stale_decode);
    // this half stops the click being made at all, so the operator is not taught to
    // click things that get rejected.
    const STALE_MS = 3 * 60 * 1000;

    // A TICKING clock, not Date.now() read during render. On the band that produced
    // this bug ONE decode arrived in five and a half minutes, so nothing would have
    // re-rendered and the row would never have greyed. The tick is what makes rows
    // age on their own; 5 s is well inside the three-minute window it drives.
    let nowMs = $state(Date.now());
    $effect(() => {
        const h = setInterval(() => (nowMs = Date.now()), 5000);
        return () => clearInterval(h);
    });

    // Unknown age is NOT old age: an unparseable slot time stays workable, the same
    // discipline the daemon keeps by returning a parse error rather than a staleness
    // one. Refusing on a fact we do not have would be worse than allowing the click.
    // Measured against the DAEMON's clock (ft8DaemonSkewMs), not the browser's: a
    // browser running fast would otherwise grey every row on arrival and refuse
    // every click, while the daemon would have accepted them (codex 9d7a3f46 P1).
    // nowMs supplies the ticking, so this still advances between slots — the quiet
    // band is the case that matters.
    const clockTrusted = $derived.by(() => {
        void nowMs; // re-checked on each tick, like daemonNow below
        return daemonClockTrusted();
    });

    const daemonNow = $derived.by(() => {
        // nowMs is the REACTIVITY TRIGGER only — the value comes from the daemon
        // clock, which tracks monotonic elapsed time rather than the wall clock, so
        // a clock correction cannot shift it (codex cc032082 P1).
        void nowMs;
        return readDaemonNowMs();
    });

    function isStale(row: DecodeRow): boolean {
        // FAIL OPEN when the two clocks disagree (codex 503f31c7 P2). A suspend
        // freezes the monotonic clock while wall time runs on; a wall-clock step
        // does the reverse, and from here the two are indistinguishable. Rather
        // than guess, stop claiming to know: mark nothing stale and let the daemon
        // — which holds the only clock that matters, and refuses a stale start
        // outright — adjudicate. Failing CLOSED would re-create the previous
        // round's deadlock, since the requests that recalibrate the clock are the
        // very clicks a closed guard blocks.
        if (!clockTrusted) return false;
        const t = Date.parse(row.d.startUtc);
        if (Number.isNaN(t)) return false;
        return daemonNow - t > STALE_MS;
    }

    // In-flight latch for the click-start POST→SSE window: set synchronously so a
    // second click can't fire a second start. Released when the daemon confirms the
    // session is active (below) or the start fails.
    let starting = $state(false);
    $effect(() => {
        if (ft8State.qso.active) starting = false;
    });

    // Same-session dupe (band-scoped): a call already engaged or logged this session
    // on this band. Cross-band the same call is not a dupe, and a prior-session
    // contact is not one either. This is ADVISORY only — the operator is the
    // licensee and may work a station as often as they choose; callers use it to say
    // so, never to refuse. (The pile-up DRAIN is the one exception: it transmits
    // with no operator present at that instant, so it still skips — see Ft8Operate.)
    // Two sources, because neither alone is timely AND durable: `session.qsos` only
    // learns of a contact after the daemon's asynchronous enrich+submit finishes
    // (the terminal idle is published first), while the engaged-call set knows the
    // instant the sequencer touches a station but is forgotten on reload. Together
    // they cover the immediate-repair window this whole feature exists for
    // (codex 0f08d2b2 P1).
    // The EVIDENCE LEVEL is reported, not folded to a boolean, because the two
    // sources support different claims: "already worked" is reserved for a
    // session.qsos hit; an engaged-only hit is a started-but-unlogged attempt
    // (abandoned, or still inside the async-logging window) and saying "worked"
    // there is false (ADR 0065 fork 4 — VK5GR, dogfood 2026-08-07). Logged is
    // checked first so the stronger truthful claim wins when both hold. The
    // duplicate-protection mechanism treats the levels identically.
    type WorkedEvidence = 'logged' | 'engaged' | '';
    function workedEvidence(call: string): WorkedEvidence {
        if (session.qsos.some((q) => q.callsign === call && q.band === rig.band)) {
            return 'logged';
        }
        if (ft8EngagedThisSession(call, rig.band)) return 'engaged';
        return '';
    }

    // The guard chain every TX-starting row interaction runs — each miss explains
    // itself (toast) rather than silently no-op'ing on a click.
    function txPreflight(call: string): {
        offset: number;
        opMHz: number;
        allowDuplicate: boolean;
    } | null {
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
        // A repeat contact INFORMS, it does not refuse. Every other check above is a
        // genuine impossibility (no TX, no rig, no offset); this one is the operator's
        // call, and the software's information is incomplete — it knows only its own
        // log, not whether the other station has the QSO. Refusing here fired exactly
        // when working again was CORRECT: XE1GM (dogfood 2026-07-26) never copied our
        // RR73 and asked eleven times, and this guard blocked the repair.
        // Carried through to the daemon as `allow_duplicate` so a deliberate repeat is
        // actually STORED. Without it the second contact hashes to the first's dedupe
        // key inside one minute and is silently dropped — the operator would transmit
        // a full exchange and see no row (codex c2a8bea6 P1).
        const evidence = workedEvidence(call);
        const allowDuplicate = evidence !== '';
        if (evidence === 'logged') {
            toasts.info(`${call} already worked this session — working again.`);
        } else if (evidence === 'engaged') {
            toasts.info(
                `You started ${call} earlier this session — nothing was logged. Working as new.`
            );
        }
        return { offset, opMHz: opHz / 1_000_000, allowDuplicate };
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
        if (staleBlocked(row)) return;
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
            allowDuplicate: pre.allowDuplicate,
        });
        if (!r.ok) {
            starting = false;
            toasts.error(r.message);
        }
    }

    async function onRowClick(e: MouseEvent, row: DecodeRow): Promise<void> {
        // ADR 0067 retired the arming chord: the session's Answer mode alone
        // decides whether a run follows a click. Ctrl/cmd+click on a calling-you
        // row BAGS the station into the pick queue (capture-only — no TX): the
        // daemon-refusal toasts explain a station that isn't listed or a session
        // that isn't in pick mode.
        if (e.ctrlKey || e.metaKey) {
            if (row.kind === 'call') {
                const r = await bagAnswerer(row.call);
                if (!r.ok) toasts.error(r.message);
            }
            return;
        }
        if (row.kind === '' || starting) return;
        if (staleBlocked(row)) return;
        const pre = txPreflight(row.call);
        if (!pre) return;
        starting = true;
        const pending = row.kind === 'cq' ? answerCqRow(row, pre) : workCallerRow(row, pre);
        if (!pending) {
            starting = false;
            return;
        }
        const r = await pending;
        if (!r.ok) {
            starting = false;
            toasts.error(r.message);
        }
    }

    // The two TX starts a plain row click can launch, split out of onRowClick so the
    // guard chain there stays readable (eslint complexity ceiling). Null when the
    // row's text doesn't parse into the expected shape — the caller releases its
    // in-flight latch.

    function answerCqRow(
        row: DecodeRow,
        pre: { offset: number; opMHz: number; allowDuplicate: boolean }
    ): Promise<Ft8TxResult> | null {
        const cq = parseCq(row.d.text);
        if (!cq) return null;
        // A CQ FD answers with the operator's Field Day exchange (daemon config); a
        // CQ from a nonstandard/compound call routes to the reduced type-4 ladder
        // (ADR 0048). theirSnr (our SNR of their CQ) is logged as RST_SENT for both
        // (neither exchanges a report). The three modes are mutually exclusive.
        const type4 = isCqType4(row.d.text);
        const fd = isCqFd(row.d.text);
        return answerCq({
            theirCall: cq.call,
            theirGrid: type4 ? '' : cq.grid,
            slotUtc: row.d.startUtc,
            offsetHz: pre.offset,
            opFreqMHz: pre.opMHz,
            fd,
            type4,
            theirSnr: row.d.snr,
            allowDuplicate: pre.allowDuplicate,
            // ADR 0067: no per-click arming — the session's Answer mode alone
            // decides whether a run follows (the wrapper carries it).
        });
    }

    function workCallerRow(
        row: DecodeRow,
        pre: { offset: number; opMHz: number; allowDuplicate: boolean }
    ): Promise<Ft8TxResult> | null {
        // Work a caller: try the FD shape first (more specific), else standard.
        const fd = parseDirectedToMeFd(row.d.text, me);
        const toMe = fd ?? parseDirectedToMe(row.d.text, me);
        if (!toMe) return null;
        return workCaller({
            theirCall: toMe.call,
            theirGrid: toMe.grid,
            theirSnr: row.d.snr, // our SNR of their call → RST_SENT
            slotUtc: row.d.startUtc,
            offsetHz: pre.offset,
            opFreqMHz: pre.opMHz,
            fd: fd ? { class: fd.class, section: fd.section } : undefined,
            allowDuplicate: pre.allowDuplicate,
        });
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
                            <tr class="text-ink {rowClass(row.kind, info?.worked, isStale(row))}">
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
                                        >{:else if row.kind === 'call' && ft8State.qso.queue.some((x) => x.call === row.call)}<span
                                            class="ml-1 rounded bg-focus px-1 text-[10px] font-bold text-white"
                                            title="Bagged — the drain works these in order">Q</span
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
