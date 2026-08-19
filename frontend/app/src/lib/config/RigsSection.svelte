<script lang="ts">
    // Rigs section — the configured rig profiles (app Settings, ADR 0044), as a
    // master-detail: the rig list on the left, a details panel on the right. The
    // model, per-rig FT8-mode / MY_RIG overrides, and the CONNECTION (serial port +
    // audio RX/TX via /v1/hardware pickers) are editable, saved via a whole-catalogue
    // PUT (see rigs.svelte.ts data-safety note). Add is an immediate structural write
    // (creates a blank rig to configure); delete lands in a follow-up increment.
    import { onMount } from 'svelte';
    import { rigsState } from './rigs.svelte';
    import { bridgeEnabledState } from './bridgeEnabled.svelte';
    import ModeMappingsEditor from './ModeMappingsEditor.svelte';
    import SerialOverridesEditor from './SerialOverridesEditor.svelte';
    import type { AudioDevice } from '../api/hardware';

    onMount(() => {
        void rigsState.load();
        void bridgeEnabledState.load();
    });

    // Add a rig — an immediate structural write (rigsState.addRig re-fetches + PUTs
    // a blank rig, then the operator configures + Saves it). nextRigModel picks an
    // unused catalogue model; if only in-use models remain, confirm before adding a
    // same-model clone (matches the config SPA's onAddRig). The confirm lives here,
    // not in the state module, so the state stays free of DOM globals.
    async function onAddRig() {
        const model = rigsState.nextRigModel();
        if (!model) return; // empty catalogue — nothing to add (the button is disabled too)
        if (rigsState.rigs.some((r) => r.model === model)) {
            const name = rigsState.catalogue[model]?.name ?? model;
            if (
                !window.confirm(`A "${name}" is already configured. Add another of the same model?`)
            ) {
                return;
            }
        }
        await rigsState.addRig(model);
    }
</script>

