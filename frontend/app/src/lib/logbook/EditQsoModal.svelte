<script lang="ts">
    // Edit one QSO in a modal (ported from the shipping logbook SPA, restyled
    // onto the app shell's tokens — the app has a shared .input primitive, so
    // the SPA's local input style is dropped). Seeds its form from the row
    // passed in (the modal is mounted fresh each time a row is opened, so a
    // one-shot seed is correct), builds a QsoPatch from the editable fields,
    // and PATCHes via the state's saveEdit. The daemon restores immutables
    // (identity, logbook, station callsign, forwarding/enrichment), re-derives
    // band from freq, and re-validates — so a bad edit comes back as a
    // message, not a silent write.
    //
    // Keyboard: ESC cancels, Ctrl/Cmd+Enter saves (the modal owns its own ESC).
    //
    // Save/close/status are INJECTED (ADR 0045 — presentation never reaches
    // into a state module), so the same modal serves both the Logbook page
    // (logbookState) and the Operate Session panel (sessionEdit): each owner
    // supplies its own onSave/onClose and mirrors saving/error.
    import { untrack } from 'svelte';
    import { formatQsoDate, formatTime } from './format';
    import { enrichCallsign } from '../api/enrichment';
    import type { EnrichmentStation, EnrichmentCountry } from '../api/enrichment';
    import type { LogbookQso } from '../api/logbooks';
    import type { QsoPatch } from '../api/qso-patch';

    interface Props {
        row: LogbookQso;
        saving: boolean;
        error: string | null;
        onSave: (patch: QsoPatch) => void;
        onClose: () => void;
    }
    const { row, saving, error, onSave, onClose }: Props = $props();

    // YYYY-MM-DD (the <input type=date> value) → ADIF YYYYMMDD; "" stays "".
    const toAdifDate = (v: string): string => v.replace(/-/g, '');
    // HH:MM (the <input type=time> value) → ADIF HHMM; "" stays "".
    const toAdifTime = (v: string): string => v.replace(/:/g, '');

    // Focus the callsign field on open (the form's natural first field).
    function focusOnMount(node: HTMLInputElement): void {
        node.focus();
    }

    // Form state, seeded ONCE from the row — untrack() because this is a
    // deliberate one-shot snapshot, not a live mirror (the modal is mounted fresh
    // each open, so `row` never changes during its life). Dates/times use native
    // pickers (YYYY-MM-DD / HH:MM); everything else is text the daemon validates.
    const form = $state(
        untrack(() => ({
            qso_date: formatQsoDate(row.qso_date),
            qso_date_off: formatQsoDate(row.qso_date_off),
            time_on: formatTime(row.time_on),
            time_off: formatTime(row.time_off),
            call: row.call ?? '',
            freq: row.freq ?? '',
            band: row.band ?? '',
            mode: row.mode ?? '',
            submode: row.submode ?? '',
            rst_sent: row.rst_sent ?? '',
            rst_rcvd: row.rst_rcvd ?? '',
            country: row.country ?? '',
            name: row.name ?? '',
            gridsquare: row.gridsquare ?? '',
            comment: row.comment ?? '',
        }))
    );

    // Re-enrich (the repair path for stale/missing enrichment — nameless
    // flaky-link QSOs, misfiled countries): force-fresh lookup
    // (refresh=true bypasses + rewrites the caches), fill the VISIBLE fields
    // for the operator to review, and stash the numeric/zone fields
    // (dxcc/cqz/ituz/cont — not shown in the form) to ride along on Save.
    // Nothing is written until the operator saves. The stored grid is kept
    // when present — the on-air grid is authoritative over a QRZ profile
    // locator (the FT8-sink rule); it fills only when empty.
    let enriching = $state(false);
    let enrichNote: string | null = $state(null);
    // enrichExtras is TAGGED with the callsign that produced it (provenance), so
    // buildPatch can independently refuse to ride A's hidden dxcc/zones out under a
    // callsign that has since changed to B (F-02).
    let enrichExtras: (Pick<QsoPatch, 'dxcc' | 'cqz' | 'ituz' | 'cont'> & { call: string }) | null =
        $state(null);

    // Re-enrichment concurrency discipline, mirrored from operate/enrich.svelte.ts:
    // a monotonic generation token plus an AbortController. A newer lookup — or a
    // callsign change, or unmount — invalidates any in-flight resolution, so a slow
    // lookup for the previous callsign can never modify the form, its hidden extras,
    // or the loading state after the operator has moved on. The generation AND the
    // current normalized callsign are checked INDEPENDENTLY: aborting is not enough,
    // because the fetch may already have completed or a test double may ignore it.
    let seq = 0;
    let inflight: AbortController | null = null;
    // The normalized callsign the in-flight / applied enrichment belongs to.
    let activeCall = '';
    // What the last lookup WROTE into each visible field, paired with the value it
    // replaced. An invalidation restores the pre-lookup value — but only while the
    // field still holds exactly what the lookup wrote, so an operator edit survives.
    let wrote: Record<'country' | 'name' | 'gridsquare', { written: string; prev: string } | null> =
        {
            country: null,
            name: null,
            gridsquare: null,
        };

    function retractWrites(): void {
        for (const f of ['country', 'name', 'gridsquare'] as const) {
            const w = wrote[f];
            if (w !== null && form[f] === w.written) form[f] = w.prev;
        }
        wrote = { country: null, name: null, gridsquare: null };
    }

    // Record what a lookup writes into a visible field, alongside the value it
    // replaced, so retractWrites can restore it on invalidation.
    function writeField(field: 'country' | 'name' | 'gridsquare', value: string): void {
        if (value === '') return;
        // Keep the ORIGINAL pre-enrichment value across successive same-callsign
        // writes, so an eventual invalidation restores what was there before ANY
        // lookup — not an intermediate lookup's value.
        const prev = wrote[field] !== null ? wrote[field].prev : form[field];
        wrote[field] = { written: value, prev };
        form[field] = value;
    }

    // Apply a fresh lookup to the form: overwrite country/name, fill grid only when
    // empty (the on-air grid is authoritative), and stash the numeric/zone extras
    // tagged with the callsign that produced them. Returns false when the lookup
    // carried nothing usable. Split out of reEnrich to keep each function's
    // branching low.
    function applyEnrichment(
        st: EnrichmentStation,
        co: EnrichmentCountry | undefined,
        call: string
    ): boolean {
        // No retractWrites here: a re-apply for the SAME callsign must update only
        // the fields the response carries and leave the rest (writeField skips an
        // empty value), so a partial or empty subsequent response never erases a
        // field the previous lookup filled. The retract belongs to invalidation
        // (callsign change / unmount), not re-apply (codex dc15e188 [P2]).
        const country = st.country ?? co?.name ?? '';
        const name = st.name ?? '';
        if (country === '' && name === '') return false;
        writeField('country', country);
        writeField('name', name);
        if (form.gridsquare.trim() === '') writeField('gridsquare', st.gridsquare ?? '');
        enrichExtras = {
            call,
            dxcc: st.dxcc ?? '',
            cqz: st.cqz ?? co?.cq_zone ?? '',
            ituz: st.ituz ?? co?.itu_zone ?? '',
            cont: st.cont ?? co?.continent ?? '',
        };
        return true;
    }

    function invalidate(): void {
        seq++;
        inflight?.abort();
        inflight = null;
        retractWrites();
        enrichExtras = null;
        enriching = false;
        enrichNote = null;
        activeCall = '';
    }

    // Abort + invalidate when the normalized callsign moves away from the one the
    // current lookup/result belongs to. Guarded so the initial mount (nothing in
    // flight, no extras) never fires it.
    $effect(() => {
        const norm = form.call.trim().toUpperCase();
        untrack(() => {
            if ((enriching || inflight !== null || enrichExtras !== null) && norm !== activeCall) {
                invalidate();
            }
        });
    });

    // Teardown: abort the in-flight request and bump the generation so a response
    // arriving after the modal closes is discarded rather than touching a destroyed
    // form (F-02: closing invalidates the lookup).
    $effect(() => () => {
        seq++;
        inflight?.abort();
        inflight = null;
    });

    async function reEnrich(): Promise<void> {
        const call = form.call.trim().toUpperCase();
        if (call === '' || enriching) return;
        const mine = ++seq;
        inflight?.abort();
        inflight = new AbortController();
        activeCall = call;
        enriching = true;
        enrichNote = null;

        let out;
        try {
            out = await enrichCallsign(call, inflight.signal, { refresh: true });
        } catch {
            if (seq === mine) {
                enriching = false;
                inflight = null;
                enrichNote = 'Lookup failed.';
            }
            return;
        }
        // Discard a stale result: the generation moved on, OR the form's callsign is
        // no longer the one this lookup was for. Either check alone suffices; both
        // are kept because an abort can be too late or ignored (F-02).
        if (seq !== mine || form.call.trim().toUpperCase() !== call) return;
        enriching = false;
        inflight = null;

        if (out.kind !== 'ok') {
            enrichNote = `Lookup failed: ${out.message}`;
            return;
        }
        const applied = applyEnrichment(out.result.station ?? {}, out.result.country, call);
        enrichNote = applied
            ? 'Fields updated from a fresh lookup — review, then Save.'
            : 'No enrichment data found for this callsign.';
    }

    function buildPatch(): QsoPatch {
        const patch: QsoPatch = {
            qso_date: toAdifDate(form.qso_date),
            qso_date_off: toAdifDate(form.qso_date_off),
            time_on: toAdifTime(form.time_on),
            time_off: toAdifTime(form.time_off),
            call: form.call.trim(),
            freq: form.freq.trim(),
            band: form.band.trim(),
            mode: form.mode.trim(),
            submode: form.submode.trim(),
            rst_sent: form.rst_sent.trim(),
            rst_rcvd: form.rst_rcvd.trim(),
            country: form.country.trim(),
            name: form.name.trim(),
            gridsquare: form.gridsquare.trim(),
            comment: form.comment.trim(),
        };
        // Enrichment extras ride along only when a Re-enrich ran AND its provenance
        // callsign still matches the form — an independent recheck of the generation
        // guard, so A's hidden dxcc/zones can never be saved under B (F-02). Only
        // non-empty values, never blanking an existing stored field.
        if (enrichExtras !== null && enrichExtras.call === form.call.trim().toUpperCase()) {
            for (const k of ['dxcc', 'cqz', 'ituz', 'cont'] as const) {
                const v = enrichExtras[k];
                if (v) patch[k] = v;
            }
        }
        return patch;
    }

    // While a save is in flight every exit/re-save path is inert — the Save
    // button is disabled, but Escape / backdrop / Ctrl+Enter would otherwise
    // bypass it, and closing (or re-saving) mid-PATCH races the write-back
    // onto whichever row the modal shows next.
    function save(): void {
        if (saving) return;
        onSave(buildPatch());
    }

    function close(): void {
        if (saving) return;
        onClose();
    }

    function onKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') {
            e.preventDefault();
            close();
        } else if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            save();
        }
    }
