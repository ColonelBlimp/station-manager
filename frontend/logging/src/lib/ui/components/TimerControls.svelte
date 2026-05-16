<script lang="ts">
    interface Props {
        // Pre-derived disabled flags from the parent. The three-state
        // machine ("no lookup" / "lookup done, not started" / "started")
        // lives in QsoPanel — this component just renders the result.
        // Start is enabled only after F2 / Tab has fired a lookup for
        // the currently-typed callsign; Stop is enabled only while
        // the timer is running.
        startDisabled: boolean;
        stopDisabled: boolean;
        onStart: () => void;
        onStop: () => void;
    }

    let { startDisabled, stopDisabled, onStart, onStop }: Props = $props();
</script>

<div class="flex w-14 mt-7">
    <button
        type="button"
        aria-label="stop"
        onclick={onStop}
        disabled={stopDisabled}
        class="cursor-pointer disabled:text-gray-400 disabled:cursor-default enabled:text-red-500"
        title="Stop the QSO Timer"
    >
        <svg
            fill="none"
            viewBox="0 0 24 24"
            stroke-width="1.5"
            stroke="currentColor"
            class="size-6"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M5.25 7.5A2.25 2.25 0 0 1 7.5 5.25h9a2.25 2.25 0 0 1 2.25 2.25v9a2.25 2.25 0 0 1-2.25 2.25h-9a2.25 2.25 0 0 1-2.25-2.25v-9Z"
            />
        </svg>
    </button>
    <button
        type="button"
        aria-label="start"
        onclick={onStart}
        disabled={startDisabled}
        class="mr-8 cursor-pointer disabled:text-gray-400 disabled:cursor-default enabled:text-green-700"
        title="Start the QSO Timer"
    >
        <svg
            fill="none"
            viewBox="0 0 24 24"
            stroke-width="1.5"
            stroke="currentColor"
            class="size-6"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.347a1.125 1.125 0 0 1 0 1.972l-11.54 6.347a1.125 1.125 0 0 1-1.667-.986V5.653Z"
            />
        </svg>
    </button>
</div>
