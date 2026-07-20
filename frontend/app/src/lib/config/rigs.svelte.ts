/*
    Rigs settings state (app Settings → Rigs section, ADR 0044) — the daemon's
    configured rig list + rigdef catalogue (GET /v1/rigs), the discovered
    hardware for the connection pickers (GET /v1/hardware), and PER-RIG editable
    connection drafts. Daemon is the source of truth (ADR 0003); no local cache.

    DATA SAFETY (review 2026-07-20 Rigs-editor): a rig save WHOLE-REPLACES the
    catalogue daemon-side, so save RE-FETCHES the fresh catalogue, applies only
    the edited rig's port/audio onto the fresh objects, and PUTs that — WITHOUT
    default_rig_id — so a concurrent change to another rig, this rig's other
    fields, or the active-rig selection is never clobbered by a stale snapshot.
    Drafts are full-fidelity JSON clones (all fields, incl. the un-rendered
    mode_mappings/overrides), kept PER RIG so switching rigs doesn't discard
    unsaved edits.
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

    // The pristine selected rig, or null (no rigs / none selected).
    selected = $derived(this.rigs.find((r) => r.id === this.selectedId) ?? null);

    // The editable draft for the current selection (the form binds via setters).
    draft = $derived(this.selectedId !== null ? (this.drafts[this.selectedId] ?? null) : null);

    // Unsaved connection edits: the current draft differs from its pristine rig.
    dirty = $derived(
        this.draft && this.selected
            ? JSON.stringify(this.draft) !== JSON.stringify(this.selected)
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
        this.#ensureDraft();
        this.loaded = true;
    }

    select(id: number): void {
        this.selectedId = id;
        this.#ensureDraft(); // keep an existing draft for this rig (don't discard edits)
    }

    // Revert the CURRENT rig's draft to its pristine form.
    resetDraft(): void {
        if (this.selectedId !== null && this.selected) {
            this.drafts[this.selectedId] = cloneRig(this.selected);
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
        // Apply ONLY the edited connection fields onto the fresh rig; every other
        // field (and every other rig) comes from the fresh fetch.
        const patched = cloneRig(freshTarget);
        patched.port = d.port;
        const audio = normalizedAudio(d.audio);
        if (audio) patched.audio = audio;
        else delete patched.audio;
        const next = fresh.data.rigs.map((r) => (r.id === id ? patched : r));

        const outcome = await saveRigs(next); // no default_rig_id — active rig untouched
        this.saving = false;
        if (outcome.kind === 'error') {
            toasts.error(`Save failed: ${outcome.message}`);
            return;
        }
        // Adopt the fresh catalogue + our patch as the new truth; reset THIS
        // rig's draft to the saved form so it's no longer dirty.
        this.#applyFetched({ ...fresh.data, rigs: next });
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
    // one so per-rig edits survive re-selection).
    #ensureDraft(): void {
        if (this.selectedId !== null && this.selected && !this.drafts[this.selectedId]) {
            this.drafts[this.selectedId] = cloneRig(this.selected);
        }
    }
}

export const rigsState = new RigsState();
