// Archive-scoped browser stores (ADR 0094 and its 2026-10-23 update; W-0021
// "alpha.4 hold: fix design", revision 3, slice 4). The Session list and the FT8
// engagement set are kept per QSO archive within the tab, under keys carrying the
// archive's UUID. Which one a page uses is decided ONCE, by the archive its boot
// bracket proved (archives.svelte.ts) — never by a label, an instance or whichever
// archive a later read reports; another archive always means a reload.
//
// Every write carries the archive of the daemon that produced it: the FT8
// connection's identity label (ft8-sse.ts) or the submit response's archive_id.
//   - matching the bound archive → applied;
//   - before binding → buffered in memory, tagged; the proof releases ONLY the
//     records tagged with the archive it proved and discards the rest, so a later
//     identity read can never adopt an earlier buffer (R3: lost if the boot fails
//     and reloads — an acknowledged panel-history limitation; the QSOs are durable);
//   - another archive → nothing is written anywhere, and verification starts at
//     once rather than waiting for the always-on stream to notice the switch;
//   - unlabelled → a protocol fault: dropped, never buffered.

import { sessionGet, sessionRemove } from '../utils/storage';
import { toasts } from '../ui/toasts.svelte';

export interface ArchiveStore {
    /** Hydrate this store from its key for `archiveId`. */
    load: (archiveId: string) => void;
    /** Forget the in-memory state (a fresh page; storage is untouched). */
    clear: () => void;
}

const stores: ArchiveStore[] = [];
// Reactive so a component deriving sessionActionBlock() updates when binding lands.
const binding: { archiveId: string | null } = $state({ archiveId: null });
let pending: { archiveId: string; apply: () => void }[] = [];
let onMismatch: () => void = () => {};
let gate: () => string | null = () => null;

/** The pre-ADR-0094 unscoped keys (R2, R4): discarded after proof, never adopted. */
const LEGACY_KEYS = ['sm.session.qsos', 'sm.ft8.engaged'];
const RESET_NOTICE =
    'Session history was reset by this update. Your logged QSOs are intact in the Logbook.';
const UNPROVEN = 'Waiting until this tab has confirmed its archive.';

/** A store joins the scheme at module init; one registering after the proof
 *  loads at once. */
export function registerArchiveStore(store: ArchiveStore): void {
    stores.push(store);
    if (binding.archiveId !== null) store.load(binding.archiveId);
}

/** The archive the stores are bound to; null until the boot bracket proves one. */
export function storesArchive(): string | null {
    return binding.archiveId;
}

/**
 * Bind the stores to the archive the boot bracket proved. Set once per page: a
 * second proof never rebinds (another archive means a reload). Hydrates every
 * store, releases the buffered records tagged with THIS archive, discards the
 * rest, then resets the legacy unscoped keys — only now, after proof, so a failed
 * boot keeps them for the reload.
 */
export function bindArchiveStores(archiveId: string): void {
    if (binding.archiveId !== null || archiveId === '') return;
    binding.archiveId = archiveId;
    for (const s of stores) s.load(archiveId);
    const released = pending;
    pending = [];
    for (const r of released) if (r.archiveId === archiveId) r.apply();
    resetLegacy();
}

// The notice shows whenever a legacy key was found. A remove that fails leaves the
// key behind, so the next proven boot discards it again and the notice repeats:
// the accepted limitation, not a once-only guarantee.
function resetLegacy(): void {
    let found = false;
    for (const k of LEGACY_KEYS) {
        if (sessionGet(k) === null) continue;
        found = true;
        sessionRemove(k);
    }
    if (found) toasts.info(RESET_NOTICE);
}

/** File one write produced by the daemon serving `archiveId` (see the header). */
export function fileUnderArchive(archiveId: string | null, what: string, apply: () => void): void {
    if (archiveId === null || archiveId === '') {
        console.warn(`[archive-stores] dropped ${what}: no archive identity on its source`);
        return;
    }
    if (binding.archiveId === null) {
        pending.push({ archiveId, apply });
        return;
    }
    if (archiveId !== binding.archiveId) {
        onMismatch();
        return;
    }
    apply();
}

/** Starts verification (archives.svelte.ts) when a write names another archive. */
export function setStoresMismatchSink(sink: () => void): void {
    onMismatch = sink;
}

/** The archive layer's reason to hold Session actions (a check or switch pending). */
export function setSessionActionGate(g: () => string | null): void {
    gate = g;
}

/** Why Session edit, export and email are unavailable now; null when they may run.
 *  Unbound pages are held too: their list belongs to no proven archive yet. */
export function sessionActionBlock(): string | null {
    return binding.archiveId === null ? UNPROVEN : gate();
}

/** Test seam: a fresh page on the same tab — unbound, buffer and memory cleared,
 *  sessionStorage kept (an archive switch is exactly this). */
export function _reloadArchiveStoresForTests(): void {
    binding.archiveId = null;
    pending = [];
    for (const s of stores) s.clear();
}
