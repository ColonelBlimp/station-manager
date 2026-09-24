/*
    QSO archives state (ADR 0071, W-0021 slice 4) — one flow, two entry points:
    the Settings → Archives tab and the header's archive selector both call
    `activate`. The attended restart is the switch: on a 202 the daemon restarts
    and the SPA waits for the NEW instance (the same reconciliation the Settings
    restart uses, ADR 0078), then reloads the catalogue. Nothing here ever calls
    an archive active on its own: the list shows the daemon's state (AC 4).
*/
import {
    activateQsoArchive,
    createQsoArchive,
    fetchQsoArchives,
    fetchDaemonIdentity,
    type CreateArchiveInput,
    type DaemonIdentity,
    type QsoArchive,
} from '../api/qso-archives';
import { fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { OUTCOME_UNKNOWN_LEAD } from '../api/_helpers';
import { toasts } from '../ui/toasts.svelte';

export const archivesState: {
    list: QsoArchive[];
    loaded: boolean;
    loading: boolean;
    error: string;
    /** The list is RETAINED from an earlier read after a later one failed: it may
     *  not be the daemon's current catalogue. Shown as such; cleared by a
     *  successful read. Nothing names an active archive from a stale list. */
    stale: boolean;
    creating: boolean;
    /** An activation request is in flight, or accepted and the restart awaited. */
    activating: boolean;
    /** An archive switch was requested (accepted, or its request timed out) and
     *  the SPA could not prove which daemon generation it now faces. Every
     *  archive-scoped store may be bound to the OLD archive, so the app is
     *  gated until a reload (review P1: fail closed). Cleared only by reload. */
    switchUnresolved: boolean;
    switchDetail: string;
    /** An identity verification (boot bracket or reconnect check) is in
     *  flight: the binding is unproven until it settles, so operations are
     *  refused meanwhile (review P1) without raising the overlay. */
    verifying: boolean;
} = $state({
    list: [],
    loaded: false,
    loading: false,
    error: '',
    stale: false,
    creating: false,
    activating: false,
    switchUnresolved: false,
    switchDetail: '',
    verifying: false,
});

/** The message that blocks an archive-scoped operation (QSO submit, FT8
 *  admission, another activation) while a switch is in flight or unresolved;
 *  null when operations may proceed. Wired into the operate seams by main.ts. */
export function archiveSwitchGate(): string | null {
    if (archivesState.switchUnresolved) {
        return 'An archive switch is unresolved — reload the page before logging or transmitting.';
    }
    if (archivesState.activating) {
        return 'An archive switch is in progress — wait for the daemon to restart and the page to reload.';
    }
    if (archivesState.verifying) {
        return 'Station Manager is confirming which archive the daemon serves — try again in a moment.';
    }
    return null;
}

// A full page reload is how the SPA rebinds EVERY archive-scoped store after a
// switch (review P1): the station context (default logbook id, name, count), the
// Phone/CW submission target, duplicate checks, a mounted Logbook view and the
// FT8 state are all fetched at boot for the archive the daemon serves, and a
// partial reconciliation would leave one of them logging into the old archive.
// Guarded: it runs only once a DIFFERENT daemon instance answered AND the fresh
// catalogue read succeeded — never on an unknown outcome. Injectable for tests
// (jsdom cannot reload).
let reloadPage: () => void = () => window.location.reload();

/** Every reload the store requests goes through here: the gate is LATCHED
 *  first (review P1), so an unload the operator cancels — the Settings
 *  leave-guard prompts when it holds unsaved edits — leaves a page that is
 *  still gated, never one running on stale bindings with the gate open. */
function requestReload(detail: string): void {
    archivesState.switchUnresolved = true;
    archivesState.switchDetail = detail;
    reloadPage();
}

/** The gate's own way out: reload now. */
export function reloadNow(): void {
    reloadPage();
}

/** How many 30 s waits the unresolved gate keeps watching for the new instance
 *  before it stops polling (the overlay stays; the operator reloads). */
const WATCH_ROUNDS = 10;

export function activeArchive(): QsoArchive | undefined {
    return archivesState.list.find((a) => a.state === 'active');
}

export function pendingArchive(): QsoArchive | undefined {
    return archivesState.list.find((a) => a.state === 'pending');
}

/** Read the catalogue. Resolves true on a fresh successful read; on failure the
 *  earlier list is retained but marked stale, with the error set. */
export async function loadArchives(): Promise<boolean> {
    archivesState.loading = true;
    try {
        const out = await fetchQsoArchives();
        if (out.kind === 'ok') {
            archivesState.list = out.archives;
            archivesState.loaded = true;
            archivesState.error = '';
            archivesState.stale = false;
            return true;
        }
        archivesState.error = out.message;
        archivesState.stale = archivesState.loaded;
        return false;
    } finally {
        archivesState.loading = false;
    }
}

/** A request key for one create attempt: kept by the form across retries so a
 *  retried submit returns the archive the first one made. */
export function mintRequestKey(): string {
    const c = globalThis.crypto;
    if (c && typeof c.randomUUID === 'function') return c.randomUUID();
    return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

/** Provision a managed archive. Resolves true when the daemon has it (created
 *  or reused) and the list is refreshed; the caller then clears its form. */
export async function createArchive(input: CreateArchiveInput): Promise<boolean> {
    if (archivesState.creating) return false;
    archivesState.creating = true;
    try {
        const out = await createQsoArchive(input);
        if (out.kind === 'ok') {
            toasts.info(
                out.reused
                    ? `“${out.archive.label}” already existed for this request; nothing new was created.`
                    : `Created archive “${out.archive.label}”. Activate it to start logging into it.`
            );
            await loadArchives();
            return true;
        }
        if (out.kind === 'refused') {
            toasts.error(out.message);
            return false;
        }
        if (out.kind === 'network') {
            // Unconfirmed, timed out or not: the creation may have landed. The
            // request key makes a resubmit reuse it rather than make a second.
            toasts.warn(
                `${OUTCOME_UNKNOWN_LEAD} The archive list has been refreshed; submit again with the same form to reuse a creation that did land.`
            );
            await loadArchives();
            return false;
        }
        toasts.error(out.message);
        return false;
    } finally {
        archivesState.creating = false;
    }
}

/** The confirmation the operator answers before the daemon restarts. */
export function activateConfirmText(label: string): string {
    return (
        `Switch to the archive “${label}”?\n\n` +
        'Station Manager restarts to open it (about 5 seconds; the connection drops briefly), ' +
        'then this page reloads — unsaved Settings edits are lost. ' +
        'Transmit must be idle and FT8 disarmed; nothing transmits until the restart completes.'
    );
}

/**
 * Request that the next start serve archive `id`. `confirm` is the operator's
 * yes/no (window.confirm in the app; injected in tests). Returns true only when
 * the daemon ACCEPTED the request — the restart then proves the switch.
 */
export async function activateArchive(
    id: string,
    confirm: (text: string) => boolean = (t) => window.confirm(t)
): Promise<boolean> {
    if (archivesState.activating) return false;
    const target = archivesState.list.find((a) => a.id === id);
    if (archivesState.switchUnresolved) {
        toasts.error(archiveSwitchGate() ?? '');
        return false;
    }
    if (!target) {
        toasts.error('That archive is not in the catalogue any more; the list has been refreshed.');
        await loadArchives();
        return false;
    }
    if (target.state === 'active') return false;
    if (!confirm(activateConfirmText(target.label))) return false;
    archivesState.activating = true;
    try {
        // Capture the current instance BEFORE the request so the wait is for a
        // DIFFERENT process, not the old one answering on a kept-alive connection.
        const before = await fetchDaemonInstance();
        const out = await activateQsoArchive(id);
        if (out.kind === 'accepted') {
            toasts.info(
                out.durability === 'uncertain'
                    ? `Switching to “${target.label}” — the request was recorded (durability unconfirmed); restarting the daemon…`
                    : `Switching to “${target.label}” — restarting the daemon…`
            );
            await settleRestart(before);
            return true;
        }
        if (out.kind === 'refused') {
            toasts.error(out.message);
            await loadArchives(); // a pending_unclear leaves state the list must show
            return false;
        }
        if (out.kind === 'network') {
            // An UNCONFIRMED write, whatever the transport failure (ADR 0078; review
            // P1): the daemon may have recorded pending and be restarting — a reset
            // after the request left is as ambiguous as a timeout. Reconcile by the
            // new-instance signal and gate when it cannot be proven; never "failed".
            toasts.warn(`${OUTCOME_UNKNOWN_LEAD} Waiting to see whether the daemon restarts…`);
            await settleRestart(before);
            return false;
        }
        // The daemon ANSWERED and did not accept: nothing was written.
        toasts.error(out.message);
        return false;
    } finally {
        archivesState.activating = false;
    }
}

/**
 * After an accepted (or ambiguous) activation the SPA's bindings are suspect
 * until it reloads on the new daemon generation. A DIFFERENT instance answering
 * proves the generation: reload at once, whatever the catalogue says. Anything
 * else — no baseline instance to compare against, or the wait expiring — leaves
 * the generation unknown: the app is gated (archivesState.switchUnresolved:
 * overlay + the operate seams refuse) and, when a baseline exists, the new
 * instance is watched for a while longer and the reload follows on sight.
 */
async function settleRestart(before: string): Promise<void> {
    if (before !== '' && (await waitForDaemonBack(before))) {
        toasts.info('Daemon restarted. Reloading…');
        requestReload('The daemon restarted on another archive; this page must reload to rebind.');
        return;
    }
    archivesState.switchUnresolved = true;
    archivesState.switchDetail =
        before === ''
            ? 'The daemon’s identity could not be read before the switch, so the new one cannot be told from the old.'
            : 'The daemon did not answer as a new instance within the wait.';
    toasts.warn(`${OUTCOME_UNKNOWN_LEAD} Reload the page before logging or transmitting.`);
    if (before !== '') void keepWatching(before);
}

async function keepWatching(before: string): Promise<void> {
    for (let round = 0; round < WATCH_ROUNDS && archivesState.switchUnresolved; round++) {
        if (await waitForDaemonBack(before)) {
            toasts.info('Daemon restarted. Reloading…');
            requestReload(
                'The daemon restarted on another archive; this page must reload to rebind.'
            );
            return;
        }
    }
}

// ---- Every tab rebinds (review P1). The tab that requested the switch reloads
// itself; every OTHER open tab (Operate, Logbook, a second-monitor Map) only sees
// its streams reconnect. So each tab records the daemon's identity at boot and
// re-reads it on every reconnect of the always-on stream: a different served
// archive means its stores belong to the old one — reload; an identity it
// cannot read after retries means it cannot prove anything — gate.

let bootIdentity: DaemonIdentity | null = null;
const IDENTITY_TRIES = 3;

/**
 * Boot: run the archive-scoped reads (station context, catalogue) BRACKETED by
 * two identity reads, so the identity this tab records is provably the one its
 * stores were built against. Both reads agree → recorded. Both readable but
 * different → the boot straddled a restart or a switch: reload now. Either
 * unreadable (after retries) → nothing is proven, and the app must not run on
 * unproven bindings: the gate latches AT ONCE (review P1: an incomplete bracket
 * is fail-closed, not a deferred check) and a watch reloads the page as soon
 * as a daemon answers again, so a plain outage at boot heals itself.
 */
export async function bootArchiveScoped<T>(reads: () => Promise<T>): Promise<T> {
    archivesState.verifying = true; // the shell may open inside `reads`; nothing is admitted until the bracket decides
    let before: DaemonIdentity | null;
    let result: T;
    let after: DaemonIdentity | null;
    try {
        before = await readIdentityWithRetries();
        result = await reads();
        after = await readIdentityWithRetries();
    } finally {
        archivesState.verifying = false;
    }
    if (before !== null && after !== null) {
        if (before.instance === after.instance && before.archiveId === after.archiveId) {
            bootIdentity = after;
        } else {
            bootIdentity = null;
            toasts.info('The daemon changed while the page was loading. Reloading…');
            requestReload(
                'The daemon changed while the page was loading; this page must reload to rebind.'
            );
        }
        return result;
    }
    bootIdentity = null;
    archivesState.switchUnresolved = true;
    archivesState.switchDetail =
        'The daemon’s identity could not be read while the page was loading, so this tab cannot prove which archive its data belongs to. It reloads by itself once the daemon answers.';
    void watchForDaemon();
    return result;
}

/** After an unproven boot: reload as soon as any daemon instance answers (the
 *  reload's own bracket then proves the binding). Bounded like the switch
 *  watch; past it the overlay stays and the operator reloads. */
async function watchForDaemon(): Promise<void> {
    for (let round = 0; round < WATCH_ROUNDS && archivesState.switchUnresolved; round++) {
        if (await waitForDaemonBack('')) {
            requestReload(
                'The daemon answered again; this page must reload to prove its archive binding.'
            );
            return;
        }
    }
}

async function readIdentityWithRetries(): Promise<DaemonIdentity | null> {
    for (let i = 0; i < IDENTITY_TRIES; i++) {
        const id = await fetchDaemonIdentity();
        if (id !== null) return id;
    }
    return null;
}

/**
 * On a reconnect of the always-on stream: compare the daemon's served archive
 * with the one this tab booted on. Same archive → nothing (an ordinary
 * restart); a different archive → reload, so every store rebinds through the
 * boot path; unreadable after retries → gate (fail closed) until reload.
 */
export async function verifyArchiveGeneration(): Promise<void> {
    if (archivesState.switchUnresolved) return;
    archivesState.verifying = true;
    let now: DaemonIdentity | null;
    try {
        now = await readIdentityWithRetries();
    } finally {
        archivesState.verifying = false;
    }
    if (now === null) {
        archivesState.switchUnresolved = true;
        archivesState.switchDetail =
            'The connection came back but the daemon’s identity could not be read, so this tab cannot tell which archive it serves.';
        return;
    }
    if (bootIdentity === null) {
        // No proven baseline (the boot bracket could not read or agree): the
        // stores may belong to an earlier archive. Rebind — never adopt.
        toasts.info('Reconnected to the daemon; reloading to rebind.');
        requestReload(
            'This page reconnected without a proven archive binding; it must reload to rebind.'
        );
        return;
    }
    if (now.archiveId !== bootIdentity.archiveId) {
        toasts.info('The daemon now serves another archive. Reloading…');
        requestReload('The daemon now serves another archive; this page must reload to rebind.');
    }
}

/** Test seams. */
export function _setBootIdentityForTests(id: DaemonIdentity | null): void {
    bootIdentity = id;
}
export function _resetArchivesForTests(): void {
    bootIdentity = null;
    archivesState.list = [];
    archivesState.loaded = false;
    archivesState.loading = false;
    archivesState.error = '';
    archivesState.stale = false;
    archivesState.creating = false;
    archivesState.activating = false;
    archivesState.switchUnresolved = false;
    archivesState.switchDetail = '';
    archivesState.verifying = false;
}
export function _setReloadForTests(fn: () => void): void {
    reloadPage = fn;
}
