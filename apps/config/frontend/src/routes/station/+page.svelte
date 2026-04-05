<script lang="ts">
    import {configState} from "$lib/states/config-state.svelte";
    import ConfigItem from "$lib/ui/components/ConfigItem.svelte";
    import TextInput from "$lib/ui/components/TextInput.svelte";

    const updateMyAltitude = (value: string) => {

    }
    const updateMyAntenna = (value: string) => {

    }
    const updateMyCity = (value: string) => {

    }
    const updateMyCountry = (value: string) => {

    }
</script>

{#snippet textInput({ value, onChange, ref }: { value: string, onChange: (v: string) => void, ref: (el: HTMLElement) => void })}
    <TextInput {value} {onChange} ref={ref} />
{/snippet}

<div class="mx-8">
    <div class="py-4 border-b border-gray-300">
        <h3 class="font-bold text-2xl">Station Settings</h3>
        <p class="my-1">Station settings are values used by the ADIF specification for the parameters related to the logging station.
        These typically follow the syntax <i>MY_[parameter]</i>.
        </p>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="my-altitude" label="Altitude" value={configState.cfg.logging_station.my_altitude} updateCallback={updateMyAltitude} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="my-antenna" label="Antenna" value={configState.cfg.logging_station.my_antenna} updateCallback={updateMyAntenna} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="my-city" label="My City" value={configState.cfg.logging_station.my_city} updateCallback={updateMyCity} inputSnippet={textInput}/>
        </dl>
    </div>
    <div class="mt-4">
        <dl class="divide-y divide-gray-100 dark:divide-white/10">
            <ConfigItem id="my-country" label="My Country" value={configState.cfg.logging_station.my_country} updateCallback={updateMyCountry} inputSnippet={textInput}/>
        </dl>
    </div>
</div>
