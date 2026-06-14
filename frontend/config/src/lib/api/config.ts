/*
    Daemon-side wrapper for `GET /v1/config` — the config SPA's broader config
    surface (station identity, FT8 display prefs, mailer/bridge projections).
    Same discriminated-union + envelope-guard conventions as rigs.ts and the
    logging SPA's config.ts.

    The ConfigResponse type below is a PRAGMATIC SUBSET of the daemon's
    ConfigResponse (handler_config.go): the blocks the config SPA's panels will
    edit or display. Extend it as panels land — don't port fields nothing renders.
    Optional fields use `?:` because the daemon's `omitempty` tags omit empties.

    The write path (PUT /v1/config) lands with the panels that need it; this
    slice wires the read only.
*/

import { isShape, readJsonBody, safeFetch, isPlainObject } from './_helpers';

export interface LoggingStationFields {
    station_callsign?: string;
    operator?: string;
    owner_callsign?: string;
    my_gridsquare?: string;
    my_name?: string;
    my_antenna?: string;
    my_cq_zone?: string;
    my_itu_zone?: string;
    my_dxcc?: string;
}

export interface StationFields {
    enabled?: boolean;
    default_power?: number;
    amp_enabled?: boolean;
    amp_multiplier?: number;
}

export interface Ft8DisplayFields {
    history_max?: number;
    feed_mode?: 'accumulate' | 'single';
    highlight_unworked?: string;
    highlight_worked?: string;
}

export interface MailerFields {
    enabled: boolean;
    default_recipient?: string;
}

export interface DefaultLogbookFields {
    id: number;
    name?: string;
    callsign?: string;
    description?: string;
}

export interface ConfigResponse {
    setup_complete: boolean;
    logging_station: LoggingStationFields;
    default_logbook: DefaultLogbookFields;
    station: StationFields;
    mailer: MailerFields;
    ft8_display?: Ft8DisplayFields;
    ft8_frequencies?: Record<string, number>;
}

export type ConfigOutcome =
    | { kind: 'ok'; config: ConfigResponse }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

export async function fetchConfig(signal?: AbortSignal): Promise<ConfigOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const body = await readJsonBody(fetched.response);
    if (fetched.response.ok) {
        if (
            !isShape<ConfigResponse>(body, ['setup_complete', 'logging_station', 'default_logbook'])
        ) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned /v1/config without the required blocks',
            };
        }
        return { kind: 'ok', config: body };
    }
    const err = isPlainObject(body) ? (body as { code?: string; message?: string }) : null;
    return {
        kind: 'server',
        code: err?.code ?? 'unknown_error',
        message: err?.message ?? `HTTP ${fetched.response.status}`,
    };
}
