<script lang="ts">
    import ConfigItem from "$lib/ui/components/ConfigItem.svelte";
    import Textinput from "$lib/ui/components/Textinput.svelte";
    import {types} from "$lib/wailsjs/go/models";
    import {configState} from "$lib/states/config-state.svelte";

    const rigs = $derived(configState.cfg.rig_configs ?? []);
    let selectedRigId = $state(configState.cfg.required_configs?.default_rig_id ?? 0);
    const selectedRig = $derived<types.RigConfig | null>(rigs.find(r => r.ID === selectedRigId) ?? null);
</script>

{#snippet textInput({ value, onChange, ref }: { value: string, onChange: (v: string) => void, ref: (el: HTMLElement) => void })}
    <Textinput {value} {onChange} ref={ref} />
{/snippet}

<div class="flex flex-row h-full divide-x divide-gray-300 dark:divide-white/10">
    <!-- Left column: rig list -->
    <div class="flex flex-col w-48 shrink-0 overflow-y-auto">
        <ul>
            {#each rigs as rig (rig.ID)}
                <li>
                    <button
                        type="button"
                        onclick={() => selectedRigId = rig.ID}
                        class="w-full text-left py-2 px-4 cursor-pointer text-sm transition-colors
                            {selectedRigId === rig.ID
                                ? 'bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 font-semibold'
                                : 'hover:bg-gray-100 dark:hover:bg-white/5'}"
                    >
                        <span class="block font-medium">{rig.Name}</span>
                        <span class="block text-xs text-gray-500">{rig.Model}</span>
                    </button>
                </li>
            {/each}
        </ul>
    </div>

    <!-- Right column: rig config -->
    <div class="flex flex-col flex-1 overflow-y-auto px-8">
        {#if selectedRig}
            <div class="py-4 border-b border-gray-300 dark:border-white/10">
                <h3 class="font-bold text-2xl">{selectedRig.Name}</h3>
                <p class="my-1 text-sm text-gray-500">{selectedRig.Model}</p>
            </div>
            <div class="mt-4">
                <dl class="divide-y divide-gray-100 dark:divide-white/10">
                    <ConfigItem cannotUpdate={true} id="rig-id" label="ID" value={selectedRig.ID.toString()} />
                    <ConfigItem id="rig-name" label="Name" value={selectedRig.Name} inputSnippet={textInput} updateCallback={(v) => console.log('name', v)} />
                    <ConfigItem id="rig-model" label="Model" value={selectedRig.Model} inputSnippet={textInput} updateCallback={(v) => console.log('model', v)} />
                    <ConfigItem id="rig-terminator" label="Terminator" value={selectedRig.Terminator ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('terminator', v)} />
                </dl>
            </div>
            {#if selectedRig.SerialConfig}
                <div class="mt-6">
                    <h4 class="font-semibold text-sm text-gray-500 uppercase tracking-wide mb-2">Serial</h4>
                    <dl class="divide-y divide-gray-100 dark:divide-white/10">
                        <ConfigItem id="serial-port" label="Port" value={selectedRig.SerialConfig.PortName ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('port', v)} />
                        <ConfigItem id="serial-baud" label="Baud Rate" value={selectedRig.SerialConfig.BaudRate?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('baud', v)} />
                        <ConfigItem id="serial-databits" label="Data Bits" value={selectedRig.SerialConfig.DataBits?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('databits', v)} />
                        <ConfigItem id="serial-stopbits" label="Stop Bits" value={selectedRig.SerialConfig.StopBits?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('stopbits', v)} />
                        <ConfigItem id="serial-rts" label="RTS" value={selectedRig.SerialConfig.RTS?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('rts', v)} />
                        <ConfigItem id="serial-dtr" label="DTR" value={selectedRig.SerialConfig.DTR?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('dtr', v)} />
                    </dl>
                </div>
            {/if}
            {#if selectedRig.CatConfig}
                <div class="mt-6">
                    <h4 class="font-semibold text-sm text-gray-500 uppercase tracking-wide mb-2">CAT</h4>
                    <dl class="divide-y divide-gray-100 dark:divide-white/10">
                        <ConfigItem id="cat-enabled" label="Enabled" value={selectedRig.CatConfig.Enabled?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('cat-enabled', v)} />
                        <ConfigItem id="cat-rate-limit" label="Rate Limiter Interval (ms)" value={selectedRig.CatConfig.ListenerRateLimiterIntervalMS?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('cat-rate', v)} />
                        <ConfigItem id="cat-read-timeout" label="Read Timeout (ms)" value={selectedRig.CatConfig.ListenerReadTimeoutMS?.toString() ?? ''} inputSnippet={textInput} updateCallback={(v) => console.log('cat-timeout', v)} />
                    </dl>
                </div>
            {/if}
        {:else}
            <p class="py-4 text-sm text-gray-400">Select a rig to view its configuration.</p>
        {/if}
    </div>
</div>
