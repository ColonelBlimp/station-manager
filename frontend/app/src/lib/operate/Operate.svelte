<script lang="ts">
    // Operate surface. Renders the active sub-mode (Phone/CW ↔ FT8, from the
    // router). This pass: the Phone/CW entry card; FT8 is a placeholder. The right
    // util rail + CAT gate land in later increments.
    import { router } from '../router.svelte';

    let callsign = $state('');
    let rstSent = $state('59');
    let rstRcvd = $state('59');

    const canLog = $derived(callsign.trim() !== '');

    function logQso(): void {
        // Stub — assembled + submitted to the daemon in a later pass.
    }
</script>

{#if router.mode === 'phone'}
    <div class="mx-auto w-fit">
        <div class="card w-[42rem]">
            <div class="flex items-end gap-4">
                <div>
                    <label for="callsign" class="block text-sm font-medium text-ink">Callsign</label>
                    <input
                        id="callsign"
                        class="input w-32 uppercase"
                        autocomplete="off"
                        spellcheck="false"
                        placeholder="Callsign"
                        bind:value={callsign}
                    />
                </div>
                <div>
                    <label for="rst-sent" class="block text-sm font-medium text-ink">RST Sent</label>
                    <input id="rst-sent" class="input w-12" bind:value={rstSent} />
                </div>
                <div>
                    <label for="rst-rcvd" class="block text-sm font-medium text-ink">RST Rcvd</label>
                    <input id="rst-rcvd" class="input w-12" bind:value={rstRcvd} />
                </div>
            </div>

            <!-- Rest of the entry form lands here (name, QTH, exchange, comment…). -->
            <div class="mt-4 h-20 rounded-lg border border-dashed border-line"></div>

            <div class="mt-4 flex justify-end">
                <button class="btn btn-primary" onclick={logQso} disabled={!canLog}>Log QSO</button>
            </div>
        </div>
    </div>
{:else}
    <div class="mx-auto max-w-3xl">
        <div class="card">
            <h2 class="text-sm font-semibold text-ink">FT8</h2>
            <p class="mt-1 text-sm text-muted">
                Band activity, occupancy / offset picker, and the sequencer ladder — built in a later
                pass.
            </p>
        </div>
    </div>
{/if}
