/*
    Logbook page state (ported from the shipping logbook SPA, ADR 0044
    consolidation) — the daemon-backed browse surface: the list of logbooks, the
    selected one, and a cursor-paginated page of its QSOs.

    Cursor paging (the daemon is forward-only: `after` → `next_cursor`): we keep the
    per-page `after` cursor in #cursors so Next/Prev/First can walk without re-deriving.
    page 0 uses no cursor; loading page N remembers page N+1's cursor when one comes
    back. No arbitrary page jumps — the daemon has no offset endpoint.

    No persistence — the logbook browse is transient view state (which page you're on
    isn't worth surviving a reload).
*/

import { SvelteSet } from 'svelte/reactivity';
import {
    fetchLogbooks,
    fetchLogbookCount,
    fetchQsoPage,
    type Logbook,
    type LogbookQso,
} from '../api/logbooks';
import { patchQso, type QsoPatch } from '../api/qso-patch';
import { fetchMailer, fetchForwarders } from '../api/config-blocks';
import { enqueueUploads } from '../api/uploads';
import { enrichCallsign } from '../api/enrichment';
import { forwarderLabel, hasUploadStamp, type ForwarderInfo } from './uploadStatus';

const PAGE_SIZES = [25, 50, 100] as const;

class LogbookState {
    /** All logbooks (the selector). */
    logbooks: Logbook[] = $state([]);
    /** Selected logbook id, or null before one is chosen / none exist. */
    selectedId: number | null = $state(null);
    /** Total QSO count for the selected logbook (the "of N"). */
    count: number = $state(0);

    /** Current page of QSO rows. */
    rows: LogbookQso[] = $state([]);
    /** 0-based index of the page currently shown. */
    pageIndex: number = $state(0);
    /** Rows per page. */
    pageSize: number = $state(25);

    /** True while a page (or the initial lists) is loading. */
    loading: boolean = $state(false);
    /** Human-readable error, or null. */
    error: string | null = $state(null);

    // The QSO currently open in the edit modal, or null when closed. Holds the
    // row as it was when opened (the modal seeds its form from it).
    editing: LogbookQso | null = $state(null);
    /** True while an edit PATCH is in flight. */
    savingEdit: boolean = $state(false);
    /** Edit error to show in the modal (validation/conflict/transport), or null. */
    editError: string | null = $state(null);

    // Selected QSO row ids (the numeric primary key), for bulk actions (email today;
    // forward/export are a follow-up). Selection persists across pages so an operator
    // can gather rows from several pages before acting; it's cleared only when
    // switching logbooks. SvelteSet so `.has()` reads are reactive.
    readonly selected = new SvelteSet<number>();
    // id → UUID for every selected row, captured at selection time. The page only
    // holds the current rows, but selection spans pages, so we can't recover a
    // paged-away row's UUID from `rows` at send time — we stash it on toggle. Plain
    // Map: read only when building the email payload, never in render.
    readonly #selectedUuids = new Map<number, string>();

    // Mailer config (SMTP enabled + default recipient), read once on init from
    // /v1/config so the email-out controls can gate + pre-fill — mirroring the
    // logging SPA's SessionEmailControls. Absent/failed fetch → disabled, no default.
    mailerEnabled: boolean = $state(false);
    mailerDefaultRecipient: string = $state('');

    // Configured forwarders (from /v1/config), for the upload-status colour and
    // the backfill destination picker (ADR 0039). The ENABLED subset is E.
    forwarders: ForwarderInfo[] = $state([]);
    // The destination picker value: '' = All (no upload context), else a forwarder
    // NAME. Driving both the "missing from X" filter and the upload target.
    selectedDestination: string = $state('');
    // When a destination is picked, hide already-uploaded rows (filter to gaps) by
    // default; the operator can flip this to see the whole logbook while keeping the
    // upload target. No effect when no destination is selected.
    showUploaded: boolean = $state(false);
    // Client-side filter: hide QSOs already sent via email (sm_fwrd_by_email_status
    // = "Y") from the current page — the "what still needs emailing" view. Purely
    // client-side (filters the loaded page), so it needs no reload; fine for the
    // common case of a handful of recent QSOs sitting on the first page.
    notEmailedOnly: boolean = $state(false);
    // True while a manual backfill upload is in flight (disables the button).
    uploading: boolean = $state(false);
    // True while a bulk re-enrich sweep runs (disables its button);
    // reEnrichProgress carries the "12/47" counter for the button label.
    reEnriching: boolean = $state(false);
    reEnrichProgress: string = $state('');
    // Transient success notice after an upload (e.g. "Queued 12 to QRZ"), or null.
    notice: string | null = $state(null);