</script>

<!-- Backdrop: click outside the panel closes. The keydown lives here so ESC /
     Ctrl+Enter work wherever focus is inside the modal. -->
<svelte:window onkeydown={onKeydown} />
<div
    class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-gray-500/75 p-4 sm:items-center dark:bg-gray-900/50"
    role="presentation"
    onclick={(e) => {
        if (e.target === e.currentTarget) close();
    }}
>
    <div
        class="w-full max-w-2xl rounded-xl border border-line bg-surface shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-label="Edit QSO"
    >
        <div class="flex items-center justify-between border-b border-line px-5 py-3">
            <h2 class="text-lg font-semibold text-ink">
                Edit QSO — {form.call || row.call || '?'}
            </h2>
            <button
                type="button"
                class="cursor-pointer rounded-md px-2 py-1 text-muted hover:bg-surface-muted hover:text-ink"
                aria-label="Close"
                onclick={close}>✕</button
            >
        </div>

        <form
            class="grid grid-cols-1 gap-x-4 gap-y-3 px-5 py-4 sm:grid-cols-3"
            onsubmit={(e) => {
                e.preventDefault();
                save();
            }}
        >
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Date</span>
                <input type="date" class="input mt-0" bind:value={form.qso_date} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Time on</span>
                <input type="time" class="input mt-0" bind:value={form.time_on} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Time off</span>
                <input type="time" class="input mt-0" bind:value={form.time_off} />
            </label>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Callsign</span>
                <input
                    type="text"
                    class="input mt-0 font-mono uppercase"
                    bind:value={form.call}
                    use:focusOnMount
                />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted"
                    >Date off <span class="text-muted/60">(if overnight)</span></span
                >
                <input type="date" class="input mt-0" bind:value={form.qso_date_off} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Country</span>
                <input type="text" class="input mt-0" bind:value={form.country} />
            </label>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Freq <span class="text-muted/60">(MHz)</span></span>
                <input type="text" class="input mt-0 font-mono" bind:value={form.freq} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted"
                    >Band <span class="text-muted/60">(follows freq)</span></span
                >
                <input type="text" class="input mt-0" bind:value={form.band} />
            </label>
            <div></div>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Mode</span>
                <input type="text" class="input mt-0 uppercase" bind:value={form.mode} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Submode</span>
                <input type="text" class="input mt-0 uppercase" bind:value={form.submode} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Grid</span>
                <input type="text" class="input mt-0" bind:value={form.gridsquare} />
            </label>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">RST sent</span>
                <input type="text" class="input mt-0 font-mono" bind:value={form.rst_sent} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">RST rcvd</span>
                <input type="text" class="input mt-0 font-mono" bind:value={form.rst_rcvd} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-muted">Name</span>
                <input type="text" class="input mt-0" bind:value={form.name} />
            </label>

            <label class="flex flex-col gap-1 text-sm sm:col-span-3">
                <span class="text-muted">Comment</span>
                <input type="text" class="input mt-0" bind:value={form.comment} />
            </label>
        </form>

        {#if error !== null}
            <p
                class="mx-5 mb-1 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-800 dark:bg-red-500/10 dark:text-red-300"
            >
                {error}
            </p>
        {/if}

        <div class="flex items-center gap-2 border-t border-line px-5 py-3">
            <!-- Re-enrich sits left, apart from Save/Cancel: it's a fetch-and-
                 review step, not a commit — nothing writes until Save. -->
            <button
                type="button"
                class="btn text-xs"
                disabled={enriching || form.call.trim() === ''}
                title="Fresh QRZ/hamnut lookup for this callsign (bypasses the cache); fills the fields for review — nothing is saved until you Save"
                onclick={() => void reEnrich()}
            >
                {enriching ? 'Looking up…' : 'Re-enrich'}
            </button>
            {#if enrichNote !== null}
                <span class="text-xs text-muted">{enrichNote}</span>
            {/if}
            <span class="ml-auto"></span>
            <button type="button" class="btn" disabled={saving} onclick={close}>Cancel</button>
            <button type="button" class="btn btn-primary" disabled={saving} onclick={save}>
                {saving ? 'Saving…' : 'Save'}
            </button>
        </div>
    </div>
</div>
