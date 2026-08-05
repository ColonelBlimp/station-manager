// Adapters from the daemon API clients to the operate-surface seams — the
// real counterparts of lib/dev/*Stub.ts, injected in main.ts (ADR 0045: the
// state modules never import this layer).
//
// Mapping posture is fail-soft throughout: any non-ok outcome collapses to
// null / '' and the UI shows its empty state — enrichment never blocks
// logging, and a daemon hiccup is never an operator-facing error here.

import { enrichCallsign } from './enrichment';
import { fetchContactHistory, type ContactHistory } from './contact-history';
import { isPlainObject, readJsonBody, safeFetch } from './_helpers';
import type { Enrichment } from '../operate/enrich.svelte';
import type { WorkedQso } from '../operate/worked.svelte';
import type { AdifModePair } from '../operate/rig.svelte';

/** Real enricher for setEnricher — GET /v1/enrich/callsign. The signal lets a
 *  superseded lookup cancel its in-flight daemon/upstream request. */
// BASELINE DEBT 2026-07-31 (complexity 27) — maps each enrichment field from the
// daemon response, tolerating absent ones (enrichment never blocks logging).
// eslint-disable-next-line complexity
export async function apiEnrich(call: string, signal?: AbortSignal): Promise<Enrichment | null> {
    const out = await enrichCallsign(call, signal);
    if (out.kind !== 'ok') return null;

    const { country, station } = out.result;
    if (country === undefined && station === undefined) return null; // both sources "none"

    return {
        country: country?.name ?? '',
        ccode: country?.ccode ?? '',
        // The numeric DXCC entity code (e.g. "291"). The daemon now populates
        // station.dxcc from its curated prefix→entity map, so this carries the
        // real number. No fall-back to country.dxcc_prefix: an alphabetic prefix
        // ("K") is NOT the DXCC identifier, and showing it under a "DXCC" label
        // misleads — '' instead hides the readout when the entity is unknown.
        dxcc: /^\d+$/.test(station?.dxcc ?? '') ? (station?.dxcc ?? '') : '',
        isNewEntity: country?.is_new_entity ?? null,
        grid: station?.gridsquare ?? '',
        name: station?.name ?? '',
        qth: station?.qth ?? '',
        email: station?.email ?? '',
        // Zones come from the country layer (matches the shipping SPA's
        // DetailsPanel — the station object also carries cqz/ituz, but the
        // country entity is the authoritative source).
        cqZone: country?.cq_zone ?? '',
        ituZone: country?.itu_zone ?? '',
    };
}

// ADIF wire formats → the panel's display formats. Pass unrecognised values
// through unchanged — a surprising daemon value should show up looking odd,
// not vanish into ''.
export function adifDateToDisplay(d: string): string {
    return /^\d{8}$/.test(d) ? `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6)}` : d;
}

export function adifTimeToDisplay(t: string): string {
    // HHMM or HHMMSS; the panel shows minute precision.
    return /^\d{4}(\d{2})?$/.test(t) ? `${t.slice(0, 2)}:${t.slice(2, 4)}` : t;
}

export function toWorkedQso(row: ContactHistory): WorkedQso {
    return {
        uuid: row.uuid,
        date: adifDateToDisplay(row.qso_date),
        timeOn: adifTimeToDisplay(row.time_on),
        band: row.band,
        mode: row.mode,
        rstSent: row.rst_sent,
        rstRcvd: row.rst_rcvd,
        name: row.name,
        notes: row.notes ?? '',
    };
}

/** Real history lookup for setHistory — GET /v1/contact-history. Fail-soft:
 *  any non-ok outcome is an empty history (the panel just doesn't open);
 *  worked-before is a convenience, never a gate. */
export async function apiHistory(call: string, signal?: AbortSignal): Promise<WorkedQso[]> {
    const out = await fetchContactHistory(call, signal);
    if (out.kind !== 'ok') return [];
    // contact-history only validated `items` is an array, so a row could be
    // malformed. WorkedPanel keys its {#each} on uuid — a missing uuid renders
    // undefined data and a duplicate uuid trips Svelte 5's duplicate-key throw.
    // Drop non-object rows + any without a unique, non-empty string uuid.
    const rows: WorkedQso[] = [];
    const seen: string[] = [];
    for (const raw of out.items as unknown[]) {
        if (!raw || typeof raw !== 'object') continue;
        const uuid = (raw as { uuid?: unknown }).uuid;
        if (typeof uuid !== 'string' || uuid === '' || seen.includes(uuid)) continue;
        seen.push(uuid);
        rows.push(toWorkedQso(raw as ContactHistory));
    }
    return rows;
}

