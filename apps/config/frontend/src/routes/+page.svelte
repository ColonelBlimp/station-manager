// noinspection
<script lang="ts">
    import ConfigItem from "$lib/ui/components/ConfigItem.svelte";
    import NumberInput from "$lib/ui/components/NumberInput.svelte";

    const updateDefaultRig = (value: string) => {
        console.log(`Updating default rig to ${value}`);
    }
    const updateDefaultFreq = (value: string) => {
        console.log(`Updating default frequency to ${value}`);
    }
</script>

{#snippet numberInput({ value, onChange }: { value: string, onChange: (v: string) => void })}
    <NumberInput {value} {onChange} />
{/snippet}

<div class="mx-8">
    <div class="py-4 border-b border-gray-300">
        <h3 class="font-bold text-2xl">General Settings</h3>
        <p class="my-1">These settings cannot be empty. They all have a <i>default value</i>, some of which <b>cannot</b> be modified here.</p>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem cannotUpdate={true} cannotUpdateReason="Update via logbook app" id="default-logbook" label="Default Logbook ID" value="1"/>
        </dl>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-rig" label="Default Rig ID" value="1" updateCallback={updateDefaultRig} inputSnippet={numberInput}/>
        </dl>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-freq" label="Default Frequency" value="14.600.000" updateCallback={updateDefaultFreq} inputSnippet={numberInput}/>
        </dl>
    </div>
</div>