    // The `after` cursor for each loaded page index (cursors[0] = null = first page).
    // Non-reactive: pure navigation bookkeeping, never rendered.
    #cursors: (string | null)[] = [null];
    // Cursor to fetch the NEXT page; reactive so `hasNext` updates the button.
    nextCursor: string | null = $state(null);

    readonly pageSizeOptions = PAGE_SIZES;

    get hasNext(): boolean {
        return this.nextCursor !== null;
    }
    get hasPrev(): boolean {
        return this.pageIndex > 0;
    }
    /** 1-based index of the first row shown (0 when empty). */
    get rangeStart(): number {
        return this.count === 0 || this.rows.length === 0 ? 0 : this.pageIndex * this.pageSize + 1;
    }
    /** 1-based index of the last row shown. */
    get rangeEnd(): number {
        return this.pageIndex * this.pageSize + this.rows.length;
    }
    get selectedLogbook(): Logbook | undefined {
        return this.logbooks.find((l) => l.id === this.selectedId);
    }

    /** Enabled forwarders (E) — drives the upload-status colour + the picker. */
    get enabledForwarders(): ForwarderInfo[] {
        return this.forwarders.filter((f) => f.enabled);
    }

    /** The `missing_from` query value for the current view: the selected
     *  destination when one is picked, it can answer "missing from?", and "show
     *  uploaded" is off. Else undefined (no filter — All, showing the whole
     *  logbook, or a destination that stamps nothing). */
    get missingFromParam(): string | undefined {
        return this.hasDestination && this.destinationTracksUploads && !this.showUploaded
            ? this.selectedDestination
            : undefined;
    }

    /** True when a destination is picked, so the Upload action + show-uploaded
     *  toggle are relevant. */
    get hasDestination(): boolean {
        return this.selectedDestination !== '';
    }

    /** Whether the picked destination records a per-QSO upload stamp, i.e.
     *  whether the gap view ("not on X") is answerable for it at all. The daemon
     *  rejects missing_from for a type that records none, so sending it anyway
     *  was a guaranteed 400 (dogfood 2026-07-27). Such a destination stays a
     *  valid upload TARGET; the operator just sees the whole logbook while it is
     *  selected. SM Cloud is the case in practice — a row mirror holding a full
     *  copy — but "no stamp" does not imply mirroring; see hasUploadStamp. */
    /**
     * What to CALL the selected destination in front of the operator.
     *
     * Exists because `selectedDestination` is a KEY — it addresses
     * POST /v1/forwarder/{name}/uploads and matches `missing_from` — and it was
     * also being rendered in three places, so a labelled destination appeared as
     * "QRZ (club account)" in the dropdown and "qrz" in the button, notice and
     * empty-state within one workflow (review 288518755c52). One getter, so the
     * display sites cannot drift from each other again.
     *
     * Falls back to the raw name for a destination no longer in the list —
     * disabled between the pick and the render — rather than going blank.
     */
    get selectedDestinationLabel(): string {
        const f = this.forwarders.find((x) => x.name === this.selectedDestination);
        return f ? forwarderLabel(f) : this.selectedDestination;
    }

    get destinationTracksUploads(): boolean {
        const f = this.forwarders.find((x) => x.name === this.selectedDestination);
        return f !== undefined && hasUploadStamp(f.type);
    }

    /** Rows to display. The server already filters emailed rows out of the page
     *  when `notEmailedOnly` is on (the not_emailed param), so this client filter
     *  is now just the immediate-vanish layer: it hides a row the instant
     *  markEmailed flips it locally, before the next fetch drops it for real.
     *  Everything that renders + the select-all helpers key off this, so
     *  "visible" consistently means "what the operator can see". */
    get visibleRows(): LogbookQso[] {
        return this.notEmailedOnly
            ? this.rows.filter((r) => r.sm_fwrd_by_email_status !== 'Y')
            : this.rows;
    }