/**
 * The station facts a submit needs, from GET /v1/config: my grid (also the
 * near end of the enrichment bearing), the station/operator callsigns for the
 * ADIF record, and the default logbook the POST targets — plus the bridge
 * facts the rig seam needs (CAT enabled gates opening the SSE; mode_mappings
 * is the merged rigdef+override table resolving rig mode literals).
 * Zero-values on any failure — the submit sink refuses with a clear message
 * rather than posting a QSO against the wrong logbook, and a config failure
 * leaves the rig surface fully manual.
 */
export interface StationContext {
    /** /v1/config was reached AND parsed. False = daemon down/malformed —
     *  distinct from "not set up", so the shell renders (fail-soft empties)
     *  instead of wrongly greeting a configured operator with first-run setup. */
    configOk: boolean;
    /** First-run setup done (config setup_complete): the default logbook row
     *  exists. False gates the whole app behind the SetupCard — every surface
     *  (map, logbook, header count) 404s against a logbook that isn't there. */
    setupComplete: boolean;
    myGrid: string;
    stationCallsign: string;
    operator: string;
    logbookId: number;
    catEnabled: boolean;
    modeMappings: Record<string, AdifModePair>;
    /** Bridge capability advertisement (BridgeInfo, ADR 0026): which rig-control
     *  ops the configured rig exposes (`set_freq`, `swap_vfo`, …), whether it
     *  supports the tune carrier (ADR 0027), and the rig's own mode literals for
     *  the live mode dropdown (Option A). Empty when the bridge is disabled or
     *  config is unavailable, so every control surface gates closed. */
    ops: string[];
    tune: boolean;
    rigModes: string[];
    /** The operator's configured operating bands (station.operating_bands) —
     *  one source for the band selector, FT8 band buttons, and (later) the
     *  keyboard band-jump. Empty = unset → the UI falls back to its default
     *  HF..6m set, so an existing config is unaffected. */
    operatingBands: string[];
    /** Mailer projection (daemon-managed, read-only): the Export dialog gates
     *  the email path on `mailerEnabled` and seeds the recipient. */
    mailerEnabled: boolean;
    mailerDefaultRecipient: string;
    /** Always-visible station identity for the header: the default logbook this
     *  session writes to (`default_logbook.name`) and the configured rig
     *  (`bridge.rig_name`). Config-sourced (not CAT), so both show even before the
     *  rig connects — the operator can always see which book + radio is in play. */
    logbookName: string;
    rigName: string;
    /** FT8 Band Activity display prefs (config.json ft8.display, daemon-resolved
     *  so always present on a current daemon). `feedMode` accumulate rolls slots
     *  up / single shows only the current slot; `cqToTop` floats CQ rows above the
     *  rest; `historyMax` caps the feed. Defaults match the daemon's when the block
     *  or a field is absent (older daemon / unset), so an existing config is honoured
     *  and a missing one is inert. `hideHashed` drops decodes with an unresolved
     *  hashed call ("<...>") from Band Activity. */
    ft8FeedMode: 'accumulate' | 'single';
    ft8HistoryMax: number;
    ft8CqToTop: boolean;
    ft8HideHashed: boolean;
    /** Per-band FT8 dial frequencies (config `ft8_frequencies`, band→Hz — WSJT-X
     *  defaults + operator overrides, merged daemon-side). Drives the FT8 rig card's
     *  band buttons (jump to the watering-hole, not the rig's band-stack freq). */
    ft8Frequencies: Record<string, number>;
    /** The rig's OWN mode literal for FT8 (config `bridge.ft8_mode` — rigdef
     *  default, overridable per rig: "DATA-U" on the FTdx10, "USB-D" on the
     *  IC-7300). An FT8 band pick asserts it so the dial move also puts the rig
     *  in data mode. '' = leave the current mode (or no driver configured). */
    ft8Mode: string;
    /** Contacts-map per-band arc colour overrides (config `map.band_colors`,
     *  band→"#rrggbb", sparse) — layered over the map's built-in palette
     *  (lib/map/bandColors). Empty = all defaults. */
    mapBandColors: Record<string, string>;
}

