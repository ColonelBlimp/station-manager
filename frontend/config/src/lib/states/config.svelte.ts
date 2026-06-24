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
    type ForwarderInfo,
    type Ft8DisplayFields,
    type LoggingStationFields,
    type LookupInfo,
    type LookupProviderInfo,
} from '../api/config';
import { fetchRigs, type RigConfig, type RigDefSummary } from '../api/rigs';
import { fetchHardware, type HardwareResponse } from '../api/hardware';
import { fetchForwarderTypes, type ForwarderType } from '../api/forwarderTypes';

// ForwarderDraft — the Forwarding tab's editable view of one destination.
// credentialsSet (from the masked GET) drives the "already set" hint; credentials
// holds the operator's newly-typed values (transient — blank = keep the stored
// secret on save).
export interface ForwarderDraft {
    name: string;
    type: string;
    enabled: boolean;
    action_filter: string[];
    credentialsSet: string[];
    credentials: Record<string, string>;
}

function forwarderDraftFrom(f: ForwarderInfo): ForwarderDraft {
    return {
        name: f.name,
        type: f.type,
        enabled: f.enabled,
        action_filter: f.action_filter ?? [],
        credentialsSet: f.credentials_set ?? [],
        credentials: {},
    };
}

/** Structural identity of a forwarder for dirty-tracking — everything the SPA
 *  edits EXCEPT the transient typed credentials (those are checked separately,
 *  since a blank/untyped credential is not a change). */
function forwarderStructKey(f: ForwarderDraft): string {
    return JSON.stringify({
        name: f.name,
        type: f.type,
        enabled: f.enabled,
        action_filter: [...f.action_filter].sort(),
    });
}

/** Deep clone of a rig catalogue, for the Rigs-tab editable draft (rig objects
 *  are JSON-safe). Keeps the draft from aliasing the loaded `rigs`. */
function cloneRigs(rigs: RigConfig[]): RigConfig[] {
    return JSON.parse(JSON.stringify(rigs)) as RigConfig[];
}

/** Ensure a draft rig has a concrete `audio` object with string rx/tx, so the
 *  Rigs-tab audio dropdowns can `bind:value` directly (no lazy-init handler).
 *  Empty strings round-trip cleanly — the daemon omits empty audio on GET, and
 *  canonRig treats '' as absent — so this never shows as a spurious change. */
