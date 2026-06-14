/*
    Daemon-side wrapper for `GET /v1/rigs` — the rig-profiles editor's data
    surface (ADR 0028 / config.md §10). Returns a discriminated union so callers
    branch on outcome without parsing envelopes inline.

    The endpoint is always 200 (config editing, no validation errors), so any
    non-2xx collapses to 'server'. The write path (save a profile / set default)
    lands with the editor.

    Types mirror the daemon DTOs (internal/api/handler_rigs.go +
    internal/types/rig.go). Override fields are nullable/optional: a `null` or
    absent `ft8_mode` / `my_rig` / `mode_mappings` / `overrides` means "inherit
    the rigdef default" — the editor resolves the effective value by joining
    `rig.model` → `catalogue[].id`.
*/

import { isPlainObject, isShape, readJsonBody, safeFetch } from './_helpers';

export interface ModeMapping {
    mode: string;
    submode?: string;
}

/** Per-rig serial overrides (mirrors types.RigOverrides). Each zero/absent field inherits the rigdef default. */
export interface RigOverrides {
    baud_rate?: number;
    data_bits?: number;
    stop_bits?: number;
    parity?: string;
    line_delimiter?: string;
    read_timeout_ms?: number;
}

/** A configured rig (mirrors types.RigConfig). Overrides null/absent = inherit. */
export interface RigConfig {
    id: number;
    model: string;
    port: string;
    audio?: { device?: string };
    ft8_mode?: string | null;
    mode_mappings?: Record<string, ModeMapping>;
    my_rig?: string | null;
    overrides?: RigOverrides;
}

/** A rigdef's serial defaults (mirrors cat.RigSerial). */
export interface RigSerial {
    baud_rate: number;
    data_bits: number;
    stop_bits: number;
    parity: string;
    line_delimiter: string;
    read_timeout_ms: number;
    write_timeout_ms?: number;
    rts?: boolean;
    dtr?: boolean;
}

/** The editor-facing rigdef projection (mirrors api.RigDefSummary) — defaults only, no CAT tables. */
export interface RigDefSummary {
    id: string;
    name: string;
    manufacturer: string;
    model: string;
    family: string;
    description?: string;
    ft8_mode?: string;
    rig_modes?: string[];
    mode_mappings?: Record<string, ModeMapping>;
    serial: RigSerial;
}

export interface RigsResponse {
    default_rig_id: number;
    rigs: RigConfig[];
    catalogue: RigDefSummary[];
}

export type RigsOutcome =
    | { kind: 'ok'; rigs: RigsResponse }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

export async function fetchRigs(signal?: AbortSignal): Promise<RigsOutcome> {
    const fetched = await safeFetch('/v1/rigs', { signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const body = await readJsonBody(fetched.response);
    if (fetched.response.ok) {
        if (!isShape<RigsResponse>(body, ['default_rig_id', 'rigs', 'catalogue'])) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned /v1/rigs without the rigs / catalogue blocks',
            };
        }
        return { kind: 'ok', rigs: body };
    }
    // /v1/rigs is always 200; a non-2xx is a server-side fault.
    const err = isPlainObject(body) ? (body as { code?: string; message?: string }) : null;
    return {
        kind: 'server',
        code: err?.code ?? 'unknown_error',
        message: err?.message ?? `HTTP ${fetched.response.status}`,
    };
}