{#snippet row(label: string, value: string, mono = false)}
    <dt class="text-muted">{label}</dt>
    <dd class="truncate text-ink {mono ? 'font-mono text-xs' : ''}" title={value}>{value}</dd>
{/snippet}

{#snippet audioPicker(
    label: string,
    which: 'rx' | 'tx',
    devices: AudioDevice[],
    current: string | undefined
)}
    <label class="flex flex-col gap-1">
        <span class="text-sm font-medium text-ink">{label}</span>
        <select
            class="input"
            value={current ?? ''}
            disabled={rigsState.saving}
            onchange={(e) => rigsState.setDraftAudio(which, e.currentTarget.value)}
        >
            <option value="">— none —</option>
            <!-- keep the stored device even if it isn't currently present -->
            {#if current && !devices.some((d) => d.name === current)}
                <option value={current}>{current} (not detected)</option>
            {/if}
            {#each devices as d (d.name)}
                <option value={d.name}>{d.name}</option>
            {/each}
        </select>
    </label>
{/snippet}

<!-- mx-auto max-w-3xl matches StationSection: the tab strip is max-w-5xl, so
     an unwrapped section sits left-aligned and wider than its neighbours. The
     master-detail layout still fits — the list is w-64 and the detail sections
     are already capped at max-w-md, so 3xl is not a squeeze. -->
<div class="mx-auto max-w-3xl">
    <!-- CAT master switch (bridge.enabled) — ported from the config SPA. Off ⇒ the
         daemon never opens the serial port, so the rig stays disconnected regardless
         of the profile. Save-on-toggle (presence-aware bridge_enabled PUT, never a
         whole-block bridge replace); the bridge binds at startup, so a change needs a
         daemon restart. Enabling is refused if the active rig has no port/driver. -->
    {#if bridgeEnabledState.loaded}
        <div class="mb-5">
            <label class="flex items-center gap-2 text-sm font-medium text-ink">
                <input
                    type="checkbox"
                    class="cursor-pointer disabled:cursor-not-allowed"
                    checked={bridgeEnabledState.enabled}
                    disabled={bridgeEnabledState.saving}
                    onchange={(e) => bridgeEnabledState.setEnabled(e.currentTarget.checked)}
                />
                Enable rig connection (CAT)
                <span class="font-normal text-muted">
                    — connect Station Manager to the active rig's serial port
                </span>
            </label>
            {#if bridgeEnabledState.restartPending}
                <p class="mt-1 text-xs text-muted">Restart the daemon to apply the CAT change.</p>
            {/if}
        </div>
    {:else if bridgeEnabledState.error}
        <!-- A failed CAT-state load must not silently hide the control; surface it
             with a retry, like the rigs/other sections do. -->
        <div class="mb-5">
            <p class="text-sm text-ink">
                Couldn’t load the CAT switch: {bridgeEnabledState.error}
            </p>
            <button class="btn mt-2" onclick={() => bridgeEnabledState.load()}>Retry</button>
        </div>
    {/if}

    {#if !rigsState.loaded && rigsState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !rigsState.loaded && rigsState.error}
        <div class="card">
            <p class="text-sm text-ink">Couldn’t load rigs: {rigsState.error}</p>
            <button class="btn mt-3" onclick={() => rigsState.load()}>Retry</button>
        </div>
    {:else if rigsState.rigs.length === 0}
        <div
            class="grid min-h-[40vh] place-items-center rounded-xl border border-dashed border-line"
        >
            <div class="text-center">
                <p class="text-sm text-muted">No rigs configured.</p>
                <!-- Add the FIRST rig: addRig makes it the active default (the daemon
                     400s on an unresolvable default_rig_id). Disabled with no catalogue
                     (nextRigModel would return '' and addRig would no-op). -->
                <button
                    class="btn btn-primary mt-3"
                    disabled={rigsState.saving || Object.keys(rigsState.catalogue).length === 0}
                    onclick={onAddRig}
                >
                    {rigsState.saving ? 'Adding…' : 'Add rig'}
                </button>
            </div>
        </div>
    {:else}
        <div class="flex gap-6">
            <!-- Master: rig list + Add -->
            <div class="w-64 shrink-0">
                <ul class="space-y-1">
                    {#each rigsState.rigs as rig (rig.id)}
                        <li>
                            <button
                                class="w-full rounded-md border px-3 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60 {rigsState.selectedId ===
                                rig.id
                                    ? 'border-focus bg-surface-muted'
                                    : 'border-line hover:bg-surface-muted'}"
                                disabled={rigsState.saving}
                                onclick={() => rigsState.select(rig.id)}
                            >
                                <div class="flex items-center gap-2">
                                    <span class="font-medium text-ink"
                                        >{rigsState.nameFor(rig)}</span
                                    >
                                    {#if rig.id === rigsState.defaultRigId}
                                        <span
                                            class="ml-auto inline-flex items-center gap-0.5 rounded border border-green-500/40 bg-green-50 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-green-700 uppercase dark:bg-green-500/10 dark:text-green-400"
                                        >
                                            <svg
                                                viewBox="0 0 20 20"
                                                fill="currentColor"
                                                aria-hidden="true"
                                                class="size-3"
                                            >
                                                <path
                                                    fill-rule="evenodd"
                                                    d="M16.704 4.153a.75.75 0 0 1 .143 1.052l-8 10.5a.75.75 0 0 1-1.127.075l-4.5-4.5a.75.75 0 0 1 1.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 0 1 1.05-.143Z"
                                                    clip-rule="evenodd"
                                                />
                                            </svg>
                                            default
                                        </span>
                                    {/if}
                                </div>
                            </button>
                        </li>
                    {/each}
                </ul>
                <!-- + Add rig — immediate write (onAddRig). Sits under the list like
                     the config SPA; disabled while a save/add is in flight. -->
                <button
                    class="mt-2 w-full rounded-md border border-dashed border-line px-3 py-2 text-sm font-medium text-muted hover:border-focus hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60"
                    disabled={rigsState.saving}
                    onclick={onAddRig}
                >
                    {rigsState.saving ? 'Adding…' : '+ Add rig'}
                </button>
            </div>

            <!-- Detail: model + per-rig FT8-mode/MY_RIG + the CONNECTION (port +
                 audio, via /v1/hardware pickers) are editable. Delete lands in a
                 follow-up increment. -->
            <div class="min-w-0 flex-1">
                {#if rigsState.selected && rigsState.draft}
                    {@const rig = rigsState.selected}
                    {@const draft = rigsState.draft}
                    <!-- def / name follow the DRAFT's model (not the pristine rig) so a
                         model change updates the heading, subtitle, inherit-placeholders,
                         and the {#key draft} sub-editors' rigdef. -->
                    {@const def = rigsState.defFor(draft)}
                    <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
                        <h2 class="text-lg font-semibold text-ink">{rigsState.nameFor(draft)}</h2>
                        {#if rig.id === rigsState.defaultRigId}
                            <!-- "default", NOT "active" — this branch tests
                                 default_rig_id, which is the rig the daemon will
                                 connect to at its NEXT start. The rig it actually has
                                 open is pinned at boot (qsoservice SetActiveRig) and
                                 is what stamps MY_RIG on a QSO; "Set as default"
                                 below only takes effect on restart, so from the
                                 moment it is pressed the two disagree. Labelling the
                                 configured default "active" asserted "you are on air
                                 with this rig" exactly when that was false. The SPA
                                 cannot say which rig is truly active — activeRigID is
                                 not on the wire yet (dogfood inbox 2026-08-02). -->
                            <span
                                class="rounded border border-green-500/40 bg-green-50 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-green-700 uppercase dark:bg-green-500/10 dark:text-green-400"
                                title="The rig the daemon connects to at startup. Changes apply on restart."
                                >default</span
                            >
                        {:else}
                            <button
                                class="rounded-md px-2 py-1.5 text-xs font-medium text-focus hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-50"
                                title="Requires a restart"
                                disabled={rigsState.settingDefault || rigsState.saving}
                                onclick={() => rigsState.setDefault(rig.id)}
                            >
                                {rigsState.settingDefault ? 'Setting…' : 'Set as default'}
                            </button>
                        {/if}
                    </div>
                    {#if def?.manufacturer || def?.model}
                        <p class="mt-0.5 text-sm text-muted">
                            {[def?.manufacturer, def?.model].filter(Boolean).join(' · ')}
                        </p>
                    {/if}

                    <!-- Rig (editable): model + per-rig FT8-mode and MY_RIG overrides.
                         Ported from the config SPA's Rigs tab. Model is a rigdef id;
                         changing it replaces the draft so the Advanced sub-editors
                         re-read the new rigdef. FT8 mode / MY_RIG blank ⇒ inherit. -->
                    <section class="mt-5 max-w-md space-y-3">
                        <h3 class="text-xs font-semibold tracking-wide text-muted uppercase">
                            Rig
                        </h3>
                        <label class="flex flex-col gap-1">
                            <span class="text-sm font-medium text-ink">Model</span>
                            <select
                                class="input"
                                value={draft.model}
                                disabled={rigsState.saving}
                                onchange={(e) => rigsState.setDraftModel(e.currentTarget.value)}
                            >
                                <!-- keep the stored model even if it's not in the catalogue -->
                                {#if !rigsState.catalogue[draft.model]}
                                    <option value={draft.model}>{draft.model} (unknown)</option>
                                {/if}
                                {#each Object.entries(rigsState.catalogue) as [id, d] (id)}
                                    <option value={id}>{d.name}</option>
                                {/each}
                            </select>
                        </label>
                        <label class="flex flex-col gap-1">
                            <span class="text-sm font-medium text-ink">FT8 mode</span>
                            <input
                                class="input"
                                value={draft.ft8_mode ?? ''}
                                placeholder={def?.ft8_mode || 'inherit'}
                                title="Rig mode literal used for FT8 (e.g. DATA-U). Blank inherits the rigdef default."
                                disabled={rigsState.saving}
                                oninput={(e) => rigsState.setDraftFt8Mode(e.currentTarget.value)}
                            />
                        </label>
                        <label class="flex flex-col gap-1">
                            <span class="text-sm font-medium text-ink">MY_RIG (ADIF)</span>
                            <input
                                class="input"
                                value={draft.my_rig ?? ''}
                                placeholder={def?.name || 'inherit'}
                                title="ADIF MY_RIG stamped on logged QSOs. Blank derives from the rig name."
                                disabled={rigsState.saving}
                                oninput={(e) => rigsState.setDraftMyRig(e.currentTarget.value)}
                            />
                        </label>
                    </section>

                    <!-- Connection (editable) -->
                    <section class="mt-5 max-w-md space-y-3">
                        <h3 class="text-xs font-semibold tracking-wide text-muted uppercase">
                            Connection
                        </h3>

                        <label class="flex flex-col gap-1">
                            <span class="text-sm font-medium text-ink">Serial port</span>
                            <select
                                class="input font-mono text-xs"
                                value={draft.port}
                                disabled={rigsState.saving}
                                onchange={(e) => rigsState.setDraftPort(e.currentTarget.value)}
                            >
                                <!-- keep the stored value even if it isn't currently detected -->
                                {#if draft.port && !rigsState.serialPorts.some((p) => p.id === draft.port)}
                                    <option value={draft.port}>{draft.port} (not detected)</option>
                                {/if}
                                {#each rigsState.serialPorts as p (p.id)}
                                    <option value={p.id}>{p.label}</option>
                                {/each}
                            </select>
                        </label>

                        {#if rigsState.audioAvailable}
                            {@render audioPicker(
                                'Audio RX',
                                'rx',
                                rigsState.capture,
                                draft.audio?.rx
                            )}
                            {@render audioPicker(
                                'Audio TX',
                                'tx',
                                rigsState.playback,
                                draft.audio?.tx
                            )}
                        {:else}
                            <!-- static/CGO-free daemon: no enumeration, show stored names -->
                            <dl class="grid grid-cols-[6rem_1fr] gap-x-4 gap-y-1 text-sm">
                                {@render row('Audio RX', draft.audio?.rx || '—')}
                                {@render row('Audio TX', draft.audio?.tx || '—')}
                            </dl>
                            <p class="text-xs text-muted">
                                Audio devices can't be enumerated by this daemon build (read-only).
                            </p>
                        {/if}
                    </section>

                    <!-- Advanced (editable, collapsed): per-rig mode mappings (rig mode
                         literal → ADIF MODE/SUBMODE) + serial overrides (baud/framing/
                         delimiter). Keyed on the DRAFT OBJECT (not id:model): these
                         editors take a one-time snapshot on mount, so they must remount
                         whenever the draft is REPLACED — switching rigs, but also Cancel
                         (resetDraft swaps in a fresh clone) and save (re-baseline). An
                         id:model key stays constant on Cancel, so the editors would keep
                         their stale snapshot and write it back into the fresh draft,
                         leaving Cancel ineffective (codex 55d85876 P1). In-place edits
                         mutate the draft without changing its identity, so they don't
                         remount. Both mutate the same draft; the shared Save/Cancel
                         below covers them. -->
                    <section class="mt-6 max-w-md space-y-2">
                        <h3 class="text-xs font-semibold tracking-wide text-muted uppercase">
                            Advanced
                        </h3>
                        {#key draft}
                            <ModeMappingsEditor
                                rig={draft}
                                rigdef={def}
                                disabled={rigsState.saving}
                            />
                            <SerialOverridesEditor
                                rig={draft}
                                rigdef={def}
                                disabled={rigsState.saving}
                            />
                        {/key}
                    </section>

                    <!-- Shared action footer — rigsState.dirty spans the WHOLE draft,
                         so this Save/Cancel covers both the connection edits and the
                         mode-mapping overrides. The restart note is gated on
                         restartDirty (not dirty): a pure MY_RIG edit is resolved live
                         per QSO, so it needs no restart and must not claim one. -->
                    {#if rigsState.restartDirty}
                        <p class="mt-4 max-w-md text-xs text-muted">
                            Changes take effect after a daemon restart.
                        </p>
                    {/if}
                    <div class="mt-3 flex items-center gap-3">
                        <button
                            class="btn btn-primary"
                            disabled={!rigsState.dirty ||
                                rigsState.saving ||
                                rigsState.settingDefault}
                            onclick={() => rigsState.save()}
                        >
                            {rigsState.saving ? 'Saving…' : 'Save'}
                        </button>
                        <button
                            class="btn"
                            disabled={!rigsState.dirty || rigsState.saving}
                            onclick={() => rigsState.resetDraft()}
                        >
                            Cancel
                        </button>
                    </div>

                    <!-- The rigdef's effective FT8 mode, serial-defaults summary, and
                         description used to render here; removed 2026-07-21 as
                         non-actionable read-only info (operator request). Serial
                         defaults now surface as placeholders in the Serial overrides
                         editor; rigsState.ft8ModeFor is retained for a future per-rig
                         FT8-mode control. -->
                {:else}
                    <div
                        class="grid min-h-[40vh] place-items-center rounded-xl border border-dashed border-line"
                    >
                        <p class="text-sm text-muted">Select a rig to view its details.</p>
                    </div>
                {/if}
            </div>
        </div>
    {/if}
</div>
