/*
    Destination bindings of the ACTIVE archive (ADR 0082 part 9, W-0021 5E) —
    the Forwarding tab's "Destinations for this archive".

    The daemon owns the rows (GET/PUT /v1/qso-archives/{uuid}/bindings); this
    store holds one DRAFT per (destination, logbook) row: its switch, the
    per-logbook fields the operator typed, and the stored fields marked for
    removal. Values the daemon holds are never on the wire — a row only says
    which keys are set — so a blank field keeps the stored value.

    Rules, each with a test (bindings.svelte.test.ts):
      - only changed rows and non-blank typed values ride a save;
      - a destination the daemon says cannot be turned on here refuses the
        switch (turning off always works);
      - a row turned on without a required per-logbook field is refused BEFORE
        the wire: the field is marked, that row's switch goes back to what the
        daemon holds, sibling rows keep their drafts;
      - a daemon refusal restores every switch (nothing was saved) and keeps
        typed values, so the switch never claims more than the daemon saved;
      - a timed-out save is re-read and reported outcome-unknown (ADR 0078),
        never "saved";
      - a stored field can be removed only from a row that ends off.
*/
import {
    fetchArchiveBindings,
    saveArchiveBindings,
    type ArchiveBindings,
    type BindingState,
    type BindingsRequest,
    type DestinationBinding,
    type LogbookBinding,
    type LogbookBindingEdit,
} from '../api/archive-bindings';
import { fetchForwarderTypes, type CredentialField, type ForwarderType } from '../api/forwarders';
import { fetchDaemonIdentity } from '../api/qso-archives';
import { OUTCOME_UNKNOWN_LEAD } from '../api/_helpers';
import { toasts } from '../ui/toasts.svelte';

/** One row as the form holds it. */
export interface RowDraft {
    enabled: boolean;
    /** What the operator typed. Blank = not retyped (keeps the stored value). */
    credentials: Record<string, string>;
    /** Stored keys marked for removal (only on a row that ends off). */
    cleared: string[];
}

export function rowKey(type: string, logbookId: number): string {
    return `${type}/${logbookId}`;
}

function draftFrom(row: LogbookBinding): RowDraft {
    return { enabled: row.enabled, credentials: {}, cleared: [] };
}

function typedValues(d: RowDraft): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(d.credentials)) {
        if (v.trim() !== '') out[k] = v;
    }
    return out;
}

/** The removal marks that still mean something against the daemon's row: a
 *  stored key, on a row that is off. Anything else would send a removal the
 *  daemon refuses (ON row) or one with nothing to remove. */
function keptRemovals(row: LogbookBinding, cleared: string[]): string[] {
    return row.enabled ? [] : cleared.filter((k) => row.credentials_set.includes(k));
}

function rowChanged(row: LogbookBinding, d: RowDraft | undefined): boolean {
    if (!d) return false;
    return (
        d.enabled !== row.enabled || Object.keys(typedValues(d)).length > 0 || d.cleared.length > 0
    );
}

/** What a destination is called in front of the operator: the station
 *  account's config.json label, else the built-in name. */
export function destinationLabel(dest: DestinationBinding): string {
    return dest.account.label || dest.display_name;
}

class BindingsState {
    loading = $state(false);
    loaded = $state(false);
    saving = $state(false);
    error = $state('');
    archiveId = $state('');
    view = $state<ArchiveBindings | null>(null);
    types = $state<ForwarderType[]>([]);
    drafts = $state<Record<string, RowDraft>>({});
    /** Required fields a refused save marked, per row key. */
    missing = $state<Record<string, string[]>>({});
    /** The last refusal — the SPA's own, or the daemon's message. */
    refusal = $state('');
    #eligibilityRefreshGeneration = 0;

    dirty = $derived(this.#anyChanged());

    #anyChanged(): boolean {
        const v = this.view;
        if (!v) return false;
        return v.destinations.some((d) =>
            d.logbooks.some((r) => rowChanged(r, this.drafts[rowKey(d.type, r.logbook_id)]))
        );
    }

