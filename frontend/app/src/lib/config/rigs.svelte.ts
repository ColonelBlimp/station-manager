/*
    Rigs settings state (app Settings → Rigs section, ADR 0044) — the daemon's
    configured rig list + rigdef catalogue (GET /v1/rigs), the discovered
    hardware for the connection pickers (GET /v1/hardware), and the selected
    rig's editable connection draft. Daemon is the source of truth (ADR 0003);
    no local cache.

    DATA SAFETY: a rig save WHOLE-REPLACES the catalogue daemon-side, so the
    draft is a full-fidelity JSON clone of the raw rig (all fields, including the
    ones the panel never renders — mode_mappings/overrides/ft8_mode/my_rig), and
    save sends the whole rigs array with the edited rig substituted in. Editing
    only mutates the draft's port/audio; every other field rides back untouched.
*/
import { fetchRigs, saveRigs, type RigConfig, type RigDef } from '../api/rigs';
import { fetchHardware, type SerialPort, type AudioDevice } from '../api/hardware';
import { toasts } from '../ui/toasts.svelte';

// A full-fidelity clone (rigs are pure JSON, so a JSON round-trip is lossless
// AND environment-independent — no structuredClone dependency). Ensures `audio`
// exists so the pickers can bind.
function cloneRig(rig: RigConfig): RigConfig {
    const d = JSON.parse(JSON.stringify(rig)) as RigConfig;
    if (!d.audio) d.audio = {};
    return d;
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

    // The editable clone of the selected rig (port/audio); null when nothing is
    // selected. The form binds to this; `selected` stays the pristine baseline.
    draft = $state<RigConfig | null>(null);

    // The pristine selected rig, or null (no rigs / none selected).
    selected = $derived(this.rigs.find((r) => r.id === this.selectedId) ?? null);

    // Unsaved connection edits: the draft differs from the pristine selected rig.
    dirty = $derived(
        this.draft && this.selected
            ? JSON.stringify(this.draft) !== JSON.stringify(this.selected)
            : false
    );

    // The rigdef catalogue entry for a rig, or undefined (unknown/legacy rigdef).
    defFor(rig: RigConfig): RigDef | undefined {
        return this.catalogue[rig.model];
    }

    // The friendly rigdef name for display, falling back to the raw model id if
    // the catalogue doesn't carry it.
    nameFor(rig: RigConfig): string {
        return this.catalogue[rig.model]?.name ?? rig.model;
    }

    // The effective FT8-mode label. Ft8Mode is *string daemon-side with THREE
    // states (types.RigConfig): nil/absent → inherit the rigdef default; an
    // EXPLICIT "" → "leave the rig's current mode" (no switch); any other value
    // → that override literal. A `||` fallback would wrongly show an explicit ""
    // as the rigdef default, so inherit is a NULLISH check, not a falsy one
    // (review 2026-07-20 Rigs #4).
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
        // Rigs are required; hardware is best-effort (a failure just degrades the
        // pickers to read-only text, it must not block the list).
        const [res, hw] = await Promise.all([fetchRigs(), fetchHardware()]);
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            return;
        }
        this.rigs = res.data.rigs;
        this.defaultRigId = res.data.defaultRigId;
        this.catalogue = res.data.catalogue;
        if (hw.kind === 'ok') {
            this.serialPorts = hw.hardware.serialPorts;
            this.audioAvailable = hw.hardware.audioAvailable;
            this.capture = hw.hardware.capture;
            this.playback = hw.hardware.playback;
        }
        // Reconcile the selection against the (possibly changed) list: keep a
        // still-valid manual selection, otherwise fall back to the active rig,
        // then the first, then null. Guarding only on `selectedId === null`
        // would strand a selection whose rig disappeared on reload — no list
        // item highlighted and an empty detail panel (review 2026-07-20 Rigs #2).
        const stillSelected =
            this.selectedId !== null && this.rigs.some((r) => r.id === this.selectedId);
        if (!stillSelected) {
            this.selectedId = this.rigs.some((r) => r.id === this.defaultRigId)
                ? this.defaultRigId
                : (this.rigs[0]?.id ?? null);
        }
        this.resetDraft();
        this.loaded = true;
    }

    select(id: number): void {
        this.selectedId = id;
        this.resetDraft(); // switching rigs discards any unsaved edit on the previous
    }

    // Reset the editable draft to a fresh clone of the current selection.
    resetDraft(): void {
        this.draft = this.selected ? cloneRig(this.selected) : null;
    }

    // Connection edits go through these (rather than bind:value) so the audio
    // sub-object is lazily ensured and the mutation is a plain property write on
    // the reactive draft — which drives the `dirty` derived.
    setDraftPort(port: string): void {
        if (this.draft) this.draft.port = port;
    }
    setDraftAudio(which: 'rx' | 'tx', name: string): void {
        if (!this.draft) return;
        if (!this.draft.audio) this.draft.audio = {};
        this.draft.audio[which] = name;
    }

    async save(): Promise<void> {
        if (this.saving || !this.dirty || !this.draft) return;
        this.saving = true;
        const draft = this.draft;
        // Substitute the edited rig into the full catalogue; every other rig (and
        // every un-rendered field of THIS rig) rides back verbatim.
        const next = this.rigs.map((r) => (r.id === draft.id ? draft : r));
        const res = await saveRigs(next, this.defaultRigId);
        this.saving = false;
        if (res.kind === 'error') {
            toasts.error(`Save failed: ${res.message}`);
            return;
        }
        // The daemon validates-not-transforms, so the sent array is now stored.
        this.rigs = next;
        this.draft = cloneRig(draft); // fresh pristine baseline → not dirty
        toasts.info('Rig connection saved — restart the daemon to reconnect.');
    }
}

export const rigsState = new RigsState();
