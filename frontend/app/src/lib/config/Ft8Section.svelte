<script lang="ts">
    // FT8 section — the FT8 subsystem's operator-facing settings, ported from
    // the standalone config SPA's FT8 tab (ADR 0044). Four blocks share one
    // Save because they are one page to the operator; they differ in WHEN they
    // take effect, which is what the restart notice at the bottom exists to
    // say. See ft8.svelte.ts for why there are no colour pickers here.
    import { onMount } from 'svelte';
    import { ft8SettingsState } from './ft8.svelte';

    onMount(() => void ft8SettingsState.load());

    // Digits at the keystroke rather than a coerce on save: a coerce turns
    // "abc" into a number the operator never chose, and blank has to survive as
    // blank — it is what asks the daemon for its default. Same helper shape as
    // EmailSection's port/timeout entry.
    function digitsOnly(e: Event, set: (v: string) => void): void {
        const el = e.currentTarget as HTMLInputElement;
        const cleaned = el.value.replace(/\D/g, '');
        el.value = cleaned;
        set(cleaned);
    }
</script>

<!-- Same shell as the other Settings sections: mx-auto max-w-3xl, the same
     loading / error-card / body branch order, space-y-8 rhythm, border-t
     footer. -->
<div class="mx-auto max-w-3xl">
    {#if !ft8SettingsState.loaded && ft8SettingsState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !ft8SettingsState.loaded && ft8SettingsState.error}
        <!-- Never a form of blanks on a failed load: every block here is a
             whole-block write, so blanks on screen are one Save press away from
             erasing the operator's FT8 configuration. -->
        <div class="card">
            <p class="text-sm text-ink">
                Couldn’t load FT8 settings: {ft8SettingsState.error}
            </p>
            <button class="btn mt-3" onclick={() => ft8SettingsState.load()}>Retry</button>
        </div>
    {:else}
        <div class="space-y-8">
            <p class="text-sm text-muted">
                The FT8 subsystem: whether it runs at all, how the Band Activity feed is presented,
                and two optional outputs — reception spots to PSK Reporter and a local decode log.
            </p>

            <section>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={ft8SettingsState.draft.enabled}
                    />
                    Enable FT8
                </label>
                <p class="mt-2 text-xs text-muted">
                    Off: no audio device is claimed and no decoders run. The display preferences
                    below still save. A build without CGO leaves the subsystem idle whatever this
                    says.
                </p>
            </section>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">Band Activity</h2>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    <label class="flex w-28 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Row cap</span>
                        <!-- The placeholder is the daemon's default, shown so a
                             blank box says what saving will store rather than
                             leaving the operator to guess. A DISPLAY duplicate
                             of types.DefaultFt8HistoryMax; the value is still
                             resolved daemon-side, so drift here misinforms but
                             cannot misconfigure. -->
                        <input
                            class="input"
                            inputmode="numeric"
                            placeholder="100"
                            value={ft8SettingsState.draft.historyMax}
                            oninput={(e) =>
                                digitsOnly(e, (v) => (ft8SettingsState.draft.historyMax = v))}
                        />
                    </label>
                    <label class="flex w-56 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Feed mode</span>
                        <select class="input" bind:value={ft8SettingsState.draft.feedMode}>
                            <option value="accumulate">Accumulate (keep history)</option>
                            <option value="single">Single (latest slot only)</option>
                        </select>
                    </label>
                </div>
                <p class="text-xs text-muted">
                    How many decode rows to keep, and whether slots roll up into a history or the
                    feed shows only the current 15-second slot. The daemon clamps the cap to
                    10–2000.
                </p>

                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={ft8SettingsState.draft.cqToTop}
                    />
                    Float CQ decodes to the top
                </label>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={ft8SettingsState.draft.hideHashedCalls}
                    />
                    Hide hashed &lt;…&gt; callsigns
                </label>
                <p class="text-xs text-muted">
                    A hashed callsign is one the decoder could not resolve to a full call, so it
                    cannot be worked or logged from the feed.
                </p>
            </section>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">PSK Reporter</h2>
                <p class="text-sm text-muted">
                    Upload what you HEAR to PSK Reporter, the public map of who is hearing whom.
                    Opt-in, and it publishes your callsign and grid from Station identity — there is
                    no separate receiver identity to enter here.
                </p>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={ft8SettingsState.draft.pskEnabled}
                    />
                    Upload reception spots
                </label>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    <label class="flex w-72 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Host</span>
                        <input
                            class="input"
                            aria-label="PSK Reporter host"
                            placeholder="report.pskreporter.info"
                            autocomplete="off"
                            spellcheck="false"
                            bind:value={ft8SettingsState.draft.pskHost}
                        />
                    </label>
                    <label class="flex w-28 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Port</span>
                        <input
                            class="input"
                            aria-label="PSK Reporter port"
                            inputmode="numeric"
                            placeholder="4739"
                            value={ft8SettingsState.draft.pskPort}
                            oninput={(e) =>
                                digitsOnly(e, (v) => (ft8SettingsState.draft.pskPort = v))}
                        />
                    </label>
                </div>
                <p class="text-xs text-muted">
                    Leave both blank for the production collector. To exercise the path without
                    writing the live database, keep the host and use port 14739.
                </p>
            </section>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">Decode log</h2>
                <p class="text-sm text-muted">
                    Append every decode and every transmission to a WSJT-X <code>ALL.TXT</code
                    >-style file — a durable record for reconstructing an exchange after the fact.
                    It grows without bound and nothing prunes it; clear it yourself when it gets
                    large.
                </p>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        aria-label="Enable the decode log"
                        bind:checked={ft8SettingsState.draft.decodeLogEnabled}
                    />
                    Write a decode log
                </label>
                <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-ink">File path</span>
                    <input
                        class="input"
                        aria-label="Decode log file path"
                        placeholder="log/ft8-all.txt"
                        autocomplete="off"
                        spellcheck="false"
                        bind:value={ft8SettingsState.draft.decodeLogPath}
                    />
                    <span class="text-xs text-muted">
                        Blank uses the default, next to <code>smd.log</code> in the data directory.
                    </span>
                </label>
            </section>

            <!-- Shown only for the blocks the daemon reads at startup. The
                 display prefs are pushed into the running FT8 view on save, so
                 claiming they need a restart would be false — and would leave
                 the operator unable to tell which of their edits is already
                 live. Nothing else in this section may use the word. -->
            {#if ft8SettingsState.restartRequired}
                <div
                    class="rounded-md border border-warning bg-surface-muted px-3 py-2 text-sm text-warning"
                >
                    ⚠ The FT8 switch, PSK Reporter and the decode log take effect when the daemon
                    restarts — the subsystem binds at startup.
                </div>
            {/if}

            <div class="flex items-center gap-3 border-t border-line pt-4">
                <button
                    class="btn btn-primary"
                    disabled={!ft8SettingsState.dirty || ft8SettingsState.saving}
                    onclick={() => ft8SettingsState.save()}
                >
                    {ft8SettingsState.saving ? 'Saving…' : 'Save'}
                </button>
                <button
                    class="btn"
                    disabled={!ft8SettingsState.dirty || ft8SettingsState.saving}
                    onclick={() => ft8SettingsState.reset()}
                >
                    Cancel
                </button>
                {#if ft8SettingsState.dirty}
                    <span class="text-xs text-muted">Unsaved changes</span>
                {/if}
            </div>
        </div>
    {/if}
</div>