    /** How many rows are selected (across all pages). */
    get selectedCount(): number {
        return this.selected.size;
    }
    /** True when every VISIBLE row is selected (and there is at least one). */
    get allVisibleSelected(): boolean {
        return (
            this.visibleRows.length > 0 && this.visibleRows.every((r) => this.selected.has(r.id))
        );
    }
    /** True when some — but not all — visible rows are selected (header checkbox indeterminate). */
    get someVisibleSelected(): boolean {
        return this.visibleRows.some((r) => this.selected.has(r.id)) && !this.allVisibleSelected;
    }

    /** UUIDs of the selected rows (skips any row lacking a UUID — e.g. a pre-UUID
     *  legacy import, which can't be emailed). The email payload keys off these. */
    get selectedUuids(): string[] {
        const out: string[] = [];
        for (const id of this.selected) {
            const uuid = this.#selectedUuids.get(id);
            if (uuid) out.push(uuid);
        }
        return out;
    }

    /** Toggle one row's selection. Takes the row (not just the id) so the UUID is
     *  captured for cross-page email — see #selectedUuids. */
    toggleRow(row: LogbookQso): void {
        if (this.selected.has(row.id)) {
            this.selected.delete(row.id);
            this.#selectedUuids.delete(row.id);
        } else {
            this.selected.add(row.id);
            if (row.uuid) this.#selectedUuids.set(row.id, row.uuid);
        }
    }
    /** Header checkbox: select all visible rows, or clear them if all are already selected. */
    toggleAllVisible(): void {
        if (this.allVisibleSelected) {
            for (const r of this.visibleRows) {
                this.selected.delete(r.id);
                this.#selectedUuids.delete(r.id);
            }
        } else {
            for (const r of this.visibleRows) {
                this.selected.add(r.id);
                if (r.uuid) this.#selectedUuids.set(r.id, r.uuid);
            }
        }
    }

