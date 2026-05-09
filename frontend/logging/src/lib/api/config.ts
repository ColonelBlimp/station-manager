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
      - 'network'    — fetch threw before a Response.

    Field shapes mirror the daemon's `ConfigResponse` (handler_config.go).
    Optional fields use TypeScript `?:` because the daemon's `omitempty`
    JSON tags omit empty strings — so the wire payload may not include
    a populated field when its value is empty.
*/

export interface ConfigResponse {
    setup_complete: boolean;
    logging_station: LoggingStationFields;
    default_logbook: DefaultLogbookFields;
    default_rig: DefaultRigFields;
    station: StationFields;
    mailer: MailerFields;
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
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function fetchConfig(): Promise<ConfigOutcome> {
    let response: Response;
    try {
        response = await fetch('/v1/config');
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { kind: 'network', message };
    }
    return parseOutcome(response);
}

export async function putConfig(
    payload: Partial<
        Pick<ConfigResponse, 'logging_station' | 'default_logbook' | 'default_rig' | 'station'>
    >
): Promise<ConfigOutcome> {
    let response: Response;
    try {
        response = await fetch('/v1/config', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { kind: 'network', message };
    }
    return parseOutcome(response);
}

async function parseOutcome(response: Response): Promise<ConfigOutcome> {
    let body: ConfigResponse | DaemonError | null;
    try {
        body = (await response.json()) as ConfigResponse | DaemonError;
    } catch {
        body = null;
    }

    if (response.ok) {
        return { kind: 'ok', config: body as ConfigResponse };
    }

    const err = body as DaemonError | null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
