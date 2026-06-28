<script lang="ts">
    import { configState } from '../../states/config.svelte';
    import { toasts } from '../../states/toasts.svelte';

    /*
        FT8 display preferences — daemon-backed (config.json `ft8.display`,
        served on /v1/config). The controls bind directly to configState.ft8Display
        (so a change previews live in Band Activity), and Save persists via the
        shared configState.saveFt8Display() (full ft8_display block + bundled
        logging_station/station — see that method). The Band Activity *filters*
        (typed filter + hide-hashed) live in the funnel popover, not here; this tab
        holds the durable display/ordering prefs (rows, feed mode, CQ-to-top).
    */
    let saving = $state(false);

    async function onSave(): Promise<void> {
        if (saving) return;
        saving = true;
        try {
            const outcome = await configState.saveFt8Display();
            switch (outcome.kind) {
                case 'ok':
                    configState.applyResponse(outcome.config);
                    toasts.info('FT8 settings saved.');
                    break;
                case 'validation':
                    toasts.error(outcome.message);
                    break;
                case 'server':
                    toasts.error('Could not save FT8 settings. Try again.');
                    break;
                case 'network':
                    toasts.error('Cannot reach the daemon — check it is running.');
                    break;
                case 'aborted':
                    break;
            }
        } finally {
            saving = false;
        }
    }
</script>

<div class="flex px-2 py-4 text-sm ft8-info-panel-height text-gray-700">
    <div class="flex flex-col w-60 h-34 border">
        <h3 class="font-semibold text-gray-800">Band Activity</h3>
        <div class="flex mt-2">
            <label class="w-22 pt-0.5" for="max_rows">Max Rows</label>
            <input
                id="max_rows"
                type="number"
                min="10"
                max="2000"
                bind:value={configState.ft8Display.historyMax}
                class="w-18 rounded border border-gray-300 px-1 pb-1 pt-0.5 text-right focus:outline-none focus:ring-2 focus:ring-indigo-500"
            />
        </div>
        <div class="flex mt-2">
            <label class="w-22 pt-0.5" for="feed_mode">Feed mode</label>
            <select
                id="feed_mode"
                bind:value={configState.ft8Display.feedMode}
                class="w-32 rounded border border-gray-300 px-2 py-1 focus:outline-none focus:ring-2 focus:ring-indigo-500"
            >
                <option value="accumulate">Accumulate</option>
                <option value="single">Single slot</option>
            </select>
        </div>
        <div class="flex mt-2">
            <label class="w-22 pt-0.5 -mt-1" for="cq_to_top">CQ to top</label>
            <input
                type="checkbox"
                bind:checked={configState.ft8Display.cqToTop}
                class="h-5 w-5 cursor-pointer rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
            />
        </div>
    </div>
    <div class="flex flex-col w-70 h-34 border">
        <h3 class="font-semibold text-gray-800">Call CQ</h3>
        <div class="flex mt-2">
            <label class="w-24 pt-0.5" for="ans_order">Answer order</label>
            <select
                id="ans_order"
                bind:value={configState.ft8CallerAnswerMode}
                class="w-32 rounded border border-gray-300 px-2 py-1 focus:outline-none focus:ring-2 focus:ring-indigo-500"
            >
                <option value="auto_first">First</option>
                <option value="auto_strongest">Strongest</option>
            </select>
        </div>
    </div>
    <div class="flex w-100 h-34 place-items-end justify-end">
        <button type="button" class="btn btn-primary h-8 w-20" onclick={onSave} disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
        </button>
    </div>
</div>
