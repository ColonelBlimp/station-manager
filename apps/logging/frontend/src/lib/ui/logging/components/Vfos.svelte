<script lang="ts">
    import VfoBox from "$lib/ui/logging/components/VfoBox.svelte";
    import VfoInput from "$lib/ui/logging/components/VfoInput.svelte";
    import {catState, isCatEnabled} from "$lib/states/cat-state.svelte";
    import {qsoState} from "$lib/states/new-qso-state.svelte";

    let isSplit = $derived(catState.split === 'ON');

</script>

<div class="flex flex-col w-62.5 h-20 mt-6 gap-y-2">
    {#if catState.select === 'VFO-A' || catState.select === ''}
        <div class="flex flex-row items-center gap-x-2">
            {#if isSplit}
                <VfoBox label='VFO-A' isSplit bgColorTopCss='bg-green-600/80' bgColorBottomCss='bg-blue-700/90'/>
                <VfoInput
                        id='vfoa'
                        bind:value={qsoState.cat_vfoa_freq}
                        band={qsoState.cat_vfoa_freq}
                        disabled={isCatEnabled.isEnabled}
                />
            {:else}
                <VfoBox label='VFO-A'/>
                <VfoInput id='vfoa'
                          bind:value={qsoState.cat_vfoa_freq}
                          disabled={isCatEnabled.isEnabled}
                          band={qsoState.cat_vfoa_freq}/>
            {/if}
        </div>
        <div class="flex flex-row items-center gap-x-2">
            {#if isSplit}
                <VfoBox label='VFO-B' action='TX' isSplit bgColorTopCss='bg-red-800/80'
                        bgColorBottomCss='bg-blue-700/90'/>
                <VfoInput id='vfob'
                          bind:value={qsoState.cat_vfob_freq}
                          disabled={isCatEnabled.isEnabled}
                          band={qsoState.cat_vfob_freq}/>
            {:else}
                <VfoBox label='VFO-B' bgColorCss='bg-gray-500/80'/>
                <VfoInput id='vfob'
                          bind:value={qsoState.cat_vfob_freq}
                          disabled={isCatEnabled.isEnabled}
                          band={qsoState.cat_vfob_freq}/>
            {/if}
        </div>
    {:else}
        <div class="flex flex-row items-center gap-x-2">
            {#if isSplit}
                <VfoBox label='VFO-B' isSplit bgColorTopCss='bg-green-600/80'
                        bgColorBottomCss='bg-blue-700/90'/>
                <VfoInput id='vfob'
                          bind:value={qsoState.cat_vfob_freq}
                          disabled={isCatEnabled.isEnabled}
                          band={qsoState.cat_vfob_freq}/>
            {:else}
                <VfoBox label='VFO-B'/>
                <VfoInput id='vfob'
                          bind:value={qsoState.cat_vfob_freq}
                          band={qsoState.cat_vfob_freq}/>
            {/if}
        </div>
        <div class="flex flex-row items-center gap-x-2">
            {#if isSplit}
                <VfoBox label='VFO-A' isSplit bgColorTopCss='bg-red-800/80' bgColorBottomCss='bg-blue-700/90'/>
                <VfoInput id='vfoa'
                          bind:value={qsoState.cat_vfoa_freq}
                          disabled={isCatEnabled.isEnabled}
                          band={qsoState.cat_vfoa_freq}/>
            {:else}
                <VfoBox label='VFO-A' bgColorCss='bg-gray-500/80'/>
                <VfoInput id='vfoa'
                          bind:value={qsoState.cat_vfoa_freq}
                          disabled={isCatEnabled.isEnabled}
                          band={qsoState.cat_vfoa_freq}/>
            {/if}
        </div>
    {/if}
</div>
