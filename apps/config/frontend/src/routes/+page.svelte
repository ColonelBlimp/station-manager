<script lang="ts">
    import ConfigItem from "$lib/ui/components/ConfigItem.svelte";
    import NumberInput from "$lib/ui/components/NumberInput.svelte";
    import TextInput from "$lib/ui/components/TextInput.svelte";
    import ModeInput from "$lib/ui/components/ModeInput.svelte";
    import {configState} from "$lib/states/config-state.svelte";

    const updateDefaultRig = (value: string) => {
        console.log(`Updating default rig to ${value}`);
    }
    const updateDefaultFreq = (value: string) => {
        console.log(`Updating default frequency to ${value}`);
    }
    const updateDefaultMode = (value: string) => {
        console.log(`Updating default mode to ${value}`);
    }
    const updatePowerMultiplier = (value: string) => {
        console.log(`Updating power multiplier to ${value}`);
    }
    const updateTxPower = (value: string) => {
        console.log(`Updating tx power to ${value}`);
    }
</script>

{#snippet textInput({ value, onChange, ref }: { value: string, onChange: (v: string) => void, ref: (el: HTMLElement) => void })}
    <TextInput {value} {onChange} ref={ref} />
{/snippet}
{#snippet modeInput({ value, onChange, ref }: { value: string, onChange: (v: string) => void, ref: (el: HTMLElement) => void })}
    <ModeInput {value} {onChange} ref={ref} />
{/snippet}

<div class="mx-8">
    <div class="py-4 border-b border-gray-300">
        <h3 class="font-bold text-2xl">General Settings</h3>
        <p class="my-1">General settings all have <i>default value</i> and cannot be empty. Some values <b>cannot</b> be modified here.</p>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem cannotUpdate={true} cannotUpdateReason="Modify via logbook app" id="default-logbook" label="Default Logbook ID" value={configState.cfg.required_configs.default_logbook_id.toString()}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-rig" label="Default Rig ID" value={configState.cfg.required_configs.default_rig_id.toString()} updateCallback={updateDefaultRig} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-freq" label="Default Frequency" value={configState.cfg.required_configs.default_freq} updateCallback={updateDefaultFreq} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-mode" label="Default Mode" value={configState.cfg.required_configs.default_mode} updateCallback={updateDefaultMode} inputSnippet={modeInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="pwr-multiplier" label="Power Multiplier" value={configState.cfg.required_configs.power_multiplier.toString()} updateCallback={updatePowerMultiplier} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="tx-pwr" label="Default TX Power" value={configState.cfg.required_configs.default_tx_power.toString()} updateCallback={updateTxPower} inputSnippet={textInput}/>
        </dl>
    </div>
</div>
