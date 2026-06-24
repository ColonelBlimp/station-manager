/*
    configState — the config SPA's daemon-data singleton. Holds the daemon-
    authoritative config (GET /v1/config) and the rig-profiles editor surface
    (GET /v1/rigs), loaded once on app mount. Components import the singleton
    directly; `$state` fields make reads reactive (same convention as the
    logging SPA's lib/states/*.svelte.ts).

    This is the daemon-authoritative source — there is no parallel store and no
    localStorage cache for it (ADR 0003). The write path (PUT) lands per panel
    as the editor surfaces are built.
*/

import {
    fetchConfig,
    putConfig,
    type ConfigOutcome,
    type ConfigResponse,
    type LoggingStationFields,
} from '../api/config';
import { fetchRigs, type RigConfig, type RigDefSummary } from '../api/rigs';
import { fetchHardware, type HardwareResponse } from '../api/hardware';

/** Deep clone of a rig catalogue, for the Rigs-tab editable draft (rig objects
 *  are JSON-safe). Keeps the draft from aliasing the loaded `rigs`. */
function cloneRigs(rigs: RigConfig[]): RigConfig[] {
    return JSON.parse(JSON.stringify(rigs)) as RigConfig[];
}

/**
 * Canonical form of a rig for change-detection, matching what the daemon
 * round-trips. The daemon nils per-rig overrides that equal the rigdef default
 * (normalizeRigOverrides) and omits nil/empty optionals from GET /v1/rigs — so
 * the loaded catalogue has those keys ABSENT while the editable draft may hold
 * them as `null`, `''`, or `{}`. Comparing raw objects would then report a
 * spurious diff (null ≠ absent), leaving the form "dirty" after a clean save.
 * Canonicalising both sides — drop null/empty optionals — makes the comparison
 * semantic. `includeMyRig=false` excludes MY_RIG for the restart-banner check
 * (MY_RIG applies live; everything else binds at boot).
 */
function canonRig(r: RigConfig, includeMyRig: boolean): Record<string, unknown> {
    const o: Record<string, unknown> = { id: r.id, model: r.model, port: r.port ?? '' };
    if (r.ft8_mode != null && r.ft8_mode !== '') o.ft8_mode = r.ft8_mode;
    if (includeMyRig && r.my_rig != null && r.my_rig !== '') o.my_rig = r.my_rig;
    const rx = r.audio?.rx ?? '';
    const tx = r.audio?.tx ?? '';
    if (rx !== '' || tx !== '') o.audio = { rx, tx };
    if (r.mode_mappings && Object.keys(r.mode_mappings).length > 0)
        o.mode_mappings = r.mode_mappings;
    if (r.overrides && Object.keys(r.overrides).length > 0) o.overrides = r.overrides;
    return o;
}

/** Canonical JSON of a catalogue for change-detection. */
function canonRigs(rigs: RigConfig[], includeMyRig: boolean): string {
    return JSON.stringify(rigs.map((r) => canonRig(r, includeMyRig)));
}

/** A rig's restart-relevant canonical string (excludes MY_RIG, which applies
 *  live). Drives the per-rig comparison behind the "restart required" banner. */
function restartRelevant(r: RigConfig): string {
    return JSON.stringify(canonRig(r, false));
}

// StationForm — the editable set-once LoggingStation MY_* fields the config
// SPA's Station tab owns (operational identity — callsign / operator / grid —
// stays in the logging SPA; design 2026-06-24). All LoggingStation fields are
// strings daemon-side, so the form is all strings.
export interface StationForm {
    my_country: string;
    my_dxcc: string;
    my_cq_zone: string;
    my_itu_zone: string;
    my_altitude: string;
    my_street: string;
    my_city: string;
    my_postal_code: string;
    my_antenna: string;
    my_morse_key_type: string;
    my_morse_key_info: string;
}

function stationFormFrom(ls: LoggingStationFields): StationForm {
    return {
        my_country: ls.my_country ?? '',
        my_dxcc: ls.my_dxcc ?? '',
        my_cq_zone: ls.my_cq_zone ?? '',
        my_itu_zone: ls.my_itu_zone ?? '',
        my_altitude: ls.my_altitude ?? '',
        my_street: ls.my_street ?? '',
        my_city: ls.my_city ?? '',
        my_postal_code: ls.my_postal_code ?? '',
        my_antenna: ls.my_antenna ?? '',
        my_morse_key_type: ls.my_morse_key_type ?? '',
        my_morse_key_info: ls.my_morse_key_info ?? '',
    };
}

