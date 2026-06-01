/**
 * Version state — the daemon's `/v1/version` diagnostics blob, shown
 * in the My Station → About sub-tab.
 *
 * Lives at the SPA level. Persistence tier: in-memory only — it's
 * pure diagnostic display, and the daemon build can change under the
 * SPA (a `task deploy:local:dev` while the tab is open), so a cached
 * value would only risk going stale.
 *
 * Hydration is LAZY: `ensureLoaded()` fires the fetch the first time
 * the operator opens the About tab, not at app boot. The panel is
 * rarely visited, so there's no reason to spend a round-trip on every
 * startup. `loading` / `error` drive the panel's three render states
 * (spinner-ish placeholder / values / failure message).
 *
 * Reactivity: every field a template reads is `$state`.
 */

import { fetchVersion, type VersionResponse } from '../api/version';

class VersionState {
    /** Latest `/v1/version` payload, or `null` before the first fetch. */
    info: VersionResponse | null = $state(null);

    /** True while a fetch is in flight (drives the loading placeholder). */
    loading: boolean = $state(false);

    /** Human-readable failure message, or `null` when the last fetch was fine. */
    error: string | null = $state(null);

    /** True once a fetch has completed (success or failure), so a
     *  failed first load doesn't re-fire on every tab revisit unless
     *  the operator explicitly retries via `refresh()`. */
    private fetched = false;

    /**
     * ensureLoaded — fetch once, the first time the About tab is
     * opened. No-op on subsequent visits. Use `refresh()` to force a
     * re-fetch (e.g. a manual retry button).
     */
    ensureLoaded(): void {
        if (this.fetched || this.loading) return;
        void this.load();
    }

    /**
     * refresh — force a re-fetch regardless of prior state. Lets the
     * operator pick up a newly-deployed daemon build without reloading
     * the whole SPA.
     */
    refresh(): void {
        if (this.loading) return;
        void this.load();
    }

    private async load(): Promise<void> {
        this.loading = true;
        this.error = null;
        try {
            const outcome = await fetchVersion();
            switch (outcome.kind) {
                case 'ok':
                    this.info = outcome.version;
                    break;
                case 'server':
                    this.error = 'Daemon returned an unexpected response.';
                    break;
                case 'network':
                    this.error = 'Cannot reach the daemon.';
                    break;
                case 'aborted':
                    // No signal is passed today, so this arm is
                    // unreachable in practice; treat it as a soft
                    // failure rather than a stuck spinner.
                    this.error = 'Request cancelled.';
                    break;
            }
        } finally {
            this.fetched = true;
            this.loading = false;
        }
    }
}

export const versionState = new VersionState();
