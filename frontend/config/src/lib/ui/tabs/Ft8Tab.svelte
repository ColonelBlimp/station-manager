<script lang="ts">
    // FT8 tab — Band Activity display preferences (the whole ft8_display block:
    // CQ highlight colours, row cap, feed mode, two toggles). SPA-consumed config
    // (the logging SPA re-reads it), so no restart needed. Saves via
    // configState.saveFt8() → PUT /v1/config (the daemon clamps the row cap +
    // validates feed mode, returning the block resolved).
    import { configState } from '../../states/config.svelte';
    import TabFooter from '../TabFooter.svelte';

    // Row cap is a number daemon-side; render it as a parsed text input (empty →
    // 0, which the daemon resolves to the default) rather than a number input,
    // avoiding the null-on-empty type friction of bind:value on type=number.
    function onRowCapInput(e: Event): void {
        const n = parseInt((e.currentTarget as HTMLInputElement).value.trim(), 10);
        configState.ft8Form.history_max = Number.isFinite(n) && n > 0 ? n : 0;
    }
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-xl space-y-8">
        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">Highlight colours</h2>
            <div class="flex flex-col gap-2 text-sm text-gray-700">
                <label class="flex items-center justify-between gap-3">
                    <span>CQ — not worked on this band</span>
                    <input
                        type="color"
                        bind:value={configState.ft8Form.highlight_unworked}
                        aria-label="Not-worked highlight colour"
                        class="h-7 w-12 cursor-pointer rounded border border-gray-300"
                    />
                </label>
                <label class="flex items-center justify-between gap-3">
                    <span>CQ — worked before (dupe)</span>
                    <input
                        type="color"
                        bind:value={configState.ft8Form.highlight_worked}
                        aria-label="Worked-before highlight colour"
                        class="h-7 w-12 cursor-pointer rounded border border-gray-300"
                    />
                </label>
                <label class="flex items-center justify-between gap-3">
                    <span>Station calling you</span>
                    <input
                        type="color"
                        bind:value={configState.ft8Form.highlight_calling}
                        aria-label="Calling-you highlight colour"
                        class="h-7 w-12 cursor-pointer rounded border border-gray-300"
                    />
                </label>
            </div>
        </section>

        <section>
            <h2 class="mb-3 text-base font-semibold text-gray-800">Display</h2>
            <div class="space-y-4">
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Row cap</span>
                    <input
                        type="text"
                        inputmode="numeric"
                        value={configState.ft8Form.history_max
                            ? String(configState.ft8Form.history_max)
                            : ''}
                        oninput={onRowCapInput}
                        class="w-32 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                    <span class="text-xs text-gray-400">
                        Max Band Activity rows kept (10–2000; the daemon clamps).
                    </span>
                </label>

                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">Feed mode</span>
                    <select
                        bind:value={configState.ft8Form.feed_mode}
                        class="w-56 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    >
                        <option value="accumulate">Accumulate (keep history)</option>
                        <option value="single">Single (latest slot only)</option>
                    </select>
                </label>

                <label class="flex items-center gap-2 text-sm text-gray-700">
                    <input
                        type="checkbox"
                        bind:checked={configState.ft8Form.cq_to_top}
                        class="cursor-pointer"
                    />
                    Float CQ decodes to the top
                </label>

                <label class="flex items-center gap-2 text-sm text-gray-700">
                    <input
                        type="checkbox"
                        bind:checked={configState.ft8Form.hide_hashed_calls}
                        class="cursor-pointer"
                    />
                    Hide hashed &lt;…&gt; callsigns
                </label>
            </div>
        </section>

        <TabFooter
            dirty={configState.ft8Dirty}
            saving={configState.savingFt8}
            status={configState.ft8Status}
            onsave={() => configState.saveFt8()}
            oncancel={() => configState.cancelFt8()}
        />
    </div>
{/if}
