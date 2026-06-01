/*
    Thin daemon-side wrapper for `GET /v1/version`. Returns a
    discriminated union so the About sub-tab can branch on outcome
    without parsing the HTTP / JSON envelope inline. Same shape
    conventions as `lib/api/config.ts`.

    Wire contract (handler_version.go):
      - 200 OK → { daemon, go, schema?: { version, dirty } }

    The endpoint always answers 200 — even a schema-query failure
    omits the `schema` field rather than erroring — so there's no
    daemon-emitted error body to decode. The only failure arms are
    transport-level (network / aborted) plus the malformed-200 guard.

    `schema` is optional because the daemon's `omitempty` JSON tag
    drops it when the migration-level query fails; the About panel
    renders "schema: unavailable" in that case.
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
        // A 200 with a non-object body means a daemon regression or
        // proxy interference — downgrade to a server error so the
        // panel shows "couldn't load" rather than dereferencing null.
        return {
            kind: 'server',
            code: 'malformed_response',
            message: 'daemon returned a non-JSON or empty body for /v1/version',
        };
    }
    return { kind: 'ok', version: body as unknown as VersionResponse };
}
