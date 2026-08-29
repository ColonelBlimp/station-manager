/*
    General settings + About — /v1/config read/write for the app Settings "General"
    section (ADR 0044 port of the config SPA's General tab), plus read-only build
    info from /v1/version.

    Data-safety contract — VERIFIED against the daemon's overlayConfig
    (internal/api/handler_config.go): a PUT replaces a block WHOLE when the field
    is present, untouched when omitted (nil). So:
      - `restore_rig_on_mode_switch` is a TOP-LEVEL scalar — sent alone, replaced.
      - `map` is a BLOCK — its OTHER fields (if any) are round-tripped whole so a
        band-colour edit never zeroes them; band_colors is the only field we edit.
      - every other block (logging_station, station, forwarders, …) is omitted and
        therefore left untouched.
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';
import { isDurabilityUnconfirmed } from '../config/durability';

export interface GeneralConfig {
    /** restore_rig_on_mode_switch (*bool, default ON) — the mode-switch rig-restore knob. */
    restoreRigOnModeSwitch: boolean;
    /** Sparse band → #rrggbb overrides (map.band_colors); a band absent takes the default palette. */
    bandColors: Record<string, string>;
    /** The rest of the `map` block, held opaque for lossless round-trip on save. */
    mapRest: Record<string, unknown>;
}

export type GeneralOutcome =
    | { kind: 'ok'; config: GeneralConfig; durabilityUnconfirmed?: boolean }
    // `timedOut` marks the AMBIGUOUS write: the PUT reached the daemon and its
    // response was lost, so it MAY already have committed. The caller must re-read
    // rather than report a plain failure — a blind retry resends the whole `map`
    // block and can revert a change made in between (mirrors saveFt8Settings).
    | { kind: 'error'; message: string; timedOut?: boolean };

function toColorMap(v: unknown): Record<string, string> {
    const out: Record<string, string> = {};
    if (isPlainObject(v)) {
        for (const [k, val] of Object.entries(v)) {
            if (typeof val === 'string') out[k] = val;
        }
    }
    return out;
}

function parseGeneral(body: unknown): GeneralConfig | null {
    if (!isPlainObject(body)) return null;
    const map = isPlainObject(body.map) ? body.map : {};
    const { band_colors, ...mapRest } = map;
    return {
        // *bool default ON: only an explicit false turns it off (matches config.applyDefaults).
        restoreRigOnModeSwitch: body.restore_rig_on_mode_switch !== false,
        bandColors: toColorMap(band_colors),
        mapRest,
    };
}

export async function fetchGeneral(signal?: AbortSignal): Promise<GeneralOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const config = parseGeneral(await readJsonBody(fetched.response));
    if (!config) return { kind: 'error', message: 'malformed /v1/config response' };
    return { kind: 'ok', config };
}

export async function saveGeneral(
    cfg: GeneralConfig,
    signal?: AbortSignal
): Promise<GeneralOutcome> {
    const body = {
        restore_rig_on_mode_switch: cfg.restoreRigOnModeSwitch,
        // Whole `map` block: preserve any other map field the daemon holds; only band_colors changes.
        map: { ...cfg.mapRest, band_colors: cfg.bandColors },
    };
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal,
    });
    if (!fetched.ok) {
        return {
            kind: 'error',
            message: fetched.message,
            timedOut: fetched.kind === 'network' && fetched.timedOut === true,
        };
    }
    const resBody = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const err = isPlainObject(resBody) ? (resBody as { message?: string }) : null;
        return { kind: 'error', message: err?.message ?? `HTTP ${fetched.response.status}` };
    }
    const config = parseGeneral(resBody);
    return config
        ? { kind: 'ok', config, durabilityUnconfirmed: isDurabilityUnconfirmed(resBody) }
        : { kind: 'error', message: 'malformed save response' };
}

// ---- About / build info (read-only, GET /v1/version) ----

export interface BuildInfo {
    /** Daemon build version (git-derived semver, or "dev"). */
    daemon: string;
    /** Go runtime the daemon was built with (e.g. "go1.24.0"). */
    go: string;
    /** Build environment: 'dev' for any source build, 'release' only for a packaged
     *  binary. Anything not exactly "dev" resolves to 'release' so a missing or odd
     *  value can never falsely mark a daemon DEV (W-0004 AC2). */
    env: 'dev' | 'release';
    /** Log-DB schema migration state; absent when the daemon's query failed. */
    schema?: { version: number; dirty: boolean };
}

export type BuildInfoOutcome = { kind: 'ok'; info: BuildInfo } | { kind: 'error'; message: string };

export async function fetchBuildInfo(signal?: AbortSignal): Promise<BuildInfoOutcome> {
    const fetched = await safeFetch('/v1/version', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body) || typeof body.daemon !== 'string' || typeof body.go !== 'string') {
        return { kind: 'error', message: 'malformed /v1/version response' };
    }
    const info: BuildInfo = {
        daemon: body.daemon,
        go: body.go,
        // Only the exact literal "dev" marks a development daemon; everything else
        // (release, absent, unexpected) is release — never fabricate DEV (AC2).
        env: body.env === 'dev' ? 'dev' : 'release',
    };
    if (isPlainObject(body.schema)) {
        const sv = body.schema as { version?: unknown; dirty?: unknown };
        if (typeof sv.version === 'number') {
            info.schema = { version: sv.version, dirty: sv.dirty === true };
        }
    }
    return { kind: 'ok', info };
}
