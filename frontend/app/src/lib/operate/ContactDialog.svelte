<script lang="ts">
    // Contact-detail overlay — the per-contact facts that DON'T belong on the
    // fast-path logging card or the header. Opened from the Worked card's View
    // button while a QSO is underway.
    //
    // Replaces the always-on Details card (operator decision 2026-07-08). Shows
    // ONLY what isn't already visible: the logging card owns callsign / RST /
    // times / Name / Comment / enrichment (flag/country/DXCC/bearing), the header
    // owns freq·mode·band — none of that is repeated here. What lives here:
    //   - read-only enrichment extras: QRZ page link, email, CQ / ITU zone
    //   - operator fields kept off the fast path: Gridsquare, QTH, Rig, Notes
    //
    // VIEW-ONLY by default; a deliberate Edit toggle unlocks the operator fields
    // (the operator "has never" hand-corrected the grid in normal use, so it's
    // off the fast path, not gone). Edits bind straight into the shared draft —
    // no separate save; whatever's there on Close is what logs. Enrichment facts
    // are looked-up, never typed, so they stay read-only. Same modal+backdrop
    // pattern as ExportDialog; LoggingCard owns Esc (its window listener closes
    // THIS first).
    import { fade } from 'svelte/transition';
    import { operate, closeContact } from './state.svelte';
    import { draft } from './qso.svelte';
    import { enrich } from './enrich.svelte';
    import { worked } from './worked.svelte';
    import { isValidMaidenhead } from '../validators/maidenhead';

    let editing = $state(false);

    // Each fresh open starts in view mode — editing is a per-visit deliberate act.
    $effect(() => {
        if (operate.contactOpen) editing = false;
    });

    function upperGrid(): void {
        draft.gridsquare = draft.gridsquare.toUpperCase();
    }

    const call = $derived(draft.callsign.trim().toUpperCase());
    // Enrichment facts, only when they belong to the call in the draft (a fast
    // edit can outrun the debounced lookup).
    const e = $derived(enrich.call === call ? enrich.data : null);
    const qrzUrl = $derived(
        call === '' ? null : `https://www.qrz.com/db/${encodeURIComponent(call)}`
    );
    const workedCount = $derived(worked.status === 'done' ? worked.qsos.length : null);
    const gridInvalid = $derived(
        draft.gridsquare !== '' && isValidMaidenhead(draft.gridsquare) !== null
    );
</script>