// FT8 highlight-colour defaults — mirror the daemon's ResolveFt8Display so a
// fresh config (block omitted) still shows the real values in the pickers.
const DEFAULT_HIGHLIGHT_UNWORKED = '#15803d';
const DEFAULT_HIGHLIGHT_WORKED = '#9ca3af';
const DEFAULT_HIGHLIGHT_CALLING = '#b45309';

class ConfigState {
    /** False until the first load() round-trip settles (success or error). */
    loaded: boolean = $state(false);
    /** Human-readable load error, or null. Set if either fetch failed; partial data still applies. */
    error: string | null = $state(null);

    /** Broader config (station identity, FT8 display, mailer). Null until loaded. */
    config: ConfigResponse | null = $state(null);

    /** Rig-profiles editor data (GET /v1/rigs) — the loaded/canonical catalogue. */
    defaultRigId: number = $state(0);
    rigs: RigConfig[] = $state([]);
    catalogue: RigDefSummary[] = $state([]);

    /** Discovered hardware (GET /v1/hardware) for the Rigs-tab Port/Audio pickers. */
    hardware: HardwareResponse | null = $state(null);

    // Rigs tab — a working DRAFT of the whole catalogue + active-rig id. The
    // editor mutates the draft (edit / add / delete / set-default); Save PUTs the
    // whole draft (the daemon write path replaces the catalogue), Cancel reverts
    // to the loaded `rigs`. Whole-catalogue staging keeps it consistent with the
    // single PUT and the shared TabFooter.
    rigDraft: RigConfig[] = $state([]);
    draftDefaultRigId: number = $state(0);
    selectedRigId: number | null = $state(null);
    /** True while a Rigs save PUT is in flight. */
    savingRigs: boolean = $state(false);
    /** Last Rigs-save status: 'ok', an error message, or null (idle/in-flight). */
    rigsStatus: string | null = $state(null);

    // FT8 highlight-colour holding place (moved out of the logging SPA's FT8
    // Settings tab). Editable copies the pickers bind to; hydrated from
    // config.ft8_display on load and re-hydrated from the PUT response on save.
    highlightUnworked: string = $state(DEFAULT_HIGHLIGHT_UNWORKED);
    highlightWorked: string = $state(DEFAULT_HIGHLIGHT_WORKED);
    highlightCalling: string = $state(DEFAULT_HIGHLIGHT_CALLING);
    /** True while a colour save PUT is in flight. */
    savingColours: boolean = $state(false);
    /** Last colour-save status: 'ok', an error message, or null (idle/in-flight). */
    coloursStatus: string | null = $state(null);

    // Station tab — editable MY_* set-once fields, hydrated from
    // config.logging_station on load and re-hydrated from the PUT response on
    // save. Bound directly by the Station tab's inputs.
    stationForm: StationForm = $state(stationFormFrom({}));
    /** True while a Station save PUT is in flight. */
    savingStation: boolean = $state(false);
    /** Last Station-save status: 'ok', an error message, or null (idle/in-flight). */
    stationStatus: string | null = $state(null);

    /** True when the Station form diverges from the loaded config (drives Save/Cancel). */
    get stationDirty(): boolean {
        if (!this.config) return false;
        return (
            JSON.stringify(this.stationForm) !==
            JSON.stringify(stationFormFrom(this.config.logging_station))
        );
    }

