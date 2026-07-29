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

    const CODE_TEXT: Record<string, string> = {
        drive_no_output:
            'Its power meter read zero for the whole transmission — check the audio drive to the radio.',
    };

    const detail = $derived(CODE_TEXT[rig.driveAlarmCode] ?? '');
    const show = $derived(rig.driveAlarmActive && !rig.driveAlarmDismissed);
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
            <strong>NO RF OUTPUT</strong> — the rig was keyed but produced nothing.
            {detail}
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
