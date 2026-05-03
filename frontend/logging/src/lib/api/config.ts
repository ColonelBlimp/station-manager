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
}

export interface LoggingStationFields {
    station_callsign?: string;
    // Additional ADIF MY_* fields appear here as the My Station
    // card surfaces them. The daemon's types.LoggingStation has the
    // full set; the SPA reads only what it needs.
    [key: string]: string | undefined;
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
    payload: Partial<Pick<ConfigResponse, 'logging_station' | 'default_logbook' | 'default_rig'>>
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
