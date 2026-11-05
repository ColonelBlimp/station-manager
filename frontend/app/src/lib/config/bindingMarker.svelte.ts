/*
    The read-only binding marker (ADR 0099 W1-2; reviewer contract 2026-11-05).

    Four attributes on the root element let an acceptance driver observe,
    passively and in every window (the FT8 view included), what this tab is
    bound to — without clicking, sending, or reading module internals:
      data-sm-archive            the actual bound archive UUID ('' = none)
      data-sm-default            the established default logbook UUID ('' = none)
      data-sm-binding            verifying | verified | unresolved | unproven
      data-sm-verified-instance  the daemon instance accepted by the latest
                                 SUCCESSFUL binding verification ('' = none)

    OBSERVATIONAL ONLY. It reflects the existing binding and gates; no
    application decision reads these attributes (a source guard in the tests
    pins that), and publishing them makes no request.

    'verified' only when no verification is pending AND neither existing gate
    (archiveSwitchGate, defaultChangeGate) blocks operations AND a successful
    verification has been recorded. Otherwise:
      unresolved — an archive switch or a failed check latched the overlay;
      verifying  — a check, an activation or a default change is in progress,
                   or the default change's status has not been read yet;
      unproven   — nothing proven: no bound archive, no recorded verification,
                   or the default change's status could not be read.
*/
import { boundArchive, boundDefault } from '../api/archive-guard';
import { archiveSwitchGate, archivesState, bindingEvidence } from './archives.svelte';
import { defaultChangeGate, defaultChangeState } from './defaultChange.svelte';

export type BindingState = 'verifying' | 'verified' | 'unresolved' | 'unproven';

export interface BindingMarker {
    archive: string;
    default: string;
    binding: BindingState;
    verifiedInstance: string;
}

function bindingState(archive: string): BindingState {
    if (archivesState.switchUnresolved) return 'unresolved';
    const gated = archiveSwitchGate() !== null || defaultChangeGate() !== null;
    if (gated) {
        if (archivesState.verifying || archivesState.activating) return 'verifying';
        if (defaultChangeState.requesting || defaultChangeState.sealed) return 'verifying';
        return defaultChangeState.read === 'unread' ? 'verifying' : 'unproven';
    }
    if (archive === '' || bindingEvidence.instance === '') return 'unproven';
    return 'verified';
}

export function computeBindingMarker(): BindingMarker {
    void bindingEvidence.revision; // the bound archive/default live outside $state
    const archive = boundArchive() ?? '';
    return {
        archive,
        default: boundDefault() ?? '',
        binding: bindingState(archive),
        verifiedInstance: bindingEvidence.instance,
    };
}

/** Write the marker onto `el`. Call inside an $effect: it re-runs on every
 *  change of the state it reads, and writes attributes only. */
export function publishBindingMarker(el: Element): void {
    const m = computeBindingMarker();
    el.setAttribute('data-sm-archive', m.archive);
    el.setAttribute('data-sm-default', m.default);
    el.setAttribute('data-sm-binding', m.binding);
    el.setAttribute('data-sm-verified-instance', m.verifiedInstance);
}
