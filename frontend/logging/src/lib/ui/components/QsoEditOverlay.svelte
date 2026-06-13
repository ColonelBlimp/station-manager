<!--
    QsoEditOverlay — modal-style edit form for a previously-logged QSO.

    Opened from a SessionPanel row click. State is held in `qsoEditState`
    (lib/states/qsoEdit.svelte.ts), independent of `qsoDraft` so a half-
    finished edit cannot contaminate the in-progress draft.

    Dismissal contract:
      - Cancel button → close without save.
      - ESC key → close without save.
      - Click on the dim backdrop (outside the modal card) → close
        without save. Click inside the card does NOT close.
      - Save button → PATCH /v1/qso/{uuid}, then on ok update the
        SessionPanel row and close.

    Layout follows QsoPanel + the Details panel content:
      - Row 1: Callsign / RST Sent / RST Rcvd / Mode / VFO-A+VFO-B pair
      - Row 2: Name / QTH / Comment
      - Row 3: Date / Time On / Time Off
      - Row 4: Rig (textarea) / Power (digits) / Request QSL checkbox
      - Row 5: Notes (textarea, full width)

    The VFO pair mirrors QsoPanel's <Vfos /> visual (VfoBox + VfoInput
    rows) but operates on `qsoEditState.freq` / `freqRx` rather than the
    live CAT state — historical edit must not perturb manualState /
    catState. Conversion shims live in this file because they're tied
    to the overlay's MHz-string ↔ Hz boundary.

    Country and band are NOT editable: country is daemon-required and
    set by enrichment at submit time; band is derived from frequency
    by the daemon's normalize step on PATCH.
