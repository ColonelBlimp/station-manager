/*
    Thin daemon-side wrapper for `GET /v1/version` — the diagnostics blob shown on
    the config SPA's General tab (moved from the logging SPA's My Station → About,
    2026-06-26). Discriminated-union outcome so the tab branches without parsing the
    HTTP/JSON envelope inline. Mirrors the logging SPA's `api/version.ts`.

    Wire contract (handler_version.go): 200 OK → { daemon, go, schema?: {version, dirty} }.
    The endpoint always answers 200 — a schema-query failure just omits `schema` —
    so the only failure arms are transport-level + the malformed-200 guard.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export interface SchemaInfo {
    /** Applied DB migration level. */
    version: number;
    /** True when a migration was interrupted mid-apply. */
    dirty: boolean;
}

export interface VersionResponse {
    /** Daemon build version (git-derived semver, or "dev"). */
    daemon: string;
    /** Go runtime version the daemon was built with (e.g. "go1.24.0"). */
    go: string;
    /** Schema migration state; absent when the daemon's query failed. */
    schema?: SchemaInfo;
}

export type VersionOutcome =
    | { kind: 'ok'; version: VersionResponse }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

export async function fetchVersion(signal?: AbortSignal): Promise<VersionOutcome> {
    const fetched = await safeFetch('/v1/version', { signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }

    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body)) {
        return {
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a non-JSON or empty body for /v1/version',
        };
    }
    return { kind: 'ok', version: body as unknown as VersionResponse };
}