{#if operate.contactOpen}
    <div
        class="fixed inset-0 z-50"
        role="dialog"
        aria-modal="true"
        aria-labelledby="contact-title"
        transition:fade={{ duration: 150 }}
    >
        <button
            type="button"
            class="absolute inset-0 cursor-default bg-gray-500/75 dark:bg-gray-900/50"
            aria-label="Close"
            onclick={closeContact}
        ></button>

        <div class="pointer-events-none relative flex min-h-full items-center justify-center p-4">
            <div
                class="pointer-events-auto w-full max-w-lg rounded-lg bg-surface p-6 shadow-xl outline-1 outline-line"
            >
                <!-- Header: subject callsign + Edit toggle + Close -->
                <div class="flex items-start justify-between">
                    <div>
                        <h3 id="contact-title" class="text-base font-semibold text-ink">
                            Contact — <span class="tabular-nums">{call || '—'}</span>
                        </h3>
                        {#if workedCount !== null}
                            <p class="mt-0.5 text-xs text-muted">
                                {workedCount === 0
                                    ? 'Not worked before'
                                    : `Worked before · ${workedCount} QSO${workedCount === 1 ? '' : 's'}`}
                            </p>
                        {/if}
                    </div>
                    <div class="flex items-center gap-x-1">
                        <button
                            class="cursor-pointer rounded-md p-1 text-muted hover:text-ink"
                            class:text-focus={editing}
                            aria-pressed={editing}
                            title={editing ? 'Done editing' : 'Edit fields'}
                            onclick={() => (editing = !editing)}
                        >
                            <span class="sr-only">{editing ? 'Done editing' : 'Edit fields'}</span>
                            <svg
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="1.5"
                                aria-hidden="true"
                                class="size-5"
                            >
                                <path
                                    d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L10.582 16.07a4.5 4.5 0 0 1-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 0 1 1.13-1.897l8.932-8.931Zm0 0L19.5 7.125"
                                    stroke-linecap="round"
                                    stroke-linejoin="round"
                                />
                            </svg>
                        </button>
                        <button
                            class="cursor-pointer rounded-md p-1 text-muted hover:text-ink"
                            aria-label="Close"
                            onclick={closeContact}
                        >
                            <svg
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="1.5"
                                aria-hidden="true"
                                class="size-5"
                            >
                                <path
                                    d="M6 18 18 6M6 6l12 12"
                                    stroke-linecap="round"
                                    stroke-linejoin="round"
                                />
                            </svg>
                        </button>
                    </div>
                </div>

                <!-- Read-only enrichment extras (looked up, not typed). -->
                <div class="mt-4 flex items-center justify-between">
                    {#if qrzUrl !== null}
                        <a
                            href={qrzUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            class="inline-flex items-center gap-x-1 text-sm font-medium text-focus hover:underline"
                        >
                            Lookup on QRZ.com
                            <svg
                                viewBox="0 0 24 24"
                                fill="none"
                                stroke="currentColor"
                                stroke-width="1.5"
                                aria-hidden="true"
                                class="size-4"
                            >
                                <path
                                    d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"
                                    stroke-linecap="round"
                                    stroke-linejoin="round"
                                />
                            </svg>
                        </a>
                    {/if}
                </div>

                <dl class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
                    <div class="col-span-2">
                        <dt class="text-xs font-medium text-muted">Email</dt>
                        <dd class="mt-0.5 text-ink">{e?.email || '—'}</dd>
                    </div>
                    <div>
                        <dt class="text-xs font-medium text-muted">CQ Zone</dt>
                        <dd class="mt-0.5 tabular-nums text-ink">{e?.cqZone || '—'}</dd>
                    </div>
                    <div>
                        <dt class="text-xs font-medium text-muted">ITU Zone</dt>
                        <dd class="mt-0.5 tabular-nums text-ink">{e?.ituZone || '—'}</dd>
                    </div>
                </dl>

                <!-- Operator fields kept off the fast path (view or edit). -->
                <div class="mt-4 border-t border-line pt-4">
                    {#if editing}
                        <div class="grid grid-cols-2 gap-x-4 gap-y-3">
                            <div>
                                <label for="cd-grid" class="text-xs font-medium text-muted"
                                    >Gridsquare</label
                                >
                                <input
                                    id="cd-grid"
                                    class="input mt-0.5 w-full uppercase"
                                    autocomplete="off"
                                    spellcheck="false"
                                    placeholder="e.g. KH66"
                                    bind:value={draft.gridsquare}
                                    oninput={upperGrid}
                                />
                                {#if gridInvalid}
                                    <p class="mt-1 text-xs text-invalid">Not a valid grid square</p>
                                {:else}
                                    <p class="mt-1 text-xs text-muted">Filled by enrichment</p>
                                {/if}
                            </div>
                            <div>
                                <label for="cd-qth" class="text-xs font-medium text-muted"
                                    >QTH</label
                                >
                                <input
                                    id="cd-qth"
                                    class="input mt-0.5 w-full"
                                    autocomplete="off"
                                    bind:value={draft.qth}
                                />
                            </div>
                            <div class="col-span-2">
                                <label for="cd-rig" class="text-xs font-medium text-muted"
                                    >Rig</label
                                >
                                <input
                                    id="cd-rig"
                                    class="input mt-0.5 w-full"
                                    autocomplete="off"
                                    placeholder="Working conditions"
                                    bind:value={draft.rig}
                                />
                            </div>
                            <div class="col-span-2">
                                <label for="cd-notes" class="text-xs font-medium text-muted"
                                    >Notes</label
                                >
                                <textarea
                                    id="cd-notes"
                                    class="input mt-0.5 w-full"
                                    rows="3"
                                    placeholder="My personal notes"
                                    bind:value={draft.notes}
                                ></textarea>
                            </div>
                        </div>
                    {:else}
                        <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
                            <div>
                                <dt class="text-xs font-medium text-muted">Gridsquare</dt>
                                <dd class="mt-0.5 text-ink">{draft.gridsquare || '—'}</dd>
                            </div>
                            <div>
                                <dt class="text-xs font-medium text-muted">QTH</dt>
                                <dd class="mt-0.5 text-ink">{draft.qth || '—'}</dd>
                            </div>
                            <div class="col-span-2">
                                <dt class="text-xs font-medium text-muted">Rig</dt>
                                <dd class="mt-0.5 text-ink">{draft.rig || '—'}</dd>
                            </div>
                            <div class="col-span-2">
                                <dt class="text-xs font-medium text-muted">Notes</dt>
                                <dd class="mt-0.5 whitespace-pre-wrap text-ink">
                                    {draft.notes || '—'}
                                </dd>
                            </div>
                        </dl>
                    {/if}
                </div>
            </div>
        </div>
    </div>
{/if}