function withAudio(r: RigConfig): RigConfig {
    return { ...r, audio: { rx: r.audio?.rx ?? '', tx: r.audio?.tx ?? '' } };
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
    if (r.mode_mappings && Object.keys(r.mode_mappings).length > 0) {
        // Sort keys so the comparison is order-insensitive: Go marshals map keys
        // sorted on GET, but the editor builds them in rigdef-literal order — an
        // unsorted compare would report a spurious diff for identical content.
        const mm = r.mode_mappings;
        const sorted: Record<string, unknown> = {};
        for (const k of Object.keys(mm).sort()) sorted[k] = mm[k];
        o.mode_mappings = sorted;
    }
    if (r.overrides && Object.keys(r.overrides).length > 0) {
        // Same key-order normalisation as mode_mappings: Go marshals the struct
        // fields in declaration order, the editor sets them in another — sort both
        // sides so identical override content doesn't read as a spurious diff.
        const ov = r.overrides as Record<string, unknown>;
        const sorted: Record<string, unknown> = {};
        for (const k of Object.keys(ov).sort()) sorted[k] = ov[k];
        o.overrides = sorted;
    }
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

// The QRZ callsign-lookup provider's service name (matches
// types.QRZLookupServiceName) — the chain entry the Enrichment tab's QRZ
// section maps to. The daemon defaults this provider's URL.
const QRZ_LOOKUP_PROVIDER = 'qrzlookupservice';

// LookupForm — the Enrichment tab's editable subset (option A: specific QRZ +
// Hamnut + TTL sections, not a generic chain editor). qrzPassword is the typed
// new value (transient — blank = keep the stored secret); qrzPasswordSet (from
// the masked GET) drives the "set" hint.
export interface LookupForm {
    qrzEnabled: boolean;
    qrzUsername: string;
    qrzPassword: string;
    qrzPasswordSet: boolean;
    hamnutEnabled: boolean;
    countryTtlDays: number;
    stationTtlDays: number;
    refreshMaxInFlight: number;
}

function lookupFormFrom(l: LookupInfo | null): LookupForm {
    const qrz = l?.chain.find((p) => p.name === QRZ_LOOKUP_PROVIDER);
    return {
        qrzEnabled: qrz?.enabled ?? false,
        qrzUsername: qrz?.username ?? '',
        qrzPassword: '',
        qrzPasswordSet: qrz?.password_set ?? false,
        hamnutEnabled: l?.hamnut.enabled ?? false,
        countryTtlDays: l?.country_ttl_days ?? 0,
        stationTtlDays: l?.station_ttl_days ?? 0,
        refreshMaxInFlight: l?.refresh_max_in_flight ?? 0,
    };
}

/** Structural identity of the lookup form for dirty-tracking — excludes the
 *  transient typed password (checked separately) and the GET-only set flag. */
function lookupFormKey(f: LookupForm): string {
    return JSON.stringify({
        qrzEnabled: f.qrzEnabled,
        qrzUsername: f.qrzUsername,
        hamnutEnabled: f.hamnutEnabled,
        countryTtlDays: f.countryTtlDays,
        stationTtlDays: f.stationTtlDays,
        refreshMaxInFlight: f.refreshMaxInFlight,
    });
}

// FT8 Band Activity display-pref defaults — mirror the daemon's
// ResolveFt8Display so a fresh config (block omitted) still shows real values.
// GET serves ft8_display RESOLVED, so these are only the cold-start fallback.
const DEFAULT_HIGHLIGHT_UNWORKED = '#15803d';
const DEFAULT_HIGHLIGHT_WORKED = '#9ca3af';
const DEFAULT_HIGHLIGHT_CALLING = '#b45309';
const DEFAULT_FT8_HISTORY_MAX = 100;
const DEFAULT_FT8_FEED_MODE = 'accumulate';

// Ft8Form — the editable FT8 Band Activity display prefs (the whole ft8_display
// block). feed_mode is a plain string (the daemon validates + defaults a bogus
// value); history_max is clamped daemon-side (10..2000).
export interface Ft8Form {
    history_max: number;
    feed_mode: string;
    highlight_unworked: string;
    highlight_worked: string;
    highlight_calling: string;
    cq_to_top: boolean;
    hide_hashed_calls: boolean;
}

function ft8FormFrom(d: Ft8DisplayFields | undefined): Ft8Form {
    return {
        history_max: d?.history_max ?? DEFAULT_FT8_HISTORY_MAX,
        feed_mode: d?.feed_mode ?? DEFAULT_FT8_FEED_MODE,
        highlight_unworked: d?.highlight_unworked ?? DEFAULT_HIGHLIGHT_UNWORKED,
        highlight_worked: d?.highlight_worked ?? DEFAULT_HIGHLIGHT_WORKED,
        highlight_calling: d?.highlight_calling ?? DEFAULT_HIGHLIGHT_CALLING,
        cq_to_top: d?.cq_to_top ?? false,
        hide_hashed_calls: d?.hide_hashed_calls ?? false,
    };
}

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

    // Forwarding tab — the data-driven type descriptors (GET /v1/forwarder-types),
    // the loaded destinations (masked, from /v1/config), and the editable draft.
    forwarderTypes: ForwarderType[] = $state([]);
    forwarders: ForwarderInfo[] = $state([]);
    forwarderDraft: ForwarderDraft[] = $state([]);
    /** True while a Forwarding save PUT is in flight. */
    savingForwarders: boolean = $state(false);
    /** Last Forwarding-save status: 'ok', an error message, or null (idle/in-flight). */
    forwardersStatus: string | null = $state(null);

    // Enrichment tab — the loaded (masked) lookup config + the editable form
    // (option A: specific QRZ + Hamnut + TTL sections).
    lookup: LookupInfo | null = $state(null);
    lookupForm: LookupForm = $state(lookupFormFrom(null));
    /** True while an Enrichment save PUT is in flight. */
    savingLookup: boolean = $state(false);
    /** Last Enrichment-save status: 'ok', an error message, or null (idle/in-flight). */
    lookupStatus: string | null = $state(null);

    // FT8 tab — the full Band Activity display prefs (colours + row cap + feed
    // mode + toggles), hydrated from config.ft8_display on load and re-hydrated
    // from the PUT response on save. Bound directly by the FT8 tab's inputs.
    ft8Form: Ft8Form = $state(ft8FormFrom(undefined));
    /** True while an FT8-display save PUT is in flight. */
    savingFt8: boolean = $state(false);
    /** Last FT8-save status: 'ok', an error message, or null (idle/in-flight). */
    ft8Status: string | null = $state(null);

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
        const [cfg, rigs, hw, ftypes] = await Promise.all([
            fetchConfig(),
            fetchRigs(),
            fetchHardware(),
            fetchForwarderTypes(),
        ]);

        const errs: string[] = [];
        if (cfg.kind === 'ok') {
            this.config = cfg.config;
            this.ft8Form = ft8FormFrom(this.config.ft8_display);
            this.stationForm = stationFormFrom(this.config.logging_station);
            this.forwarders = this.config.forwarders ?? [];
            this.hydrateForwarders();
            this.lookup = this.config.lookup ?? null;
            this.lookupForm = lookupFormFrom(this.lookup);
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
        if (ftypes.kind === 'ok') {
            this.forwarderTypes = ftypes.types;
        } else {
            errs.push(`forwarder-types: ${outcomeMessage(ftypes)}`);
        }

        this.error = errs.length > 0 ? errs.join('; ') : null;
        this.loaded = true;
    }

    /** Reset the Rigs draft to the loaded catalogue (initial hydrate, Cancel, and
     *  post-save re-hydrate). Preserves the current selection when it still
     *  exists, else selects the first rig (or none). */
    private hydrateRigs(): void {
        this.rigDraft = cloneRigs(this.rigs).map(withAudio);
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

    /** The model a newly-added rig should default to: the first catalogue model
     *  not already configured (so Add doesn't silently create a duplicate),
     *  falling back to the first catalogue entry when every model is already in
     *  use. The Rigs tab confirms before adding when this returns an in-use model. */
    nextRigModel(): string {
        const used = new Set(this.rigDraft.map((r) => r.model));
        const unused = this.catalogue.find((c) => !used.has(c.id));
        return (unused ?? this.catalogue[0])?.id ?? '';
    }

    /** Append a blank rig with the given model (next free id) and select it. */
    addRig(model: string): void {
        const nextId = this.rigDraft.reduce((m, r) => Math.max(m, r.id), 0) + 1;
        this.rigDraft = [...this.rigDraft, withAudio({ id: nextId, model, port: '' })];
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

    /** True when the FT8 display form diverges from the loaded config. */
    get ft8Dirty(): boolean {
        if (!this.config) return false;
        return (
            JSON.stringify(this.ft8Form) !== JSON.stringify(ft8FormFrom(this.config.ft8_display))
        );
    }

    /**
     * Persist the FT8 Band Activity display prefs via PUT /v1/config. Sends the
     * FULL ft8_display block (the daemon replaces it raw, then clamps/validates +
     * returns it resolved) and echoes logging_station/station verbatim (the
     * daemon overwrites those unconditionally — see ConfigPatch). Re-hydrates from
     * the response on success.
     */
    async saveFt8(): Promise<void> {
        if (this.savingFt8 || !this.config || !this.ft8Dirty) return;
        this.savingFt8 = true;
        this.ft8Status = null;
        const outcome: ConfigOutcome = await putConfig({
            logging_station: this.config.logging_station,
            station: this.config.station,
            ft8_display: { ...this.ft8Form },
        });
        if (outcome.kind === 'ok') {
            this.config = outcome.config;
            this.ft8Form = ft8FormFrom(this.config.ft8_display);
            this.ft8Status = 'ok';
        } else {
            this.ft8Status = outcomeMessage(outcome);
        }
        this.savingFt8 = false;
    }

    /** Revert the FT8 display form to the loaded config (discard unsaved edits). */
    cancelFt8(): void {
        if (this.config) this.ft8Form = ft8FormFrom(this.config.ft8_display);
        this.ft8Status = null;
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

    // ── Forwarding tab ─────────────────────────────────────────────────────────

    /** Reset the forwarders draft to the loaded list (initial hydrate, Cancel,
     *  post-save re-hydrate). Clears any typed-but-unsaved credentials. */
    private hydrateForwarders(): void {
        this.forwarderDraft = this.forwarders.map(forwarderDraftFrom);
    }

    /** The descriptor for a forwarder type, or undefined if the type isn't
     *  registered in this daemon (a stale config entry). */
    forwarderTypeFor(type: string): ForwarderType | undefined {
        return this.forwarderTypes.find((t) => t.type === type);
    }

    /** True when the forwarders draft diverges from the loaded list — a
     *  structural change (add/remove/rename/type/enabled/action) OR any typed
     *  credential value. Drives Save/Cancel. */
    get forwardersDirty(): boolean {
        const draftKeys = this.forwarderDraft.map(forwarderStructKey);
        const loadedKeys = this.forwarders.map((f) => forwarderStructKey(forwarderDraftFrom(f)));
        if (JSON.stringify(draftKeys) !== JSON.stringify(loadedKeys)) return true;
        return this.forwarderDraft.some((f) =>
            Object.values(f.credentials).some((v) => v.trim() !== '')
        );
    }

    /** Append a destination of the given type — name defaults to a unique handle
     *  (type, then type-2…), action_filter to the type's supported actions, and
     *  enabled true. */
    addForwarder(type: string): void {
        const used = new Set(this.forwarderDraft.map((f) => f.name));
        let name = type;
        for (let n = 2; used.has(name); n++) name = `${type}-${n}`;
        const td = this.forwarderTypeFor(type);
        this.forwarderDraft = [
            ...this.forwarderDraft,
            {
                name,
                type,
                enabled: true,
                action_filter: td ? [...td.supported_actions] : [],
                credentialsSet: [],
                credentials: {},
            },
        ];
    }

    removeForwarder(index: number): void {
        this.forwarderDraft = this.forwarderDraft.filter((_, i) => i !== index);
    }

    /**
     * Persist the forwarders draft via PUT /v1/config. Sends the whole list
     * (presence-aware daemon-side) with only the NON-EMPTY typed credentials per
     * destination — the daemon merges them onto the stored secrets, so a blank
     * field keeps its value. Echoes logging_station/station; omits rigs/ft8 so
     * those are left untouched. Re-hydrates from the (masked) PUT response.
     */
    async saveForwarders(): Promise<void> {
        if (this.savingForwarders || !this.config || !this.forwardersDirty) return;
        this.savingForwarders = true;
        this.forwardersStatus = null;
        const payload: ForwarderInfo[] = this.forwarderDraft.map((f) => {
            const creds: Record<string, string> = {};
            for (const [k, v] of Object.entries(f.credentials)) {
                if (v.trim() !== '') creds[k] = v;
            }
            return {
                name: f.name,
                type: f.type,
                enabled: f.enabled,
                action_filter: f.action_filter,
                ...(Object.keys(creds).length > 0 ? { credentials: creds } : {}),
            };
        });
        const outcome: ConfigOutcome = await putConfig({
            logging_station: this.config.logging_station,
            station: this.config.station,
            forwarders: payload,
        });
        if (outcome.kind === 'ok') {
            this.config = outcome.config;
            this.forwarders = outcome.config.forwarders ?? [];
            this.hydrateForwarders();
            this.forwardersStatus = 'ok';
        } else {
            this.forwardersStatus = outcomeMessage(outcome);
        }
        this.savingForwarders = false;
    }

    /** Discard staged forwarder edits — reset the draft to the loaded list. */
    cancelForwarders(): void {
        this.hydrateForwarders();
        this.forwardersStatus = null;
    }

    // ── Enrichment tab ─────────────────────────────────────────────────────────

    /** True when the Enrichment form diverges from the loaded config — a
     *  structural change OR a freshly-typed QRZ password. */
    get lookupDirty(): boolean {
        if (!this.config) return false;
        const base = lookupFormFrom(this.lookup);
        return (
            lookupFormKey(this.lookupForm) !== lookupFormKey(base) ||
            this.lookupForm.qrzPassword.trim() !== ''
        );
    }

    /**
     * Persist the Enrichment form via PUT /v1/config. Overlays the form's edits
     * (QRZ enabled/username/password, hamnut enabled, TTLs) onto a copy of the
     * loaded lookup block — preserving daemon-defaulted URLs/timeouts and any
     * other chain entries — and sends only a non-empty typed QRZ password (the
     * daemon merges it onto the stored secret). Re-hydrates from the (masked)
     * response.
     */
    async saveLookup(): Promise<void> {
        if (this.savingLookup || !this.config || !this.lookupDirty) return;
        this.savingLookup = true;
        this.lookupStatus = null;

        const base = this.lookup;
        const hamnut: LookupProviderInfo = {
            ...(base?.hamnut ?? { name: 'hamnutlookupservice', enabled: false }),
            enabled: this.lookupForm.hamnutEnabled,
        };
        const chain: LookupProviderInfo[] = (base?.chain ?? []).map((p) => ({ ...p }));
        let qrz = chain.find((p) => p.name === QRZ_LOOKUP_PROVIDER);
        if (!qrz) {
            qrz = { name: QRZ_LOOKUP_PROVIDER, enabled: false };
            chain.push(qrz);
        }
        qrz.enabled = this.lookupForm.qrzEnabled;
        qrz.username = this.lookupForm.qrzUsername;
        if (this.lookupForm.qrzPassword.trim() !== '') qrz.password = this.lookupForm.qrzPassword;
        // password_set is GET-only; the daemon ignores it on PUT, but drop it for cleanliness.
        for (const p of chain) delete p.password_set;
        delete hamnut.password_set;

        const payload: LookupInfo = {
            hamnut,
            chain,
            country_ttl_days: this.lookupForm.countryTtlDays,
            station_ttl_days: this.lookupForm.stationTtlDays,
            refresh_max_in_flight: this.lookupForm.refreshMaxInFlight,
        };
        const outcome: ConfigOutcome = await putConfig({
            logging_station: this.config.logging_station,
            station: this.config.station,
            lookup: payload,
        });
        if (outcome.kind === 'ok') {
            this.config = outcome.config;
            this.lookup = outcome.config.lookup ?? null;
            this.lookupForm = lookupFormFrom(this.lookup);
            this.lookupStatus = 'ok';
        } else {
            this.lookupStatus = outcomeMessage(outcome);
        }
        this.savingLookup = false;
    }

    /** Discard staged Enrichment edits — reset the form to the loaded config. */
    cancelLookup(): void {
        this.lookupForm = lookupFormFrom(this.lookup);
        this.lookupStatus = null;
    }
}

/** A non-'ok' outcome's display string (the two API wrappers share these arms). */
function outcomeMessage(
    o: { kind: 'server'; code: string; message: string } | { kind: string; message: string }
): string {
    return 'code' in o ? `${o.kind} (${o.code})` : `${o.kind}: ${o.message}`;
}

export const configState = new ConfigState();
