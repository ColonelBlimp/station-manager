/*
    Rigs settings state (app Settings → Rigs section, ADR 0044) — the daemon's
    configured rig list + rigdef catalogue (GET /v1/rigs), the discovered
    hardware for the connection pickers (GET /v1/hardware), and PER-RIG editable
    connection drafts. Daemon is the source of truth (ADR 0003); no local cache.

    DATA SAFETY (review 2026-07-20 Rigs-editor): a rig save WHOLE-REPLACES the
    catalogue daemon-side, so save RE-FETCHES the fresh catalogue, applies only
    the FIELDS the operator actually changed — port, audio RX, and audio TX diffed
    INDEPENDENTLY against the draft's baseline, plus the per-rig mode_mappings and
    serial overrides, each diffed as one whole object — onto the fresh objects, and
    PUTs that WITHOUT default_rig_id. So a concurrent change to another rig, this rig's other
    fields, the OTHER audio direction, or the active-rig selection is never
    clobbered by a stale snapshot. The port/audio
    pickers are disabled while a save is in flight, so a mid-save edit can't be
    silently dropped by the post-save re-baseline. Each draft carries an IMMUTABLE
    baseline (the snapshot it was cloned from); dirty + the save diff compare
    against that, and #applyFetched re-baselines PRISTINE retained drafts onto
    fresh data — so a rig that changed concurrently shows the new values and
    doesn't read falsely dirty, while a dirty draft's unsaved edits survive.
    Drafts are full-fidelity JSON clones (all fields, incl. the un-rendered
    mode_mappings/overrides), kept PER RIG so switching rigs doesn't discard edits.

    ACCEPTED LIMITATION — the re-fetch→PUT is not atomic (review #2): a second
    client writing rig config in the millisecond window between our GET and PUT
    is last-writer-wins. SM is a single-operator daemon (the realistic case is
    two browser tabs, rare and self-inflicted), and closing this fully needs
    SERVER-SIDE optimistic concurrency (a rigs revision / precondition) —
    disproportionate for a local config editor. Revisit if multi-client rig
    editing ever becomes real (e.g. a hosted config surface). The active-rig badge
    can also show the pre-PUT default until the next load (#5) — cosmetic, no data
    loss, since the PUT omits default_rig_id.
*/
import { fetchRigs, saveRigs, setDefaultRig, type RigConfig, type RigDef } from '../api/rigs';
import { noteConfigDurability } from './durability';
import { fetchHardware, type SerialPort, type AudioDevice } from '../api/hardware';
import { bridgeEnabledState } from './bridgeEnabled.svelte';
import { toasts } from '../ui/toasts.svelte';

// A full-fidelity clone (rigs are pure JSON, so a JSON round-trip is lossless
// AND environment-independent). Does NOT inject an empty `audio` — the daemon
// omits zero-valued audio, so injecting `{}` would make a no-audio rig compare
// unequal to its server form and read dirty on load (review Rigs-editor #2);
// the pickers + setDraftAudio handle an absent audio object.
function cloneRig(rig: RigConfig): RigConfig {
    return JSON.parse(JSON.stringify(rig)) as RigConfig;
}

// Build a normalised audio block from a draft, omitting empty rx/tx to match the
// daemon's omitempty shape (so a saved rig round-trips back not-dirty).
function normalizedAudio(audio: RigConfig['audio']): RigConfig['audio'] | undefined {
    const out: { rx?: string; tx?: string } = {};
    if (audio?.rx) out.rx = audio.rx;
    if (audio?.tx) out.tx = audio.tx;
    return out.rx || out.tx ? out : undefined;
}

// The form a new rig is saved in: the draft under its assigned id, with empty
// audio and empty override maps dropped so it matches the daemon's omitempty
// shape and reloads not-dirty.
function savedFormOf(draft: RigConfig, id: number): RigConfig {
    const out = cloneRig({ ...draft, id });
    const audio = normalizedAudio(out.audio);
    if (audio) out.audio = audio;
    else delete out.audio;
    if (out.mode_mappings && Object.keys(out.mode_mappings).length === 0) delete out.mode_mappings;
    if (out.overrides && Object.keys(out.overrides).length === 0) delete out.overrides;
    return out;
}