-->
<script lang="ts">
    import { qsoEditState } from '../../states/qsoEdit.svelte';
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { patchQso } from '../../api/qso-update';
    import { toasts } from '../../states/toasts.svelte';
    import { frequencyToBand, formatFrequency } from '../../utils/frequency';
    import TextInput from './TextInput.svelte';
    import Rst from './Rst.svelte';
    import Mode from './Mode.svelte';
    import DateInput from './DateInput.svelte';
    import TimeInput from './TimeInput.svelte';
    import Comment from './Comment.svelte';
    import VfoBox from './VfoBox.svelte';
    import VfoInput from './VfoInput.svelte';

    /*
        Mode list — same set as the log form. Kept inline rather than
        extracted into a shared module because the canonical source is
        the daemon's enums/modes package, and each form's exposed subset
        is a UX choice (the daemon's full list is wider than what makes
        sense to show in a dropdown). When a future "all ADIF modes"
        affordance lands, this list should grow rather than become a
        shared constant.
    */
    const modes: string[] = ['USB', 'LSB', 'CW', 'FM', 'AM', 'RTTY', 'FT8', 'FT4', 'PSK31'];

    /*
        MHz string ↔ Hz integer conversion, scoped to this file because
        the boundary is local to the overlay (qsoEditState speaks the
        daemon's MHz string; VfoInput speaks Hz). Daemon storage is
        kHz integer, so anything below kHz precision is lost on the
        round-trip — these helpers truncate to that granularity to keep
        the SPA's view consistent with what the daemon will persist.
    */
    function mhzStringToHz(mhz: string): number {
        const n = parseFloat(mhz);
        if (Number.isNaN(n)) return 0;
        return Math.round(n * 1000) * 1000; // round to kHz then up to Hz
    }

    function hzToMhzString(hz: number): string {
        const kHz = Math.round(hz / 1000);
        const mhz = Math.floor(kHz / 1000);
        const frac = (kHz % 1000).toString().padStart(3, '0');
        return `${mhz}.${frac}`;
    }

    /*
        Display values — derived from the MHz strings on each render.
        Kept as $derived so an operator commit on VFO-A immediately
        updates the band label without an explicit re-format step.
    */
    const vfoAHz: number = $derived(mhzStringToHz(qsoEditState.freq));
    const vfoBHz: number = $derived(mhzStringToHz(qsoEditState.freqRx));
    const vfoAValue: string = $derived(vfoAHz > 0 ? formatFrequency(vfoAHz) : '');
    const vfoBValue: string = $derived(vfoBHz > 0 ? formatFrequency(vfoBHz) : '');
    const vfoABand: string = $derived(vfoAHz > 0 ? frequencyToBand(vfoAHz) : '');
    const vfoBBand: string = $derived(vfoBHz > 0 ? frequencyToBand(vfoBHz) : '');

    /*
        Focus-trap plumbing. `dialogEl` is bound to the inner card so
        we can query its focusable descendants; `previouslyFocused`
        captures whatever the operator was on before the overlay opened
        (typically the SessionPanel row that triggered the open) so
        focus can be restored on close. `wasOpen` and `initialFocused`
        are plain `let` — no reactive consumer, just per-effect-run
        snapshots — so they avoid the `$state` overhead.
    */
    let dialogEl: HTMLDivElement | undefined = $state();
    let previouslyFocused: HTMLElement | null = null;
    let wasOpen = false;
    let initialFocused = false;

    /*
        Modal-dialog focus management:
          - On the open transition (false → true) snapshot the active
            element so we can restore on close.
          - Once the form has rendered (open && !loading) focus the
            first input. Gated by `initialFocused` so a later loading
            flip doesn't re-yank focus from wherever the operator
            has moved it.
          - On the close transition (true → false) restore focus.
    */
    $effect(() => {
        const open = qsoEditState.open;
        const loading = qsoEditState.loading;

        if (open && !wasOpen) {
            previouslyFocused = document.activeElement as HTMLElement | null;
            initialFocused = false;
        }
        if (open && !loading && !initialFocused) {
            const first = dialogEl?.querySelector<HTMLElement>(
                'input:not([disabled]),textarea:not([disabled]),select:not([disabled])'
            );
            first?.focus();
            initialFocused = true;
        }
        if (!open && wasOpen) {
            previouslyFocused?.focus();
            previouslyFocused = null;
            initialFocused = false;
        }
        wasOpen = open;
    });

    /*
        Collect every focusable descendant of the dialog card. Used by
        the Tab trap below to find first/last focus targets. Excludes
        `tabindex="-1"` elements (the backdrop carries that as the
        click-target sentinel, not a focusable role).
    */
    function focusableInDialog(): HTMLElement[] {
        if (!dialogEl) return [];
        const sel =
            'a[href],button:not([disabled]),input:not([disabled]),' +
            'select:not([disabled]),textarea:not([disabled]),' +
            '[tabindex]:not([tabindex="-1"])';
        return Array.from(dialogEl.querySelectorAll<HTMLElement>(sel));
    }

    /*
        Window-level keydown for ESC (dismiss) and Tab (focus trap).

        ESC uses stopImmediatePropagation rather than stopPropagation
        because every <svelte:window> in the SPA registers its handler
        on the same target — `stopPropagation` would not stop a sibling
        window handler (notably QsoPanel.handleKeydown, which clears
        the live draft on ESC). The contract is "this overlay owns
        ESC while it is open." QsoPanel's `if (qsoEditState.open) return`
        guard is preserved as belt-and-braces against listener-
        registration-order surprises.

        Tab is trapped on the boundaries: shift+Tab from the first
        focusable wraps to the last, Tab from the last wraps to the
        first, and if focus has somehow escaped the dialog (rare, but
        possible if external code stole focus) it is pulled back to
        the first focusable.
    */
    function handleKeydown(e: KeyboardEvent): void {
        if (!qsoEditState.open) return;

        if (e.key === 'Escape') {
            if (!qsoEditState.saving) {
                qsoEditState.close();
            }
            e.stopImmediatePropagation();
            return;
        }

        if (e.key === 'Tab') {
            const focusables = focusableInDialog();
            if (focusables.length === 0) return;
            const first = focusables[0];
            const last = focusables[focusables.length - 1];
            const active = document.activeElement as HTMLElement | null;

            if (active && dialogEl && !dialogEl.contains(active)) {
                e.preventDefault();
                first.focus();
                return;
            }
            if (e.shiftKey && active === first) {
                e.preventDefault();
                last.focus();
            } else if (!e.shiftKey && active === last) {
                e.preventDefault();
                first.focus();
            }
        }
    }

    /*
        Backdrop click — close only if the click target IS the backdrop,
        not a descendant. Children stop the bubble before the listener
        runs. Prevents the modal from closing when the operator drags a
        selection across the card.
    */
    function handleBackdropClick(e: MouseEvent): void {
        if (e.target === e.currentTarget && !qsoEditState.saving) {
            qsoEditState.close();
        }
    }

    /*
        Save flow:
          - PATCH the daemon. Daemon revalidates and may reject with
            duplicate / validation / not_found / server / network — all
            handled with appropriate toasts.
          - On ok, update the session-list row in place via
            sessionQsosState.update. Only the columns the SessionPanel
            renders need refreshing; uuid stays stable.
          - Close on success; keep open on failure so the operator can
            try again without retyping.
    */
    async function handleSave(): Promise<void> {
        if (qsoEditState.saving) return;
        qsoEditState.saving = true;

        try {
            // Build inside the try so a throw from toPatchBody (rare
            // but possible for malformed-draft edge cases) still hits
            // the finally and clears `saving` — otherwise the Save
            // button would stay disabled until the overlay was
            // dismissed and reopened.
            const uuid = qsoEditState.uuid;
            const body = qsoEditState.toPatchBody();
            const outcome = await patchQso(uuid, body);
            switch (outcome.kind) {
                case 'ok': {
                    /*
                        Update the matching SessionPanel row. Use the
                        operator-time band derivation for consistency
                        with the log path; the daemon's response also
                        carries a freshly-derived band but
                        SessionPanel's column is presentational.
                    */
                    const freqHz = mhzStringToHz(qsoEditState.freq);
                    const existing = sessionQsosState.items.find((q) => q.uuid === uuid);
                    if (existing) {
                        sessionQsosState.update(uuid, {
                            ...existing,
                            callsign: qsoEditState.callsign,
                            name: qsoEditState.name,
                            freqHz,
                            band: freqHz > 0 ? frequencyToBand(freqHz) : existing.band,
                            rstSent: qsoEditState.rstSent,
                            rstRcvd: qsoEditState.rstRcvd,
                            mode: qsoEditState.mode,
                            timeOn: qsoEditState.timeOn,
                            qsoDate: qsoEditState.qsoDate,
                        });
                    }
                    toasts.info('QSO updated');
                    qsoEditState.close();
                    break;
                }
                case 'not_found':
                    toasts.error('QSO no longer exists');
                    qsoEditState.close();
                    break;
                case 'duplicate':
                    toasts.error(outcome.message);
                    break;
                case 'validation':
                    toasts.error(outcome.message);
                    break;
                case 'server':
                    toasts.error(`Update failed: ${outcome.message}`);
                    break;
                case 'network':
                    toasts.error('Cannot reach daemon');
                    break;
            }
        } finally {
            qsoEditState.saving = false;
        }
    }

    /*
        rxPwr digit-strip — same pattern as DetailsPanel. Keeps the
        field strictly numeric without an explicit number input
        (number-typed inputs leak step controls and IME behaviour we
        don't want here).
    */
    function handleRxPwrInput(e: Event): void {
        const t = e.currentTarget as HTMLInputElement;
        const cleaned = t.value.replace(/[^0-9]/g, '');
        if (cleaned !== t.value) {
            t.value = cleaned;
        }
        qsoEditState.rxPwr = cleaned;
    }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if qsoEditState.open}
    <!--
        Backdrop. fixed inset-0 + z-50 keeps it above the rest of the
        UI; bg-black/40 dims the underlay. role="dialog" + aria-modal
        marks the modal nature for assistive tech; the keydown handler
        on svelte:window makes ESC universally close.
    -->
    <div
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
        role="dialog"
        aria-modal="true"
        aria-label="Edit QSO"
        onclick={handleBackdropClick}
        onkeydown={(e) => {
            // Click-as-keypress accessibility: pressing Enter on the
            // backdrop element should dismiss the same way as a click.
            if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) {
                e.preventDefault();
                if (!qsoEditState.saving) qsoEditState.close();
            }
        }}
        tabindex="-1"
    >
        <div
            bind:this={dialogEl}
            class="bg-white rounded-lg shadow-2xl w-4xl max-w-[95vw] max-h-[95vh] overflow-y-auto p-6"
            role="document"
        >
            <div class="flex flex-row items-center justify-between mb-4">
                <h2 class="text-lg font-semibold text-gray-800">Edit QSO</h2>
                <span class="text-xs text-gray-500 font-mono">{qsoEditState.uuid}</span>
            </div>

            {#if qsoEditState.loading}
                <p class="text-sm text-gray-500 italic py-8 text-center">Loading…</p>
            {:else}
                <div class="flex flex-col space-y-2">
                    <!-- Row 1: Callsign / RST pair / Mode / VFO-A+VFO-B -->
                    <div class="flex flex-row space-x-2">
                        <TextInput
                            id="edit-callsign"
                            label="Callsign"
                            bind:value={qsoEditState.callsign}
                            widthClass="w-40"
                        />
                        <Rst
                            id="edit-rst-sent"
                            label="RST Sent"
                            bind:value={qsoEditState.rstSent}
                            mode={qsoEditState.mode}
                        />
                        <Rst
                            id="edit-rst-rcvd"
                            label="RST Rcvd"
                            bind:value={qsoEditState.rstRcvd}
                            mode={qsoEditState.mode}
                        />
                        <Mode
                            id="edit-mode"
                            label="Mode"
                            value={qsoEditState.mode}
                            onchange={(v: string) => {
                                qsoEditState.mode = v;
                            }}
                            list={modes}
                        />
                        <!--
                            VFO pair — VFO-A on top (TX), VFO-B below (RX
                            for split). pt-2 mirrors Vfos.svelte's internal
                            label-compensation so the boxes align with the
                            input baseline of sibling labelled fields.
                        -->
                        <div class="flex flex-col space-y-2 pt-2 w-56">
                            <div class="flex">
                                <VfoBox label="VFO-A" action="TX" disabled isSelected />
                                <VfoInput
                                    id="edit-vfo-a"
                                    value={vfoAValue}
                                    band={vfoABand}
                                    onCommit={(hz: number) => {
                                        qsoEditState.freq = hzToMhzString(hz);
                                    }}
                                />
                            </div>
                            <div class="flex">
                                <VfoBox label="VFO-B" action="RX" disabled />
                                <VfoInput
                                    id="edit-vfo-b"
                                    value={vfoBValue}
                                    band={vfoBBand}
                                    onCommit={(hz: number) => {
                                        qsoEditState.freqRx = hzToMhzString(hz);
                                    }}
                                />
                            </div>
                        </div>
                    </div>

                    <!-- Row 2: Name / QTH / Comment -->
                    <div class="flex flex-row space-x-2 -mt-1">
                        <TextInput id="edit-name" label="Name" bind:value={qsoEditState.name} />
                        <TextInput
                            id="edit-qth"
                            label="QTH"
                            widthClass="w-46"
                            bind:value={qsoEditState.qth}
                        />
                        <Comment
                            id="edit-comment"
                            label="Comment"
                            bind:value={qsoEditState.comment}
                        />
                    </div>

                    <!-- Row 3: Date / Time On / Time Off -->
                    <div class="flex flex-row space-x-2 -mt-4.5">
                        <DateInput
                            id="edit-qso-date"
                            label="Date"
                            bind:value={qsoEditState.qsoDate}
                        />
                        <TimeInput
                            id="edit-time-on"
                            label="Time On (UTC)"
                            bind:value={qsoEditState.timeOn}
                        />
                        <TimeInput
                            id="edit-time-off"
                            label="Time Off (UTC)"
                            bind:value={qsoEditState.timeOff}
                        />
                    </div>

                    <!-- Row 4: Rig / Power / Request QSL — Details panel parity -->
                    <div class="flex flex-row space-x-3 items-end">
                        <div class="flex-1">
                            <label for="edit-rig" class="input-label">Rig</label>
                            <textarea
                                id="edit-rig"
                                placeholder="Working conditions"
                                bind:value={qsoEditState.rig}
                                rows="2"
                                class="textarea-base mt-1 w-full"
                            ></textarea>
                        </div>
                        <div class="w-24">
                            <label for="edit-rx-pwr" class="input-label">Power</label>
                            <input
                                id="edit-rx-pwr"
                                type="text"
                                inputmode="numeric"
                                placeholder="Watts"
                                value={qsoEditState.rxPwr}
                                oninput={handleRxPwrInput}
                                class="input-base mt-1"
                                autocomplete="off"
                            />
                        </div>
                        <div class="flex flex-row items-center gap-x-2 pb-2">
                            <label for="edit-request-qsl" class="input-label">Request QSL:</label>
                            <input
                                id="edit-request-qsl"
                                type="checkbox"
                                bind:checked={qsoEditState.requestQsl}
                                class="size-4"
                            />
                        </div>
                    </div>

                    <!-- Row 5: Notes — full width -->
                    <div>
                        <label for="edit-notes" class="input-label">Notes</label>
                        <textarea
                            id="edit-notes"
                            placeholder="My personal notes"
                            bind:value={qsoEditState.notes}
                            rows="2"
                            class="textarea-base mt-1 w-full"
                        ></textarea>
                    </div>
                </div>

                <div class="flex flex-row justify-end space-x-2 mt-6 pt-4 border-t border-gray-200">
                    <button
                        type="button"
                        onclick={() => qsoEditState.close()}
                        disabled={qsoEditState.saving}
                        class="px-4 py-2 rounded text-sm font-medium text-gray-700 bg-gray-100 hover:bg-gray-200 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onclick={handleSave}
                        disabled={qsoEditState.saving}
                        class="px-4 py-2 rounded text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
                    >
                        {qsoEditState.saving ? 'Saving…' : 'Save'}
                    </button>
                </div>
            {/if}
        </div>
    </div>
{/if}
