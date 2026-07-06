<script lang="ts">
    // Phone/CW logging card — the FAST PATH (pile-up / S&P): only the fields typed
    // on nearly every QSO (callsign, RST, time, name). Rarely-touched fields (QTH,
    // grid, notes) live in the Details card, a deliberate action — a rag-chew is
    // unhurried, so opening notes for it costs nothing, whereas a comment box on the
    // fast path taxes every quick QSO for the rare one.
    //
    // Reads/writes the shared QSO draft (qso.svelte); it does NOT submit directly
    // (logDraft() calls the injected sink) and makes no assumption about where it's
    // positioned (ADR 0045). Rig fields (freq/mode/band) belong to the Rig panel.
    import { onMount } from 'svelte';
    import { draft, canLog, logDraft, resetDraft, stampOn } from './qso.svelte';

    function upperCall(): void {
        draft.callsign = draft.callsign.toUpperCase();
    }

    // QSO start = when the card appears. Time off is stamped at log.
    onMount(stampOn);
</script>

<div class="card w-[var(--card-w)]">
    <div class="flex items-end gap-4">
        <div>
            <label for="lc-call" class="block text-sm font-medium text-ink">Callsign</label>
            <input
                id="lc-call"
                class="input w-32 uppercase"
                autocomplete="off"
                spellcheck="false"
                placeholder="Callsign"
                bind:value={draft.callsign}
                oninput={upperCall}
            />
        </div>
        <div>
            <label for="lc-rst-s" class="block text-sm font-medium text-ink">RST Sent</label>
            <input id="lc-rst-s" class="input w-13" bind:value={draft.rstSent} />
        </div>
        <div>
            <label for="lc-rst-r" class="block text-sm font-medium text-ink">RST Rcvd</label>
            <input id="lc-rst-r" class="input w-13" bind:value={draft.rstRcvd} />
        </div>
    </div>

    <div class="mt-4 flex items-end gap-3">
        <div>
            <label for="lc-date-on" class="block text-sm font-medium text-ink">Date On</label>
            <input id="lc-date-on" class="input w-32" placeholder="YYYY-MM-DD" bind:value={draft.dateOn} />
        </div>
        <div>
            <label for="lc-time-on" class="block text-sm font-medium text-ink">Time On</label>
            <input id="lc-time-on" class="input w-24" placeholder="HH:MM:SS" bind:value={draft.timeOn} />
        </div>
    </div>

    <div class="mt-3 flex items-end gap-3">
        <div>
            <label for="lc-date-off" class="block text-sm font-medium text-ink">Date Off</label>
            <input id="lc-date-off" class="input w-32" placeholder="YYYY-MM-DD" bind:value={draft.dateOff} />
        </div>
        <div>
            <label for="lc-time-off" class="block text-sm font-medium text-ink">Time Off</label>
            <input id="lc-time-off" class="input w-24" placeholder="HH:MM:SS" bind:value={draft.timeOff} />
        </div>
    </div>

    <div class="mt-4">
        <label for="lc-name" class="block text-sm font-medium text-ink">Name</label>
        <input id="lc-name" class="input" autocomplete="off" bind:value={draft.name} />
    </div>

    <div class="mt-5 flex justify-end gap-2">
        <button class="btn" onclick={resetDraft}>Clear</button>
        <button class="btn btn-primary" onclick={logDraft} disabled={!canLog()}>Log QSO</button>
    </div>
</div>
