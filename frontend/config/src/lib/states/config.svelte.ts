/*
    configState — the config SPA's daemon-data singleton. Holds the daemon-
    authoritative config (GET /v1/config) and the rig-profiles editor surface
    (GET /v1/rigs), loaded once on app mount. Components import the singleton
    directly; `$state` fields make reads reactive (same convention as the
    logging SPA's lib/states/*.svelte.ts).

    This is the daemon-authoritative source — there is no parallel store and no
    localStorage cache for it (ADR 0003). The write path (PUT) lands per panel
    as the editor surfaces are built.
*/

import { fetchConfig, type ConfigResponse } from '../api/config';
import { fetchRigs, type RigConfig, type RigDefSummary } from '../api/rigs';

class ConfigState {
    /** False until the first load() round-trip settles (success or error). */
    loaded: boolean = $state(false);
    /** Human-readable load error, or null. Set if either fetch failed; partial data still applies. */
    error: string | null = $state(null);

    /** Broader config (station identity, FT8 display, mailer). Null until loaded. */
    config: ConfigResponse | null = $state(null);

    /** Rig-profiles editor data (GET /v1/rigs). */
    defaultRigId: number = $state(0);
    rigs: RigConfig[] = $state([]);
    catalogue: RigDefSummary[] = $state([]);

    /**
     * Fetch /v1/config and /v1/rigs concurrently and hydrate. Either failing
     * sets `error` but whatever succeeded is still applied, so a partially
     * healthy daemon still renders. Idempotent enough to call once on mount.
     */
    async load(): Promise<void> {
        const [cfg, rigs] = await Promise.all([fetchConfig(), fetchRigs()]);

        const errs: string[] = [];
        if (cfg.kind === 'ok') {
            this.config = cfg.config;
        } else {
            errs.push(`config: ${outcomeMessage(cfg)}`);
        }
        if (rigs.kind === 'ok') {
            this.defaultRigId = rigs.rigs.default_rig_id;
            this.rigs = rigs.rigs.rigs;
            this.catalogue = rigs.rigs.catalogue;
        } else {
            errs.push(`rigs: ${outcomeMessage(rigs)}`);
        }

        this.error = errs.length > 0 ? errs.join('; ') : null;
        this.loaded = true;
    }

    /** The rigdef defaults for a configured rig's model, or undefined if unknown. */
    rigdefFor(model: string): RigDefSummary | undefined {
        return this.catalogue.find((c) => c.id === model);
    }
}

/** A non-'ok' outcome's display string (the two API wrappers share these arms). */
function outcomeMessage(
    o: { kind: 'server'; code: string; message: string } | { kind: string; message: string }
): string {
    return 'code' in o ? `${o.kind} (${o.code})` : `${o.kind}: ${o.message}`;
}

export const configState = new ConfigState();
