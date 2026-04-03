<script lang="ts">
    import ConfigItem from "$lib/ui/components/ConfigItem.svelte";

    const updateDefaultLogbook = (value: string) => {
        console.log(`Updating default logbook to ${value}`);
    }
    const updateDefaultRig = (value: string) => {
        console.log(`Updating default rig to ${value}`);
    }
    const updateDefaultFreq = (value: string) => {
        console.log(`Updating default frequency to ${value}`);
    }
</script>

<div class="mx-8">
    <div class="py-4 border-b border-gray-300">
        <h3 class="font-bold text-2xl">General Settings</h3>
        <p class="my-1">These settings cannot be empty. They all have a <i>default value</i>, some of which <b>cannot</b> be modified here.</p>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem canUpdate={false} cannotUpdateReason="Update via logbook app" id="default-logbook" label="Default Logbook ID" value="1" onClickUpdate={updateDefaultLogbook}/>
        </dl>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-rig" label="Default Rig ID" value="1" onClickUpdate={updateDefaultFreq}/>
        </dl>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="default-freq" label="Default Frequency" value="14.600.000" onClickUpdate={updateDefaultRig}/>
        </dl>
    </div>
</div>
