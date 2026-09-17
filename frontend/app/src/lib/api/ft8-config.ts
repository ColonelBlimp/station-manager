/*
    FT8-scoped /v1/config read + write for the app Settings view (ADR 0044) — the
    port of the standalone config SPA's FT8 tab. Four blocks ride together
    because they are one page to the operator: the subsystem master switch, the
    Band Activity display prefs, PSK Reporter, and the decode log.

    Data-safety contract — VERIFIED against the daemon's overlayConfig
    (internal/api/handler_config.go:515, :557, :563, :569):

      - All four fields are PRESENCE-AWARE on PUT: present replaces the block
        WHOLE, omitted leaves the stored block untouched.
      - `ft8_display` is therefore an all-or-nothing write, and this shell
        renders no colour pickers (the operator's 2026-08-05 ruling — the app's
        Band Activity uses a theme-aware palette instead). The three
        highlight_* values are still LOADED and SENT BACK VERBATIM: dropping
        them from the payload would erase a hand-set colour from config.json on
        the first save from this page.
      - GET serves ft8_display RESOLVED (types.ResolveFt8Display — row cap
        clamped to 10..2000, feed_mode defaulted, colours defaulted), and the
        PUT handler stores the resolved shape too (handler_config.go:757). So
        the save response is what is actually on disk, not what we sent.
      - `psk_reporter` and `ft8_decode_log` are served RAW (sparse overrides),
        so an unset field arrives absent and means "the daemon's default".
      - `ft8_max_repeats` (the repeat cap, ruling 2026-09-16) rides beside them
        as a plain presence-aware int: served RESOLVED on GET (default 5,
        clamped 1..10), validated 1..10 on PUT with NO blank-means-default —
        an out-of-range value is a 400 that rejects the whole save — and the
        one /v1/config field the daemon applies LIVE to the running sequencer.
      - NOTHING but these five keys is sent. Echoing logging_station or station
        would clobber a concurrent identity or power change made between our GET
        and our PUT — the trap review 2026-07-20 #3 removed from the Station
        section, and which the standalone config SPA still carries.
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';
import { isDurabilityUnconfirmed } from '../config/durability';

/** Band Activity display prefs, as GET reports them (resolved). */
export interface Ft8DisplayEntry {
    history_max: number;
    /** Strict enum daemon-side; anything else is normalised to accumulate. */
    feed_mode: 'accumulate' | 'single';
    cq_to_top: boolean;
    hide_hashed_calls: boolean;
    /* Round-tripped, never rendered — see the erasure note in the header. */
    highlight_unworked: string;
    highlight_worked: string;
    highlight_calling: string;
}

/** PSK Reporter upload settings. Blank host / 0 port = the daemon's default. */
export interface PskEntry {
    enabled: boolean;
    host: string;
    port: number;
}

/** The JTDX ALL.TXT-style decode log. Blank path = the daemon's default. */
export interface DecodeLogEntry {
    enabled: boolean;
    path: string;
}

export interface Ft8Settings {
    /** The subsystem master switch (config ft8.enabled). Restart to apply. */
    enabled: boolean;
    display: Ft8DisplayEntry;
    psk: PskEntry;
    decodeLog: DecodeLogEntry;
    /** The unanswered-rung repeat cap (config ft8.tx.max_repeats), 1..10 —
     *  served resolved, applied live on save. 0 only when an older daemon
     *  served no value. */
    maxRepeats: number;
}

export type Ft8SettingsOutcome =
    | { kind: 'ok'; settings: Ft8Settings; durabilityUnconfirmed?: boolean }
    /** `timedOut` marks the AMBIGUOUS write: the PUT reached the daemon and the
     *  response never came, so it may already have committed. Callers must not
     *  report that as a failure — see the reconcile in ft8.svelte.ts. */
    | { kind: 'error'; message: string; timedOut?: boolean };

function str(v: unknown): string {
    return typeof v === 'string' ? v : '';
}

function toDisplay(v: unknown): Ft8DisplayEntry {
    const o = isPlainObject(v) ? v : {};
    return {
        history_max: typeof o.history_max === 'number' ? o.history_max : 0,
        // Matches seams.ts: an older daemon, a malformed value or an absent
        // block all fall back to accumulate rather than reaching the <select>
        // as a value it has no <option> for (which would render blank).
        feed_mode: o.feed_mode === 'single' ? 'single' : 'accumulate',
        cq_to_top: o.cq_to_top === true,
        hide_hashed_calls: o.hide_hashed_calls === true,
        highlight_unworked: str(o.highlight_unworked),
        highlight_worked: str(o.highlight_worked),
        highlight_calling: str(o.highlight_calling),
    };
}

function toPsk(v: unknown): PskEntry {
    const o = isPlainObject(v) ? v : {};
    return {
        enabled: o.enabled === true,
        host: str(o.host),
        port: typeof o.port === 'number' ? o.port : 0,
    };
}

function toDecodeLog(v: unknown): DecodeLogEntry {
    const o = isPlainObject(v) ? v : {};
    return { enabled: o.enabled === true, path: str(o.path) };
}

function toSettings(body: Record<string, unknown>): Ft8Settings {
    return {
        enabled: body.ft8_enabled === true,
        display: toDisplay(body.ft8_display),
        psk: toPsk(body.psk_reporter),
        decodeLog: toDecodeLog(body.ft8_decode_log),
        // The 0 fallback is type narrowing, not a supported state: every daemon
        // that can serve this SPA (it is embedded in the same binary) has served
        // ft8_max_repeats RESOLVED on GET since db13a4cd (2026-07-03), a month
        // before this section existed. A blank cap therefore never comes from
        // the daemon, and the form's 1..10 gate is the right answer to one — it
        // is not a lenient "omit the key" path for a daemon that does not exist
        // (codex review 42cff385, refuted).
        maxRepeats: typeof body.ft8_max_repeats === 'number' ? body.ft8_max_repeats : 0,
    };
}

export async function fetchFt8Settings(signal?: AbortSignal): Promise<Ft8SettingsOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body)) return { kind: 'error', message: 'malformed /v1/config response' };
    return { kind: 'ok', settings: toSettings(body) };
}

export async function saveFt8Settings(
    s: Ft8Settings,
    signal?: AbortSignal
): Promise<Ft8SettingsOutcome> {
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        // Only the five FT8 keys — see the clobber note in the module header.
        body: JSON.stringify({
            ft8_enabled: s.enabled,
            ft8_display: s.display,
            psk_reporter: s.psk,
            ft8_decode_log: s.decodeLog,
            ft8_max_repeats: s.maxRepeats,
        }),
        signal,
    });
    if (!fetched.ok) {
        // Carry the ambiguity outward rather than flattening it into "failed".
        // An HTTP status, by contrast, IS an answer: the daemon replied, so a
        // non-2xx below is a definite rejection with nothing committed.
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
    if (!isPlainObject(body)) return { kind: 'error', message: 'malformed save response' };
    return {
        kind: 'ok',
        settings: toSettings(body),
        durabilityUnconfirmed: isDurabilityUnconfirmed(body),
    };
}