// The saved rig as the server now has it, plus only the fields the operator
// changed after `submitted` was sent. Used when a timed-out add turns out to have
// landed: a field untouched since the submit keeps the server's value, so a change
// made meanwhile (another tab) is shown and never written back over (clean-room
// review 91a3939d P2). Field granularity matches save(): audio RX and TX
// independently, every other field whole — save() then diffs against the server rig.
function withEditsSince(server: RigConfig, submitted: RigConfig, current: RigConfig): RigConfig {
    const out = cloneRig(server) as unknown as Record<string, unknown>;
    const sub = submitted as unknown as Record<string, unknown>;
    const cur = current as unknown as Record<string, unknown>;
    for (const k of new Set([...Object.keys(sub), ...Object.keys(cur)])) {
        if (k === 'id' || k === 'audio' || JSON.stringify(sub[k]) === JSON.stringify(cur[k])) {
            continue;
        }
        if (cur[k] === undefined) delete out[k];
        else out[k] = JSON.parse(JSON.stringify(cur[k])) as unknown;
    }
    const audio = { rx: server.audio?.rx, tx: server.audio?.tx };
    for (const which of ['rx', 'tx'] as const) {
        const now = current.audio?.[which] ?? '';
        if (now !== (submitted.audio?.[which] ?? '')) audio[which] = now || undefined;
    }
    const merged = normalizedAudio(audio);
    if (merged) out.audio = merged;
    else delete out.audio;
    return out as unknown as RigConfig;
}

// Mirror an optional string override (ft8_mode / my_rig) from the draft onto the
// patched fresh rig, but only when it CHANGED vs the baseline: set when present,
// delete when the operator cleared it. The setters delete the key on clear, so an
// absent field means "inherit the rigdef default" and round-trips not-dirty.
function patchOptional(
    patched: RigConfig,
    base: RigConfig,
    draft: RigConfig,
    key: 'ft8_mode' | 'my_rig'
): void {
    if (base[key] === draft[key]) return;
    const v = draft[key];
    if (v === undefined || v === null) delete patched[key];
    else patched[key] = v;
}

// A rig's restart-relevant canonical string: every field EXCEPT my_rig. The
// bridge binds model/port/audio/serial overrides at startup and the FT8
// subsystem reads ft8_mode there, so a change to any of them needs a daemon
// restart to take effect. MY_RIG alone is resolved LIVE, per QSO, at submit
// (qsoservice ResolveMyRigFor), so a MY_RIG-only edit applies without a restart.
// Mirrors the config SPA's restartRelevant (canonRig sans my_rig), the parity
// source, so the app doesn't mis-report a pure MY_RIG change as restart-only
// (clean-room review 8c42755e P3). Order-sensitive raw JSON, consistent with the
// `dirty` compare (the app deliberately has no canonicalisation layer).
function restartRelevant(rig: RigConfig): string {
    const rest: Record<string, unknown> = { ...rig };
    delete rest.my_rig;
    return JSON.stringify(rest);
}

// Announce one successful rig-config save: on a durability-unconfirmed write the
// caveat replaces the ordinary toast, so the operator sees a single outcome
// (PT-6). Centralised because every rig-save site shares this decision, and it
// keeps the branch out of the already-heavy save().
function announceRigSaved(outcome: { durabilityUnconfirmed?: boolean }, message: string): void {
    if (!noteConfigDurability(outcome.durabilityUnconfirmed ?? false)) {
        toasts.info(message);
    }
}

class RigsState {
    loading = $state(false);
    loaded = $state(false);
    error = $state('');
    saving = $state(false);
    settingDefault = $state(false);
    rigs = $state<RigConfig[]>([]);
    defaultRigId = $state(0);
    selectedId = $state<number | null>(null);
    // rigdef id → catalogue entry (name + identity + defaults); see nameFor/defFor.
    catalogue = $state<Record<string, RigDef>>({});

    // Discovered hardware for the connection pickers. When audioAvailable is
    // false (static/CGO-free daemon build), the audio picker degrades to the
    // stored device name shown read-only.
    serialPorts = $state<SerialPort[]>([]);
    audioAvailable = $state(false);
    capture = $state<AudioDevice[]>([]);
    playback = $state<AudioDevice[]>([]);

    // Editable connection clones, keyed by rig id — one per rig visited, so
    // switching rigs preserves unsaved edits.
    drafts = $state<Record<number, RigConfig>>({});

    // The IMMUTABLE baseline each draft was cloned from, keyed by rig id.
    // dirty + the save diff compare the draft against THIS, not against
    // this.selected — because this.selected is drawn from the mutable this.rigs
    // snapshot, which #applyFetched rebases on every refresh. Comparing against a
    // drifting baseline made a rig whose fields changed concurrently read falsely
    // dirty, and then a save wrote the stale draft back over the concurrent
    // change (review 2026-07-20 Rigs-editor #6).
    baselines = $state<Record<number, RigConfig>>({});

    // A rig picked from '+ Add rig' but not yet saved (fresh-install ruling
    // 2026-09-26). It is NOT in `rigs` and has no id until Save assigns one on the
    // fresh list, so Cancel leaves the rig list and the default untouched. While it
    // is open the editor edits it instead of the selection.
    newRig = $state<RigConfig | null>(null);