export async function fetchStationContext(): Promise<StationContext> {
    const none: StationContext = {
        configOk: false,
        setupComplete: false,
        myGrid: '',
        stationCallsign: '',
        operator: '',
        logbookId: 0,
        catEnabled: false,
        modeMappings: {},
        ops: [],
        tune: false,
        rigModes: [],
        operatingBands: [],
        mailerEnabled: false,
        mailerDefaultRecipient: '',
        ft8FeedMode: 'accumulate',
        ft8HistoryMax: 100,
        ft8CqToTop: false,
        ft8HideHashed: false,
        ft8Frequencies: {},
        ft8Mode: '',
        mapBandColors: {},
        logbookName: '',
        rigName: '',
    };
    const fetched = await safeFetch('/v1/config', { method: 'GET' });
    if (!fetched.ok || !fetched.response.ok) return none;
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body)) return none;

    const ls = isPlainObject(body.logging_station) ? body.logging_station : {};
    const lb = isPlainObject(body.default_logbook) ? body.default_logbook : {};
    const br = isPlainObject(body.bridge) ? body.bridge : {};
    const ml = isPlainObject(body.mailer) ? body.mailer : {};
    const st = isPlainObject(body.station) ? body.station : {};
    const fd = isPlainObject(body.ft8_display) ? body.ft8_display : {};
    const str = (v: unknown): string => (typeof v === 'string' ? v : '');
    return {
        configOk: true,
        setupComplete: body.setup_complete === true,
        myGrid: str(ls.my_gridsquare),
        stationCallsign: str(ls.station_callsign),
        operator: str(ls.operator),
        logbookId: typeof lb.id === 'number' ? lb.id : 0,
        catEnabled: br.enabled === true,
        modeMappings: toModeMappings(br.mode_mappings),
        // ops/tune/rig_modes carry `omitempty`, so they're simply absent when
        // the rig exposes none — toStringArray/=== true default them closed.
        ops: toStringArray(br.ops),
        tune: br.tune === true,
        rigModes: toStringArray(br.rig_modes),
        operatingBands: toStringArray(st.operating_bands),
        mailerEnabled: ml.enabled === true,
        mailerDefaultRecipient: str(ml.default_recipient),
        // feed_mode is a strict enum daemon-side ("accumulate"|"single"); anything
        // else (absent, older daemon, malformed) falls back to accumulate.
        ft8FeedMode: fd.feed_mode === 'single' ? 'single' : 'accumulate',
        ft8HistoryMax: typeof fd.history_max === 'number' ? fd.history_max : 100,
        ft8CqToTop: fd.cq_to_top === true,
        ft8HideHashed: fd.hide_hashed_calls === true,
        ft8Frequencies: toNumberMap(body.ft8_frequencies),
        // In the BRIDGE block beside ops/rig_modes — it is rig-driver data.
        ft8Mode: str(br.ft8_mode),
        mapBandColors: toStringMap(isPlainObject(body.map) ? body.map.band_colors : undefined),
        logbookName: str(lb.name),
        rigName: str(br.rig_name),
    };
}

/** Keep only the string-valued members of a wire object (band→colour); anything
 *  else (or a non-object) yields an empty map — malformed config is inert. */
function toStringMap(v: unknown): Record<string, string> {
    if (!isPlainObject(v)) return {};
    const out: Record<string, string> = {};
    for (const [k, val] of Object.entries(v)) {
        if (typeof val === 'string') out[k] = val;
    }
    return out;
}

/** Keep only the number-valued members of a wire object (band→Hz); anything else
 *  (or a non-object) yields an empty map, so a malformed config is simply inert. */
function toNumberMap(v: unknown): Record<string, number> {
    if (!isPlainObject(v)) return {};
    const out: Record<string, number> = {};
    for (const [k, val] of Object.entries(v)) {
        if (typeof val === 'number') out[k] = val;
    }
    return out;
}

/**
 * Total QSO count for a logbook (GET /v1/logbook/{id}/count) — the header's live
 * "Logbook (n)" readout. Fail-soft: any non-ok outcome (network blip, daemon
 * restart) returns null so the caller keeps the last good count on screen rather
 * than flashing a wrong value. id < 1 (pre-setup) → null.
 */
export async function fetchLogbookCount(logbookId: number): Promise<number | null> {
    if (logbookId < 1) return null;
    const fetched = await safeFetch(`/v1/logbook/${logbookId}/count`, { method: 'GET' });
    if (!fetched.ok || !fetched.response.ok) return null;
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body)) return null;
    return typeof body.count === 'number' ? body.count : null;
}

/** Keep only the string members of a wire array; anything else (or a non-array)
 *  yields an empty list — a malformed caps advertisement gates every control
 *  surface closed rather than throwing. */
function toStringArray(v: unknown): string[] {
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
}

/** Narrow the wire's mode_mappings to well-formed entries; anything odd is
 *  dropped (an unmapped literal then passes through raw downstream). */
function toModeMappings(v: unknown): Record<string, AdifModePair> {
    if (!isPlainObject(v)) return {};
    const out: Record<string, AdifModePair> = {};
    for (const [literal, pair] of Object.entries(v)) {
        if (!isPlainObject(pair) || typeof pair.mode !== 'string') continue;
        out[literal] = {
            mode: pair.mode,
            submode: typeof pair.submode === 'string' ? pair.submode : undefined,
        };
    }
    return out;
}
