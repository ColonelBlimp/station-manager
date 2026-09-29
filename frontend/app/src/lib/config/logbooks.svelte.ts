/*
    Settings → Logbooks, first slice (W-0021 dossier, operator ruling 2026-09-29):
    the ACTIVE archive's logbooks — the only file the daemon has open — managed
    over the existing POST / PATCH / DELETE /v1/logbook. Creating a logbook never
    activates an archive, makes it the default, or enables an upload the operator
    did not tick; the SM Cloud option (ADR 0082 part 2) is offered only when this
    archive can turn it on. Every write re-reads the list; a timed-out write is an
    unknown outcome (ADR 0078), never a failure. Default selection is out of scope:
    only the Default logbook receives live contacts.
*/
import {
    createLogbook,
    deleteLogbook,
    fetchLogbookCount,
    fetchLogbooks,
    renameLogbook,
    type LogbookWriteOutcome,
} from '../api/logbooks';
import { fetchStationContext } from '../api/seams';
import { fetchDaemonIdentity } from '../api/qso-archives';
import { fetchArchiveBindings, saveArchiveBindings } from '../api/archive-bindings';
import { OUTCOME_UNKNOWN_LEAD } from '../api/_helpers';
import { archiveSwitchGate, archivesState } from './archives.svelte';
import { bindingsState } from './bindings.svelte';
import { setStationInfo } from '../operate/station.svelte';
import { toasts } from '../ui/toasts.svelte';

export interface LogbookRow {
    id: number;
    name: string;
    callsign: string;
    /** QSOs held; null when the count could not be read. */
    count: number | null;
}

const qsos = (n: number): string => `${n.toLocaleString()} ${n === 1 ? 'QSO' : 'QSOs'}`;

class LogbooksState {
    rows = $state<LogbookRow[]>([]);
    /** The Default logbook — where live contacts go. 0 when unknown. */
    defaultId = $state(0);
    stationCallsign = $state('');
    archiveLabel = $state('');
    /** SM Cloud can be turned on for a logbook of this archive. */
    smcloudAvailable = $state(false);
    loaded = $state(false);
    loading = $state(false);
    error = $state('');
    /** A create, rename or delete is in flight. */
    busy = $state(false);

    // The drafts live here, not in the component, so the Settings leave guard
    // (unsaved.ts) can see and discard them (operator ruling 2026-09-29).
    addName = $state('');
    addCallsign = $state('');
    /** The operator typed a callsign; a later load no longer prefills it. */
    addCallsignTouched = $state(false);
    addSmcloud = $state(false);
    /** The logbook being renamed, 0 when none; renameFrom is its current name. */
    renameId = $state(0);
    renameName = $state('');
    renameFrom = $state('');
    #archiveId = '';
    // Each read takes a generation; only the newest may apply, so an older read
    // that answers last (a tab opening while a write's re-read runs) never
    // replaces a newer list.
    #generation = 0;