    /**
     * Fetch /v1/config, /v1/rigs, and /v1/hardware concurrently and hydrate.
     * Any failing sets `error` but whatever succeeded is still applied, so a
     * partially healthy daemon still renders. Idempotent enough to call once on
     * mount.
     */
    async load(): Promise<void> {
        const [cfg, rigs, hw] = await Promise.all([fetchConfig(), fetchRigs(), fetchHardware()]);

        const errs: string[] = [];
        if (cfg.kind === 'ok') {
            this.config = cfg.config;
            this.hydrateColours();
            this.stationForm = stationFormFrom(this.config.logging_station);
        } else {
            errs.push(`config: ${outcomeMessage(cfg)}`);
        }
        if (rigs.kind === 'ok') {
            this.defaultRigId = rigs.rigs.default_rig_id;
            this.rigs = rigs.rigs.rigs;
            this.catalogue = rigs.rigs.catalogue;
            this.hydrateRigs();
        } else {
            errs.push(`rigs: ${outcomeMessage(rigs)}`);
        }
        if (hw.kind === 'ok') {
            this.hardware = hw.hardware;
        } else {
            errs.push(`hardware: ${outcomeMessage(hw)}`);
        }

        this.error = errs.length > 0 ? errs.join('; ') : null;
        this.loaded = true;
    }

    /** Reset the Rigs draft to the loaded catalogue (initial hydrate, Cancel, and
     *  post-save re-hydrate). Preserves the current selection when it still
     *  exists, else selects the first rig (or none). */
    private hydrateRigs(): void {
        this.rigDraft = cloneRigs(this.rigs);
        this.draftDefaultRigId = this.defaultRigId;
        if (!this.rigDraft.some((r) => r.id === this.selectedRigId)) {
            this.selectedRigId = this.rigDraft[0]?.id ?? null;
        }
    }

    /** The selected rig within the editable draft, or undefined. */
    get selectedRig(): RigConfig | undefined {
        return this.rigDraft.find((r) => r.id === this.selectedRigId);
    }

    /** True when the Rigs draft diverges from the loaded catalogue (drives Save/Cancel). */
    get rigsDirty(): boolean {
        return (
            this.draftDefaultRigId !== this.defaultRigId ||
            canonRigs(this.rigDraft, true) !== canonRigs(this.rigs, true)
        );
    }

    /** True when a restart-only change (model / port / audio / ft8_mode / serial /
     *  active-rig / add / remove) is staged — drives the "restart required" banner.
     *  A pure MY_RIG edit applies live, so it doesn't raise it. */
    get rigsRestartRequired(): boolean {
        if (this.draftDefaultRigId !== this.defaultRigId) return true;
        const draft = this.rigDraft.map(restartRelevant).join(' ');
        const loaded = this.rigs.map(restartRelevant).join(' ');
        return draft !== loaded;
    }

    selectRig(id: number): void {
        this.selectedRigId = id;
    }

    /** Append a blank rig (next free id, first catalogue model) and select it. */
    addRig(): void {
        const nextId = this.rigDraft.reduce((m, r) => Math.max(m, r.id), 0) + 1;
        const model = this.catalogue[0]?.id ?? '';
        this.rigDraft = [...this.rigDraft, { id: nextId, model, port: '' }];
        this.selectedRigId = nextId;
    }

    /** Remove a rig from the draft; re-point default/selection if it was either. */
    deleteRig(id: number): void {
        this.rigDraft = this.rigDraft.filter((r) => r.id !== id);
        if (this.draftDefaultRigId === id) {
            this.draftDefaultRigId = this.rigDraft[0]?.id ?? 0;
        }
        if (this.selectedRigId === id) {
            this.selectedRigId = this.rigDraft[0]?.id ?? null;
        }
    }

    /** Mark a rig as the active (default) one in the draft. */
    setDefaultRig(id: number): void {
        this.draftDefaultRigId = id;
    }

    /** The rigdef defaults for a configured rig's model, or undefined if unknown. */
    rigdefFor(model: string): RigDefSummary | undefined {
        return this.catalogue.find((c) => c.id === model);
    }

    /** Copy the resolved highlight colours from config into the editable fields. */
    private hydrateColours(): void {
        const fd = this.config?.ft8_display;
        this.highlightUnworked = fd?.highlight_unworked ?? DEFAULT_HIGHLIGHT_UNWORKED;
        this.highlightWorked = fd?.highlight_worked ?? DEFAULT_HIGHLIGHT_WORKED;
        this.highlightCalling = fd?.highlight_calling ?? DEFAULT_HIGHLIGHT_CALLING;
    }

