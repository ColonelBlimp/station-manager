<script lang="ts">
    // Phone/CW logging card — the fast-path entry fields (callsign, RST, time),
    // a Name + Comment row, and a collapsible Contact-details disclosure for the
    // contacted-station facts kept off the fast path (QTH / Rig / RX power / Notes
    // to edit; QRZ page link + looked-up email + CQ/ITU zone to read). QTH and
    // Gridsquare are enrichment-filled (fill-if-empty in enrich.svelte), correctable
    // here — the former ContactDialog overlay was folded in (its only non-duplicate
    // fields were Gridsquare + the two zones; everything else already lived here).
    //
    // The right column hosts EnrichmentCard (flag / DXCC + NEW / bearing SP-LP /
    // distance — mirrors the FT8 Band-Activity enrichment). This card is only its
    // host, not its owner: EnrichmentCard fills whatever box it's given, so a
    // future drag/pin layout can relocate it without touching either component.
    //
    // Reads/writes the shared QSO draft (qso.svelte); it does NOT submit directly
    // (logDraft() calls the injected sink) and makes no assumption about where it's
    // positioned (ADR 0045). Rig fields (freq/mode/band) belong to the Rig panel.
    import {
        draft,
        canLog,
        logDraft,
        clearDraft,
        dismissDuplicate,
        startQso,
        holdOffTimes,
        submitState,
        draftProblems,
        qsoClock,
    } from './qso.svelte';
    import DuplicateDialog from './DuplicateDialog.svelte';
    import { observeWorked, openWorkedForQso } from './worked.svelte';
    import { rigReady, rigGate } from './rig.svelte';
    import { operate, closeExport, registerCallsignInput } from './state.svelte';
    import { toasts } from '../ui/toasts.svelte';
    import EnrichmentCard from './EnrichmentCard.svelte';
    import { enrich } from './enrich.svelte';
    import { isValidMaidenhead } from '../validators/maidenhead';
    import { isValidCallsign } from '../validators/callsign';
    import { callsignStack } from './callsignStack.svelte';
    import { commentHistory } from './commentHistory.svelte';
    import CommentField from './CommentField.svelte';
    import { sessionEdit } from './sessionEdit.svelte';

    // Contact-details disclosure (grid / QTH / rig / RX power / notes to edit; QRZ
    // page link + looked-up email + CQ/ITU zone to read — all for the contacted
    // station). Enrichment is trusted only when it belongs to the call in the draft
    // (a fast edit can outrun the debounced lookup).
    const detailsCall = $derived(draft.callsign.trim().toUpperCase());
    const enrichData = $derived(enrich.call === detailsCall ? enrich.data : null);
    const qrzUrl = $derived(
        detailsCall === '' ? null : `https://www.qrz.com/db/${encodeURIComponent(detailsCall)}`
    );
    // Gridsquare is enrichment-filled but hand-correctable here (folded off the
    // former ContactDialog overlay); validate it the same way the overlay did —
    // isValidMaidenhead returns non-null on a bad grid.
    const gridInvalid = $derived(
        draft.gridsquare !== '' && isValidMaidenhead(draft.gridsquare) !== null
    );
    // Disclosure open-state, mirrored from the native <details> via bind:open.
    // Drives which of the two action-row sites renders accessibly and the
    // details' open-state rounding — see the markup comments at both places.
    let detailsOpen = $state(false);
    function upperGrid(): void {
        draft.gridsquare = draft.gridsquare.toUpperCase();
    }

    // Why the Log button is gated, for the tooltip (undefined when it isn't).
    const gateTitle = $derived(
        rigGate() === 'lost'
            ? 'CAT link lost — rig context may be stale'
            : rigGate() === 'unconfirmed'
              ? 'Cannot log yet — confirm band/mode/frequency in the Rig panel first'
              : undefined
    );

    function upperCall(): void {
        draft.callsign = draft.callsign.toUpperCase();
    }

    // Keyboard fast path (card-scoped: the svelte:window listener lives and
    // dies with this card, so the shortcuts exist only on Phone/CW):
    //   Ctrl+Enter — log; on SUCCESS focus returns to the callsign field for
    //                the next contact (a refusal leaves focus where the
    //                operator is fixing things).
    //   Escape     — clear the draft and start over at the callsign field.
    let callInput: HTMLInputElement | undefined;

    async function logAndRefocus(): Promise<void> {
        // Comment-history recording lives in logDraft's shared success path (so a
        // forced-duplicate "Log anyway" records it too); this only refocuses.
        if (await logDraft()) callInput?.focus();
    }

    function windowKeydown(e: KeyboardEvent): void {
        // Session-edit modal open: it owns the keyboard OUTRIGHT (its own
        // window handler saves on Ctrl+Enter and closes on Escape) — and
        // window listeners do not shadow each other, so without this guard
        // Escape ALSO wiped the draft being typed below and Ctrl+Enter could
        // LOG it. The retired SPA's handleKeydown opened with exactly this
        // guard; the port dropped it (keyboard audit 2026-08-06, A27).
        if (sessionEdit.row !== null) return;
        // Export modal open: it owns the keys. Esc closes it; the log/clear
        // shortcuts are inert so they can't act on the card behind the modal.
        if (operate.exportOpen) {
            if (e.key === 'Escape') {
                e.preventDefault();
                closeExport();
            }
            return;
        }
        // Duplicate dialog open: it owns the keys. Esc dismisses the DIALOG
        // (never the draft underneath); Ctrl+Enter is inert so a key-repeat
        // can't accidentally force-log. Enter acts on the focused button.
        if (submitState.duplicate) {
            if (e.key === 'Escape') {
                e.preventDefault();
                dismissDuplicate();
                callInput?.focus();
            }
            return;
        }
        // NO `operate.pileup` GUARD HERE. It used to stand the logging
        // shortcuts down whenever FT8's pile-up drawer was open, on the
        // reasoning that the drawer owns its own Escape. Sound in FT8 — but
        // that drawer used to render over Phone/CW too, where it can never hold
        // anything, so opening it killed Ctrl+Enter and Esc mid-pile-up for no
        // benefit. Worse, the flag is view state that survives a mode switch,
        // so Phone/CW could inherit it SET with no drawer on screen to explain
        // the silence. The drawer is now FT8-only (Operate.svelte) and this card
        // is Phone/CW-only, so the two can no longer be on screen together.
        if (e.key === 'Enter' && e.ctrlKey && !e.altKey && !e.shiftKey) {
            e.preventDefault();
            void logAndRefocus();
            return;
        }
        if (e.key === 'Escape') {
            e.preventDefault();
            clearDraft();
            callInput?.focus();
            return;
        }
        if (functionKeydown(e)) return;
        pileupKeydown(e);
    }

    // Function-key fast path (F2 peek, F3 timer). Both are focus-independent (a
    // function key has no text-editing meaning) and both no-op on auto-repeat —
    // one PRESS is one action. Split out of windowKeydown to keep it readable, not
    // to satisfy a complexity budget. Returns true when it handled the event.
    function functionKeydown(e: KeyboardEvent): boolean {
        // F2 — lookup-only "peek" (restored by operator direction 2026-08-18,
        // W-0003; the 2026-08-06 "ruled moot" was reversed). Reveal the
        // worked-before panel for the typed call WITHOUT starting the QSO timer,
        // so the operator can scan prior contacts and station info and decide
        // whether to commit: Tab is the commit signal, F2 is the peek. Enrichment
        // already auto-loads (EnrichmentCard); F2 adds the contact-history reveal,
        // which otherwise only auto-opens on a hit. Gated on a valid call.
        if (e.key === 'F2') {
            e.preventDefault();
            if (e.repeat) return true;
            const trimmed = draft.callsign.trim();
            if (trimmed !== '' && isValidCallsign(trimmed) === null) {
                openWorkedForQso(draft.callsign);
            }
            return true;
        }
        // F3 — the timer toggle (A28, ported from the retired SPA's TimerControls
        // mirror by operator direction): freeze a running Time Off (the contact has
        // ended; details are still being typed), or start the clock for a typed
        // call. After a hold it is a SILENT no-op — re-ticking would overwrite an
        // end time set by hand, the exact value holdOffTimes exists to protect.
        if (e.key === 'F3') {
            e.preventDefault();
            // A repeat landing after the start would freeze Time Off ms later — one
            // PRESS is one action, however long it lasts (review 6af12ca9).
            if (e.repeat) return true;
            if (qsoClock.ticking) holdOffTimes();
            else if (!qsoClock.started && draft.callsign.trim() !== '') startQso();
            return true;
        }
        return false;
    }

    // Pile-up capture (ported from the retired SPA's QsoPanel). Shift+Enter sets
    // the typed call aside and clears the draft — the same "start fresh" effect
    // as Esc, plus the push. Shift+Up/Down take one back: newest and oldest, so
    // a pile-up can be worked from either end. Split out of windowKeydown to
    // keep either half readable, not to satisfy a complexity budget.
    function pileupKeydown(e: KeyboardEvent): void {
        if (e.key === 'Enter' && e.shiftKey && !e.ctrlKey && !e.metaKey) {
            // Only from the callsign field, or from no field at all. In Notes
            // Shift+Enter is an ordinary NEWLINE, and stacking there also ran
            // clearDraft() — erasing every field of a QSO in progress from a
            // keystroke that looked like typing. v1 reasoned that Shift+Enter
            // has "no text-editing meaning, so it stays live even in a field",
            // which holds for an <input> and not for a <textarea>.
            if (isTextEntry(e.target) && e.target !== callInput) return;
            e.preventDefault();
            stackCall();
            return;
        }
        // NOT while typing — Shift+Arrow is native select-to-line — and NOT with
        // Ctrl, which is the rig freq-step family in RigKeys.
        if (isTextEntry(e.target) || !e.shiftKey || e.ctrlKey || e.metaKey) return;
        if (e.key === 'ArrowUp') {
            e.preventDefault();
            loadPopped(callsignStack.popTop());
        } else if (e.key === 'ArrowDown') {
            e.preventDefault();
            loadPopped(callsignStack.popBottom());
        }
    }

    function isTextEntry(t: EventTarget | null): boolean {
        if (!(t instanceof HTMLElement)) return false;
        const tag = t.tagName;
        return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t.isContentEditable;
    }

    // Capture the typed call. Validated the same way logging is — an empty or
    // malformed field is a silent no-op rather than a junk stack entry.
    function stackCall(): void {
        const call = draft.callsign.trim().toUpperCase();
        if (call === '' || isValidCallsign(call) !== null) return;
        callsignStack.push(call);
        clearDraft();
        callInput?.focus();
    }

    // Pop = load: the call moves into the draft and leaves the stack, so it is
    // never in both. An empty stack leaves whatever is typed alone.
    function loadPopped(call: string | undefined): void {
        if (call === undefined) return;
        draft.callsign = call;
        callInput?.focus();
    }

    // Committing the call = "I'm working this station": stamps Date/Time On,
    // starts the ticking Time Off (the QSO timer), and surfaces the
    // worked-before panel if nothing is open. If the rig gate isn't confirmed
    // the QSO still starts (the clock runs), but it CAN'T be logged — so warn
    // at start rather than let the operator discover it only at the disabled
    // Log button.
    function commitCall(): void {
        const fresh = !qsoClock.started;
        startQso();
        openWorkedForQso(draft.callsign);
        if (fresh && !rigReady()) {
            toasts.warn(
                rigGate() === 'lost'
                    ? 'CAT link lost — confirm the rig in the Rig panel before you can log this QSO.'
                    : 'Rig not confirmed — confirm the band in the Rig panel before you can log this QSO.'
            );
        }
    }

    // Callsign-field keys (A29, retired-SPA semantics by operator direction):
    // Tab commits and moves focus on to RST as normal (not swallowed; its
    // shipped non-empty gate is deliberately untouched). Enter commits
    // WITHOUT moving focus, and Space both commits and is swallowed — a
    // callsign is a single token, so a literal space is never wanted, even
    // mid-edit of an invalid call. Enter/Space require a VALID call (the
    // retired gate): a Space habit must not start the clock on a half-typed
    // call. Modified variants bail untouched so the window-level meanings
    // survive — Shift+Enter stacks, Ctrl+Enter logs.
    function callKeydown(e: KeyboardEvent): void {
        if (e.shiftKey || e.ctrlKey || e.metaKey || e.altKey) return;
        if (e.key === 'Tab') {
            if (draft.callsign.trim() !== '') commitCall();
            return;
        }
        const isSpace = e.key === ' ' || e.code === 'Space';
        if (isSpace) e.preventDefault();
        if (e.key !== 'Enter' && !isSpace) return;
        if (e.key === 'Enter') e.preventDefault();
        const trimmed = draft.callsign.trim();
        if (trimmed === '' || isValidCallsign(trimmed) !== null) return;
        commitCall();
    }

    // The worked-before lookup is driven here, not in WorkedPanel: it must run
    // while the panel is CLOSED so a hit can auto-open it. This card owns the
    // callsign field, so it hosts the observation. (Enrichment observes from
    // EnrichmentCard, which is always mounted.)
    $effect(() => observeWorked(draft.callsign));

    // Per-field malformed flags → red outlines; canLog() blocks on any of them.
    const p = $derived(draftProblems());

    // Entering Phone/CW = about to log: start at the callsign field. Runs on
    // mount only (callInput is bound once), same landing spot as Esc/log.
    // Registration lets the util rail hand focus back here (state.svelte).
    $effect(() => {
        registerCallsignInput(callInput ?? null);
        callInput?.focus();
        return () => registerCallsignInput(null);
    });
