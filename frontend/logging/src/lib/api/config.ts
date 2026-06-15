/*
    Thin daemon-side wrapper for `GET /v1/config` and `PUT /v1/config`.
    Returns a discriminated union so callers can branch on outcome
    without parsing HTTP / JSON envelopes inline. Same shape conventions
    as `lib/api/qso.ts` for consistency.

    Wire contract (api.md §7a Config):
      - 200 OK       → ConfigResponse
      - 4xx          → { code, message, op? }   (validation / client error)
      - 5xx          → { code, message, op? }   (server / disk-write error)

    `kind` collapses to:
      - 'ok'         — config received / written; caller hydrates state
      - 'validation' — 4xx with a daemon-emitted code (e.g.
                       'invalid_field_value' for malformed callsign).
      - 'server'     — 5xx; daemon logged a stack-tagged error.
      - 'aborted'    — caller cancelled via AbortSignal before a response.
      - 'network'    — fetch threw before a Response.

    Field shapes mirror the daemon's `ConfigResponse` (handler_config.go).
    Optional fields use TypeScript `?:` because the daemon's `omitempty`
    JSON tags omit empty strings — so the wire payload may not include
    a populated field when its value is empty.
*/

import { isPlainObject, isShape, readJsonBody, safeFetch } from './_helpers';

export interface ConfigResponse {
    setup_complete: boolean;
    logging_station: LoggingStationFields;
    default_logbook: DefaultLogbookFields;
    default_rig: DefaultRigFields;
    station: StationFields;
    bridge: BridgeFields;
    mailer: MailerFields;
    /**
     * FT8 Band Activity display preferences (row cap, feed mode, CQ highlight
     * colours). Operator-writable, daemon-resolved (always present on GET with
     * defaults filled). Optional on the type for forward-compat with a daemon
     * build that predates it. PUT it back to persist (presence-aware on the
     * daemon — omitting it leaves the stored block untouched).
     */
    ft8_display?: Ft8DisplayFields;
    /**
     * FT8 per-band dial frequencies (band label → Hz, e.g. `{"20m": 14074000}`),
     * daemon-resolved (defaults + operator overrides, always present on GET). Read by
     * the Main-Freq band buttons to tune + highlight. Optional for forward-compat with
     * a daemon build that predates it. Read-only over /v1/config (edited in config.json).
     */
    ft8_frequencies?: Record<string, number>;
}

export interface Ft8DisplayFields {
    history_max?: number;
    feed_mode?: 'accumulate' | 'single';
    highlight_unworked?: string;
    highlight_worked?: string;
}

export interface BridgeFields {
    /**
     * Whether the daemon's bridge subsystem is enabled. The SPA
     * mirrors this into configState.station.enabled so the three-flag
     * isLive rule (ADR 0009) reflects the operator's persisted intent.
     * Flipping bridge.enabled in config.json + restarting the daemon
     * is how CAT is turned on/off — same "operator owns config.json
     * directly" pattern as the SMTP-creds / hardware-config blocks.
     */
    enabled: boolean;
    /**
     * Rig-driver id (e.g. "yaesu-ftdx10"). Used SPA-side to key into
     * per-rig sub-maps (Mode Mappings). Empty when bridge is disabled
     * or unconfigured.
     */
    driver?: string;
    /**
     * Rigdef's human-readable name (e.g. "Yaesu FTdx10") resolved
     * daemon-side from the driver. Used by My Station Equipment panel
     * + ADIF MY_RIG fallback.
     */
    rig_name?: string;
    /**
     * Mode strings the configured rigdef can push (e.g. ["LSB","USB",
     * "CW-U","DATA-U",...]). Used to render one row per rig mode in
     * the My Station → Mode Mappings sub-tab.
     */
    rig_modes?: string[];
    /**
     * Semantic ops the configured rig exposes for the inbound command
     * path (ADR 0026), e.g. ["set_freq","set_mode","swap_vfo"]. The SPA
     * gates rig-control affordances on membership here. Empty when the
     * rig defines no exposed commands.
     */
    ops?: string[];
    /**
     * Whether the configured rig can run the tune-carrier feature (ADR
     * 0027) — rigdef-derived (the rig defines set_mode/set_power/tx_on/
     * tx_off). The SPA shows the Tune button only when true; absent
     * (treated as false) for a rig that can't tune (e.g. the FT-710).
     */
    tune?: boolean;
    /**
     * Merged view of the rigdef's shipped defaults + the operator's
     * overrides for the configured driver. Keyed by rig mode string;
     * value is an ADIF (MODE, SUBMODE) pair. The SPA consumes this
     * for displayedState mode resolution (catState.mode → ADIF MODE)
     * and for the Mode Mappings sub-tab's edit form.
     */
    mode_mappings?: Record<string, AdifModePair>;
    /**
     * Rig CAT mode literal for FT8 (e.g. "USB-D" on the IC-7300, "DATA-U" on the
     * FTdx10) — rigdef default, overridable per-rig. The FT8 Main-Freq band
     * buttons drive set_mode with it so picking a band also asserts data mode.
     * Absent when no driver is configured.
     */
    ft8_mode?: string;
}