    #dest(type: string): DestinationBinding | undefined {
        return this.view?.destinations.find((d) => d.type === type);
    }

    #row(key: string): { dest: DestinationBinding; row: LogbookBinding } | undefined {
        for (const dest of this.view?.destinations ?? []) {
            for (const row of dest.logbooks) {
                if (rowKey(dest.type, row.logbook_id) === key) return { dest, row };
            }
        }
        return undefined;
    }

    /** The per-logbook (binding-owned) fields of a destination type. */
    logbookFields(type: string): CredentialField[] {
        return (this.types.find((t) => t.type === type)?.credential_fields ?? []).filter(
            (f) => f.scope === 'logbook'
        );
    }

    rowEdited(type: string, row: LogbookBinding): boolean {
        return rowChanged(row, this.drafts[rowKey(type, row.logbook_id)]);
    }

    destinationEdited(dest: DestinationBinding): boolean {
        return dest.logbooks.some((r) => this.rowEdited(dest.type, r));
    }

    /** The aggregate over the DRAFTS — what the destination's switch shows
     *  while editing. The daemon's own aggregate is `dest.state`. */
    draftState(dest: DestinationBinding): BindingState {
        const n = dest.logbooks.length;
        const on = dest.logbooks.filter(
            (r) => this.drafts[rowKey(dest.type, r.logbook_id)]?.enabled === true
        ).length;
        if (n > 0 && on === n) return 'on';
        return on === 0 ? 'off' : 'mixed';
    }

    #setDraft(dest: DestinationBinding, logbookId: number, on: boolean): void {
        const d = this.drafts[rowKey(dest.type, logbookId)];
        if (!d) return;
        d.enabled = on;
        // A removal is only possible from a row that ends off.
        if (on) d.cleared = [];
    }

    /** Turn every logbook's binding of a destination on or off. Turning on is
     *  refused where the daemon said this destination cannot be turned on. */
    setAll(type: string, on: boolean): void {
        if (this.saving) return;
        const dest = this.#dest(type);
        if (!dest || (on && dest.reason !== '')) return;
        for (const r of dest.logbooks) this.#setDraft(dest, r.logbook_id, on);
    }

    setRow(type: string, logbookId: number, on: boolean): void {
        if (this.saving) return;
        const dest = this.#dest(type);
        if (!dest || (on && dest.reason !== '')) return;
        this.#setDraft(dest, logbookId, on);
    }

    /** Record what the operator typed into a per-logbook field; typing into a
     *  field a refused save marked drops the mark. */
    setField(type: string, logbookId: number, key: string, value: string): void {
        if (this.saving) return;
        const k = rowKey(type, logbookId);
        const d = this.drafts[k];
        if (!d) return;
        d.credentials[key] = value;
        if (value.trim() !== '' && this.missing[k]?.includes(key)) {
            this.missing = { ...this.missing, [k]: this.missing[k].filter((x) => x !== key) };
        }
    }

    /** Mark a STORED per-logbook field for removal — only on a row that ends
     *  off (the daemon refuses a removal from a row that ends on). */
    clear(type: string, logbookId: number, key: string): void {
        if (this.saving) return;
        const k = rowKey(type, logbookId);
        const found = this.#row(k);
        const d = this.drafts[k];
        if (!found || !d || d.enabled) return;
        if (!found.row.credentials_set.includes(key)) return;
        if (!this.logbookFields(type).some((f) => f.key === key)) return;
        delete d.credentials[key];
        if (!d.cleared.includes(key)) d.cleared.push(key);
    }

    uncleared(type: string, logbookId: number, key: string): void {
        if (this.saving) return;
        const d = this.drafts[rowKey(type, logbookId)];
        if (d) d.cleared = d.cleared.filter((x) => x !== key);
    }

    /** Required per-logbook fields missing from rows that END ON, per row key:
     *  set on the daemon (and not being removed) or typed now counts. */
    validate(): Record<string, string[]> {
        const missing: Record<string, string[]> = {};
        for (const dest of this.view?.destinations ?? []) {
            for (const row of dest.logbooks) {
                const k = rowKey(dest.type, row.logbook_id);
                const d = this.drafts[k];
                if (!d || !d.enabled || !rowChanged(row, d)) continue;
                const keys = this.#missingFields(dest.type, row, d);
                if (keys.length > 0) missing[k] = keys;
            }
        }
        return missing;
    }

    /** The required (non-clearable) logbook fields a row ending on would
     *  lack: neither stored (and kept) nor typed. */
    #missingFields(type: string, row: LogbookBinding, d: RowDraft): string[] {
        const typed = typedValues(d);
        return (
            this.logbookFields(type)
                .filter((f) => !f.clearable)
                // The daemon fills it from the logbook's callsign on save.
                .filter(
                    (f) =>
                        !(
                            f.defaults_to === 'logbook_callsign' &&
                            row.logbook_callsign.trim() !== ''
                        )
                )
                .filter((f) => {
                    const stored =
                        row.credentials_set.includes(f.key) && !d.cleared.includes(f.key);
                    return !stored && typed[f.key] === undefined;
                })
                .map((f) => f.key)
        );
    }

    /** The PUT body: changed rows only, grouped by destination. */
    buildRequest(): BindingsRequest {
        const out: BindingsRequest = { destinations: [] };
        for (const dest of this.view?.destinations ?? []) {
            const logbooks: LogbookBindingEdit[] = [];
            for (const row of dest.logbooks) {
                const d = this.drafts[rowKey(dest.type, row.logbook_id)];
                if (!d || !rowChanged(row, d)) continue;
                const edit: LogbookBindingEdit = { logbook_id: row.logbook_id, enabled: d.enabled };
                const typed = typedValues(d);
                if (Object.keys(typed).length > 0) edit.credentials = typed;
                if (d.cleared.length > 0) edit.credentials_clear = [...d.cleared];
                logbooks.push(edit);
            }
            if (logbooks.length > 0) out.destinations.push({ type: dest.type, logbooks });
        }
        return out;
    }

    async load(): Promise<void> {
        if (this.loading) return;
        // A full reload supersedes any narrower eligibility read already in
        // flight; that older snapshot must not patch the newly loaded view.
        this.#eligibilityRefreshGeneration++;
        this.loading = true;
        // Invalidate first: a pending reload's list is not known-current.
        this.loaded = false;
        this.error = '';
        try {
            const identity = await fetchDaemonIdentity();
            if (!identity || identity.archiveId === '') {
                this.error = 'Station Manager did not say which archive it is serving.';
                return;
            }
            const [types, view] = await Promise.all([
                fetchForwarderTypes(),
                fetchArchiveBindings(identity.archiveId),
            ]);
            if (view.kind === 'error') {
                this.error = view.message;
                return;
            }
            this.archiveId = identity.archiveId;
            this.types = types.kind === 'ok' ? types.types : [];
            this.#apply(view.bindings);
            this.loaded = true;
        } finally {
            this.loading = false;
        }
    }

    #apply(v: ArchiveBindings): void {
        const drafts: Record<string, RowDraft> = {};
        for (const d of v.destinations) {
            for (const r of d.logbooks) drafts[rowKey(d.type, r.logbook_id)] = draftFrom(r);
        }
        this.view = v;
        this.drafts = drafts;
        this.missing = {};
    }

    /** Re-read only the live queue counts. A queue request can finish after a
     *  binding save, so it must not replace that save's newer baseline. */
    async refresh(): Promise<boolean> {
        if (this.archiveId === '') return false;
        const out = await fetchArchiveBindings(this.archiveId);
        if (out.kind !== 'ok') return false;
        const current = this.view;
        if (!current) return false;
        const fresh = new Map(out.bindings.destinations.map((d) => [d.type, d]));
        this.view = {
            ...current,
            destinations: current.destinations.map((dest) => {
                const rows = fresh.get(dest.type)?.logbooks ?? [];
                return {
                    ...dest,
                    logbooks: dest.logbooks.map((row) => {
                        const next = rows.find(
                            (candidate) =>
                                candidate.logbook_id === row.logbook_id &&
                                candidate.forwarder_name === row.forwarder_name
                        );
                        return next ? { ...row, queue: next.queue } : row;
                    }),
                };
            }),
        };
        return true;
    }

    /** Re-read only station-account eligibility after config.json changes.
     *  Binding rows, their drafts, queue counts and restart state belong to a
     *  different save boundary and remain untouched. */
    async refreshEligibility(): Promise<boolean> {
        if (this.archiveId === '' || !this.view) return false;
        const generation = ++this.#eligibilityRefreshGeneration;
        const out = await fetchArchiveBindings(this.archiveId);
        // A later account save (or a full load) owns a newer read. Treat this
        // response as superseded: it neither applies stale data nor reports a
        // stale failure after the newer read has already settled the view.
        if (generation !== this.#eligibilityRefreshGeneration) return true;
        if (out.kind !== 'ok') return false;
        const current = this.view;
        if (!current) return false;
        const fresh = new Map(out.bindings.destinations.map((d) => [d.type, d]));
        this.view = {
            ...current,
            destinations: current.destinations.map((dest) => {
                const next = fresh.get(dest.type);
                return next ? { ...dest, account: next.account, reason: next.reason } : dest;
            }),
        };
        return true;
    }

    #missingMessage(missing: Record<string, string[]>): string {
        const parts: string[] = [];
        for (const [k, keys] of Object.entries(missing)) {
            const found = this.#row(k);
            if (!found) continue;
            const labels = keys.map(
                (key) =>
                    this.logbookFields(found.dest.type).find((f) => f.key === key)?.label ?? key
            );
            parts.push(
                `${destinationLabel(found.dest)} for ${found.row.logbook_name}: ${labels.join(', ')} ${labels.length === 1 ? 'is' : 'are'} required to turn it on`
            );
        }
        return `Not saved. ${parts.join('; ')}.`;
    }

    /** Every switch back to what the daemon holds; typed values stay. A row
     *  restored to ON loses its removal marks, which need a row that ends off. */
    #restoreSwitches(): void {
        for (const dest of this.view?.destinations ?? []) {
            for (const row of dest.logbooks) {
                const d = this.drafts[rowKey(dest.type, row.logbook_id)];
                if (!d) continue;
                d.enabled = row.enabled;
                d.cleared = keptRemovals(row, d.cleared);
            }
        }
    }

    async save(): Promise<void> {
        if (this.saving || !this.loaded || !this.dirty || !this.view) return;
        const missing = this.validate();
        if (Object.keys(missing).length > 0) {
            for (const k of Object.keys(missing)) {
                const found = this.#row(k);
                const d = this.drafts[k];
                if (found && d) d.enabled = found.row.enabled;
            }
            this.missing = missing;
            this.refusal = this.#missingMessage(missing);
            return;
        }
        this.missing = {};
        this.refusal = '';
        this.saving = true;
        try {
            const res = await saveArchiveBindings(this.archiveId, this.buildRequest());
            if (res.kind === 'ok') {
                this.#apply(res.bindings);
                toasts.info(
                    res.bindings.restart_required
                        ? 'Bindings saved. They apply when the daemon restarts.'
                        : 'Bindings saved.'
                );
                return;
            }
            if (res.timedOut) {
                await this.#reconcileAfterTimeout();
                return;
            }
            this.refusal = `Not saved. ${res.message}`;
            this.#restoreSwitches();
        } finally {
            this.saving = false;
        }
    }

    /** A save whose response never came may have committed (ADR 0078): re-read,
     *  show the daemon's switches, keep what the operator typed. */
    async #reconcileAfterTimeout(): Promise<void> {
        const out = await fetchArchiveBindings(this.archiveId);
        if (out.kind !== 'ok') {
            toasts.warn(
                `${OUTCOME_UNKNOWN_LEAD} The bindings could not be re-read either; reload this tab before saving again.`
            );
            return;
        }
        const keep = this.drafts;
        const drafts: Record<string, RowDraft> = {};
        for (const d of out.bindings.destinations) {
            for (const r of d.logbooks) {
                const k = rowKey(d.type, r.logbook_id);
                drafts[k] = {
                    enabled: r.enabled,
                    credentials: { ...(keep[k]?.credentials ?? {}) },
                    cleared: keptRemovals(r, keep[k]?.cleared ?? []),
                };
            }
        }
        this.view = out.bindings;
        this.drafts = drafts;
        toasts.warn(
            `${OUTCOME_UNKNOWN_LEAD} The switches now show what Station Manager holds; what you typed is kept — check it and save again if needed.`
        );
    }

    /** Discard every edit (Cancel), back to the daemon's rows. */
    reset(): void {
        if (this.saving) return;
        this.refusal = '';
        if (this.view) this.#apply(this.view);
        else {
            this.drafts = {};
            this.missing = {};
        }
    }
}

export const bindingsState = new BindingsState();

/** Test hook: forget everything, as a fresh module would. */
export function _resetBindingsForTests(): void {
    bindingsState.view = null;
    bindingsState.loaded = false;
    bindingsState.loading = false;
    bindingsState.saving = false;
    bindingsState.error = '';
    bindingsState.archiveId = '';
    bindingsState.types = [];
    bindingsState.reset();
}