    // A new-rig save whose PUT timed out and whose reconcile re-read also failed:
    // the rig may or may not exist. The draft is kept (it is the only copy of the
    // operator's entries), and the next Save re-checks for this id before adding,
    // so a retry cannot double-add (clean-room review 3fc11918 P2).
    #unsettledAdd: RigConfig | null = null; // the form that was sent, id included

    // The pristine selected rig, or null (no rigs / none selected).
    selected = $derived(this.rigs.find((r) => r.id === this.selectedId) ?? null);

    // The editable draft on screen (the form binds via setters): the unsaved new
    // rig when one is open, else the selection's draft.
    draft = $derived(
        this.newRig ?? (this.selectedId !== null ? (this.drafts[this.selectedId] ?? null) : null)
    );

    // Unsaved edits on screen. A new rig is unsaved by definition, so Save and
    // Cancel are live from the moment it is picked; otherwise the draft differs
    // from ITS OWN baseline (the snapshot it was cloned from), not this.selected.
    dirty = $derived(
        this.newRig !== null ||
            (this.draft && this.selectedId !== null && this.baselines[this.selectedId]
                ? JSON.stringify(this.draft) !== JSON.stringify(this.baselines[this.selectedId])
                : false)
    );

    // Unsaved edits on ANY rig, not just the one on screen. Drafts persist per
    // rig id so that switching rigs doesn't discard edits, which means `dirty`
    // — scoped to the SELECTION — answers "no" while rig 1 still holds unsaved
    // changes. Anything asking on behalf of the whole section (the exit guard)
    // needs this one; anything driving the editor's own Save/Cancel wants
    // `dirty`.
    anyDirty = $derived(
        this.newRig !== null ||
            Object.keys(this.drafts).some((k) => {
                const id = Number(k);
                const b = this.baselines[id];
                return b !== undefined && JSON.stringify(this.drafts[id]) !== JSON.stringify(b);
            })
    );

    // Of the SELECTED rig's unsaved edits, do any require a daemon restart? True
    // when the draft differs from its baseline in a field OTHER than my_rig (see
    // restartRelevant). Gates the "restart to apply" note + save toast so a pure
    // MY_RIG edit — resolved live per QSO — doesn't prompt a needless restart.
    // A new rig needs a restart only when it will become the default (the first
    // rig); any other added rig is not the one the daemon connects to.
    restartDirty = $derived(
        this.newRig !== null
            ? !this.rigs.some((r) => r.id === this.defaultRigId)
            : this.draft && this.selectedId !== null && this.baselines[this.selectedId]
              ? restartRelevant(this.draft) !== restartRelevant(this.baselines[this.selectedId])
              : false
    );

    defFor(rig: RigConfig): RigDef | undefined {
        return this.catalogue[rig.model];
    }
    nameFor(rig: RigConfig): string {
        return this.catalogue[rig.model]?.name ?? rig.model;
    }

    // Open an unsaved rig of the picked catalogue model. A model already
    // configured is allowed (two of the same rig is legitimate); the picker marks
    // it instead. Nothing is written until save().
    startNewRig(model: string): void {
        if (this.saving || this.settingDefault || !this.loaded || this.newRig) return;
        if (!this.catalogue[model]) return;
        this.newRig = { id: 0, model, port: '' };
    }

