/*
    CAT master-switch state (Rigs settings) — the config bridge.enabled toggle,
    "Enable rig connection (CAT)". Ported from the config SPA's Rigs tab by operator
    direction (W-0003 config-SPA parity, restoring a gap the app dropped).

    Save-on-toggle via the presence-aware `bridge_enabled` PUT (never a whole-block
    `bridge` replace, so the rig CAT/serial config is untouched). The bridge binds at
    startup, so a change needs a daemon restart — surfaced as a restart-to-apply note
    + a toast, session-only like the FT8 master switch's notice. Enabling can be
    refused when the active rig has no port/driver; the toggle then reverts and the
    daemon's message is shown.
*/
import { fetchBridgeEnabled, saveBridgeEnabled } from '../api/rigs';
import { toasts } from '../ui/toasts.svelte';

class BridgeEnabledState {
    enabled = $state(false);
    loaded = $state(false);
    loading = $state(false);
    saving = $state(false);
    error = $state('');
    /** A saved change awaits a daemon restart (the bridge binds at startup).
     *  Session-only — a reload shows the persisted value with no pending note. */
    restartPending = $state(false);

    async load(): Promise<void> {
        if (this.loading) return;
        this.loading = true;
        this.error = '';
        const res = await fetchBridgeEnabled();
        this.loading = false;
        if (res.kind === 'error') {
            this.error = res.message;
            return;
        }
        this.enabled = res.enabled;
        this.loaded = true;
    }

    async setEnabled(next: boolean): Promise<void> {
        if (this.saving || next === this.enabled) return;
        this.saving = true;
        this.error = '';
        const prev = this.enabled;
        this.enabled = next; // optimistic — the checkbox reflects the intent
        const res = await saveBridgeEnabled(next);
        this.saving = false;
        if (res.kind === 'error') {
            this.enabled = prev; // the daemon refused (e.g. active rig has no port/driver)
            this.error = res.message;
            toasts.error(`Couldn't ${next ? 'enable' : 'disable'} CAT: ${res.message}`);
            return;
        }
        this.restartPending = true;
        toasts.info(`CAT ${next ? 'enabled' : 'disabled'} — restart the daemon to apply.`);
    }
}

export const bridgeEnabledState = new BridgeEnabledState();
