/*
    Daemon-side wrapper for `GET /v1/forwarder-types` — the data-driven
    descriptor for every forwarder type the daemon supports (display name,
    supported actions, credential-field shapes). The Forwarding tab renders its
    add-forwarder credential form from these, so a new forwarder type in Go needs
    zero SPA changes. Same discriminated-union conventions as the other wrappers.

    Types mirror the daemon DTOs (internal/forwarding TypeDescriptor /
    CredentialField).
*/

import { isPlainObject, isShape, readJsonBody, safeFetch } from './_helpers';

export interface CredentialField {
    key: string;
    label: string;
    /** 'password' fields are masked on GET /v1/config and rendered as password inputs. */
    kind: 'text' | 'password';
    help?: string;
}

export interface ForwarderType {
    type: string;
    display_name: string;
    supported_actions: string[];
    credential_fields: CredentialField[];
}

export type ForwarderTypesOutcome =
    | { kind: 'ok'; types: ForwarderType[] }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

export async function fetchForwarderTypes(signal?: AbortSignal): Promise<ForwarderTypesOutcome> {
    const fetched = await safeFetch('/v1/forwarder-types', { signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const body = await readJsonBody(fetched.response);
    if (fetched.response.ok) {
        if (!isShape<{ types: ForwarderType[] }>(body, ['types'])) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned /v1/forwarder-types without a types array',
            };
        }
        return { kind: 'ok', types: body.types };
    }
    const err = isPlainObject(body) ? (body as { code?: string; message?: string }) : null;
    return {
        kind: 'server',
        code: err?.code ?? 'unknown_error',
        message: err?.message ?? `HTTP ${fetched.response.status}`,
    };
}