    async load(): Promise<void> {
        const generation = ++this.#generation;
        this.loading = true;
        try {
            const [list, ctx, identity] = await Promise.all([
                fetchLogbooks(),
                fetchStationContext(),
                fetchDaemonIdentity(),
            ]);
            if (generation !== this.#generation) return;
            if (list.kind === 'error') {
                this.error = list.message;
                this.loaded = false;
                return;
            }
            const counts = await Promise.all(list.logbooks.map((l) => fetchLogbookCount(l.id)));
            if (generation !== this.#generation) return;
            this.rows = list.logbooks.map((l, i) => {
                const c = counts[i];
                return {
                    id: l.id,
                    name: l.name,
                    callsign: l.callsign,
                    count: c.kind === 'ok' ? c.count : null,
                };
            });
            this.defaultId = ctx.logbookId;
            this.stationCallsign = ctx.stationCallsign;
            this.prefillCallsign(ctx.stationCallsign);
            this.#archiveId = identity?.archiveId ?? '';
            await this.#readBindings(generation);
            if (generation !== this.#generation) return;
            this.error = '';
            this.loaded = true;
        } finally {
            if (generation === this.#generation) this.loading = false;
        }
    }

    async #readBindings(generation: number): Promise<void> {
        this.smcloudAvailable = false;
        this.archiveLabel =
            archivesState.list.find((a) => a.id === this.#archiveId)?.label ?? this.archiveLabel;
        if (this.#archiveId === '') return;
        const out = await fetchArchiveBindings(this.#archiveId);
        if (generation !== this.#generation || out.kind !== 'ok') return;
        if (out.bindings.archive_label !== '') this.archiveLabel = out.bindings.archive_label;
        const sm = out.bindings.destinations.find((d) => d.type === 'smcloud');
        this.smcloudAvailable = sm !== undefined && sm.reason === '';
    }

    /** Unsaved drafts: a typed name, a callsign other than the prefill, a ticked
     *  SM Cloud, or a changed rename. The prefilled form and a merely opened
     *  Rename are not edits. */
    get dirty(): boolean {
        const callsignChanged =
            this.addCallsignTouched &&
            this.addCallsign.trim().toUpperCase() !== this.stationCallsign.trim().toUpperCase();
        const addDirty = this.addName.trim() !== '' || callsignChanged || this.addSmcloud;
        const renameDirty = this.renameId !== 0 && this.renameName.trim() !== this.renameFrom;
        return addDirty || renameDirty;
    }

    /** A create, rename or delete is on the wire: its outcome cannot be recalled. */
    get saving(): boolean {
        return this.busy;
    }

    /** Prefill the Add form's callsign, unless the operator typed their own. */
    prefillCallsign(station: string): void {
        if (!this.addCallsignTouched) this.addCallsign = station;
    }

    setAddCallsign(value: string): void {
        this.addCallsignTouched = true;
        this.addCallsign = value.toUpperCase();
    }

    clearAdd(): void {
        this.addName = '';
        this.addSmcloud = false;
        this.addCallsignTouched = false;
        this.addCallsign = this.stationCallsign;
    }

    startRename(row: LogbookRow): void {
        this.renameId = row.id;
        this.renameName = row.name;
        this.renameFrom = row.name;
    }

    cancelRename(): void {
        this.renameId = 0;
        this.renameName = '';
        this.renameFrom = '';
    }

    /** The leave guard's confirmed discard: clear the Add form, cancel the rename. */
    reset(): void {
        this.clearAdd();
        this.cancelRename();
    }

    /** Why Delete is unavailable for a row, or null when it may be offered. The
     *  daemon still decides: its refusal is shown if the state changed. */
    deleteBlock(row: LogbookRow): string | null {
        if (row.id === this.defaultId) return 'The Default logbook can’t be deleted';
        if (row.count === null) return 'Its QSO count could not be read';
        if (row.count > 0) return `Holds ${qsos(row.count)} — only an empty logbook can be deleted`;
        return null;
    }

    /** Create a logbook; true when it exists (the caller clears its form). */
    async create(input: { name: string; callsign: string; smcloud: boolean }): Promise<boolean> {
        if (!this.#mayWrite()) return false;
        this.busy = true;
        try {
            const out = await createLogbook({ name: input.name, callsign: input.callsign });
            if (out.kind === 'error') {
                this.#reportFailure(out, 'Check the Logbooks list before adding it again.');
                return false;
            }
            if (input.smcloud) await this.#enableSmCloud(out.id, input.name);
            else toasts.info(`Logbook “${input.name}” added.`);
            this.clearAdd();
            return true;
        } finally {
            await this.#afterWrite();
        }
    }

    async #enableSmCloud(id: number, name: string): Promise<void> {
        const out = await saveArchiveBindings(this.#archiveId, {
            destinations: [{ type: 'smcloud', logbooks: [{ logbook_id: id, enabled: true }] }],
        });
        if (out.kind === 'ok') {
            toasts.info(`Logbook “${name}” added. Its SM Cloud uploads start after a restart.`);
        } else if (out.timedOut === true) {
            toasts.warn(
                `Logbook “${name}” was added. ${OUTCOME_UNKNOWN_LEAD} Check SM Cloud on the Forwarding tab.`
            );
        } else {
            toasts.warn(
                `Logbook “${name}” was added, but SM Cloud was not turned on: ${out.message}`
            );
        }
    }

    /** Rename a logbook; true when renamed. */
    async rename(id: number, name: string): Promise<boolean> {
        if (!this.#mayWrite()) return false;
        this.busy = true;
        try {
            const out = await renameLogbook(id, name);
            if (out.kind === 'error') {
                this.#reportFailure(out, 'Check the Logbooks list before renaming it again.');
                return false;
            }
            // The header names the Default logbook; keep it in step.
            if (id === this.defaultId) setStationInfo({ logbookName: name });
            toasts.info(`Logbook renamed “${name}”.`);
            this.cancelRename();
            return true;
        } finally {
            await this.#afterWrite();
        }
    }

    /** Delete a logbook (the caller has asked first). */
    async remove(id: number): Promise<void> {
        if (!this.#mayWrite()) return;
        const name = this.rows.find((r) => r.id === id)?.name ?? 'The logbook';
        this.busy = true;
        try {
            const out = await deleteLogbook(id);
            if (out.kind === 'error') {
                this.#reportFailure(out, 'Check the Logbooks list before deleting it again.');
                return;
            }
            toasts.info(`Logbook “${name}” deleted.`);
        } finally {
            await this.#afterWrite();
        }
    }

    #mayWrite(): boolean {
        if (this.busy) return false;
        const gate = archiveSwitchGate();
        if (gate !== null) {
            toasts.error(gate);
            return false;
        }
        return true;
    }

    #reportFailure(out: Extract<LogbookWriteOutcome, { kind: 'error' }>, check: string): void {
        if (out.timedOut === true) toasts.warn(`${OUTCOME_UNKNOWN_LEAD} ${check}`);
        else toasts.error(out.message);
    }

    // Every write re-reads the list, whatever its outcome, and re-reads the
    // Forwarding rows unless they hold unsaved edits (those are never
    // overwritten; their save merges by row).
    async #afterWrite(): Promise<void> {
        this.busy = false;
        await this.load();
        if (bindingsState.loaded && !bindingsState.dirty && !bindingsState.saving) {
            void bindingsState.load();
        }
    }
}

export const logbooksState = new LogbooksState();

export function _resetLogbooksForTests(): void {
    logbooksState.rows = [];
    logbooksState.defaultId = 0;
    logbooksState.stationCallsign = '';
    logbooksState.archiveLabel = '';
    logbooksState.smcloudAvailable = false;
    logbooksState.loaded = false;
    logbooksState.loading = false;
    logbooksState.error = '';
    logbooksState.busy = false;
    logbooksState.addName = '';
    logbooksState.addCallsign = '';
    logbooksState.addCallsignTouched = false;
    logbooksState.addSmcloud = false;
    logbooksState.cancelRename();
}
