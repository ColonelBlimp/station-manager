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
import { fetchRigs, saveRigs, type RigConfig, type RigDef } from '../api/rigs';
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

class RigsState {
    loading = $state(false);
    loaded = $state(false);
    error = $state('');
    saving = $state(false);
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

    defFor(rig: RigConfig): RigDef | undefined {
        return this.catalogue[rig.model];
    }
    nameFor(rig: RigConfig): string {
        return this.catalogue[rig.model]?.name ?? rig.model;
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
        this.error = '';
        // Rigs are required; hardware is best-effort (a failure degrades the
        // pickers to read-only text, it must not block the list).
        const [res, hw] = await Promise.all([fetchRigs(), fetchHardware()]);
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
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

    async save(): Promise<void> {
        const id = this.selectedId;
        const d = this.draft;
        if (this.saving || !this.dirty || !d || id === null) return;
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
        toasts.info('Rig connection saved — restart the daemon to reconnect.');
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