</script>

<svelte:window onkeydown={windowKeydown} />

<DuplicateDialog />

<!-- mx-auto is load-bearing, not cosmetic. --card-w is a FIXED 558px (φ × 354),
     and a fixed-width box does not stretch inside Operate's `flex flex-col`
     container (max-w-3xl / 768px) — it is placed at the START of the cross axis,
     i.e. hard left, ~105px off centre.
     It used to be centred by the ADR 0046 tile board, whose CSS did it with auto
     margins ("Centred when it fits", app.css:306). ADR 0058 retired that board
     for a plain flex column and the card kept its width but lost the mechanism
     that centred it. Fixed here rather than with `items-center` on the
     container: the sibling tiles have NO fixed width and rely on the default
     stretch, so centring the container would shrink them all to content width. -->
<!-- relative z-10: "the whole logging panel at a greater z" (operator,
     2026-08-06) — the Contact-details expansion overflows the card's slot,
     and this is what paints it (and the card) OVER the Worked panel below
     instead of underneath it. Still under the drawers (z-20) and the ambient
     host (z-40). -->
<div class="card relative z-10 mx-auto w-(--card-w)">
    <div class="flex flex-col">
        <div class="flex flex-row gap-x-6">
            <div class="flex flex-col">
                <div class="flex items-end gap-x-2">
                    <div>
                        <label for="lc-call" class="block text-sm font-medium text-ink"
                            >Callsign</label
                        >
                        <div class="relative">
                            <input
                                id="lc-call"
                                class="input w-32 pr-7 uppercase"
                                class:input-error={p.callsign}
                                autocomplete="off"
                                spellcheck="false"
                                placeholder="Callsign"
                                bind:this={callInput}
                                bind:value={draft.callsign}
                                oninput={upperCall}
                                onkeydown={callKeydown}
                            />
                            <!-- Mouse-accessible stack push (restored by operator
                                 direction 2026-08-18, W-0003): the ≡ glyph stacks the
                                 typed call, the pointer equivalent of Shift+Enter.
                                 tabindex=-1 keeps it out of the Callsign→RST tab order
                                 so the keyboard flow is unbroken; the title exposes
                                 the shortcut. Same validate-or-no-op as Shift+Enter
                                 (stackCall). -->
                            <button
                                type="button"
                                tabindex={-1}
                                title="Stack callsign (Shift+Enter)"
                                aria-label="Stack callsign"
                                onclick={stackCall}
                                class="absolute inset-y-0 right-0 flex items-center px-2 leading-none text-muted hover:text-ink"
                            >
                                <span aria-hidden="true">≡</span>
                            </button>
                        </div>
                    </div>
                    <div>
                        <label for="lc-rst-s" class="block text-sm font-medium text-ink"
                            >RST Sent</label
                        >
                        <input
                            id="lc-rst-s"
                            class="input w-15"
                            class:input-error={p.rstSent}
                            bind:value={draft.rstSent}
                        />
                    </div>
                    <div>
                        <label for="lc-rst-r" class="block text-sm font-medium text-ink"
                            >RST Rcvd</label
                        >
                        <input
                            id="lc-rst-r"
                            class="input w-15"
                            class:input-error={p.rstRcvd}
                            bind:value={draft.rstRcvd}
                        />
                    </div>
                </div>
                <div class="mt-2 flex items-end gap-x-2">
                    <div>
                        <label for="lc-date-on" class="block text-sm font-medium text-ink"
                            >Date On</label
                        >
                        <input
                            id="lc-date-on"
                            class="input w-32"
                            class:input-error={p.dateOn}
                            placeholder="YYYY-MM-DD"
                            bind:value={draft.dateOn}
                        />
                    </div>
                    <div>
                        <label for="lc-time-on" class="block text-sm font-medium text-ink"
                            >Time On</label
                        >
                        <input
                            id="lc-time-on"
                            class="input w-24"
                            class:input-error={p.timeOn}
                            placeholder="HH:MM:SS"
                            bind:value={draft.timeOn}
                        />
                    </div>
                </div>
                <div class="mt-2 flex items-end gap-2">
                    <div>
                        <label for="lc-date-off" class="block text-sm font-medium text-ink"
                            >Date Off</label
                        >
                        <input
                            id="lc-date-off"
                            class="input w-32"
                            class:input-error={p.dateOff}
                            placeholder="YYYY-MM-DD"
                            bind:value={draft.dateOff}
                            oninput={holdOffTimes}
                        />
                    </div>
                    <div>
                        <label for="lc-time-off" class="block text-sm font-medium text-ink"
                            >Time Off</label
                        >
                        <input
                            id="lc-time-off"
                            class="input w-24"
                            class:input-error={p.timeOff}
                            placeholder="HH:MM:SS"
                            bind:value={draft.timeOff}
                            oninput={holdOffTimes}
                        />
                    </div>
                </div>
            </div>
            <div class="flex w-56 h-45 shrink-0">
                <EnrichmentCard call={draft.callsign} />
            </div>
        </div>
        <div class="mt-2 flex w-full items-end gap-x-2">
            <div class="flex-1">
                <label for="lc-name" class="block text-sm font-medium text-ink">Name</label>
                <input
                    id="lc-name"
                    class="input w-full"
                    autocomplete="off"
                    bind:value={draft.name}
                />
            </div>
            <CommentField
                id="lc-comment"
                label="Comment"
                class="flex-1"
                items={commentHistory.items}
                bind:value={draft.comment}
            />
        </div>
        <!-- Contact details: contacted-station fields kept off the fast path —
     QTH / Rig / RX power / Notes to edit, QRZ page link + looked-up email to
     read. QTH is enrichment-filled (correctable here). Sits where the Comment
     row used to be.

     The open UI is UNCHANGED from the original in-flow disclosure (operator,
     2026-08-06, three rounds: the Worked panel must not move; "a disclosure
     EXTENDS the current panel - the Clear and Log QSO buttons should move
     down"; "the disclosures' UI should remain unchanged"): one bordered box
     under the summary, the action row below the box at card level, the card
     frame wrapping it all. What changed is WHERE that lower half lives: z
     alone cannot stop a reflow (an in-flow expansion pushes siblings
     whatever it paints over), so everything from the content down is an
     out-of-flow extension that REPRODUCES the card's lower half — content
     box, mt-4 action row, card padding (p-5 = the 1.25rem in the inset
     calc), border and bottom rounding — spanning the full card width. The
     details is the relative anchor; top-full puts the extension flush under
     the summary (border-b-0 while open, so the box continues seamlessly).
     While open, the in-flow action row keeps its SPACE invisibly: the card
     must not shrink either, or the Worked panel moves UP instead of down.
     The card's z-10 paints the whole card over the Worked panel (whose
     auto-open is fine to obscure); drawers (z-20) and the ambient host
     (z-40) still cover it. W2 pins the contract. -->
        <details
            class="relative mt-2 border border-line"
            class:rounded-md={!detailsOpen}
            class:rounded-t-md={detailsOpen}
            class:border-b-0={detailsOpen}
            bind:open={detailsOpen}
        >
            <summary class="cursor-pointer px-3 py-2 text-sm font-medium text-ink select-none">
                Contact details
            </summary>
            <div
                class="absolute top-full -inset-x-[calc(1.25rem+1px)] rounded-b-xl border-x border-b border-line bg-surface px-5 pb-5 shadow-sm"
            >
                <div class="space-y-3 rounded-b-md border border-line px-3 py-3">
                    <!-- Read-only, looked-up: QRZ page link + email. -->
                    <div class="flex flex-col gap-y-1 text-sm">
                        {#if qrzUrl !== null}
                            <a
                                href={qrzUrl}
                                target="_blank"
                                rel="noopener noreferrer"
                                class="inline-flex w-fit items-center gap-x-1 font-medium text-focus hover:underline"
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
                        {:else}
                            <span class="text-muted">Enter a callsign for the QRZ link.</span>
                        {/if}
                        <div>
                            <span class="text-muted">Email:</span>
                            <span class="text-ink">{enrichData?.email || '—'}</span>
                        </div>
                        <div class="flex gap-x-6">
                            <div>
                                <span class="text-muted">CQ Zone:</span>
                                <span class="tabular-nums text-ink"
                                    >{enrichData?.cqZone || '—'}</span
                                >
                            </div>
                            <div>
                                <span class="text-muted">ITU Zone:</span>
                                <span class="tabular-nums text-ink"
                                    >{enrichData?.ituZone || '—'}</span
                                >
                            </div>
                        </div>
                    </div>

                    <!-- Editable contacted-station fields. Gridsquare + QTH are
                     enrichment-filled (correctable); Rig + RX power share a row
                     (Rig fills; power stays narrow). -->
                    <div>
                        <label for="lc-grid" class="block text-sm font-medium text-ink"
                            >Gridsquare</label
                        >
                        <input
                            id="lc-grid"
                            class="input w-full uppercase"
                            class:input-error={gridInvalid}
                            autocomplete="off"
                            spellcheck="false"
                            placeholder="e.g. KH66"
                            bind:value={draft.gridsquare}
                            oninput={upperGrid}
                        />
                        {#if gridInvalid}
                            <p class="mt-1 text-xs text-invalid">Not a valid grid square</p>
                        {/if}
                    </div>
                    <div>
                        <label for="lc-qth" class="block text-sm font-medium text-ink">QTH</label>
                        <input
                            id="lc-qth"
                            class="input w-full"
                            autocomplete="off"
                            placeholder="City / town"
                            bind:value={draft.qth}
                        />
                    </div>
                    <div class="flex items-end gap-x-2">
                        <div class="flex-1">
                            <label for="lc-rig" class="block text-sm font-medium text-ink"
                                >Rig</label
                            >
                            <input
                                id="lc-rig"
                                class="input w-full"
                                autocomplete="off"
                                bind:value={draft.rig}
                            />
                        </div>
                        <div>
                            <label for="lc-rxpwr" class="block text-sm font-medium text-ink"
                                >RX Power (W)</label
                            >
                            <input
                                id="lc-rxpwr"
                                class="input w-24"
                                class:input-error={p.rxPwr}
                                inputmode="numeric"
                                maxlength="10"
                                autocomplete="off"
                                bind:value={draft.rxPwr}
                            />
                        </div>
                    </div>
                    <div>
                        <label for="lc-notes" class="block text-sm font-medium text-ink"
                            >Notes</label
                        >
                        <textarea
                            id="lc-notes"
                            class="input w-full resize-y"
                            rows="2"
                            autocomplete="off"
                            bind:value={draft.notes}
                        ></textarea>
                    </div>
                </div>
                {#if detailsOpen}
                    <div class="mt-4" data-action-row>{@render actionRow()}</div>
                {/if}
            </div>
        </details>
        <!-- ONE action row in exactly one ACCESSIBLE place: the expansion's
             bottom while open (the {#if} above), in flow otherwise. The
             in-flow wrapper below always renders its copy and hides it with
             visibility (not an {#if}): an emptied wrapper collapses, the card
             shrinks, and the Worked panel moves UP — the same niggle in the
             other direction. visibility:hidden keeps the space and takes the
             copy out of the focus order and accessibility tree. -->
        {#snippet actionRow()}
            <div class="flex justify-end gap-x-2">
                <button
                    class="btn"
                    title="Esc"
                    onclick={() => {
                        clearDraft();
                        callInput?.focus();
                    }}>Clear</button
                >
                <!-- CAT/rig gate (ADR 0044): 'lost' and 'unconfirmed' block
                     logging — the context may be stale or never asserted;
                     'live' and confirmed-'manual' log. Enforced in logDraft
                     too; this disabled state is the UX face of it.
                     busy = in-flight POST (double-log guard). -->
                <button
                    class="btn btn-primary"
                    onclick={() => logAndRefocus()}
                    disabled={!canLog() || !rigReady() || submitState.busy}
                    title={gateTitle ?? 'Ctrl+Enter'}
                    >{submitState.busy ? 'Logging…' : 'Log QSO'}</button
                >
            </div>
        {/snippet}
        <div class="mt-4" data-action-row class:invisible={detailsOpen}>
            {@render actionRow()}
        </div>
    </div>
</div>
