/*
    Set as default (ADR 0097; W-0024 slice 5a): make another logbook of the
    active archive the default at an attended restart.

    The operator confirms first (ADR 0097 §4a, ADR 0087's policy): a Log in
    flight in this tab refuses; this tab's unfinished entry, an unknown Log
    outcome and this tab's unsaved Settings edits are named; unlogged work in
    any other window is stated as lost. Cancel sends nothing. The request names
    the default of this tab's established station context, never a fresh read:
    a status naming another default means this tab is stale, and it reloads.

    From the request until the restart is proven, live work in this tab is
    gated (defaultChangeGate, wired by main.ts): the reload discards it. An
    answer that cannot prove what happened is reconciled — by the new instance,
    else by the status read — and the request is never retried.
*/
import {
    fetchDefaultChangeStatus,
    requestDefaultChange,
    type DefaultChangeRequestOutcome,
    type DefaultChangeStatus,
} from '../api/default-change';
import { fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { fetchArchiveBindings } from '../api/archive-bindings';
import { boundArchive, boundDefault } from '../api/archive-guard';
import { OUTCOME_UNKNOWN_LEAD } from '../api/_helpers';
import { submitState } from '../operate/qso.svelte';
import { toasts } from '../ui/toasts.svelte';
import {
    archiveSwitchGate,
    gateUnprovenRestart,
    reloadForAnotherDefault,
    reloadRestarted,
    settleRestart,
    unfinishedEntryText,
} from './archives.svelte';
import { logbooksState, type LogbookRow } from './logbooks.svelte';
import { listOf, savingSections, unsavedSections } from './unsaved';

interface DefaultChangeState {
    /** unread: not read yet; unsupported: the routes are not served (404);
     *  unreadable: no readable answer (never idle, never unsupported). */
    read: 'unread' | 'ok' | 'unsupported' | 'unreadable';
    status: DefaultChangeStatus | null;
    /** A Set as default request is on the wire, or its restart awaited. */
    requesting: boolean;
}

/** Whether this daemon serves Set as default, and what it last reported. */
export const defaultChangeState: DefaultChangeState = $state({
    read: 'unread',
    status: null,
    requesting: false,
});

const RESTARTED_WITH_ANOTHER_DEFAULT =
    'The daemon restarted with another default logbook; this page must reload to rebind.';

/** Why live work in this tab is refused while a change is pending, or null.
 *  Wired into the operate seams by main.ts, beside the archive gate. */
export function defaultChangeGate(): string | null {
    if (!defaultChangeState.requesting && !admissionSealed()) return null;
    return 'A default logbook change is in progress — wait for the daemon to restart and the page to reload.';
}

/** pending_restart: the daemon holds its admission seals until it restarts,
 *  though the status still names the old default (ADR 0097 §4). Unlike
 *  pending_unclear, where admission has reopened. */
function admissionSealed(): boolean {
    return defaultChangeState.status?.change?.state === 'pending_restart';
}

// Overlapping reads: only the latest one lands.
let readGeneration = 0;

/** Read whether Set as default is served, and any change in play. */
export async function loadDefaultChange(): Promise<void> {
    const generation = ++readGeneration;
    const out = await fetchDefaultChangeStatus();
    if (generation !== readGeneration) return;
    if (out.kind === 'ok') {
        defaultChangeState.read = 'ok';
        defaultChangeState.status = out.status;
        return;
    }
    defaultChangeState.read = out.kind === 'unsupported' ? 'unsupported' : 'unreadable';
    defaultChangeState.status = null;
}

function isCurrentDefault(row: LogbookRow): boolean {
    const effective = defaultChangeState.status?.effective.uuid;
    return (
        row.id === logbooksState.defaultId ||
        (row.uuid !== '' && (row.uuid === boundDefault() || row.uuid === effective))
    );
}

/** Whether a row offers Set as default: hidden (offered false, no reason),
 *  offered, or shown disabled with the reason. */
export function setDefaultOffer(row: LogbookRow): { offered: boolean; reason: string } {
    const read = defaultChangeState.read;
    if (read === 'unread' || read === 'unsupported' || isCurrentDefault(row)) {
        return { offered: false, reason: '' };
    }
    if (read === 'unreadable') {
        return {
            offered: false,
            reason: 'The default logbook’s status could not be read; reopen Settings to try again.',
        };
    }
    if (defaultChangeState.status?.change?.unconfirmed) {
        return { offered: false, reason: 'A default logbook change is already in progress.' };
    }
    if (row.uuid === '') {
        return {
            offered: false,
            reason: 'The daemon did not report this logbook’s identifier, so it cannot be named.',
        };
    }
    const station =
        logbooksState.stationCallsign || (defaultChangeState.status?.effective.callsign ?? '');
    if (row.callsign.toUpperCase() !== station.toUpperCase()) {
        return {
            offered: false,
            reason: `Its callsign ${row.callsign} is not the station callsign ${station}: only a logbook for the station callsign can be the default.`,
        };
    }
    return { offered: true, reason: '' };
}

/** What stops a change now, or null: a Log in flight (its answer would be lost
 *  to the reload), an archive switch or verification, or a Settings write on
 *  the wire. Checked before the prompt, after each await, and after it. */
function refusal(): string | null {
    if (submitState.busy) {
        return 'A QSO is being logged on Phone / CW — wait for it to finish, then change the default.';
    }
    const gate = archiveSwitchGate();
    if (gate !== null) return gate;
    const saving = savingSections();
    if (saving.length > 0) return `Saving ${listOf(saving)} — wait for it to finish.`;
    return null;
}

/**
 * ADR 0097 §5: an enabled SM Cloud binding on the current default that is not
 * yet adopted, reserved or born on identity can start its adoption only while
 * its logbook is the default. Detected from the bindings read — the daemon's
 * projection locks a claimed adoption's fields and hides those a binding born
 * on identity does not use (internal/archive/bindings.go). Unreadable bindings
 * give the note conditionally rather than drop it.
 */
async function smCloudNote(defaultUuid: string, name: string): Promise<string> {
    const archive = boundArchive();
    const out = archive !== null ? await fetchArchiveBindings(archive) : null;
    const effect =
        `its existing uploads continue, but to start its adoption later, ` +
        `make “${name}” the default again.`;
    if (out === null || out.kind !== 'ok') {
        return (
            `If “${name}” uploads to SM Cloud and its binding is not yet adopted, ${effect} ` +
            '(The SM Cloud bindings could not be read.)\n\n'
        );
    }
    const smcloud = out.bindings.destinations.find((d) => d.type === 'smcloud');
    const unmarked = smcloud?.logbooks.some(
        (l) =>
            l.logbook_uuid === defaultUuid &&
            l.enabled &&
            l.locked_fields.length === 0 &&
            l.hidden_fields.length === 0
    );
    if (!unmarked) return '';
    return `“${name}” uploads to SM Cloud and its binding is not yet adopted: ${effect}\n\n`;
}

function confirmText(target: LogbookRow, fromName: string, cloudNote: string): string {
    const label = `${target.name} (${target.callsign})`;
    const unsaved = unsavedSections();
    const edits =
        unsaved.length > 0
            ? `The reload loses the unsaved Settings edits in this window: ${listOf(unsaved)}. Cancel keeps them.\n\n`
            : '';
    return (
        `Set ‘${label}’ as default and restart Station Manager?\n\n` +
        `Live contacts — Phone / CW and FT8 — will go to “${label}” after the restart.\n\n` +
        unfinishedEntryText(`the Logbook “${fromName}”`, 'restarts') +
        edits +
        cloudNote +
        'Station Manager restarts (about 5 seconds; the connection drops briefly), then every ' +
        'open window reloads: unlogged work in any other open window — a Phone / CW QSO, ' +
        'unsaved Settings edits — is lost. Logged QSOs are never affected. ' +
        'Transmit must be idle and FT8 disarmed; nothing transmits until the restart completes.'
    );
}

// Set from the click until the outcome: a second click meanwhile is refused.
let inProgress = false;

/** Ask the operator, then request the change. True only when the daemon
 *  accepted it (the restart then proves it). */
export async function setAsDefault(
    rowId: number,
    confirm: (text: string) => boolean = (t) => window.confirm(t)
): Promise<boolean> {
    if (inProgress) return false;
    const target = logbooksState.rows.find((r) => r.id === rowId);
    if (!target || !setDefaultOffer(target).offered) return false;
    const believed = boundDefault();
    if (believed === null) {
        toasts.error('This page has no confirmed default logbook, so nothing was sent. Reload it.');
        return false;
    }
    const stop = refusal();
    if (stop !== null) {
        toasts.error(stop);
        return false;
    }
    const effective = defaultChangeState.status?.effective;
    if (effective && effective.uuid !== believed) {
        reloadForAnotherDefault();
        return false;
    }
    inProgress = true;
    try {
        return await confirmAndRequest(target, believed, confirm);
    } finally {
        inProgress = false;
        defaultChangeState.requesting = false;
    }
}

function refusedAfterWait(believed: string): boolean {
    const stop = refusal();
    if (stop !== null) {
        toasts.error(stop);
        return true;
    }
    return boundDefault() !== believed;
}

async function confirmAndRequest(
    target: LogbookRow,
    believed: string,
    confirm: (text: string) => boolean
): Promise<boolean> {
    const fromName =
        logbooksState.rows.find((r) => r.uuid === believed)?.name ??
        defaultChangeState.status?.effective.name ??
        'the current default';
    const note = await smCloudNote(believed, fromName);
    if (refusedAfterWait(believed)) return false;
    if (!confirm(confirmText(target, fromName, note))) return false;
    // Checked again: a Log, a save or a switch may have started while the
    // prompt was open.
    if (refusedAfterWait(believed)) return false;
    defaultChangeState.requesting = true;
    // The baseline instance, read BEFORE the request, is what tells the
    // restarted daemon from the old one answering on a kept-alive connection.
    // Without it a restart cannot be proven, so nothing is sent.
    const before = await fetchDaemonInstance();
    if (before === '') {
        toasts.error(
            'Station Manager could not read the daemon’s identity, so it could not confirm the restart. Nothing was changed; try again.'
        );
        return false;
    }
    // Checked again: the live-work gate does not stop a Settings write or an
    // archive operation starting during that read.
    if (refusedAfterWait(believed)) return false;
    const out = await requestDefaultChange(target.uuid, believed);
    return settle(out, before, target);
}

async function settle(
    out: DefaultChangeRequestOutcome,
    before: string,
    target: LogbookRow
): Promise<boolean> {
    if (out.kind === 'accepted') {
        toasts.info(
            out.durability === 'uncertain'
                ? `Making “${target.name}” the default — the request was recorded (durability unconfirmed); restarting the daemon…`
                : `Making “${target.name}” the default — restarting the daemon…`
        );
        await settleRestart(before, RESTARTED_WITH_ANOTHER_DEFAULT);
        return true;
    }
    if (out.kind === 'refused') {
        if (out.code === 'default_stale') {
            reloadForAnotherDefault();
            return false;
        }
        // The daemon answered: the drafts stay, and the list and status show
        // what it holds now (a failed restart leaves a record to show).
        toasts.error(out.message);
        await Promise.all([logbooksState.load(), loadDefaultChange()]);
        return false;
    }
    toasts.warn(`${OUTCOME_UNKNOWN_LEAD} Waiting to see whether the daemon restarts…`);
    await reconcileUnknown(before);
    return false;
}

/** An unknown outcome: a new instance proves the restart; otherwise the old
 *  daemon's status says what it holds. Only an unreadable status gates. */
async function reconcileUnknown(before: string): Promise<void> {
    if (await waitForDaemonBack(before)) {
        reloadRestarted(RESTARTED_WITH_ANOTHER_DEFAULT);
        return;
    }
    await loadDefaultChange();
    const status = defaultChangeState.status;
    if (defaultChangeState.read !== 'ok' || status === null) {
        gateUnprovenRestart(before, RESTARTED_WITH_ANOTHER_DEFAULT);
        return;
    }
    if (status.effective.uuid !== boundDefault()) {
        reloadForAnotherDefault();
        return;
    }
    if (admissionSealed()) {
        toasts.warn(
            'The daemon recorded the change and has not restarted yet; live work stays paused until it does.'
        );
        void watchSealedRestart(before);
        return;
    }
    toasts.warn(
        'The daemon has not restarted. Settings → Logbooks shows what it holds for the change.'
    );
    void logbooksState.load();
}

/** How many waits a sealed change keeps watching for the restart. */
const SEALED_WATCH_ROUNDS = 10;

async function watchSealedRestart(before: string): Promise<void> {
    for (let round = 0; round < SEALED_WATCH_ROUNDS && admissionSealed(); round++) {
        if (await waitForDaemonBack(before)) {
            reloadRestarted(RESTARTED_WITH_ANOTHER_DEFAULT);
            return;
        }
    }
}

/** Test seam. */
export function _resetDefaultChangeForTests(): void {
    readGeneration++;
    inProgress = false;
    defaultChangeState.read = 'unread';
    defaultChangeState.status = null;
    defaultChangeState.requesting = false;
}
