/*
    Rig reader for the app Settings → Rigs section (ADR 0044). GET /v1/rigs
    returns the configured rigs, the active-rig id, and the rigdef catalogue. A
    configured rig's `model` is a rigdef id (e.g. "yaesu-ftdx10"); the catalogue
    resolves it to the friendly name + identity + defaults the read-only detail
    panel shows. The WRITE path (edit model/port/audio, set-default, add/delete)
    + the discovered-device lists (/v1/hardware) land with the editable panel.
    Mirrors the daemon DTOs (internal/api/handler_rigs.go, internal/types/rig.go)
    and the config SPA's api/rigs.ts.
*/
import { safeFetch, readJsonBody, isShape, isPlainObject } from './_helpers';

/** One rig-mode literal's ADIF mapping (mirrors types.ModeMapping): an ADIF MODE
 *  and an optional SUBMODE refinement. */
export interface ModeMapping {
    mode: string;
    submode?: string;
}

/** Per-rig serial overrides (mirrors types.RigOverrides). Each zero/absent field
 *  inherits the rigdef default; the bridge composes the effective serial.Config
 *  at open (internal/rigserial). */
export interface RigOverrides {
    baud_rate?: number;
    data_bits?: number;
    stop_bits?: number;
    parity?: string;
    line_delimiter?: string;
    read_timeout_ms?: number;
}

/** A configured rig (subset of types.RigConfig — the fields the panel needs). */
export interface RigConfig {
    id: number;
    model: string;
    port: string;
    audio?: { rx?: string; tx?: string };
    ft8_mode?: string | null;
    my_rig?: string | null;
    // Operator override layer keyed by the rig's own mode literal (e.g. "DATA-U").
    // Absent → inherit the rigdef defaults. Carried through fetch untouched (raw
    // passthrough) and edited by ModeMappingsEditor; the save diff treats the whole
    // map as one field.
    mode_mappings?: Record<string, ModeMapping>;
    // Per-rig serial parameter overrides (baud/framing/delimiter). Absent → inherit
    // the rigdef serial defaults. Same raw-passthrough + edited-by-SerialOverridesEditor
    // + one-field-in-the-save-diff treatment as mode_mappings.
    overrides?: RigOverrides;
}

/** A rigdef's serial defaults (subset of cat.RigSerial). */
export interface RigSerial {
    baud_rate?: number;
    data_bits?: number;
    stop_bits?: number;
    parity?: string;
    line_delimiter?: string;
}

/** The catalogue projection of a rigdef (subset of api.RigDefSummary) the detail
 *  panel reads: the friendly name + identity + defaults for a configured rig. */
export interface RigDef {
    name: string;
    manufacturer?: string;
    model?: string;
    description?: string;
    ft8_mode?: string;
    rig_modes?: string[];
    // The rigdef's DEFAULT mode mappings (rig literal → ADIF) that a rig's
    // per-rig mode_mappings override. ModeMappingsEditor shows these as the
    // baseline each row inherits until the operator overrides it.
    mode_mappings?: Record<string, ModeMapping>;
    serial?: RigSerial;
}

export interface RigsData {
    defaultRigId: number;
    rigs: RigConfig[];
    /** rigdef id (a rig's `model`) → its catalogue entry. */
    catalogue: Record<string, RigDef>;
}

export type RigsOutcome = { kind: 'ok'; data: RigsData } | { kind: 'error'; message: string };

function asString(v: unknown): string | undefined {
    return typeof v === 'string' ? v : undefined;
}
function asNumber(v: unknown): number | undefined {
    return typeof v === 'number' ? v : undefined;
}

function parseSerial(v: unknown): RigSerial | undefined {
    if (!isPlainObject(v)) return undefined;
    return {
        baud_rate: asNumber(v.baud_rate),
        data_bits: asNumber(v.data_bits),
        stop_bits: asNumber(v.stop_bits),
        parity: asString(v.parity),
        line_delimiter: asString(v.line_delimiter),
    };
}

