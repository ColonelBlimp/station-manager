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
import { noteConfigDurability } from './durability';
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
        // Invalidate before awaiting: a pending or FAILED reload must not leave the
        // last-loaded value on screen as though current. Without this, a failed
        // remount reload keeps `loaded` true, so RigsSection's error+Retry branch is
        // shadowed and a stale, interactive toggle stays on screen (clean-room review
        // 7f7ef966 P2 — same rule stationState/etc. follow).
        this.loaded = false;
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
        if (res.kind === 'error') {
            if (res.timedOut) {
                // Ambiguous: the PUT may already have committed. Re-read the
                // authoritative value instead of blindly reverting — reverting a
                // change that actually landed would misreport the CAT state and
                // could prompt the operator to undo a successful change
                // (clean-room review 3e892067 P2).
                await this.#reconcileAfterTimeout(prev);
                return;
            }
            this.saving = false;
            this.enabled = prev; // the daemon refused (e.g. active rig has no port/driver)
            this.error = res.message;
            toasts.error(`Couldn't ${next ? 'enable' : 'disable'} CAT: ${res.message}`);
            return;
        }
        this.saving = false;
        this.restartPending = true;
        if (!noteConfigDurability(res.durabilityUnconfirmed ?? false)) {
            toasts.info(`CAT ${next ? 'enabled' : 'disabled'} — restart the daemon to apply.`);
        }
    }

    // Settle a toggle whose PUT timed out: re-read the daemon's authoritative value
    // rather than assuming success or failure. If it changed, a restart is owed.
    async #reconcileAfterTimeout(prev: boolean): Promise<void> {
        const reread = await fetchBridgeEnabled();
        this.saving = false;
        if (reread.kind === 'error') {
            this.enabled = prev; // can't tell — fall back to the pre-toggle value
            this.error = reread.message;
            toasts.error(
                'CAT change timed out and the daemon could not be re-read — its state is unknown.'
            );
            return;
        }
        this.enabled = reread.enabled;
        if (reread.enabled !== prev) this.restartPending = true; // it did change ⇒ restart owed
        toasts.warn(
            `CAT change timed out — re-read the daemon: CAT is now ${
                reread.enabled ? 'enabled' : 'disabled'
            }.${reread.enabled !== prev ? ' Restart to apply.' : ''}`
        );
    }
}

export const bridgeEnabledState = new BridgeEnabledState();