    /**
     * Persist the three FT8 highlight colours via PUT /v1/config. Echoes
     * logging_station + station verbatim (the daemon overwrites them
     * unconditionally — see ConfigPatch) and sends the FULL ft8_display block
     * (the daemon replaces it raw), changing only the three colours and
     * round-tripping the rest. Re-hydrates from the response on success.
     */
    async saveColours(): Promise<void> {
        if (this.savingColours || !this.config) return;
        this.savingColours = true;
        this.coloursStatus = null;
        const outcome: ConfigOutcome = await putConfig({
            logging_station: this.config.logging_station,
            station: this.config.station,
            ft8_display: {
                ...this.config.ft8_display,
                highlight_unworked: this.highlightUnworked,
                highlight_worked: this.highlightWorked,
                highlight_calling: this.highlightCalling,
            },
        });
        if (outcome.kind === 'ok') {
            this.config = outcome.config;
            this.hydrateColours();
            this.coloursStatus = 'ok';
        } else {
            this.coloursStatus = outcomeMessage(outcome);
        }
        this.savingColours = false;
    }

    /**
     * Persist the Station tab's MY_* edits via PUT /v1/config. The daemon
     * overwrites `logging_station` unconditionally, so we merge the form onto
     * the FULL logging_station block from the last GET (round-tripping the
     * operational fields — callsign / operator / grid — the logging SPA owns)
     * and echo `station` verbatim. `ft8_display` / `qsl` are presence-aware, so
     * omitting them leaves those blocks untouched. Re-hydrates on success.
     */
    async saveStation(): Promise<void> {
        if (this.savingStation || !this.config || !this.stationDirty) return;
        this.savingStation = true;
        this.stationStatus = null;
        const outcome: ConfigOutcome = await putConfig({
            logging_station: { ...this.config.logging_station, ...this.stationForm },
            station: this.config.station,
        });
        if (outcome.kind === 'ok') {
            this.config = outcome.config;
            this.stationForm = stationFormFrom(this.config.logging_station);
            this.stationStatus = 'ok';
        } else {
            this.stationStatus = outcomeMessage(outcome);
        }
        this.savingStation = false;
    }

    /** Revert the Station form to the loaded config (discard unsaved edits). */
    cancelStation(): void {
        if (this.config) this.stationForm = stationFormFrom(this.config.logging_station);
        this.stationStatus = null;
    }

    /**
     * Persist the Rigs draft via PUT /v1/config (the catalogue write path).
     * Sends the WHOLE draft catalogue + active-rig id (both presence-aware
     * daemon-side) and echoes logging_station/station verbatim so they aren't
     * wiped. The daemon validates the catalogue (validateRigs) and a bad one
     * comes back as a 400 surfaced in the footer. On success re-fetches
     * /v1/rigs (the PUT response doesn't carry the catalogue) to re-hydrate the
     * canonical view + draft.
     */
    async saveRigs(): Promise<void> {
        if (this.savingRigs || !this.config || !this.rigsDirty) return;
        this.savingRigs = true;
        this.rigsStatus = null;
        const outcome: ConfigOutcome = await putConfig({
            logging_station: this.config.logging_station,
            station: this.config.station,
            rigs: this.rigDraft,
            default_rig_id: this.draftDefaultRigId,
        });
        if (outcome.kind === 'ok') {
            this.config = outcome.config;
            const rigs = await fetchRigs();
            if (rigs.kind === 'ok') {
                this.defaultRigId = rigs.rigs.default_rig_id;
                this.rigs = rigs.rigs.rigs;
                this.catalogue = rigs.rigs.catalogue;
                this.hydrateRigs();
            }
            this.rigsStatus = 'ok';
        } else {
            this.rigsStatus = outcomeMessage(outcome);
        }
        this.savingRigs = false;
    }

    /** Discard staged Rigs edits — reset the draft to the loaded catalogue. */
    cancelRigs(): void {
        this.hydrateRigs();
        this.rigsStatus = null;
    }
}

/** A non-'ok' outcome's display string (the two API wrappers share these arms). */
function outcomeMessage(
    o: { kind: 'server'; code: string; message: string } | { kind: string; message: string }
): string {
    return 'code' in o ? `${o.kind} (${o.code})` : `${o.kind}: ${o.message}`;
}

export const configState = new ConfigState();
