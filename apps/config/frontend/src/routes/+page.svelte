<script lang="ts">
    import Updatable from "$lib/ui/components/Updatable.svelte";

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
    <div>
        <h3>General Settings</h3>
        <p>These settings cannot be empty. The all have a <i>default value</i>.</p>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <Updatable id="default-logbook" label="Default Logbook ID" value="1" onClickUpdate={updateDefaultLogbook}/>
        </dl>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <Updatable id="default-rig" label="Default Rig ID" value="1" onClickUpdate={updateDefaultFreq}/>
        </dl>
    </div>
    <div class="mt-6 border-t border-gray-100 dark:border-white/10">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <Updatable id="default-freq" label="Default Frequency" value="14.600.000" onClickUpdate={updateDefaultRig}/>
        </dl>
    </div>
</div>
