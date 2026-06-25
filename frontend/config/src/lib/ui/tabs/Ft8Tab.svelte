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

    // PSK Reporter port — empty/0 means "use the default" (the daemon resolves it).
    function onPskPortInput(e: Event): void {
        const n = parseInt((e.currentTarget as HTMLInputElement).value.trim(), 10);
        configState.pskForm.port = Number.isFinite(n) && n > 0 ? n : 0;
    }
</script>

{#if !configState.config}
    <p class="text-sm text-gray-500">Loading…</p>
{:else}
    <div class="mx-auto max-w-xl space-y-8">
        <!-- Master FT8 switch (ft8.enabled). Off → the FT8 subsystem doesn't run
             (no capture, no decode); the display prefs below still save. -->
        <label class="flex items-center gap-2 text-sm font-medium text-gray-700">
            <input type="checkbox" bind:checked={configState.ft8Enabled} class="cursor-pointer" />
            Enable FT8
            <span class="font-normal text-gray-400">— run the FT8 capture + decode subsystem</span>
        </label>

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

        <section>
            <h2 class="text-base font-semibold text-gray-800">PSK Reporter</h2>
            <p class="mt-0.5 mb-3 text-sm text-gray-500">
                Upload your FT8 reception spots to PSK Reporter — a public map of who's hearing
                whom. Opt-in: your callsign and grid (from My Station) are published. The receiver
                identity and antenna come from your station config — nothing to enter here.
            </p>
            <div class="space-y-4">
                <label class="flex items-center gap-2 text-sm text-gray-700">
                    <input
                        type="checkbox"
                        bind:checked={configState.pskForm.enabled}
                        class="cursor-pointer"
                    />
                    Enable PSK Reporter uploads
                </label>
                <div class="flex flex-wrap gap-x-6 gap-y-3">
                    <label class="flex flex-col gap-1">
                        <span class="text-sm font-medium text-gray-700">Host</span>
                        <input
                            type="text"
                            bind:value={configState.pskForm.host}
                            placeholder="report.pskreporter.info (default)"
                            autocomplete="off"
                            spellcheck="false"
                            class="w-72 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                        />
                    </label>
                    <label class="flex flex-col gap-1">
                        <span class="text-sm font-medium text-gray-700">Port</span>
                        <input
                            type="text"
                            inputmode="numeric"
                            value={configState.pskForm.port ? String(configState.pskForm.port) : ''}
                            oninput={onPskPortInput}
                            placeholder="4739"
                            class="w-28 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                        />
                    </label>
                </div>
                <p class="text-xs text-gray-400">
                    Leave host/port blank for the production collector. To test without writing the
                    live database, keep the host and set port 14739.
                </p>
            </div>
        </section>

        <section>
            <h2 class="text-base font-semibold text-gray-800">Decode log</h2>
            <p class="mt-0.5 mb-3 text-sm text-gray-500">
                Append every FT8 RX decode and your own transmissions to a JTDX
                <code>ALL.TXT</code>-style file — a durable record for reconstructing an on-air
                exchange after the fact (handy when answering enquiries). The file grows unbounded,
                like WSJT-X's <code>ALL.TXT</code>; clear it yourself when it gets large.
            </p>
            <div class="space-y-4">
                <label class="flex items-center gap-2 text-sm text-gray-700">
                    <input
                        type="checkbox"
                        bind:checked={configState.decodeLogForm.enabled}
                        class="cursor-pointer"
                    />
                    Enable the FT8 decode log
                </label>
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700">File path</span>
                    <input
                        type="text"
                        bind:value={configState.decodeLogForm.path}
                        placeholder="log/ft8-all.txt (default, in the data directory)"
                        autocomplete="off"
                        spellcheck="false"
                        class="w-full rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    />
                    <span class="text-xs text-gray-400">
                        Leave blank for the default (<code>ft8-all.txt</code> in the data
                        directory's <code>log/</code> folder).
                    </span>
                </label>
            </div>
        </section>

        {#if configState.ft8RestartRequired}
            <div
                class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800"
            >
                ⚠ Enabling/disabling FT8, or changing PSK Reporter or the decode log, applies on
                daemon restart.
            </div>
        {/if}

        <TabFooter
            dirty={configState.ft8Dirty}
            saving={configState.savingFt8}
            status={configState.ft8Status}
            onsave={() => configState.saveFt8()}
            oncancel={() => configState.cancelFt8()}
        />
    </div>
{/if}
