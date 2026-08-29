/*
    PT-6 config-save durability caveat.

    A PUT /v1/config can come back with the optional `durability:"unconfirmed"` field:
    the save is APPLIED and live on disk, but the daemon could not confirm it will
    survive a crash (the parent-directory fsync failed after the atomic rename). The
    operator must see ONE unambiguous outcome, so a Settings save suppresses its
    ordinary "Saved" toast when this fires — noteConfigDurability reports whether it
    did — while first-run setup, which has no success toast, just surfaces the caveat
    and continues.
*/
import { toasts } from '../ui/toasts.svelte';
import { isPlainObject } from '../api/_helpers';

/** Reads the optional PT-6 durability caveat from a PUT /v1/config response body. */
export function isDurabilityUnconfirmed(body: unknown): boolean {
    return isPlainObject(body) && body.durability === 'unconfirmed';
}

/**
 * Shows the single combined "applied, durability unconfirmed" outcome toast and returns
 * true WHEN it did — so the caller suppresses its ordinary "Saved" toast. false (and no
 * toast) on an ordinary durable save, so the caller shows its normal success message.
 */
export function noteConfigDurability(unconfirmed: boolean): boolean {
    if (!unconfirmed) return false;
    toasts.warn(
        'Configuration saved and live — but the daemon could not confirm the change will survive a crash (directory sync failed). Re-save to try again.'
    );
    return true;
}
