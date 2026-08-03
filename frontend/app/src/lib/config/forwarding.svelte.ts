/*
    Forwarding settings state (app Settings view, ADR 0044) — the port of the
    standalone config SPA's Forwarding tab.

    Per ADR 0039 the forwarder list is NON-SPARSE: the daemon seeds an entry for
    every supported destination and re-adds any missing one at load, so this is a
    FIXED list. There is deliberately no add/remove — a destination is turned off
    by disabling it.
*/
import {
    fetchForwarders,
    fetchForwarderTypes,
    saveForwarders,
    type ForwarderEntry,
    type ForwarderPayload,
    type ForwarderType,
} from '../api/forwarders';
import { toasts } from '../ui/toasts.svelte';

/** One destination as the form holds it: the masked entry plus local edits. */
export interface ForwarderDraft {
    name: string;
    type: string;
    /** Operator's config.json label; '' means fall back to the built-in name. */
    label: string;
    enabled: boolean;
    action_filter?: string[];
    /** Keys the daemon reports as holding a value. Read-only. */
    credentialsSet: string[];
    /** What the operator typed. Blank = not retyped. */
    credentials: Record<string, string>;
    /** Keys the operator explicitly reset (clearable fields only). */
    cleared: string[];
}

class ForwardingState {
    loading = $state(false);
    saving = $state(false);
    loaded = $state(false);
    error = $state('');
    types = $state<ForwarderType[]>([]);
    drafts = $state<ForwarderDraft[]>([]);

    // TWO snapshots, deliberately. #pristine is the dirty-compare projection
    // (name/enabled/credentials/cleared); #pristineEntries is the full daemon
    // shape Cancel restores from.
    //
    // They were one field until F9 caught it: reset() parsed #pristine as
    // ForwarderEntry[], but that projection carries no `type` and no
    // `credentials_set`. Cancel therefore rebuilt every draft with type
    // undefined — after which typeFor() found nothing, every destination
    // rendered as "unsupported", and every field became non-clearable. Silent,
    // and only reachable by pressing Cancel.
    #pristine = $state('[]');
    #pristineEntries = $state('[]');

