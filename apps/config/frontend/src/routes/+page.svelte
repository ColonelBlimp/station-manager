<script lang="ts">
    import ConfigItem from "$lib/ui/components/ConfigItem.svelte";
    import NumberInput from "$lib/ui/components/NumberInput.svelte";
    import Textinput from "$lib/ui/components/Textinput.svelte";
    import ModeInput from "$lib/ui/components/ModeInput.svelte";

    const updateDefaultRig = (value: string) => {
        console.log(`Updating default rig to ${value}`);
    }
    const updateDefaultFreq = (value: string) => {
        console.log(`Updating default frequency to ${value}`);
    }
    const updateDefaultMode = (value: string) => {
        console.log(`Updating default mode to ${value}`);
    }
</script>

{#snippet numberInput({ value, onChange, ref }: { value: string, onChange: (v: string) => void, ref: (el: HTMLElement) => void })}
    <NumberInput {value} {onChange} ref={ref} />
{/snippet}
{#snippet textInput({ value, onChange, ref }: { value: string, onChange: (v: string) => void, ref: (el: HTMLElement) => void })}
    <Textinput {value} {onChange} ref={ref} />
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
            <ConfigItem cannotUpdate={true} cannotUpdateReason="Modify via logbook app" id="default-logbook" label="Default Logbook ID" value="1"/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-rig" label="Default Rig ID" value="1" updateCallback={updateDefaultRig} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-freq" label="Default Frequency" value="14.600.000" updateCallback={updateDefaultFreq} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-mode" label="Default Mode" value="SSB" updateCallback={updateDefaultMode} inputSnippet={modeInput}/>
        </dl>
    </div>
</div>
