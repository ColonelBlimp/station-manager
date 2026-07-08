<script lang="ts">
    // Phone/CW logging card — the fast-path entry fields (callsign, RST, time,
    // name) plus a comment line. Rarely touched lookups (QTH, grid) live in the
    // Details card; the grid is enrichment-filled into the draft, not typed here.
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
    } from './qso.svelte';
    import DuplicateDialog from './DuplicateDialog.svelte';
    import { observeWorked, openWorkedForQso } from './worked.svelte';
    import { rigReady, rigGate } from './rig.svelte';
    import { operate, closeContact, registerCallsignInput } from './state.svelte';
    import EnrichmentCard from './EnrichmentCard.svelte';

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
        if (await logDraft()) callInput?.focus();
    }

    function windowKeydown(e: KeyboardEvent): void {
        // Contact overlay open: it owns the keys. Esc closes the OVERLAY (never
        // clears the draft underneath); log/clear shortcuts are inert so a
        // key-repeat can't act on the card behind it.
        if (operate.contactOpen) {
            if (e.key === 'Escape') {
                e.preventDefault();
                closeContact();
                callInput?.focus();
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
        if (e.key === 'Enter' && e.ctrlKey && !e.altKey && !e.shiftKey) {
            e.preventDefault();
            void logAndRefocus();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            clearDraft();
            callInput?.focus();
        }
    }

    // Tab out of the callsign field = "I'm working this station": stamps
    // Date/Time On and starts the ticking Time Off (the QSO timer), and surfaces
    // the worked-before panel if nothing is open. Tab is not swallowed — focus
    // moves on to RST as normal.
    function callKeydown(e: KeyboardEvent): void {
        if (e.key === 'Tab' && !e.shiftKey && draft.callsign.trim() !== '') {
            startQso();
            openWorkedForQso(draft.callsign);
        }
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

<div class="card w-(--card-w)">
    <div class="flex gap-x-6">
        <!-- Left: fast-path entry fields -->
        <div class="flex flex-col">
            <div class="flex items-end gap-x-2">
                <div>
                    <label for="lc-call" class="block text-sm font-medium text-ink">Callsign</label>
                    <input
                        id="lc-call"
                        class="input w-32 uppercase"
                        class:input-error={p.callsign}
                        autocomplete="off"
                        spellcheck="false"
                        placeholder="Callsign"
                        bind:this={callInput}
                        bind:value={draft.callsign}
                        oninput={upperCall}
                        onkeydown={callKeydown}
                    />
                </div>
                <div>
                    <label for="lc-rst-s" class="block text-sm font-medium text-ink">RST Sent</label
                    >
                    <input
                        id="lc-rst-s"
                        class="input w-15"
                        class:input-error={p.rstSent}
                        bind:value={draft.rstSent}
                    />
                </div>
                <div>
                    <label for="lc-rst-r" class="block text-sm font-medium text-ink">RST Rcvd</label
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

            <div class="mt-2">
                <label for="lc-name" class="block text-sm font-medium text-ink">Name</label>
                <input id="lc-name" class="input w-60" autocomplete="off" bind:value={draft.name} />
            </div>

            <div class="mt-2">
                <label for="lc-comment" class="block text-sm font-medium text-ink">Comment</label>
                <input
                    id="lc-comment"
                    class="input w-60"
                    autocomplete="off"
                    bind:value={draft.comment}
                />
            </div>
        </div>

        <!-- Right: always-on enrichment (relocatable — this square is just its first host) -->
        <div class="flex flex-col">
            <div class="flex w-56 h-45 shrink-0 mt-0">
                <EnrichmentCard />
            </div>
            <div class="relative mt-auto flex justify-end gap-x-2">
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
        </div>
    </div>
</div>