    dirty = $derived(JSON.stringify(this.#comparable()) !== this.#pristine);

    typeFor(type: string): ForwarderType | undefined {
        return this.types.find((t) => t.type === type);
    }

    /**
     * True when THIS destination has unsaved edits — a toggled enable, a typed
     * credential, or a pending reset.
     *
     * Exists because the section collapses each destination into a disclosure:
     * the footer can say "unsaved changes" but not where, so a collapsed card
     * has to be able to mark itself. A blank credential box does NOT count —
     * the component's inputs write '' for every rendered field, so counting key
     * presence would mark a card the moment it was merely opened.
     */
    hasEdits(name: string): boolean {
        const d = this.drafts.find((x) => x.name === name);
        if (!d) return false;
        const base = (JSON.parse(this.#pristine) as { name: string; enabled: boolean }[]).find(
            (p) => p.name === name
        );
        if (!base) return true; // unknown to the snapshot — treat as changed
        return (
            d.enabled !== base.enabled ||
            d.cleared.length > 0 ||
            Object.values(d.credentials).some((v) => v.trim() !== '')
        );
    }

    /** True when this field may be reset to its default (declared in Go). */
    clearable(type: string, key: string): boolean {
        return this.typeFor(type)?.credential_fields.find((f) => f.key === key)?.clearable === true;
    }

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        // Invalidate before awaiting: while a reload is pending the retained
        // list is not known-current and must neither render nor save
        // (clean-room review 2c64c7aa P1).
        this.loaded = false;
        this.error = '';
        const [cfg, types] = await Promise.all([fetchForwarders(), fetchForwarderTypes()]);
        this.loading = false;
        if (cfg.kind === 'error') {
            this.error = cfg.message;
            // Unloaded, not merely errored — see the note on emailState.load.
            // Settings unmounts on navigation while this module survives, so a
            // failed remount reload would leave stale destinations on screen
            // looking current, and the forwarders block is replaced WHOLE.
            return;
        }
        // A failed type fetch is NOT fatal: the destinations still render with
        // their enable toggles, they just cannot have credentials edited.
        this.types = types.kind === 'ok' ? types.types : [];
        this.#apply(cfg.forwarders);
        this.loaded = true;
    }

    async save(): Promise<void> {
        // Whole-list writes require a successfully loaded baseline. The
        // component hides Save while unloaded; this is the last line before
        // the wire if another caller or a rendering race invokes it anyway.
        if (this.saving || !this.loaded || !this.dirty) return;
        this.saving = true;
        try {
            const res = await saveForwarders(this.buildPayload());
            if (res.kind === 'error') {
                toasts.error(`Save failed: ${res.message}`);
                return;
            }
            this.#apply(res.forwarders);
            toasts.info('Forwarding settings saved. Restart the daemon to apply.');
        } finally {
            this.saving = false;
        }
    }

    /**
     * Mark a clearable field for reset to its constructor default.
     *
     * REFUSED for anything not declared Clearable in Go, rather than merely
     * hidden in the UI. Emptying a required credential is not a reset — the
     * forwarder's New() rejects it, aborting spawnForwarderWorkers at the next
     * restart, with the PUT long since returned 200. The state module is the
     * last line: a component bug must not be able to reach that.
     *
     * Also fails closed when the type descriptors could not be fetched, since
     * `clearable` is unknowable then.
     */
    clear(name: string, key: string): void {
        const d = this.drafts.find((x) => x.name === name);
        if (!d || !this.clearable(d.type, key)) return;
        // Drop any half-typed value: reset and "set it to this" are different
        // intentions and the last one expressed wins.
        delete d.credentials[key];
        if (!d.cleared.includes(key)) d.cleared.push(key);
    }

    /** Undo a pending reset. */
    uncleared(name: string, key: string): void {
        const d = this.drafts.find((x) => x.name === name);
        if (!d) return;
        d.cleared = d.cleared.filter((k) => k !== key);
    }

    reset(): void {
        this.#apply(JSON.parse(this.#pristineEntries) as ForwarderEntry[]);
    }

    /**
     * Build the PUT payload. THREE blank states exist and only one may send "":
     *
     *   - never touched      → key omitted  → daemon keeps the stored value
     *   - typed then erased  → key omitted  → backing out of an edit is not a
     *                                          reset (F8)
     *   - explicitly reset   → key sent ""  → daemon applies the default
     *
     * Omitting blanks is what makes the whole surface safe, because GET never
     * echoes a value: a blank box overwhelmingly means "not retyped". The
     * explicit-reset list is the only thing that can express the third state.
     *
     * The `clearable` re-check here is DELIBERATELY redundant with clear()'s,
     * and the two have different jobs: clear() refuses to record an impossible
     * intent (F5c), this one refuses to put it on the wire (F5b). Each has its
     * own rule because removing either alone left the suite green — redundancy
     * with a single proof is redundancy nobody is testing. An earlier draft of
     * this comment claimed the check also covered "a reset marked before the
     * type descriptors arrived"; that is FALSE — clear() fails closed when
     * types are missing, so no such reset can be recorded. The honest reason to
     * keep it is that this is the last line before the daemon.
     *
     * The whole list rides every time: the daemon replaces the forwarders block
     * WHOLE, so a destination missing from the payload is a destination removed
     * from config until the next restart re-seeds it (F6).
     */
    buildPayload(): ForwarderPayload[] {
        return this.drafts.map((d) => {
            const creds: Record<string, string> = {};
            for (const [k, v] of Object.entries(d.credentials)) {
                if (v.trim() !== '') creds[k] = v;
            }
            for (const k of d.cleared) {
                if (this.clearable(d.type, k)) creds[k] = '';
            }
            const out: ForwarderPayload = {
                name: d.name,
                type: d.type,
                enabled: d.enabled,
                action_filter: d.action_filter,
            };
            if (Object.keys(creds).length > 0) out.credentials = creds;
            return out;
        });
    }

    #comparable(): unknown {
        return this.drafts.map((d) => ({
            name: d.name,
            enabled: d.enabled,
            credentials: d.credentials,
            cleared: d.cleared,
        }));
    }

    #apply(entries: ForwarderEntry[]): void {
        this.drafts = entries.map((e) => ({
            name: e.name,
            type: e.type,
            label: e.label ?? '',
            enabled: e.enabled,
            action_filter: e.action_filter,
            credentialsSet: e.credentials_set ?? [],
            credentials: {},
            cleared: [],
        }));
        this.#pristine = JSON.stringify(this.#comparable());
        this.#pristineEntries = JSON.stringify(entries);
    }
}

export const forwardingState = new ForwardingState();
