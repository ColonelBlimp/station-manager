/*
    Logbook SPA state — the daemon-backed browse surface: the list of logbooks, the
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
import { patchQso, type QsoPatch } from '../api/qso';
import { fetchMailer, fetchForwarders } from '../api/config';
import { enqueueUploads } from '../api/uploads';
import type { ForwarderInfo } from '../utils/uploadStatus';

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
    // True while a manual backfill upload is in flight (disables the button).
    uploading: boolean = $state(false);
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
     *  destination when one is picked and "show uploaded" is off, else undefined
     *  (no filter — All, or showing the whole logbook). */
    get missingFromParam(): string | undefined {
        return this.selectedDestination !== '' && !this.showUploaded
            ? this.selectedDestination
            : undefined;
    }

    /** True when a destination is picked, so the Upload action + show-uploaded
     *  toggle are relevant. */
    get hasDestination(): boolean {
        return this.selectedDestination !== '';
    }

    /** How many rows are selected (across all pages). */
    get selectedCount(): number {
        return this.selected.size;
    }
    /** True when every row on the current page is selected (and there is at least one). */
    get allVisibleSelected(): boolean {
        return this.rows.length > 0 && this.rows.every((r) => this.selected.has(r.id));
    }
    /** True when some — but not all — visible rows are selected (header checkbox indeterminate). */
    get someVisibleSelected(): boolean {
        return this.rows.some((r) => this.selected.has(r.id)) && !this.allVisibleSelected;
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
            for (const r of this.rows) {
                this.selected.delete(r.id);
                this.#selectedUuids.delete(r.id);
            }
        } else {
            for (const r of this.rows) {
                this.selected.add(r.id);
                if (r.uuid) this.#selectedUuids.set(r.id, r.uuid);
            }
        }
    }
    /** Drop the entire selection. */
    clearSelection(): void {
        this.selected.clear();
        this.#selectedUuids.clear();
    }

    /** Flip the visible rows whose UUID was just emailed to "forwarded by email"
     *  so the callsign tint updates immediately (the daemon has durably stamped
     *  sm_fwrd_by_email_*; this mirrors that onto the in-memory page). Rows on other
     *  pages reflect it on their next fetch. */
    markEmailed(uuids: string[]): void {
        if (uuids.length === 0) return;
        const set = new Set(uuids);
        for (let i = 0; i < this.rows.length; i++) {
            const uuid = this.rows[i].uuid;
            if (uuid && set.has(uuid)) {
                this.rows[i] = { ...this.rows[i], sm_fwrd_by_email_status: 'Y' };
            }
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
        const bits = [`Queued ${r.enqueued} to ${dest}`];
        if (r.skipped_uploaded > 0) bits.push(`${r.skipped_uploaded} already uploaded`);
        const skippedDeleted = r.skipped_deleted?.length ?? 0;
        if (skippedDeleted > 0) bits.push(`${skippedDeleted} deleted (skipped)`);
        const notFound = r.not_found?.length ?? 0;
        if (notFound > 0) bits.push(`${notFound} not found`);
        this.notice = bits.join(' · ') + '.';
        this.clearSelection();
        await Promise.all([this.#loadCount(), this.#loadPage(this.pageIndex)]);
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

    async #loadCount(): Promise<void> {
        if (this.selectedId === null) return;
        const out = await fetchLogbookCount(this.selectedId, this.missingFromParam);
        if (out.kind === 'ok') this.count = out.count;
        // A count failure is non-fatal — the table still works; just no "of N".
    }

    async #loadPage(index: number): Promise<void> {
        if (this.selectedId === null) return;
        this.loading = true;
        this.error = null;
        const after = this.#cursors[index] ?? undefined;
        const out = await fetchQsoPage(
            this.selectedId,
            this.pageSize,
            after ?? undefined,
            this.missingFromParam
        );
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
