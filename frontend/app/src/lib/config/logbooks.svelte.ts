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
    updateLogbook,
    type LogbookWriteOutcome,
} from '../api/logbooks';
import { fetchStationContext } from '../api/seams';
import { fetchDaemonIdentity } from '../api/qso-archives';
import {
    fetchArchiveBindings,
    saveArchiveBindings,
    type DestinationBinding,
} from '../api/archive-bindings';
import { OUTCOME_UNKNOWN_LEAD } from '../api/_helpers';
import { archiveSwitchGate, archivesState } from './archives.svelte';
import { bindingsState } from './bindings.svelte';
import { setStationInfo } from '../operate/station.svelte';
import { toasts } from '../ui/toasts.svelte';

export interface LogbookRow {
    id: number;
    name: string;
    callsign: string;
    /** Its description; '' when it has none. */
    description: string;
    /** QSOs held; null when the count could not be read. */
    count: number | null;
}

/** What the Add form submits; the description is optional. */
export interface AddInput {
    name: string;
    callsign: string;
    smcloud: boolean;
    description?: string;
}

const qsos = (n: number): string => `${n.toLocaleString()} ${n === 1 ? 'QSO' : 'QSOs'}`;

class LogbooksState {
    rows = $state<LogbookRow[]>([]);
    /** The Default logbook — where live contacts go. 0 when unknown. */
    defaultId = $state(0);
    stationCallsign = $state('');
    archiveLabel = $state('');
    /** SM Cloud can be turned on for a logbook created now in this archive. */
    smcloudAvailable = $state(false);
    /** Why it cannot ('' when it can, or when the bindings are unread). */
    smcloudRefusal = $state('');
    /** SM Cloud's server support is still being checked (W-0021 5F.4): the
     *  answer is re-read (refreshSmcloud) until it settles. */
    smcloudChecking = $state(false);
    loaded = $state(false);
    loading = $state(false);
    error = $state('');
    /** A create, edit or delete is in flight. */
    busy = $state(false);

