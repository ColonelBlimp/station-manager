/*
    Station-scoped /v1/config read + write for the app Settings view (ADR 0044).
    The Station section edits the `logging_station` block (ADIF MY_* identity);
    this reads it plus the operational `station` block and writes both back.

    Data-safety contract — VERIFIED against the daemon's overlayConfig
    (internal/api/handler_config.go): a PUT replaces a block WHOLE when the field
    is present, and leaves it untouched when the field is omitted (nil). Two
    consequences this module relies on:
      - `logging_station` is round-tripped in FULL: every field the GET returned
        rides back — including the daemon-derived my_lat/my_lon and any field the
        form doesn't render — because an omitted field would be zeroed.
      - the operational `station` block (amp/power/bands) is NOT sent at all. The
        Station section never edits it, and echoing a value captured at load time
        would CLOBBER a concurrent change made in another tab/client between our
        GET and PUT (review 2026-07-20 #3). Omitting it leaves the daemon's
        current operational block untouched.
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';

// Every logging_station value is a string (ADIF MY_* fields), so a string map
// both types the known fields and preserves any the form doesn't render.
export type StationFields = Record<string, string>;

export interface StationConfig {
    /** The full logging_station block (round-tripped losslessly). */
    station: StationFields;
}

export type StationOutcome =
    | { kind: 'ok'; config: StationConfig }
    | { kind: 'error'; message: string };

// toStringMap normalises the received block to a string map. logging_station is
// all-string by the daemon's types.LoggingStation, so this is defensive rather
// than lossy — a stray non-string coerces to its string form for the text form.
function toStringMap(v: unknown): StationFields {
    const out: StationFields = {};
    if (isPlainObject(v)) {
        for (const [k, val] of Object.entries(v)) {
            if (typeof val === 'string') out[k] = val;
            else if (typeof val === 'number' || typeof val === 'boolean') out[k] = String(val);
            // objects/arrays/null are dropped — logging_station is all-string, so
            // this can't happen; a text-form string map can't represent them anyway.
        }
    }
    return out;
}

function parseConfig(body: unknown): StationConfig | null {
    if (!isPlainObject(body)) return null;
    return { station: toStringMap(body.logging_station) };
}

export async function fetchStation(signal?: AbortSignal): Promise<StationOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const config = parseConfig(await readJsonBody(fetched.response));
    if (!config) return { kind: 'error', message: 'malformed /v1/config response' };
    return { kind: 'ok', config };
}

export async function saveStation(
    cfg: StationConfig,
    signal?: AbortSignal,
): Promise<StationOutcome> {
    // Only logging_station — never the operational `station` block (see the
    // data-safety note above). The daemon leaves omitted blocks untouched.
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ logging_station: cfg.station }),
        signal,
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const err = isPlainObject(body) ? (body as { message?: string }) : null;
        return { kind: 'error', message: err?.message ?? `HTTP ${fetched.response.status}` };
    }
    // Re-apply the daemon's authoritative post-save view: it re-derives my_lat/
    // my_lon from the grid square, so the form should reflect what was stored.
    const config = parseConfig(body);
    return config
        ? { kind: 'ok', config }
        : { kind: 'error', message: 'malformed save response' };
}
