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
import { fetchHardware, type SerialPort, type AudioDevice } from '../api/hardware';
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

    // The pristine selected rig, or null (no rigs / none selected).
    selected = $derived(this.rigs.find((r) => r.id === this.selectedId) ?? null);

    // The editable draft for the current selection (the form binds via setters).
    draft = $derived(this.selectedId !== null ? (this.drafts[this.selectedId] ?? null) : null);

    // Unsaved connection edits: the current draft differs from ITS OWN baseline
    // (the snapshot it was cloned from), not from this.selected.
    dirty = $derived(
        this.draft && this.selectedId !== null && this.baselines[this.selectedId]
            ? JSON.stringify(this.draft) !== JSON.stringify(this.baselines[this.selectedId])
            : false
    );

    // Unsaved edits on ANY rig, not just the one on screen. Drafts persist per
    // rig id so that switching rigs doesn't discard edits, which means `dirty`
    // — scoped to the SELECTION — answers "no" while rig 1 still holds unsaved
    // changes. Anything asking on behalf of the whole section (the exit guard)
    // needs this one; anything driving the editor's own Save/Cancel wants
    // `dirty`.
    anyDirty = $derived(
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
    restartDirty = $derived(
        this.draft && this.selectedId !== null && this.baselines[this.selectedId]
            ? restartRelevant(this.draft) !== restartRelevant(this.baselines[this.selectedId])
            : false
    );

    defFor(rig: RigConfig): RigDef | undefined {
        return this.catalogue[rig.model];
    }
    nameFor(rig: RigConfig): string {
        return this.catalogue[rig.model]?.name ?? rig.model;
    }

    // The model a newly-added rig should default to: the first catalogue rigdef
    // not already configured (so Add doesn't silently clone a model), falling back
    // to the first catalogue entry when every model is in use, or '' when the
    // catalogue is empty (Add is then disabled). The Rigs section confirms before
    // adding when this returns an in-use model. Mirrors the config SPA's
    // nextRigModel; "used" is the committed list (unsaved model edits are transient
    // and the add re-fetches anyway).
    nextRigModel(): string {
        const used = new Set(this.rigs.map((r) => r.model));
        const keys = Object.keys(this.catalogue);
        return keys.find((k) => !used.has(k)) ?? keys[0] ?? '';
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
        this.drafts = {};
        this.baselines = {};
        this.#ensureDraft();
    }

    select(id: number): void {
        this.selectedId = id;
        this.#ensureDraft(); // keep an existing draft for this rig (don't discard edits)
    }

    // Cancel: discard the operator's unsaved edits and adopt the CURRENT server
    // value (this.selected), re-baselining to it. Not the old baseline — if the
    // rig changed concurrently while this draft stayed dirty, #applyFetched kept
    // the stale baseline, so reverting to it would show an obsolete value as
    // clean; Cancel should surface what's actually on the server now (review
    // 2026-07-20 Rigs-editor #7).
    resetDraft(): void {
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

    // BASELINE DEBT 2026-07-31 (complexity 38) — validation across the whole rig-def
    // surface before a write.
    // eslint-disable-next-line complexity
    async save(): Promise<void> {
        const id = this.selectedId;
        const d = this.draft;
        // settingDefault is checked BOTH ways (setDefault also refuses while saving)
        // so a connection save and a set-default can't overlap — otherwise a save
        // that re-fetched the OLD default could apply it via #applyFetched after
        // set-default moved the badge, reverting it (codex e539a080 P2).
        if (this.saving || this.settingDefault || !this.loaded || !this.dirty || !d || id === null)
            return;
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
        toasts.info(
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
        toasts.info('Default rig set — restart to connect to it.');
    }

    // Add a rig — an IMMEDIATE structural write (operator ruling 2026-08-19: the
    // app's per-rig field-merge model has no section Save, and Set-default already
    // writes immediately, so Add follows that pattern rather than a pending draft).
    // Creates a blank rig; the operator then configures its connection and Saves via
    // the normal per-rig editor. `model` comes from nextRigModel via the section's
    // duplicate-confirm handler.
    //
    // Data safety: RE-FETCH first and append onto the FRESH list (never the mount
    // snapshot), exactly like save(), so a concurrent add/edit to another rig
    // survives the whole-replace. The re-fetch→PUT is not atomic — same accepted
    // last-writer-wins window as save() (a second client in the millisecond gap).
    async addRig(model: string): Promise<void> {
        if (this.saving || this.settingDefault || !this.loaded || model === '') return;
        this.saving = true;
        const fresh = await fetchRigs();
        if (fresh.kind === 'error') {
            this.saving = false;
            toasts.error(`Couldn't add a rig: refreshing the list failed (${fresh.message}).`);
            return;
        }
        // Client-assigned id: max over the FRESH list + 1 (ids are >0 and unique).
        const id = fresh.data.rigs.reduce((m, r) => Math.max(m, r.id), 0) + 1;
        const newRig: RigConfig = { id, model, port: '' };
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
                // Ambiguous: the PUT may already have committed. The immediate add
                // is NON-idempotent (a retry assigns a NEW id and appends a second
                // rig), so re-read the authoritative list and reconcile instead of
                // leaving stale state a blind retry would double-add against
                // (clean-room review 7b5ed1d2 P2; mirrors #reconcileAfterTimeout).
                await this.#reconcileAfterAdd(id, model);
                return;
            }
            this.saving = false;
            toasts.error(`Couldn't add a rig: ${outcome.message}`);
            return;
        }
        this.saving = false;
        this.#applyFetched({
            rigs: nextRigs,
            defaultRigId: nextDefault ?? fresh.data.defaultRigId,
            catalogue: fresh.data.catalogue,
        });
        this.selectedId = id; // focus the new rig so the operator can configure it
        this.#ensureDraft();
        toasts.info('Rig added — set its connection, then Save.');
    }

    // Settle an add whose PUT timed out by re-reading the rig list. If our rig (the
    // id we assigned + our model) landed, adopt it; if not, report that it didn't
    // take effect. This is trustworthy — though NOT claimed infallible — because of
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
    async #reconcileAfterAdd(id: number, model: string): Promise<void> {
        const reread = await fetchRigs();
        this.saving = false;
        if (reread.kind === 'error') {
            toasts.error(
                'Add timed out and the rig list could not be re-read — state unknown. ' +
                    'Reload Settings before trying again.'
            );
            return;
        }
        this.#applyFetched(reread.data);
        const landed = reread.data.rigs.some((r) => r.id === id && r.model === model);
        if (landed) {
            this.selectedId = id;
            this.#ensureDraft();
            toasts.warn('Add timed out, but the rig was created — set its connection, then Save.');
        } else {
            toasts.warn('Add timed out and did not take effect — try again.');
        }
    }

    // Delete a rig — an IMMEDIATE structural write, the mirror of addRig (operator
    // ruling 2026-08-19). RE-FETCH first and remove from the FRESH list so a
    // concurrent edit to another rig survives the whole-replace. Never deletes the
    // only rig (the button is disabled too, matching the config SPA). Unlike Add,
    // delete is IDEMPOTENT on retry (removing an already-gone rig is a no-op), so a
    // timed-out delete needs no reconcile — a retry is safe.
    async deleteRig(id: number): Promise<void> {
        if (this.saving || this.settingDefault || !this.loaded) return;
        if (this.rigs.length <= 1) return; // never delete the only rig
        this.saving = true;
        const fresh = await fetchRigs();
        if (fresh.kind === 'error') {
            this.saving = false;
            toasts.error(`Couldn't delete the rig: refreshing the list failed (${fresh.message}).`);
            return;
        }
        // A concurrent delete may already have removed it, or left a single rig.
        if (!fresh.data.rigs.some((r) => r.id === id)) {
            this.saving = false;
            this.#applyFetched(fresh.data);
            toasts.info('That rig was already removed.');
            return;
        }
        if (fresh.data.rigs.length <= 1) {
            this.saving = false;
            this.#applyFetched(fresh.data);
            toasts.error("Can't delete the only rig.");
            return;
        }
        const nextRigs = fresh.data.rigs.filter((r) => r.id !== id);
        // Repoint the active default ONLY when deleting it: the daemon 400s on an
        // unresolvable default_rig_id, and omitting it would keep the stale
        // (now-deleted) one. Deleting a non-default rig leaves the default resolving
        // → OMIT default_rig_id so a concurrent active-rig change isn't clobbered
        // (presence-aware, like save()). delete is disabled at ≤1 rig, so nextRigs is
        // non-empty here and nextRigs[0] always exists when repointing.
        const deletingDefault = fresh.data.defaultRigId === id;
        const nextDefault = deletingDefault ? (nextRigs[0]?.id ?? 0) : undefined;
        const outcome = await saveRigs(nextRigs, nextDefault);
        this.saving = false;
        if (outcome.kind === 'error') {
            toasts.error(`Couldn't delete the rig: ${outcome.message}`);
            return;
        }
        // Drop the deleted rig's draft + baseline so a lingering (possibly dirty)
        // entry can't keep anyDirty true for a rig that no longer exists.
        delete this.drafts[id];
        delete this.baselines[id];
        // #applyFetched reconciles the selection if the deleted rig was selected.
        this.#applyFetched({
            rigs: nextRigs,
            defaultRigId: nextDefault ?? fresh.data.defaultRigId,
            catalogue: fresh.data.catalogue,
        });
        // Ensure the survivor the selection fell back to has a draft: on load only
        // the SELECTED rig is drafted, so deleting the selected default would leave
        // `selected` set but `draft` null, hiding the whole editor until it's
        // re-clicked (clean-room review 1e3a7bed P2).
        this.#ensureDraft();
        toasts.info('Rig deleted.');
    }

    // Apply a fetched rigs payload + reconcile the selection against the list.
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
