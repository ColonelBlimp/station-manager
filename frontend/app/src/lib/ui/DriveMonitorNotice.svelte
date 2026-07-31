<script lang="ts">
    // "Drive monitoring off" — rendered at shell level beside DriveAlarmBanner.
    //
    // The daemon's drive-collapse detector works by timing GAPS in the rig's own
    // meter push stream, and that stream carries whichever meter the rig has
    // SELECTED. Only PO says anything about RF: on ALC (or COMP, or SWR) a
    // correctly-driven FT8 signal reads near zero, the rig pushes on change, and
    // a meter parked at zero simply goes quiet. So the daemon declines to arm —
    // and this tells the operator, because being silently unprotected is worse
    // than being told (operator's instruction, 2026-07-31, after two false NO RF
    // OUTPUT alarms raised while RF was leaving the rig normally).
    //
    // SEPARATE COMPONENT FROM THE ALARM, not a branch inside it, because the exit
    // contracts are opposites. The alarm has no daemon clear — nothing observable
    // proves a drive fault is over — so dismissal is its only exit. This state
    // ends observably: the rig reports the meter selection, so the daemon knows.
    // Giving it a Dismiss button would let the operator hide something still true.
    //
    // status, not alert: nothing has failed. The operator is being told a check
    // is not running, which is information, not a fault — and the two must never
    // read as each other, since they call for opposite responses.
    import { rig } from '../operate/rig.svelte';

    // Driven by the daemon's code, never by comparing meter names here: the rule
    // for what permits monitoring lives in one place (driveMonitorFor, in
    // internal/bridge/drivealarm.go) and is read both by the detector that acts
    // on it and by this notice. Re-deriving it from a raw meter name would let
    // the banner claim monitoring is on while the detector had declined to arm.
    const show = $derived(rig.driveMonitor === 'meter_not_po');
</script>

{#if show}
    <div
        class="flex items-center gap-x-3 border-b border-sky-700 bg-sky-100 px-4 py-2 text-sm text-sky-950"
        role="status"
    >
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            class="size-5 shrink-0"
            aria-hidden="true"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.852l.041-.02M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z"
            />
        </svg>
        <span>
            <strong>Drive monitoring off</strong> — the rig's meter is not on PO, so a quiet meter says
            nothing about your output. Set the rig's meter back to PO to restore it.
        </span>
    </div>
{/if}
