/*
    Settings navigation guard.

    Settings renders behind a router branch, so leaving UNMOUNTS it while these
    state modules — module-level singletons — survive. The edits are therefore
    not lost on the way out; they are lost on the way BACK, when the remount's
    load() applies the daemon's stored values over the draft. That timing is why
    the guard lives here at the exit: by the time the loss happens there is no
    longer anything to ask about.

    Preserving the drafts instead was considered and rejected (operator's
    ruling, 2026-08-04). Station, Forwarding, Email and Enrichment all PUT the
    whole block built from the draft, so a draft preserved across an absence
    rewrites every field at its stale value the next time it is saved — the
    defect clean-room review dcb0316e69b9 filed as P1. Keeping the reload is
    what keeps that fix intact.

    RIGS IS NOT EXEMPT, though it looks like it should be. Its #applyFetched
    re-baselines only pristine drafts, so a dirty rig draft survives THAT step
    (review Rigs-editor #6) — and load() then wipes drafts and baselines
    outright two statements later. The preservation is real and is immediately
    undone by its own caller. An earlier version of this file exempted Rigs from
    the prompt on the strength of the first half; the drafts were silently lost
    on return exactly like everyone else's. Read to the END of the sequence.

    WHILE A SECTION IS SAVING, LEAVING IS REFUSED OUTRIGHT. The prompt promises
    a discard, and a PUT already on the wire cannot be recalled: it lands, the
    daemon persists the edits the operator was just told were thrown away, and
    #apply() writes them back into the form. Offering a choice we cannot honour
    is worse than making them wait — and the wait is bounded, because these
    config writes use safeFetch's default 15 s timeout, so a wedged daemon
    cannot strand anyone in Settings.
*/

import { setLeaveGuard } from '../router.svelte';
import { toasts } from '../ui/toasts.svelte';
import { emailState } from './email.svelte';
import { enrichmentState } from './enrichment.svelte';
import { forwardingState } from './forwarding.svelte';
import { ft8SettingsState } from './ft8.svelte';
import { rigsState } from './rigs.svelte';
import { stationState } from './station.svelte';

interface Section {
    /** As it appears on the tab strip — the prompt has to name it findably. */
    label: string;
    dirty: () => boolean;
    /** A write is in flight; its outcome is no longer ours to promise away. */
    saving: () => boolean;
    /** Drop the draft back to its last loaded values. */
    discard: () => void;
}

// Tab order, matching Settings.svelte — the prompt reads as a walk across the
// strip rather than an arbitrary list.
const SECTIONS: Section[] = [
    {
        label: 'Station',
        dirty: () => stationState.dirty,
        saving: () => stationState.saving,
        discard: () => stationState.reset(),
    },
    {
        // anyDirty, not dirty: `dirty` covers only the SELECTED rig, so a rig
        // edited and then switched away from would go uncounted.
        label: 'Rigs',
        dirty: () => rigsState.anyDirty,
        saving: () => rigsState.saving,
        discard: () => rigsState.discardDrafts(),
    },
    {
        label: 'FT8',
        dirty: () => ft8SettingsState.dirty,
        saving: () => ft8SettingsState.saving,
        discard: () => ft8SettingsState.reset(),
    },
    {
        label: 'Forwarding',
        dirty: () => forwardingState.dirty,
        saving: () => forwardingState.saving,
        discard: () => forwardingState.reset(),
    },
    {
        label: 'Email',
        dirty: () => emailState.dirty,
        saving: () => emailState.saving,
        discard: () => emailState.reset(),
    },
    {
        label: 'Enrichment',
        dirty: () => enrichmentState.dirty,
        saving: () => enrichmentState.saving,
        discard: () => enrichmentState.reset(),
    },
];

/** The sections an exit would cost the operator, in tab order. */
function atRisk(): Section[] {
    return SECTIONS.filter((s) => s.dirty());
}

/** Sections holding unsaved edits, in tab order. */
export function unsavedSections(): string[] {
    return atRisk().map((s) => s.label);
}

/** "Station, Email and Enrichment" — an Oxford-comma-free English list. */
function listOf(labels: string[]): string {
    if (labels.length <= 1) return labels[0] ?? '';
    return `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`;
}

export function leavePrompt(labels: string[]): string {
    return `Unsaved changes in ${listOf(labels)} will be discarded.\n\nLeave Settings?`;
}

/**
 * Install both guards. Returns the uninstall, so the caller's lifecycle owns
 * them rather than the module owning a listener nothing can take back.
 */
export function installSettingsGuards(): () => void {
    setLeaveGuard(() => {
        // A save in flight outranks the question: see the header. Refuse
        // silently-but-visibly — the click does nothing and a toast says why.
        const busy = SECTIONS.filter((x) => x.saving());
        if (busy.length > 0) {
            toasts.info(`Saving ${listOf(busy.map((x) => x.label))} — wait for it to finish.`);
            return false;
        }
        const unsaved = atRisk();
        if (unsaved.length === 0) return true;
        if (!window.confirm(leavePrompt(unsaved.map((s) => s.label)))) return false;
        // Keep the promise NOW rather than on return. The remount's load()
        // would clear these anyway, but until then the app holds edits it has
        // just told the operator are gone — and beforeunload would warn about
        // those same edits a second time, which is precisely the false alarm
        // this guard is supposed to be worth trusting about.
        unsaved.forEach((s) => s.discard());
        return true;
    });

    const onUnload = (e: BeforeUnloadEvent): void => {
        if (unsavedSections().length === 0) return;
        // The browser shows its own wording and ignores any message we supply,
        // so there is nothing to say here — only whether to ask at all.
        //
        // MDN, Window: beforeunload event: the dialog is triggered by
        // "Calling the event object's preventDefault() method" or "Setting the
        // event object's returnValue property to a non-empty string value or
        // any other truthy value", and "The last two mechanisms are legacy
        // features; best practice is to trigger the dialog by invoking
        // preventDefault() on the event object, while also setting returnValue
        // to support legacy cases" — its example annotates returnValue
        // "Included for legacy support, e.g. Chrome/Edge < 119". Hence both.
        e.preventDefault();
        e.returnValue = true;
    };
    window.addEventListener('beforeunload', onUnload);

    return () => {
        setLeaveGuard(null);
        window.removeEventListener('beforeunload', onUnload);
    };
}