export interface AdifModePair {
    /** ADIF MODE (e.g. "SSB", "CW", "FT8"). Always non-empty. */
    mode: string;
    /** ADIF SUBMODE (e.g. "USB", "PSK31"). Empty when no refinement. */
    submode?: string;
}

export interface MailerFields {
    /**
     * Whether the daemon's SMTP block is configured (Host non-empty).
     * SessionPanel hides its email controls when false — there's no
     * point letting the operator click a button that always 503s.
     */
    enabled: boolean;
    /**
     * Operator's pre-configured QSL-manager / default recipient. Used
     * to pre-fill the SessionPanel recipient input. Empty string when
     * unset; SPA falls through to "operator types it every time".
     */
    default_recipient?: string;
}

export interface StationFields {
    /** Whether the linear-amp multiplier is applied to TX power. */
    amp_enabled: boolean;
    /** Linear-amp gain factor (e.g. 10 → 50W rig becomes 500W effective). */
    amp_multiplier: number;
    /** TX power in watts when CAT is unavailable; 0 = not set (TX_PWR omitted). */
    default_power: number;
}

export interface LoggingStationFields {
    // Identity fallback chain — daemon enforces, SPA mirrors.
    station_callsign?: string;
    operator?: string;
    owner_callsign?: string;

    // Operator-typed via the My Station panel (session 34 scope).
    my_altitude?: string;
    my_antenna?: string;
    my_city?: string;
    my_country?: string;
    my_cq_zone?: string;
    my_dxcc?: string;
    my_gridsquare?: string;
    my_itu_zone?: string;
    my_morse_key_info?: string;
    my_morse_key_type?: string;
    my_name?: string;
    my_postal_code?: string;
    my_rig?: string;
    my_street?: string;

    // Daemon-derived from my_gridsquare; surfaced read-only in the SPA.
    my_lat?: string;
    my_lon?: string;

    // Per-QSO calculated client-side (bearing utility, task #47); part
    // of types.LoggingStation but normally absent from /v1/config.
    ant_az?: string;

    // Per-activation, deferred (not surfaced in My Station yet but
    // present on types.LoggingStation so the wire shape stays in
    // parity with the daemon).
    my_iota?: string;
    my_iota_island_id?: string;
    my_sig?: string;
    my_sig_info?: string;
    my_wwff_ref?: string;
}

export interface DefaultLogbookFields {
    id: number;
    name?: string;
    callsign?: string;
    description?: string;
}

export interface DefaultRigFields {
    id: number;
    model?: string;
    port?: string;
}

export type ConfigOutcome =
    | { kind: 'ok'; config: ConfigResponse }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function fetchConfig(signal?: AbortSignal): Promise<ConfigOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    return parseOutcome(fetched.response);
}

export async function putConfig(
    payload: Partial<
        Pick<
            ConfigResponse,
            | 'logging_station'
            | 'default_logbook'
            | 'default_rig'
            | 'station'
            | 'bridge'
            | 'ft8_display'
        >
    >,
    signal?: AbortSignal
): Promise<ConfigOutcome> {
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    return parseOutcome(fetched.response);
}

async function parseOutcome(response: Response): Promise<ConfigOutcome> {
    const body = await readJsonBody(response);

    if (response.ok) {
        // Guard against a 200 OK with an unparseable / non-object body
        // — without this the caller would deref `config.logging_station`
        // on null and crash. Treat as a server-side malformed response.
        if (!isPlainObject(body)) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned a non-JSON or empty body for /v1/config',
            };
        }
        // `applyResponse` dereferences these three containers unconditionally
        // (`resp.logging_station.operator`, `default_logbook.id`,
        // `default_rig.id`), so a 200 missing any of them would crash
        // hydration. Surface it as a controlled malformed response instead
        // (review 2026-06-04 M4). station / bridge / mailer are NOT required
        // here — applyResponse already guards each with `if (resp.X)`.
        if (!isShape<ConfigResponse>(body, ['logging_station', 'default_logbook', 'default_rig'])) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message:
                    'daemon returned /v1/config without the required station / logbook / rig blocks',
            };
        }
        return { kind: 'ok', config: body as unknown as ConfigResponse };
    }

    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