// Parse a mode-mappings object (rig literal → {mode, submode?}); skips entries
// without a string MODE. Used for both the rigdef defaults (catalogue) and — via
// the raw rig passthrough — a rig's overrides. Returns undefined when empty so an
// absent map reads as "no mappings" rather than an empty object.
function parseModeMappings(v: unknown): Record<string, ModeMapping> | undefined {
    if (!isPlainObject(v)) return undefined;
    const out: Record<string, ModeMapping> = {};
    for (const [lit, m] of Object.entries(v)) {
        if (!isPlainObject(m) || typeof m.mode !== 'string') continue;
        out[lit] =
            typeof m.submode === 'string' ? { mode: m.mode, submode: m.submode } : { mode: m.mode };
    }
    return Object.keys(out).length > 0 ? out : undefined;
}

// catalogueById builds the rigdef-id → catalogue-entry map. Only entries with a
// string id + name are kept; a rig whose model isn't found falls back to the id.
function catalogueById(catalogue: unknown): Record<string, RigDef> {
    const out: Record<string, RigDef> = {};
    if (Array.isArray(catalogue)) {
        for (const e of catalogue) {
            if (!isPlainObject(e) || typeof e.id !== 'string' || typeof e.name !== 'string')
                continue;
            out[e.id] = {
                name: e.name,
                manufacturer: asString(e.manufacturer),
                model: asString(e.model),
                description: asString(e.description),
                ft8_mode: asString(e.ft8_mode),
                rig_modes: Array.isArray(e.rig_modes)
                    ? e.rig_modes.filter((m): m is string => typeof m === 'string')
                    : undefined,
                mode_mappings: parseModeMappings(e.mode_mappings),
                serial: parseSerial(e.serial),
            };
        }
    }
    return out;
}

export type RigsSaveOutcome = { kind: 'ok' } | { kind: 'error'; message: string };

/**
 * Save the rig catalogue via PUT /v1/config. The daemon WHOLE-REPLACES base.Rigs
 * with what's sent (handler_config.go), so `rigs` MUST be the full list with
 * every rig's every field intact — callers pass the raw objects from fetchRigs
 * (never a reconstructed subset), so fields the panel doesn't render (overrides,
 * ft8_mode, my_rig) round-trip losslessly through JSON.stringify, and the ones it
 * DOES edit (port, audio, mode_mappings) are patched onto those raw objects.
 * `default_rig_id` is OPTIONAL and OMITTED by the connection
 * editor (which doesn't change the active rig): it's presence-aware daemon-side,
 * so omitting it leaves the active-rig selection untouched rather than
 * clobbering a concurrent change to it (review 2026-07-20 Rigs-editor #1). Both
 * blocks are presence-aware; sending only these leaves the rest of the config
 * alone.
 */
export async function saveRigs(
    rigs: RigConfig[],
    defaultRigId?: number,
    signal?: AbortSignal
): Promise<RigsSaveOutcome> {
    const patch: { rigs: RigConfig[]; default_rig_id?: number } = { rigs };
    if (defaultRigId !== undefined) patch.default_rig_id = defaultRigId;
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
        signal,
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const err = isPlainObject(body) ? (body as { message?: string }) : null;
        return { kind: 'error', message: err?.message ?? `HTTP ${fetched.response.status}` };
    }
    return { kind: 'ok' };
}

export async function fetchRigs(signal?: AbortSignal): Promise<RigsOutcome> {
    const fetched = await safeFetch('/v1/rigs', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isShape<{ default_rig_id: number; rigs: RigConfig[] }>(body, ['default_rig_id', 'rigs'])) {
        return { kind: 'error', message: 'malformed /v1/rigs response' };
    }
    return {
        kind: 'ok',
        data: {
            defaultRigId: typeof body.default_rig_id === 'number' ? body.default_rig_id : 0,
            rigs: Array.isArray(body.rigs) ? body.rigs : [],
            catalogue: catalogueById((body as { catalogue?: unknown }).catalogue),
        },
    };
}
