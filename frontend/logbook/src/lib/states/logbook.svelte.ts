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

    // Selected QSO row ids (the numeric primary key), for bulk actions
    // (forward/export/email — those actions are a follow-up). Selection persists
    // across pages so an operator can gather rows from several pages before acting;
    // it's cleared only when switching logbooks. SvelteSet so `.has()` reads are reactive.
    readonly selected = new SvelteSet<number>();

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

    /** Toggle one row's selection. */
    toggleRow(id: number): void {
        if (this.selected.has(id)) this.selected.delete(id);
        else this.selected.add(id);
    }
    /** Header checkbox: select all visible rows, or clear them if all are already selected. */
    toggleAllVisible(): void {
        if (this.allVisibleSelected) {
            for (const r of this.rows) this.selected.delete(r.id);
        } else {
            for (const r of this.rows) this.selected.add(r.id);
        }
    }
    /** Drop the entire selection. */
    clearSelection(): void {
        this.selected.clear();
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

    /** Load the logbook list on mount, then auto-select the first one. */
    async init(): Promise<void> {
        this.loading = true;
        this.error = null;
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
        const out = await fetchLogbookCount(this.selectedId);
        if (out.kind === 'ok') this.count = out.count;
        // A count failure is non-fatal — the table still works; just no "of N".
    }

    async #loadPage(index: number): Promise<void> {
        if (this.selectedId === null) return;
        this.loading = true;
        this.error = null;
        const after = this.#cursors[index] ?? undefined;
        const out = await fetchQsoPage(this.selectedId, this.pageSize, after ?? undefined);
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
