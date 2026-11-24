/*
    What closing the app would stop or lose, as the page knows it (W-0027 A3, section A).

    Three read-only attributes on the root element, of their own and separate from the
    binding marker (bindingMarker.svelte.ts, whose contract is pinned elsewhere):
      data-sm-close-entry    '' | draft | unknown
                             draft: a contact is being entered;
                             unknown: a log attempt's outcome is not known (in flight, or
                             reported uncertain). It may already be stored, so it is never
                             also reported as a draft to discard: the contact is named once.
      data-sm-close-unsaved  Settings sections with unsaved changes, by their tab names,
                             separated by '|'; '' = none
      data-sm-close-saving   Settings sections being saved, the same way. A section being
                             saved is usually still unsaved until its answer comes; it is
                             listed here only, so it is named once.

    The desktop app reads them when a close is asked for and names them in its one question
    (desktop/src/closing.mjs). THEY DECIDE ONLY WHETHER TO ASK AND WHICH LINES ARE SHOWN:
    what a close does never depends on them, and the app accepts only the values it knows.
    Nothing here says anything about FT8: that is the daemon's own word (GET /v1/ft8/state).

    Observational only: attributes written, nothing read back, no request made.
*/
import { savingSections, unsavedSections } from './config/unsaved';
import { draftInProgress, logOutcomeUnknown } from './operate/qso.svelte';

export type EntryReason = '' | 'draft' | 'unknown';

export interface CloseReasons {
    entry: EntryReason;
    unsaved: string[];
    saving: string[];
}

function entryReason(): EntryReason {
    if (logOutcomeUnknown()) return 'unknown';
    return draftInProgress() ? 'draft' : '';
}

export function computeCloseReasons(): CloseReasons {
    const saving = savingSections();
    return {
        entry: entryReason(),
        unsaved: unsavedSections().filter((label) => !saving.includes(label)),
        saving,
    };
}

/** Write the reasons onto `el`. Call inside an $effect: it re-runs on every change of the
 *  state it reads, and writes attributes only. */
export function publishCloseReasons(el: Element): void {
    const r = computeCloseReasons();
    el.setAttribute('data-sm-close-entry', r.entry);
    el.setAttribute('data-sm-close-unsaved', r.unsaved.join('|'));
    el.setAttribute('data-sm-close-saving', r.saving.join('|'));
}
