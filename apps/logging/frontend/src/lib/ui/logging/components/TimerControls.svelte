<script lang="ts">
    import {qsoState, qsoTimerState} from "$lib/states/new-qso-state.svelte";
    import {shortcut} from "@svelte-put/shortcut";
    import {isValidCallsignForLog, isValidCallsignLength} from "$lib/constants/callsign";
    import {buildTrigger, SHORTCUTS} from "$lib/constants/shortcuts";
    import {getFocusContext} from "@station-manager/shared-utils/svelte";

    const {focus} = getFocusContext();

    let cannotStart = $derived.by(() => {
        return qsoTimerState.running || qsoState.call === '';
    });
    let cannotStop = $derived.by(() => {
        return !qsoTimerState.running;
    })

    const onclickStopTimer = (): void => {
        qsoState.stopTimer();
        focus('callsignInput');
    }

    const onclickStartTimer = (): void => {
        qsoState.startTimer();
    }

    const toggleTimer = (): void => {
        if (!isValidCallsignLength(qsoState.call) || !isValidCallsignForLog(qsoState.call)) return;
        if (qsoState.isTimerRunning()) {
            qsoState.stopTimer();
        } else {
            qsoState.startTimer();
        }
    }
</script>

<div class="flex gap-x-1 w-13.5">
    <button
            disabled={cannotStop}
            aria-label="stop"
            class="cursor-pointer disabled:text-gray-400 disabled:cursor-default enabled:text-red-500"
            title="{SHORTCUTS.TOGGLE_TIMER.displayKey}: Stop the QSO Timer"
            onclick={onclickStopTimer}>
        <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
            <path stroke-linecap="round" stroke-linejoin="round"
                  d="M5.25 7.5A2.25 2.25 0 0 1 7.5 5.25h9a2.25 2.25 0 0 1 2.25 2.25v9a2.25 2.25 0 0 1-2.25 2.25h-9a2.25 2.25 0 0 1-2.25-2.25v-9Z"/>
        </svg>
    </button>
    <button
            disabled={cannotStart}
            onclick={onclickStartTimer}
            aria-label="start"
            class="mr-8 cursor-pointer disabled:text-gray-400 disabled:cursor-default enabled:text-green-700"
            title="{SHORTCUTS.TOGGLE_TIMER.displayKey}: Start the QSO Timer">
        <svg fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
            <path stroke-linecap="round" stroke-linejoin="round"
                  d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.347a1.125 1.125 0 0 1 0 1.972l-11.54 6.347a1.125 1.125 0 0 1-1.667-.986V5.653Z"/>
        </svg>
    </button>
</div>
<svelte:window
        use:shortcut={{
        trigger: [
            buildTrigger('TOGGLE_TIMER', toggleTimer),
        ],
    }}
></svelte:window>