<script lang="ts">
    // Edit one QSO in a modal. Seeds its form from the row passed in (the modal
    // is mounted fresh each time a row is opened, so a one-shot seed is correct),
    // builds a QsoPatch from the editable fields, and PATCHes via the state's
    // saveEdit. The daemon restores immutables (identity, logbook, station
    // callsign, forwarding/enrichment), re-derives band from freq, and
    // re-validates — so a bad edit comes back as a message, not a silent write.
    //
    // Keyboard: ESC cancels, Ctrl/Cmd+Enter saves (the modal owns its own ESC).
    import { untrack } from 'svelte';
    import { logbookState } from '../states/logbook.svelte';
    import { formatQsoDate, formatTime } from '../utils/format';
    import type { LogbookQso } from '../api/logbooks';
    import type { QsoPatch } from '../api/qso';

    const { row }: { row: LogbookQso } = $props();

    // YYYY-MM-DD (the <input type=date> value) → ADIF YYYYMMDD; "" stays "".
    const toAdifDate = (v: string): string => v.replace(/-/g, '');
    // HH:MM (the <input type=time> value) → ADIF HHMM; "" stays "".
    const toAdifTime = (v: string): string => v.replace(/:/g, '');

    // Focus the callsign field on open (the form's natural first field).
    function focusOnMount(node: HTMLInputElement): void {
        node.focus();
    }

    // Form state, seeded ONCE from the row — untrack() because this is a
    // deliberate one-shot snapshot, not a live mirror (the modal is mounted fresh
    // each open, so `row` never changes during its life). Dates/times use native
    // pickers (YYYY-MM-DD / HH:MM); everything else is text the daemon validates.
    const form = $state(
        untrack(() => ({
            qso_date: formatQsoDate(row.qso_date),
            qso_date_off: formatQsoDate(row.qso_date_off),
            time_on: formatTime(row.time_on),
            time_off: formatTime(row.time_off),
            call: row.call ?? '',
            freq: row.freq ?? '',
            band: row.band ?? '',
            mode: row.mode ?? '',
            submode: row.submode ?? '',
            rst_sent: row.rst_sent ?? '',
            rst_rcvd: row.rst_rcvd ?? '',
            country: row.country ?? '',
            name: row.name ?? '',
            gridsquare: row.gridsquare ?? '',
            comment: row.comment ?? '',
        }))
    );

    function buildPatch(): QsoPatch {
        return {
            qso_date: toAdifDate(form.qso_date),
            qso_date_off: toAdifDate(form.qso_date_off),
            time_on: toAdifTime(form.time_on),
            time_off: toAdifTime(form.time_off),
            call: form.call.trim(),
            freq: form.freq.trim(),
            band: form.band.trim(),
            mode: form.mode.trim(),
            submode: form.submode.trim(),
            rst_sent: form.rst_sent.trim(),
            rst_rcvd: form.rst_rcvd.trim(),
            country: form.country.trim(),
            name: form.name.trim(),
            gridsquare: form.gridsquare.trim(),
            comment: form.comment.trim(),
        };
    }

    async function save(): Promise<void> {
        await logbookState.saveEdit(buildPatch());
    }

    function onKeydown(e: KeyboardEvent): void {
        if (e.key === 'Escape') {
            e.preventDefault();
            logbookState.closeEdit();
        } else if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault();
            void save();
        }
    }
</script>

<!-- Backdrop: click outside the panel closes. The keydown lives here so ESC /
     Ctrl+Enter work wherever focus is inside the modal. -->
<svelte:window onkeydown={onKeydown} />
<div
    class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 sm:items-center"
    role="presentation"
    onclick={(e) => {
        if (e.target === e.currentTarget) logbookState.closeEdit();
    }}
>
    <div
        class="w-full max-w-2xl rounded-lg bg-white shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-label="Edit QSO"
    >
        <div class="flex items-center justify-between border-b border-gray-200 px-5 py-3">
            <h2 class="text-lg font-semibold text-gray-900">
                Edit QSO — {form.call || row.call || '?'}
            </h2>
            <button
                type="button"
                class="cursor-pointer rounded-md px-2 py-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700"
                aria-label="Close"
                onclick={() => logbookState.closeEdit()}>✕</button
            >
        </div>

        <form
            class="grid grid-cols-1 gap-x-4 gap-y-3 px-5 py-4 sm:grid-cols-3"
            onsubmit={(e) => {
                e.preventDefault();
                void save();
            }}
        >
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Date</span>
                <input type="date" class="input" bind:value={form.qso_date} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Time on</span>
                <input type="time" class="input" bind:value={form.time_on} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Time off</span>
                <input type="time" class="input" bind:value={form.time_off} />
            </label>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Callsign</span>
                <input
                    type="text"
                    class="input font-mono uppercase"
                    bind:value={form.call}
                    use:focusOnMount
                />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600"
                    >Date off <span class="text-gray-400">(if overnight)</span></span
                >
                <input type="date" class="input" bind:value={form.qso_date_off} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Country</span>
                <input type="text" class="input" bind:value={form.country} />
            </label>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Freq <span class="text-gray-400">(MHz)</span></span>
                <input type="text" class="input font-mono" bind:value={form.freq} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600"
                    >Band <span class="text-gray-400">(follows freq)</span></span
                >
                <input type="text" class="input" bind:value={form.band} />
            </label>
            <div></div>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Mode</span>
                <input type="text" class="input uppercase" bind:value={form.mode} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Submode</span>
                <input type="text" class="input uppercase" bind:value={form.submode} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Grid</span>
                <input type="text" class="input" bind:value={form.gridsquare} />
            </label>

            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">RST sent</span>
                <input type="text" class="input font-mono" bind:value={form.rst_sent} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">RST rcvd</span>
                <input type="text" class="input font-mono" bind:value={form.rst_rcvd} />
            </label>
            <label class="flex flex-col gap-1 text-sm">
                <span class="text-gray-600">Name</span>
                <input type="text" class="input" bind:value={form.name} />
            </label>

            <label class="flex flex-col gap-1 text-sm sm:col-span-3">
                <span class="text-gray-600">Comment</span>
                <input type="text" class="input" bind:value={form.comment} />
            </label>
        </form>

        {#if logbookState.editError !== null}
            <p
                class="mx-5 mb-1 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800"
            >
                {logbookState.editError}
            </p>
        {/if}

        <div class="flex items-center justify-end gap-2 border-t border-gray-200 px-5 py-3">
            <button
                type="button"
                class="cursor-pointer rounded-md border border-gray-300 px-3 py-1.5 text-sm hover:bg-gray-50"
                onclick={() => logbookState.closeEdit()}>Cancel</button
            >
            <button
                type="button"
                class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
                disabled={logbookState.savingEdit}
                onclick={() => void save()}
            >
                {logbookState.savingEdit ? 'Saving…' : 'Save'}
            </button>
        </div>
    </div>
</div>

<style>
    /* Local input style shared by the form fields — kept here so the modal is
       self-contained (the SPA has no shared input primitive yet). */
    .input {
        border-radius: 0.375rem;
        border: 1px solid var(--color-gray-300, #d1d5db);
        padding: 0.25rem 0.5rem;
        font-size: 0.875rem;
    }
    .input:focus {
        outline: none;
        border-color: var(--color-indigo-500, #6366f1);
        box-shadow: 0 0 0 1px var(--color-indigo-500, #6366f1);
    }
</style>
