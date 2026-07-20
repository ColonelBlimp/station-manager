/*
    Rig-list reader for the app Settings → Rigs section (ADR 0044). GET /v1/rigs
    returns the configured rigs plus the active-rig id (and a rigdef catalogue
    the details editor will use later). This first increment needs only the list
    and the default marker; the write path + catalogue resolution land with the
    rig-details editor. Mirrors the daemon DTOs (internal/api/handler_rigs.go,
    internal/types/rig.go) and the config SPA's api/rigs.ts.
*/
import { safeFetch, readJsonBody, isShape } from './_helpers';

/** A configured rig (subset of types.RigConfig — the fields the list needs). */
export interface RigConfig {
    id: number;
    model: string;
    port: string;
    audio?: { rx?: string; tx?: string };
    my_rig?: string | null;
}

export interface RigsData {
    defaultRigId: number;
    rigs: RigConfig[];
}

export type RigsOutcome = { kind: 'ok'; data: RigsData } | { kind: 'error'; message: string };

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
        },
    };
}
