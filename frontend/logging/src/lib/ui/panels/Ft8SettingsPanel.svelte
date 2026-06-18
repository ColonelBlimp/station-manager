<script lang="ts">
    import { configState } from '../../states/config.svelte';
    import { putConfig } from '../../api/config';
    import { toasts } from '../../states/toasts.svelte';

    /*
        FT8 display preferences — daemon-backed (config.json `ft8.display`,
        served on /v1/config). Same save flow as the My Station tab: the
        controls bind directly to configState.ft8Display (so a change previews
        live in Band Activity), and Save PUTs once. Because /v1/config overwrites
        logging_station/station unconditionally, the PUT bundles the current
        writable set (configState helpers) alongside the edited ft8_display so it
        doesn't zero the station identity; the daemon's ft8_display handling is
        presence-aware, so this is the only block that actually changes.
    */
    let saving = $state(false);

    async function onSave(): Promise<void> {
        if (saving) return;
        saving = true;
        try {
            const fd = configState.ft8Display;
            const outcome = await putConfig({
                logging_station: configState.toLoggingStationFields(),
                station: configState.toStationFields(),
                ft8_display: {
                    history_max: fd.historyMax,
                    feed_mode: fd.feedMode,
                    // The three highlight colours have no LSPA picker — they're edited
                    // in the config SPA. But the PUT replaces the whole ft8_display
                    // block, so we round-trip them unchanged here; omitting them would
                    // reset them to default on a Band Activity save.
                    highlight_unworked: fd.highlightUnworked,
                    highlight_worked: fd.highlightWorked,
                    highlight_calling: fd.highlightCalling,
                    cq_to_top: fd.cqToTop,
                    hide_hashed_calls: fd.hideHashedCalls,
                },
            });
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

<div class="flex gap-x-6 px-2 py-4 text-sm ft8-info-panel-height text-gray-700">
    <div class="flex flex-col w-80">
        <h3 class="font-semibold text-gray-800">Band Activity</h3>
        <label class="flex items-center justify-between gap-3">
            <span>Max rows shown</span>
            <!-- Bound directly to the daemon-mirrored config; the daemon clamps the
             value (10–2000) on save, so out-of-range typing is corrected there. -->
            <input
                type="number"
                min="10"
                max="2000"
                bind:value={configState.ft8Display.historyMax}
                class="w-24 rounded border border-gray-300 px-2 py-1 text-right focus:outline-none focus:ring-2 focus:ring-indigo-500"
            />
        </label>

        <label class="flex items-center justify-between gap-3 pt-1">
            <span>Feed mode</span>
            <select
                bind:value={configState.ft8Display.feedMode}
                class="w-44 rounded border border-gray-300 px-2 py-1 focus:outline-none focus:ring-2 focus:ring-indigo-500"
            >
                <option value="accumulate">Accumulate (roll up)</option>
                <option value="single">Single slot</option>
            </select>
        </label>
    </div>
    <div>
        <label class="flex items-center justify-between gap-3 pt-1">
            <!-- Floats CQ rows (the answerable ones) to the top of the feed; in this
                 mode per-slot separators are suppressed (the list is no longer slot-ordered). -->
            <span>Float CQ calls to top</span>
            <input
                type="checkbox"
                bind:checked={configState.ft8Display.cqToTop}
                class="h-4 w-4 cursor-pointer rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
            />
        </label>

        <label class="flex items-center justify-between gap-3 pt-1">
            <!-- Hides decodes with an unresolved hashed call ("<...>") — dross you can't
                 identify or work. Stations calling you still show through. -->
            <span>Hide hashed calls (&lt;...&gt;)</span>
            <input
                type="checkbox"
                bind:checked={configState.ft8Display.hideHashedCalls}
                class="h-4 w-4 cursor-pointer rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
            />
        </label>
    </div>
</div>
<div class="mt-2">
    <button type="button" class="btn btn-primary" onclick={onSave} disabled={saving}>
        {saving ? 'Saving…' : 'Save'}
    </button>
</div>
