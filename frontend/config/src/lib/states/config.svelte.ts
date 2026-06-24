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

    /** Rig-profiles editor data (GET /v1/rigs). */
    defaultRigId: number = $state(0);
    rigs: RigConfig[] = $state([]);
    catalogue: RigDefSummary[] = $state([]);

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
     * Fetch /v1/config and /v1/rigs concurrently and hydrate. Either failing
     * sets `error` but whatever succeeded is still applied, so a partially
     * healthy daemon still renders. Idempotent enough to call once on mount.
     */
    async load(): Promise<void> {
        const [cfg, rigs] = await Promise.all([fetchConfig(), fetchRigs()]);

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
        } else {
            errs.push(`rigs: ${outcomeMessage(rigs)}`);
        }

        this.error = errs.length > 0 ? errs.join('; ') : null;
        this.loaded = true;
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
}

/** A non-'ok' outcome's display string (the two API wrappers share these arms). */
function outcomeMessage(
    o: { kind: 'server'; code: string; message: string } | { kind: string; message: string }
): string {
    return 'code' in o ? `${o.kind} (${o.code})` : `${o.kind}: ${o.message}`;
}

export const configState = new ConfigState();