    /** Flip the "not emailed only" filter and reload count + first page so it
     *  applies across the WHOLE logbook (server-side not_emailed), not just the
     *  loaded page — the page-local behaviour was the reported bug. Mirrors the
     *  toggleShowUploaded reload shape (reset the cursor trail: cursors from the
     *  unfiltered view don't address the filtered set). */
    async toggleNotEmailedOnly(): Promise<void> {
        this.notEmailedOnly = !this.notEmailedOnly;
        this.#resetPaging();
        await Promise.all([this.#loadCount(), this.#loadPage(0)]);
    }
    /** Drop the entire selection. */
    clearSelection(): void {
        this.selected.clear();
        this.#selectedUuids.clear();
    }

    /** Flip the loaded rows whose UUID was just emailed to "forwarded by email"
     *  so the callsign tint updates immediately (the daemon has durably stamped
     *  sm_fwrd_by_email_*; this mirrors that onto the in-memory page). Rows on other
     *  pages reflect it on their next fetch.
     *
     *  `originLogbookId` is the logbook the send was issued from, captured BEFORE
     *  the async send. When the "not emailed only" filter is on we reload to resync
     *  paging — but only if the operator is still on that same logbook, so a send
     *  that lands after a logbook switch can't reset the unrelated one. */
    markEmailed(uuids: string[], originLogbookId: number | null): void {
        if (uuids.length === 0) return;
        const set = new Set(uuids);
        for (let i = 0; i < this.rows.length; i++) {
            const uuid = this.rows[i].uuid;
            if (uuid && set.has(uuid)) {
                this.rows[i] = { ...this.rows[i], sm_fwrd_by_email_status: 'Y' };
            }
        }
        // With the server-side "not emailed only" filter on, the just-stamped rows
        // leave the filtered set: the visibleRows client filter hides them
        // instantly, but the count + cursor trail would be left stale (the pager
        // reading "of N" too high, or an empty table with a nonzero total). Reload
        // PAGE 0 to resync — not the current pageIndex, whose start cursor is a
        // fixed tuple that no longer bounds a shrunk page (refetching it in place
        // can strand the operator on an emptied last page). We deliberately do NOT
        // pre-clear rows/cursors: #loadPage replaces them only on success and
        // resets pageIndex to 0 there, rebuilding the forward trail as the operator
        // navigates — so a FAILED refresh preserves the current page instead of
        // blanking it with no way back. Guarded on the origin logbook so a stale
        // completion can't reset a logbook the operator has since switched to.
        // Fire-and-forget — the reactive state updates when it lands.
        if (this.notEmailedOnly && this.selectedId === originLogbookId) {
            void Promise.all([this.#loadCount(), this.#loadPage(0)]);
        }
    }

    /** Open the edit modal on a row. */
    openEdit(row: LogbookQso): void {
        this.editing = row;
        this.editError = null;
    }
    /** Close the edit modal (discarding any unsaved form changes). */
    closeEdit(): void {
        this.editing = null;
        this.editError = null;
        this.savingEdit = false;
    }

    /**
     * Apply an edit to the QSO currently open in the modal. On success the row
     * is replaced in place (the daemon returns the canonical merged QSO, with
     * band re-derived from freq etc.) and the modal closes. On failure the
     * daemon's message lands in editError and the modal stays open so the
     * operator can fix it. Returns true on success.
     */
    async saveEdit(patch: QsoPatch): Promise<boolean> {
        const target = this.editing;
        if (target?.uuid === undefined || target.uuid === '') {
            this.editError = 'This QSO has no id and cannot be edited.';
            return false;
        }
        this.savingEdit = true;
        this.editError = null;
        const out = await patchQso(target.uuid, patch);
        this.savingEdit = false;
        if (out.kind !== 'ok') {
            this.editError = out.message;
            return false;
        }
        // Replace the row in place by id ($state arrays are deeply reactive, so
        // an index assignment re-renders just that row).
        const i = this.rows.findIndex((r) => r.id === target.id);
        if (i !== -1) this.rows[i] = out.qso;
        this.closeEdit();
        return true;
    }

    /** Load the logbook list on mount, then auto-select the first one. The mailer
     *  block is fetched alongside (fire-and-forget): the email-out controls gate on
     *  it, but a failure there must not block browsing, so it's not awaited here. */
    async init(): Promise<void> {
        this.loading = true;
        this.error = null;
        void this.loadMailer();
        void this.loadForwarders();
        const out = await fetchLogbooks();
        if (out.kind !== 'ok') {
            this.error = out.message;
            this.loading = false;
            return;
        }
        this.logbooks = out.logbooks;
        this.loading = false;
        if (out.logbooks.length > 0) {
            await this.selectLogbook(out.logbooks[0].id);
        }
    }

    /** Read the mailer block (SMTP enabled + default recipient) from /v1/config for
     *  the email-out controls. Best-effort: a failure leaves the controls disabled
     *  (mailerEnabled stays false), never an error — browsing is unaffected. */
    async loadMailer(): Promise<void> {
        const out = await fetchMailer();
        if (out.kind === 'ok') {
            this.mailerEnabled = out.mailer.enabled;
            this.mailerDefaultRecipient = out.mailer.defaultRecipient;
        }
    }

    /** Read the configured forwarders from /v1/config for the upload-status colour
     *  + destination picker. Best-effort: a failure leaves the list empty (no
     *  colour, no picker), never an error — browsing is unaffected. */
    async loadForwarders(): Promise<void> {
        const out = await fetchForwarders();
        if (out.kind === 'ok') this.forwarders = out.forwarders;
    }

    /** Pick a backfill destination (forwarder name, or '' for All). Resets the
     *  "show uploaded" toggle to the gap-filtered default and reloads. */
    async selectDestination(name: string): Promise<void> {
        if (name === this.selectedDestination) return;
        this.selectedDestination = name;
        this.showUploaded = false;
        this.notice = null;
        this.#resetPaging();
        await Promise.all([this.#loadCount(), this.#loadPage(0)]);
    }

    /** Toggle showing already-uploaded rows (only meaningful with a destination
     *  picked); reloads with/without the missing_from filter. */
    async toggleShowUploaded(): Promise<void> {
        this.showUploaded = !this.showUploaded;
        this.#resetPaging();
        await Promise.all([this.#loadCount(), this.#loadPage(0)]);
    }

    /**
     * Queue the selected QSOs for upload to the picked destination (ADR 0039
     * backfill). On success: a notice with the daemon's summary, the selection is
     * cleared, and the page+count reload (already-uploaded rows the daemon skipped,
     * and the just-queued ones, stay visible until the worker stamps them, so the
     * gap list shrinks as uploads complete). A no-destination/empty-selection call
     * is a no-op. force is false — never silently re-send.
     */
    async uploadSelected(): Promise<void> {
        if (this.selectedDestination === '' || this.selectedUuids.length === 0) return;
        const dest = this.selectedDestination;
        // Captured WITH dest, before the await, for the same reason dest is: the
        // destination <select> stays live during an upload (only the button is
        // disabled), so resolving the label afterwards would name whatever the
        // operator switched to — a notice pointing at somewhere the QSOs were
        // never sent (review 72a61e962f52).
        const destLabel = this.selectedDestinationLabel;
        const uuids = this.selectedUuids;
        this.uploading = true;
        this.error = null;
        this.notice = null;
        const out = await enqueueUploads(dest, uuids, false);
        this.uploading = false;
        if (out.kind !== 'ok') {
            this.error = out.message;
            return;
        }
        const r = out.result;
        const bits = [`Queued ${r.enqueued} to ${destLabel}`];
        if (r.skipped_uploaded > 0) bits.push(`${r.skipped_uploaded} already uploaded`);
        const skippedDeleted = r.skipped_deleted?.length ?? 0;
        if (skippedDeleted > 0) bits.push(`${skippedDeleted} deleted (skipped)`);
        const notFound = r.not_found?.length ?? 0;
        if (notFound > 0) bits.push(`${notFound} not found`);
        this.notice = bits.join(' · ') + '.';
        this.clearSelection();
        await Promise.all([this.#loadCount(), this.#loadPage(this.pageIndex)]);
    }

    /**
     * Bulk Re-enrich (the backfill pattern as a toolbar action): for each
     * SELECTED row on the CURRENT page, force-refresh the enrichment
     * (refresh=true — cache-bypass) and PATCH only what actually changed.
     * Policies, both proven by the 2026-07-13 backfill:
     *   - skip-if-unchanged: a PATCH re-arms that QSO's QRZ update upload, so
     *     an already-correct row must not fire a no-op re-upload;
     *   - only non-empty fresh values are written (never blank a stored
     *     field), and gridsquare fills ONLY when the stored one is empty —
     *     the on-air grid is authoritative over a profile locator.
     * Scope is the current page because the comparison needs live row data;
     * selected rows on other pages are reported as skipped, not silently
     * dropped. Sequential (one lookup at a time) — natural pacing for the
     * upstream providers.
     */
    // BASELINE DEBT 2026-07-31 (complexity 36) — per-field merge decisions over the
    // enrichment result, each with its own keep/overwrite rule.
    // eslint-disable-next-line complexity
    async reEnrichSelected(): Promise<void> {
        if (this.reEnriching) return;
        const onPage = this.rows.filter((r) => this.selected.has(r.id));
        const targets = onPage.filter((r) => r.uuid && (r.call ?? '').trim() !== '');
        const offPage = this.selected.size - onPage.length;
        if (targets.length === 0) {
            this.notice = 'No selected rows on this page to re-enrich.';
            return;
        }
        this.reEnriching = true;
        this.notice = null;
        this.error = null;
        let changed = 0;
        let unchanged = 0;
        let noData = 0;
        let failed = 0;
        for (let i = 0; i < targets.length; i++) {
            const row = targets[i];
            this.reEnrichProgress = `${i + 1}/${targets.length}`;
            const out = await enrichCallsign((row.call ?? '').trim(), undefined, {
                refresh: true,
            });
            if (out.kind !== 'ok') {
                failed++;
                continue;
            }
            const st = out.result.station ?? {};
            const co = out.result.country;
            // Candidate fresh values (empty = no data for that field).
            const fresh: Partial<Record<keyof QsoPatch, string>> = {
                country: st.country ?? co?.name ?? '',
                name: st.name ?? '',
                dxcc: st.dxcc ?? '',
                cqz: st.cqz ?? co?.cq_zone ?? '',
                ituz: st.ituz ?? co?.itu_zone ?? '',
                cont: st.cont ?? co?.continent ?? '',
            };
            if ((row.gridsquare ?? '').trim() === '') {
                fresh.gridsquare = st.gridsquare ?? '';
            }
            if (Object.values(fresh).every((v) => v === '')) {
                noData++;
                continue;
            }
            const patch: QsoPatch = {};
            for (const [k, v] of Object.entries(fresh) as [keyof QsoPatch, string][]) {
                if (v !== '' && v !== ((row as unknown as Record<string, unknown>)[k] ?? '')) {
                    patch[k] = v;
                }
            }
            if (Object.keys(patch).length === 0) {
                unchanged++;
                continue;
            }
            const res = await patchQso(row.uuid as string, patch);
            if (res.kind !== 'ok') {
                failed++;
                continue;
            }
            const idx = this.rows.findIndex((r) => r.id === row.id);
            if (idx !== -1) this.rows[idx] = res.qso;
            changed++;
        }
        this.reEnriching = false;
        this.reEnrichProgress = '';
        const bits = [`Re-enriched ${changed}`];
        if (unchanged > 0) bits.push(`${unchanged} unchanged`);
        if (noData > 0) bits.push(`${noData} no data`);
        if (failed > 0) bits.push(`${failed} failed`);
        if (offPage > 0) bits.push(`${offPage} selected on other pages skipped`);
        this.notice = bits.join(' \u00b7 ') + '.';
    }

    /** Switch logbooks: reset paging, refresh count + first page. */
    async selectLogbook(id: number): Promise<void> {
        this.selectedId = id;
        this.clearSelection();
        this.#resetPaging();
        await Promise.all([this.#loadCount(), this.#loadPage(0)]);
    }

    /** Change page size: reset paging and reload from the first page. */
    async setPageSize(size: number): Promise<void> {
        if (size === this.pageSize) return;
        this.pageSize = size;
        this.#resetPaging();
        await this.#loadPage(0);
    }

    async nextPage(): Promise<void> {
        if (this.hasNext) await this.#loadPage(this.pageIndex + 1);
    }
    async prevPage(): Promise<void> {
        if (this.hasPrev) await this.#loadPage(this.pageIndex - 1);
    }
    async firstPage(): Promise<void> {
        if (this.pageIndex !== 0) await this.#loadPage(0);
    }

    #resetPaging(): void {
        this.#cursors = [null];
        this.nextCursor = null;
        this.pageIndex = 0;
        this.rows = [];
    }

    // Request generations: the selector/filter/paging controls stay enabled
    // while a load is in flight, so rapid picks race and whichever RESPONSE
    // lands last would win — showing logbook A's rows under selector B and
    // corrupting the cursor trail. Each loader bumps its own generation on
    // entry and discards any completion that is no longer the newest call.
    // (Per-loader counters, not one shared: #loadCount and #loadPage run
    // concurrently via Promise.all and must not cancel each other.)
    #countGen = 0;
    #pageGen = 0;

    async #loadCount(): Promise<void> {
        if (this.selectedId === null) return;
        const gen = ++this.#countGen;
        const out = await fetchLogbookCount(
            this.selectedId,
            this.missingFromParam,
            this.notEmailedOnly
        );
        if (gen !== this.#countGen) return; // superseded — a newer load owns the state
        if (out.kind === 'ok') this.count = out.count;
        // A count failure is non-fatal — the table still works; just no "of N".
    }

    async #loadPage(index: number): Promise<void> {
        if (this.selectedId === null) return;
        const gen = ++this.#pageGen;
        this.loading = true;
        this.error = null;
        const after = this.#cursors[index] ?? undefined;
        const out = await fetchQsoPage(
            this.selectedId,
            this.pageSize,
            after ?? undefined,
            this.missingFromParam,
            this.notEmailedOnly
        );
        if (gen !== this.#pageGen) return; // superseded — the newer call owns loading/rows
        if (out.kind === 'ok') {
            this.rows = out.items;
            this.pageIndex = index;
            this.nextCursor = out.nextCursor;
            // Remember the next page's cursor so Next can fetch it.
            if (out.nextCursor !== null) this.#cursors[index + 1] = out.nextCursor;
        } else {
            this.error = out.message;
        }
        this.loading = false;
    }
}

export const logbookState = new LogbookState();
