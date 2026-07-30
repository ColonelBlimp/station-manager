<script lang="ts">
    // Drive-collapse banner. Rendered at shell level beside TxAlarmBanner so it
    // is visible on every view: the daemon raised drive-alarm because the rig
    // was keyed and its own meter reported nothing for the whole silence
    // window, with the instrument known good from the receive-time stream.
    //
    // Deliberately NOT the stuck-TX banner, and deliberately not red. That one
    // is a safety emergency — the rig may be transmitting right now and the
    // operator should go and look at the radio. This one is a fault on a rig
    // that is behaving: nothing is on air, and what needs attention is the audio
    // path. Both sit in the same shell slot, so colour and wording are the only
    // things telling the operator which response is called for.
    //
    // There is no daemon clear for this alarm — nothing the daemon can observe
    // proves a drive fault is over — so dismissal is the only exit, and a NEW
    // alarm re-shows the banner.
    import { rig, dismissDriveAlarm } from '../operate/rig.svelte';

    // NO TENSED CLAIM ABOUT THE RIG'S KEY STATE, in either direction, and that
    // is deliberate — three review rounds were spent flipping between them.
    // The banner SPANS BOTH STATES: the daemon raises it mid-slot with the rig
    // still keyed (~9 s of a 12.6 s slot left), and there is no daemon clear, so
    // it then persists past the end of the slot until dismissed. "Is keyed"
    // becomes false at unkey — and is what the STUCK-TX banner means, so it
    // blurs the separation in the dangerous direction. "Was keyed" is false for
    // the whole time the operator is most likely to be reading it. Describe the
    // OBSERVATION instead; that stays true for the banner's whole life.
    //
    // Wording is otherwise bounded by what the detector actually establishes: the rig's
    // meter stopped reporting output for the silence window. It never observes a
    // zero reading (the rig pushes on change, so no drive means no frames at
    // all), never waits for the transmission to end, and fires equally when
    // output was present and then collapsed. Claiming an all-slot zero would be
    // false in that second case and would send the operator looking for a fault
    // that never happened.
    const CODE_TEXT: Record<string, string> = {
        drive_no_output:
            'Output may have failed from the start or collapsed part-way in — check the audio drive to the radio.',
    };

    const detail = $derived(CODE_TEXT[rig.driveAlarmCode] ?? '');
    const show = $derived(rig.driveAlarmActive && !rig.driveAlarmDismissed);

    // WHEN it fired, as a wall clock. The operator asked for absolute rather than
    // relative time, which also means no refresh timer: "3 minutes ago" would go
    // stale silently, and staleness is the exact fault this closes. Time-of-day
    // only, matching the daemon log lines the operator reads alongside it.
    const p2 = (n: number): string => String(n).padStart(2, '0');
    const firedAt = $derived(
        rig.driveAlarmAt === null
            ? ''
            : `${p2(rig.driveAlarmAt.getHours())}:${p2(rig.driveAlarmAt.getMinutes())}:${p2(rig.driveAlarmAt.getSeconds())}`
    );

    // Reported only once the daemon has WATCHED a later transmission and seen
    // output — never inferred here from time passing or from frames resuming. It
    // does not hide the banner: the rig came back, but nobody has looked at it.
    const recovery = $derived(
        rig.driveAlarmRecovered
            ? 'Output has been normal since: the meter reported output on a later transmission.'
            : ''
    );
</script>

{#if show}
    <div
        class="flex items-center gap-x-3 border-b border-amber-700 bg-amber-500 px-4 py-2 text-sm font-medium text-black"
        role="alert"
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
                d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"
            />
        </svg>
        <span>
            <strong>NO RF OUTPUT</strong> — the power meter reported nothing for several seconds{firedAt
                ? ` at ${firedAt}`
                : ''}.
            {detail}
            {recovery}
        </span>
        <button
            type="button"
            class="ml-auto shrink-0 cursor-pointer rounded border border-black/30 px-2 py-0.5 text-xs hover:bg-amber-600"
            onclick={dismissDriveAlarm}
        >
            Dismiss
        </button>
    </div>
{/if}
