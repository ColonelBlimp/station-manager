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
import { isDurabilityUnconfirmed } from '../config/durability';

// Every logging_station value is a string (ADIF MY_* fields), so a string map
// both types the known fields and preserves any the form doesn't render.
export type StationFields = Record<string, string>;

// The standing outgoing-QSL defaults (config `qsl` block = types.QslDefaults,
// exactly these three fields — ADIF QSL_VIA / QSLMSG / QSL_SENT_VIA). Saved WITH
// the Station section, like the retired config SPA folded them into its Station
// tab. Whole-block replace is safe because the block has no other fields.
export interface QslFields {
    qsl_via: string;
    qslmsg: string;
    /** ADIF QSL_SENT_VIA: default send method — B/D/E/M or blank. */
    qsl_sent_via: string;
}

export interface StationConfig {
    /** The full logging_station block (round-tripped losslessly). */
    station: StationFields;
    /** The standing QSL defaults block. */
    qsl: QslFields;
}

export type StationOutcome =
    | { kind: 'ok'; config: StationConfig; durabilityUnconfirmed?: boolean }
    // `timedOut` marks the AMBIGUOUS write (F-04c, ADR 0078): the PUT reached the
    // daemon and its response was lost, so the block may already have been
    // replaced. The section MUST reconcile by re-reading rather than declaring a
    // definite failure. An HTTP status is an answer, so the `!response.ok` path
    // below is a definite rejection and carries no marker.
    | { kind: 'error'; message: string; timedOut?: boolean };

// strictStationFields decodes the REQUIRED logging_station block, returning null
// (a rejection) rather than a silently-empty map when the read is semantically
// invalid (F-01). logging_station is a whole-block round-trip, so a permissive
// parse that turned a missing/null/array block — or one with a non-string member
// — into `{}` would mark the section "loaded" and let a later PUT send blanks over
// the operator's identity, including the read-only station_callsign. It is
// therefore rejected unless it is a plain object of all-string members. Once setup
// is complete, station_callsign must additionally be a non-empty string (the
// identity invariant); a pre-setup config legitimately carries an empty block.
function strictStationFields(v: unknown, setupComplete: boolean): StationFields | null {
    if (!isPlainObject(v)) return null; // missing / null / array / non-object
    const out: StationFields = {};
    for (const [k, val] of Object.entries(v)) {
        if (typeof val !== 'string') return null; // logging_station is all-string
        out[k] = val;
    }
    if (setupComplete && (out.station_callsign ?? '').trim() === '') return null;
    return out;
}

function parseQsl(v: unknown): QslFields {
    const q = isPlainObject(v) ? v : {};
    const str = (x: unknown): string => (typeof x === 'string' ? x : '');
    return { qsl_via: str(q.qsl_via), qslmsg: str(q.qslmsg), qsl_sent_via: str(q.qsl_sent_via) };
}

function parseConfig(body: unknown): StationConfig | null {
    if (!isPlainObject(body)) return null;
    if (typeof body.setup_complete !== 'boolean') return null;
    const station = strictStationFields(body.logging_station, body.setup_complete);
    if (station === null) return null;
    return { station, qsl: parseQsl(body.qsl) };
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
    cfg: Partial<StationConfig>,
    signal?: AbortSignal
): Promise<StationOutcome> {
    // Presence-aware: send ONLY the blocks this save changed. The daemon replaces
    // each present block WHOLE and leaves omitted blocks untouched, so resending an
    // UNEDITED block would clobber a concurrent change to it — the same reason the
    // operational `station` block is never sent. logging_station (identity) and qsl
    // (QSL defaults) are independently editable here, so a station-only edit must
    // not resend a stale qsl, and vice versa (clean-room review 17bb2ffa P1). Each
    // block present still travels WHOLE (unrendered fields preserved).
    const patch: Record<string, unknown> = {};
    if (cfg.station !== undefined) patch.logging_station = cfg.station;
    if (cfg.qsl !== undefined) patch.qsl = cfg.qsl;
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
        signal,
    });
    if (!fetched.ok) {
        // Carry the ambiguity outward rather than flattening it into "failed".
        return {
            kind: 'error',
            message: fetched.message,
            timedOut: fetched.kind === 'network' && fetched.timedOut === true,
        };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const err = isPlainObject(body) ? (body as { message?: string }) : null;
        return { kind: 'error', message: err?.message ?? `HTTP ${fetched.response.status}` };
    }
    // Re-apply the daemon's authoritative post-save view: it re-derives my_lat/
    // my_lon from the grid square, so the form should reflect what was stored.
    const config = parseConfig(body);
    return config
        ? { kind: 'ok', config, durabilityUnconfirmed: isDurabilityUnconfirmed(body) }
        : { kind: 'error', message: 'malformed save response' };
}