    // The effective FT8-mode label. Ft8Mode is *string daemon-side with THREE
    // states (types.RigConfig): nil/absent → inherit the rigdef default; an
    // EXPLICIT "" → "leave the rig's current mode" (no switch); any other value
    // → that override literal. Inherit is a NULLISH check, not a falsy one, so
    // an explicit "" isn't shown as the default (review 2026-07-20 Rigs #4).
    ft8ModeFor(rig: RigConfig): string {
        if (rig.ft8_mode === null || rig.ft8_mode === undefined) {
            return this.catalogue[rig.model]?.ft8_mode ?? '';
        }
        return rig.ft8_mode === '' ? 'leave current mode' : rig.ft8_mode;
    }

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        // Invalidate before awaiting: while a reload is pending the retained
        // catalogue is not known-current and must neither render nor save
        // (clean-room review 2c64c7aa P1).
        this.loaded = false;
        this.error = '';
        // Rigs are required; hardware is best-effort (a failure degrades the
        // pickers to read-only text, it must not block the list).
        const [res, hw] = await Promise.all([fetchRigs(), fetchHardware()]);
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            // Unloaded, not merely errored — see the note on emailState.load.
            // Settings unmounts on navigation while this module survives, so a
            // failed remount reload would leave the previous catalogue on
            // screen looking current, and a rigs save PUTs the WHOLE catalogue.
            return;
        }
        this.#applyFetched(res.data);
        if (hw.kind === 'ok') {
            this.serialPorts = hw.hardware.serialPorts;
            this.audioAvailable = hw.hardware.audioAvailable;
            this.capture = hw.hardware.capture;
            this.playback = hw.hardware.playback;
        }
        this.drafts = {}; // fresh load discards any stale drafts
        this.baselines = {};
        this.newRig = null;
        this.#unsettledAdd = null;
        this.#ensureDraft();
        this.loaded = true;
    }

    // Drop every unsaved connection edit and re-clone from the current server
    // values — exactly what a fresh load() does to them (it wipes drafts and
    // baselines outright), made callable so the navigation guard can honour
    // "will be discarded" at the moment the operator agrees rather than leaving
    // it to the next mount. Not resetDraft(): that covers only the SELECTED
    // rig, and edits persist per rig id.
    discardDrafts(): void {
        this.newRig = null;
        this.#unsettledAdd = null;
        this.drafts = {};
        this.baselines = {};
        this.#ensureDraft();
    }

    // Refused while a new rig is open: switching would drop it without asking. The
    // list is disabled then too; Save or Cancel the new rig first.
    select(id: number): void {
        if (this.newRig) return;
        this.selectedId = id;
        this.#ensureDraft(); // keep an existing draft for this rig (don't discard edits)
    }

    // Cancel: discard the operator's unsaved edits and adopt the CURRENT server
    // value (this.selected), re-baselining to it. Not the old baseline — if the
    // rig changed concurrently while this draft stayed dirty, #applyFetched kept
    // the stale baseline, so reverting to it would show an obsolete value as
    // clean; Cancel should surface what's actually on the server now (review
    // 2026-07-20 Rigs-editor #7).
    // For an unsaved new rig, Cancel discards it and returns to the selection.
    resetDraft(): void {
        if (this.newRig) {
            this.newRig = null;
            this.#unsettledAdd = null;
            return;
        }
        const id = this.selectedId;
        if (id !== null && this.selected) {
            this.drafts[id] = cloneRig(this.selected);
            this.baselines[id] = cloneRig(this.selected);
        }
    }

    setDraftPort(port: string): void {
        if (this.draft) this.draft.port = port;
    }
    setDraftAudio(which: 'rx' | 'tx', name: string): void {
        if (!this.draft) return;
        if (!this.draft.audio) this.draft.audio = {};
        this.draft.audio[which] = name;
    }

    // Change the rig's model (a rigdef id from the catalogue). REPLACES the draft
    // OBJECT (not just the field) so the {#key draft} Advanced sub-editors remount
    // and re-read the new rigdef's defaults. The operator's other edits carry over;
    // mode_mappings/overrides are NOT auto-cleared on a model swap (matching the
    // config SPA — they stay the operator's to adjust).
    setDraftModel(model: string): void {
        if (this.newRig) {
            this.newRig = { ...cloneRig(this.newRig), model };
            return;
        }
        const id = this.selectedId;
        const d = this.draft;
        if (id === null || !d) return;
        this.drafts[id] = { ...cloneRig(d), model };
    }

    // ft8_mode / my_rig — optional per-rig overrides. Empty ⇒ DELETE the key
    // (inherit the rigdef default), never store '' or null: the dirty compare is
    // raw JSON, so a cleared override must match the loaded-absent form (no spurious
    // dirty). Inherit-only, matching the config SPA — the explicit-"" state
    // (ft8_mode "" = "leave the rig's current mode"; my_rig "" = suppress MY_RIG)
    // stays a config.json hand-edit that neither editor can author (see ft8ModeFor).
    //
    // ACCEPTED (clean-room review 8c42755e P2, config-SPA parity): this two-states-
    // to-the-UI mapping is the SAME limitation the config SPA has — its canonRig
    // treats ft8_mode "" as identical to absent, so it can neither represent nor
    // preserve an explicit "" across an edit either. A rig loaded WITH an explicit
    // "" that the operator never touches is preserved regardless, because
    // patchOptional is change-gated (base "" === draft "" ⇒ no-op ⇒ the fresh
    // server value, still "", is kept). Only actively editing-then-clearing the
    // field converts "" → inherit — exactly the config SPA's behaviour. Faithful
    // parity is the retirement goal; representing the tri-state would exceed it.
    setDraftFt8Mode(v: string): void {
        const d = this.draft;
        if (!d) return;
        if (v === '') delete d.ft8_mode;
        else d.ft8_mode = v;
    }
    setDraftMyRig(v: string): void {
        const d = this.draft;
        if (!d) return;
        if (v === '') delete d.my_rig;
        else d.my_rig = v;
    }

    // A connection save may start: loaded, dirty, and no write in flight.
    // settingDefault is checked BOTH ways (setDefault also refuses while saving)
    // so a connection save and a set-default can't overlap — otherwise a save
    // that re-fetched the OLD default could apply it via #applyFetched after
    // set-default moved the badge, reverting it (codex e539a080 P2).
    #canSaveEdits(): boolean {
        return !this.saving && !this.settingDefault && this.loaded && this.dirty;
    }

    // BASELINE DEBT 2026-07-31 (complexity 38) — validation across the whole rig-def
    // surface before a write.
    // eslint-disable-next-line complexity
    async save(): Promise<void> {
        if (this.newRig) return this.#saveNew();
        const id = this.selectedId;
        const d = this.draft;
        if (!this.#canSaveEdits() || !d || id === null) return;
        this.saving = true;
        // Re-fetch so we merge onto the CURRENT catalogue, not the mount snapshot
        // — otherwise the whole-replace would overwrite a concurrent change to
        // another rig / this rig's other fields (review Rigs-editor #1).
        const fresh = await fetchRigs();
        if (fresh.kind === 'error') {
            this.saving = false;
            toasts.error(`Save failed: couldn't refresh rigs (${fresh.message}).`);
            return;
        }
        const freshTarget = fresh.data.rigs.find((r) => r.id === id);
        if (!freshTarget) {
            this.saving = false;
            toasts.error('That rig no longer exists — reload Settings.');
            return;
        }
        // Apply ONLY the connection fields the operator actually CHANGED — diffed
        // against the draft's BASELINE (what it was cloned from), per independent
        // field: port, audio RX, and audio TX are patched separately. Everything
        // else — every other rig, and any connection field the operator didn't
        // touch — comes from the fresh fetch. So editing only RX preserves a
        // concurrent TX change (and port/audio don't clobber each other)
        // instead of writing a stale draft value back (review 2026-07-20
        // Rigs-editor #1 + #5). Field-level, down to each audio direction.
        const base = this.baselines[id] ?? freshTarget;
        const patched = cloneRig(freshTarget);
        if (d.port !== base.port) patched.port = d.port;

        const draftRx = d.audio?.rx ?? '';
        const draftTx = d.audio?.tx ?? '';
        const baseRx = base.audio?.rx ?? '';
        const baseTx = base.audio?.tx ?? '';
        if (draftRx !== baseRx || draftTx !== baseTx) {
            // Start from the FRESH rig's audio (keeps a concurrent change to the
            // direction the operator didn't edit), then override only the
            // direction(s) they did.
            const merged = { rx: patched.audio?.rx, tx: patched.audio?.tx };
            if (draftRx !== baseRx) merged.rx = draftRx || undefined;
            if (draftTx !== baseTx) merged.tx = draftTx || undefined;
            const audio = normalizedAudio(merged);
            if (audio) patched.audio = audio;
            else delete patched.audio;
        }

        // mode_mappings is a whole-map override edited by ModeMappingsEditor;
        // diff it as ONE field against the baseline. Changed → the operator's map
        // wins (last-writer-wins on the map, per the accepted concurrent-edit
        // limitation); untouched → keep the FRESH server value so a concurrent
        // mode-mapping change on this rig isn't clobbered. An empty/absent map
        // clears the override (inherit the rigdef defaults).
        if (
            JSON.stringify(base.mode_mappings ?? null) !== JSON.stringify(d.mode_mappings ?? null)
        ) {
            if (d.mode_mappings && Object.keys(d.mode_mappings).length > 0) {
                patched.mode_mappings = d.mode_mappings;
            } else {
                delete patched.mode_mappings;
            }
        }

        // Serial overrides — same whole-object, field-independent treatment as
        // mode_mappings (edited by SerialOverridesEditor). Changed → the operator's
        // overrides win; untouched → keep the fresh server value; empty → clear
        // (inherit the rigdef serial defaults).
        if (JSON.stringify(base.overrides ?? null) !== JSON.stringify(d.overrides ?? null)) {
            if (d.overrides && Object.keys(d.overrides).length > 0) {
                patched.overrides = d.overrides;
            } else {
                delete patched.overrides;
            }
        }

        // model — a rigdef id, always present. Changed ⇒ the operator's choice
        // wins. ft8_mode / my_rig — optional overrides, mirrored by presence (set
        // when present, delete when cleared). Same field-independent, diff-vs-baseline
        // discipline as the rest, so an untouched field keeps the fresh server value.
        if (d.model !== base.model) patched.model = d.model;
        patchOptional(patched, base, d, 'ft8_mode');
        patchOptional(patched, base, d, 'my_rig');

        // Did the operator change anything that binds at startup? If only MY_RIG
        // moved, the daemon picks it up live per QSO — don't tell them to restart.
        const restartNeeded = restartRelevant(base) !== restartRelevant(d);

        const next = fresh.data.rigs.map((r) => (r.id === id ? patched : r));

        const outcome = await saveRigs(next); // no default_rig_id — active rig untouched
        this.saving = false;
        if (outcome.kind === 'error') {
            toasts.error(`Save failed: ${outcome.message}`);
            return;
        }
        // Adopt the fresh catalogue + our patch as the new truth; re-baseline THIS
        // rig's draft to the saved form so it's no longer dirty.
        this.#applyFetched({ ...fresh.data, rigs: next });
        this.baselines[id] = cloneRig(patched);
        this.drafts[id] = cloneRig(patched);
        announceRigSaved(
            outcome,
            restartNeeded
                ? 'Rig saved — restart the daemon to apply.'
                : // MY_RIG is resolved live per QSO, so no restart instruction here.
                  // Not "applies to the next QSO": the daemon stamps only the ACTIVE
                  // rig's MY_RIG, and the SPA can't tell which rig is truly active.
                  'MY_RIG saved.'
        );
    }

    // Set the active/default rig. Sends ONLY default_rig_id (a single-field PUT),
    // so it never touches the connection drafts or the catalogue — no re-fetch, no
    // clobber (the reason set-default is its own path, not folded into save()).
    // Optimistic: the badge moves on success. No-op if it's already the default or
    // another write is in flight.
    async setDefault(id: number): Promise<void> {
        if (this.settingDefault || this.saving || !this.loaded || id === this.defaultRigId) return;
        this.settingDefault = true;
        const outcome = await setDefaultRig(id);
        this.settingDefault = false;
        if (outcome.kind === 'error') {
            toasts.error(`Couldn't set the default rig: ${outcome.message}`);
            return;
        }
        this.defaultRigId = id;
        announceRigSaved(outcome, 'Default rig set — restart to connect to it.');
    }

    // Save the unsaved new rig — the only point an add writes (fresh-install ruling
    // 2026-09-26, superseding the 2026-08-19 immediate-write add).
    //
    // Data safety: RE-FETCH first and append onto the FRESH list (never the mount
    // snapshot), exactly like save(), so a concurrent add/edit to another rig
    // survives the whole-replace. The re-fetch→PUT is not atomic — same accepted
    // last-writer-wins window as save() (a second client in the millisecond gap).
    // On failure the draft stays open so the operator can fix and retry.
    async #saveNew(): Promise<void> {
        const draft = this.newRig;
        if (!draft || this.saving || this.settingDefault || !this.loaded) return;
        this.saving = true;
        if (this.#unsettledAdd) {
            if ((await this.#reconcileAfterAdd(this.#unsettledAdd)) !== 'absent') {
                this.saving = false;
                return;
            }
        }
        const fresh = await fetchRigs();
        if (fresh.kind === 'error') {
            this.saving = false;
            toasts.error(`Couldn't add the rig: refreshing the list failed (${fresh.message}).`);
            return;
        }
        // Client-assigned id: max over the FRESH list + 1 (ids are >0 and unique).
        const id = fresh.data.rigs.reduce((m, r) => Math.max(m, r.id), 0) + 1;
        const newRig = savedFormOf(draft, id);
        const nextRigs = [...fresh.data.rigs, newRig];
        // First rig becomes the active default: the daemon 400s on an unresolvable
        // default_rig_id, so when the fresh default doesn't resolve (empty list, or
        // a dangling id) point it at the new rig. Otherwise OMIT default_rig_id so a
        // concurrent active-rig change isn't clobbered (presence-aware, like save()).
        const defaultResolves = fresh.data.rigs.some((r) => r.id === fresh.data.defaultRigId);
        const nextDefault = defaultResolves ? undefined : id;
        const outcome = await saveRigs(nextRigs, nextDefault);
        if (outcome.kind === 'error') {
            if (outcome.timedOut) {
                // Ambiguous: the PUT may already have committed. Saving a new rig
                // is NON-idempotent (a retry assigns a NEW id and appends a second
                // rig), so re-read the authoritative list and reconcile instead of
                // leaving a draft a blind retry would double-add from (clean-room
                // review 7b5ed1d2 P2).
                const settled = await this.#reconcileAfterAdd(newRig);
                this.saving = false;
                if (settled === 'absent') {
                    toasts.warn('Add timed out and did not take effect — try again.');
                }
                return;
            }
            this.saving = false;
            toasts.error(`Couldn't add the rig: ${outcome.message}`);
            return;
        }
        this.saving = false;
        this.newRig = null;
        this.#applyFetched({
            rigs: nextRigs,
            defaultRigId: nextDefault ?? fresh.data.defaultRigId,
            catalogue: fresh.data.catalogue,
        });
        this.selectedId = id; // the saved rig stays on screen
        this.#ensureDraft();
        announceRigSaved(
            outcome,
            nextDefault === undefined ? 'Rig added.' : 'Rig added — restart needed.'
        );
    }

    // Settle an add whose PUT timed out by re-reading the rig list: 'landed' when
    // our rig (the id we assigned + our model) is there — adopt it; 'absent' when
    // it isn't — the caller may add; 'unknown' when the re-read fails — keep the
    // draft and re-check at the next Save. This is trustworthy — though NOT claimed infallible — because of
    // the daemon's lock ordering: the PUT persists INSIDE config.Service.Update,
    // which holds the config WRITE lock (config.go:2079) across both the disk write
    // and the in-memory swap, and only then does the handler write its 200
    // (handler_config.go handlePutConfig). The reconcile GET reads via Snapshot's
    // RLock (config.go:2029), and Go's sync.RWMutex blocks NEW readers once a writer
    // is waiting — so a reconcile RLock issued after the timeout cannot overtake a
    // PUT that is committing OR already queued behind another reader: it reads the
    // committed list or blocks behind the writer. A genuinely stuck daemon makes
    // THIS GET time out too → the reread-error branch below (state unknown), never a
    // false "did not commit".
    //
    // FORMAL RESIDUAL (accepted — same family as the non-atomic re-fetch→PUT note
    // above): a PUT handler stalled for longer than the client timeout BEFORE it
    // enters Update (before the write lock is even queued — e.g. a multi-second
    // stall in request read/validation) could let this reread observe pre-commit
    // state and report "did not take effect" for an add that later commits, so a
    // retry would double-add. Finite polling can't close it (it can't prove
    // non-commit); the complete fix is server-side idempotency, disproportionate for
    // a local single-operator config editor (operator ruling 2026-08-19).
    async #reconcileAfterAdd(submitted: RigConfig): Promise<'landed' | 'absent' | 'unknown'> {
        const { id, model } = submitted;
        const reread = await fetchRigs();
        if (reread.kind === 'error') {
            this.#unsettledAdd = submitted;
            toasts.error(
                'Add timed out and the rig list could not be re-read, so the rig may or may ' +
                    'not have been added. Your entries are kept; Save checks again before adding.'
            );
            return 'unknown';
        }
        this.#unsettledAdd = null;
        this.#applyFetched(reread.data);
        const landed = reread.data.rigs.find((r) => r.id === id && r.model === model);
        if (!landed) return 'absent';
        // It landed. Show the saved rig, carrying any entries made since the timed-out
        // save as its unsaved edits rather than dropping them.
        const draft = this.newRig;
        this.newRig = null;
        this.selectedId = id;
        this.#ensureDraft();
        if (draft) this.drafts[id] = withEditsSince(landed, submitted, savedFormOf(draft, id));
        toasts.warn('Add timed out, but the rig was saved.');
        return 'landed';
    }

    // The last rig can be deleted only with CAT off: the daemon refuses an enabled
    // bridge with no rig to take a port from (validateBridge; daemon check
    // 2026-09-28). Unknown CAT state counts as on. The operator turns CAT off
    // themselves — deleting never switches it off for them (ruling 2026-09-28).
    get catBlocksLastDelete(): boolean {
        return !bridgeEnabledState.loaded || bridgeEnabledState.enabled;
    }

    // Delete a rig — an IMMEDIATE, confirmed structural write. RE-FETCH first and
    // remove from the FRESH list so a concurrent edit to another rig survives the
    // whole-replace. The default rig is not deletable while other rigs exist: the
    // operator sets another rig as default first (alpha.2 dogfood Finding 3,
    // W-0012). The LAST rig is deletable with CAT off, clearing the rigs and the
    // default in one PUT (fresh-install ruling 2026-09-26). The button is disabled
    // for the refused cases; the store refuses regardless. Delete is IDEMPOTENT on
    // retry (removing an already-gone rig is a no-op), so a timed-out delete needs
    // no reconcile — a retry is safe.
    async deleteRig(id: number): Promise<void> {
        if (this.saving || this.settingDefault || !this.loaded || this.newRig) return;
        const last = this.rigs.length <= 1;
        if (last && this.catBlocksLastDelete) return;
        if (!last && id === this.defaultRigId) return; // never delete the default rig
        this.saving = true;
        const fresh = await fetchRigs();
        if (fresh.kind === 'error') {
            this.saving = false;
            toasts.error(`Couldn't delete the rig: refreshing the list failed (${fresh.message}).`);
            return;
        }
        // A concurrent delete may already have removed it.
        if (!fresh.data.rigs.some((r) => r.id === id)) {
            this.saving = false;
            this.#applyFetched(fresh.data);
            toasts.info('That rig was already removed.');
            return;
        }
        const freshLast = fresh.data.rigs.length === 1;
        // The operator confirmed deleting one of several rigs; a concurrent delete
        // has since made this the last one. Deleting it now would leave no rig
        // without their having agreed to that — show the new list instead.
        if (freshLast && !last) {
            this.saving = false;
            this.#applyFetched(fresh.data);
            toasts.info(
                'The rig list changed — this is now your only rig. Check it and try again.'
            );
            return;
        }
        // Checked on the FRESH state too: a concurrent default change can make the
        // target the default after the button was pressed. Sending the PUT anyway
        // would omit default_rig_id and the daemon would 400 on the unresolvable
        // default — the rule is stated here instead.
        if (!freshLast && fresh.data.defaultRigId === id) {
            this.saving = false;
            this.#applyFetched(fresh.data);
            toasts.error("Can't delete the default rig — set another rig as default first.");
            return;
        }
        const nextRigs = fresh.data.rigs.filter((r) => r.id !== id);
        // The last rig takes the default with it (0 = no rig, valid only with no
        // rigs). Otherwise the default is never the target, so it keeps resolving →
        // OMIT default_rig_id so a concurrent default change isn't clobbered.
        const outcome = await saveRigs(nextRigs, freshLast ? 0 : undefined);
        this.saving = false;
        if (outcome.kind === 'error') {
            toasts.error(`Couldn't delete the rig: ${outcome.message}`);
            return;
        }
        // Drop the deleted rig's draft + baseline so a lingering (possibly dirty)
        // entry can't keep anyDirty true for a rig that no longer exists.
        delete this.drafts[id];
        delete this.baselines[id];
        // #applyFetched reconciles the selection if the deleted rig was selected AND
        // drafts the survivor (so the editor stays visible — see its note).
        this.#applyFetched({
            rigs: nextRigs,
            defaultRigId: freshLast ? 0 : fresh.data.defaultRigId,
            catalogue: fresh.data.catalogue,
        });
        announceRigSaved(outcome, 'Rig deleted.');
    }

    // Apply a fetched rigs payload, reconcile the selection against the list, and
    // ensure that selection has a draft. Folding #ensureDraft in makes EVERY caller
    // that can move the selection — save, add, delete (all its branches), the add
    // reconcile — leave a usable draft: on load only the selected rig is drafted, so
    // any path that reselects a survivor must draft it or RigsSection's
    // `{#if selected && draft}` hides the whole editor (clean-room review 825983c2
    // P2; the hand-copied #ensureDraft it replaced missed the already-removed branch).
    #applyFetched(data: {
        rigs: RigConfig[];
        defaultRigId: number;
        catalogue: Record<string, RigDef>;
    }): void {
        this.rigs = data.rigs;
        this.defaultRigId = data.defaultRigId;
        this.catalogue = data.catalogue;
        // Re-baseline PRISTINE retained drafts (draft === baseline) onto the fresh
        // rig, so a rig whose fields changed concurrently shows the new values and
        // stays not-dirty. A DIRTY draft keeps its own baseline — the operator's
        // unsaved edits (and the baseline the next save diffs against) survive
        // (review 2026-07-20 Rigs-editor #6).
        for (const rig of this.rigs) {
            const b = this.baselines[rig.id];
            const dr = this.drafts[rig.id];
            if (!b || !dr) continue;
            if (JSON.stringify(dr) === JSON.stringify(b)) {
                this.baselines[rig.id] = cloneRig(rig);
                this.drafts[rig.id] = cloneRig(rig);
            }
        }
        // Keep a still-valid selection; else the active rig, then the first, then
        // null (review 2026-07-20 Rigs #2 — don't strand a vanished selection).
        const stillSelected =
            this.selectedId !== null && this.rigs.some((r) => r.id === this.selectedId);
        if (!stillSelected) {
            this.selectedId = this.rigs.some((r) => r.id === this.defaultRigId)
                ? this.defaultRigId
                : (this.rigs[0]?.id ?? null);
        }
        // A reconciled survivor may have no draft yet — give it one so the editor
        // stays visible (a no-op when a draft already exists).
        this.#ensureDraft();
    }

    // Ensure the current selection has a draft (lazy clone; preserves an existing
    // one so per-rig edits survive re-selection). Captures the immutable baseline
    // alongside so dirty/save diff against a stable snapshot.
    #ensureDraft(): void {
        const id = this.selectedId;
        if (id !== null && this.selected && !this.drafts[id]) {
            this.drafts[id] = cloneRig(this.selected);
            this.baselines[id] = cloneRig(this.selected);
        }
    }
}

export const rigsState = new RigsState();