    // The drafts live here, not in the component, so the Settings leave guard
    // (unsaved.ts) can see and discard them (operator ruling 2026-09-29).
    addName = $state('');
    addCallsign = $state('');
    /** The operator typed a callsign; a later load no longer prefills it. */
    addCallsignTouched = $state(false);
    addSmcloud = $state(false);
    addDescription = $state('');
    /** The logbook being edited, 0 when none; editFromName / editFromDescription
     *  are its saved values, which the draft is measured against. */
    editId = $state(0);
    editName = $state('');
    editDescription = $state('');
    editFromName = $state('');
    editFromDescription = $state('');
    #archiveId = '';
    // Each read takes a generation; only the newest may apply, so an older read
    // that answers last (a tab opening while a write's re-read runs) never
    // replaces a newer list.
    #generation = 0;
    // One SM Cloud re-read on the wire at a time; a load (a new generation)
    // supersedes its answer.
    #smcloudInFlight = false;

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
                    description: l.description,
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
        this.smcloudRefusal = '';
        this.smcloudChecking = false;
        this.archiveLabel =
            archivesState.list.find((a) => a.id === this.#archiveId)?.label ?? this.archiveLabel;
        if (this.#archiveId === '') return;
        const out = await fetchArchiveBindings(this.#archiveId);
        if (generation !== this.#generation || out.kind !== 'ok') return;
        if (out.bindings.archive_label !== '') this.archiveLabel = out.bindings.archive_label;
        this.#takeSmcloud(out.bindings.destinations.find((d) => d.type === 'smcloud'));
    }

    #takeSmcloud(sm: DestinationBinding | undefined): void {
        // A new logbook is never the default one, so it takes the daemon's
        // new-logbook answer, not the destination's (W-0021 5F.0).
        this.smcloudRefusal = sm?.new_logbook_reason || sm?.reason || '';
        this.smcloudAvailable = sm !== undefined && this.smcloudRefusal === '';
        this.smcloudChecking = sm?.support?.state === 'checking';
    }

    /** Re-read only SM Cloud's answer for a new logbook while its server
     *  support is still being checked (W-0021 5F.4); the list and the drafts
     *  are untouched. One on the wire at a time, never during a load; an
     *  answer superseded by a later load is dropped. False when nothing was
     *  applied. */
    async refreshSmcloud(): Promise<boolean> {
        // Not during a load: it shares the load's generation, so its late
        // answer would overwrite the load's newer one (operator review).
        if (this.loading || this.#smcloudInFlight || this.#archiveId === '') return false;
        const archiveId = this.#archiveId;
        const generation = this.#generation;
        this.#smcloudInFlight = true;
        try {
            const out = await fetchArchiveBindings(archiveId);
            if (
                out.kind !== 'ok' ||
                generation !== this.#generation ||
                archiveId !== this.#archiveId
            ) {
                return false;
            }
            this.#takeSmcloud(out.bindings.destinations.find((d) => d.type === 'smcloud'));
            return true;
        } finally {
            this.#smcloudInFlight = false;
        }
    }

    /** Unsaved drafts: a typed name or description, a callsign other than the
     *  prefill, a ticked SM Cloud, or a changed edit. The prefilled form, a
     *  merely opened Edit, and one typed back to its saved values are not
     *  edits; values compare trimmed, as the daemon stores them. */
    get dirty(): boolean {
        const callsignChanged =
            this.addCallsignTouched &&
            this.addCallsign.trim().toUpperCase() !== this.stationCallsign.trim().toUpperCase();
        const addDirty =
            this.addName.trim() !== '' ||
            this.addDescription.trim() !== '' ||
            callsignChanged ||
            this.addSmcloud;
        return addDirty || this.editChanged;
    }

    /** The open edit differs from its saved values. */
    get editChanged(): boolean {
        return this.editId !== 0 && Object.keys(this.#editChanges()).length > 0;
    }

    /** A create, edit or delete is on the wire: its outcome cannot be recalled. */
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

    #addDraftIs(input: AddInput): boolean {
        return (
            this.addName.trim() === input.name &&
            this.addDescription.trim() === (input.description ?? '').trim() &&
            this.addCallsign.trim().toUpperCase() === input.callsign.toUpperCase() &&
            // As the form submits it: a tick counts only while SM Cloud is offered.
            (this.addSmcloud && this.smcloudAvailable) === input.smcloud
        );
    }

    clearAdd(): void {
        this.addName = '';
        this.addDescription = '';
        this.addSmcloud = false;
        this.addCallsignTouched = false;
        this.addCallsign = this.stationCallsign;
    }

    startEdit(row: LogbookRow): void {
        this.editId = row.id;
        this.editName = row.name;
        this.editFromName = row.name;
        this.editDescription = row.description;
        this.editFromDescription = row.description;
    }

    /** The open edit's fields that differ from their saved values, trimmed as
     *  the daemon stores them; a cleared description is an explicit ''. */
    #editChanges(): { name?: string; description?: string } {
        const changes: { name?: string; description?: string } = {};
        const name = this.editName.trim();
        const description = this.editDescription.trim();
        if (name !== this.editFromName) changes.name = name;
        if (description !== this.editFromDescription) changes.description = description;
        return changes;
    }

    cancelEdit(): void {
        this.editId = 0;
        this.editName = '';
        this.editDescription = '';
        this.editFromName = '';
        this.editFromDescription = '';
    }

    /** The leave guard's confirmed discard: clear the Add form, cancel the edit. */
    reset(): void {
        this.clearAdd();
        this.cancelEdit();
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
    async create(input: AddInput): Promise<boolean> {
        if (!this.#mayWrite()) return false;
        // Refused whole, before the wire: creating the logbook and then failing
        // to bind it would leave a logbook the operator did not ask for alone.
        if (input.smcloud && !this.smcloudAvailable) {
            const why = this.smcloudRefusal || 'this archive cannot turn it on';
            toasts.error(`SM Cloud can’t be turned on for a new logbook: ${why}.`);
            return false;
        }
        this.busy = true;
        try {
            // A blank description is not sent: the daemon stores it empty.
            const description = (input.description ?? '').trim();
            const out = await createLogbook({
                name: input.name,
                callsign: input.callsign,
                ...(description !== '' ? { description } : {}),
            });
            if (out.kind === 'error') {
                this.#reportFailure(out, 'Check the Logbooks list before adding it again.');
                return false;
            }
            if (input.smcloud) await this.#enableSmCloud(out.id, input.name);
            else toasts.info(`Logbook “${input.name}” added.`);
            // Only the draft it submitted: one typed while the request was on
            // the wire stays, and stays guarded (review 412cca37 P2).
            if (this.#addDraftIs(input)) this.clearAdd();
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

    /** Save the open edit; true when saved. Only the changed fields are sent;
     *  nothing is sent when nothing changed or the name is blank. */
    async saveEdit(): Promise<boolean> {
        const id = this.editId;
        const changes = this.#editChanges();
        if (id === 0 || changes.name === '' || Object.keys(changes).length === 0) return false;
        if (!this.#mayWrite()) return false;
        const savedName = changes.name ?? this.editFromName;
        this.busy = true;
        try {
            const out = await updateLogbook(id, changes);
            if (out.kind === 'error') {
                this.#reportFailure(out, 'Check the Logbooks list before saving it again.');
                return false;
            }
            // The header names the Default logbook; keep it in step — only
            // with a name this save sent, never the draft's unsent one.
            if (id === this.defaultId && changes.name !== undefined) {
                setStationInfo({ logbookName: changes.name });
            }
            toasts.info(`Logbook “${savedName}” saved.`);
            if (this.editId === id) {
                // A draft typed while saving stays open, measured against the
                // values just saved (not the ones they replaced).
                if (changes.name !== undefined) this.editFromName = changes.name;
                if (changes.description !== undefined) {
                    this.editFromDescription = changes.description;
                }
                if (!this.editChanged) this.cancelEdit();
            }
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

    // Every write re-reads the list, whatever its outcome, and asks Forwarding
    // to re-read its rows (now, or once its unsaved edits are discarded).
    async #afterWrite(): Promise<void> {
        this.busy = false;
        await this.load();
        bindingsState.requestReload();
    }
}

export const logbooksState = new LogbooksState();

export function _resetLogbooksForTests(): void {
    logbooksState.rows = [];
    logbooksState.defaultId = 0;
    logbooksState.stationCallsign = '';
    logbooksState.archiveLabel = '';
    logbooksState.smcloudAvailable = false;
    logbooksState.smcloudRefusal = '';
    logbooksState.smcloudChecking = false;
    logbooksState.loaded = false;
    logbooksState.loading = false;
    logbooksState.error = '';
    logbooksState.busy = false;
    logbooksState.addName = '';
    logbooksState.addCallsign = '';
    logbooksState.addCallsignTouched = false;
    logbooksState.addSmcloud = false;
    logbooksState.addDescription = '';
    logbooksState.cancelEdit();
}
