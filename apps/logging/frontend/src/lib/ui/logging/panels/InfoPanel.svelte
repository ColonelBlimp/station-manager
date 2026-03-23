<script lang="ts">
    import InfoPanelHeader from "$lib/ui/logging/panels/InfoPanelHeader.svelte";
    import {DETAILS_TAB_TITLE, STATION_PANEL, WORKED_TAB_TITLE} from "$lib/ui/logging/panels/constants";
    import WorkedPanel from "$lib/ui/logging/panels/WorkedPanel.svelte";
    import DetailsPanel from "$lib/ui/logging/panels/DetailsPanel.svelte";
    import StationPanel from "$lib/ui/logging/panels/StationPanel.svelte";
    import SessionPanel from "$lib/ui/logging/panels/SessionPanel.svelte";
    import {appState} from "$lib/states/app-state.svelte";
</script>

<div class="flex flex-col h-92.5">
    <InfoPanelHeader/>
    {#if appState.activePanel === WORKED_TAB_TITLE}
        <WorkedPanel/>
    {:else if appState.activePanel === DETAILS_TAB_TITLE}
        <DetailsPanel/>
    {:else if appState.activePanel === STATION_PANEL}
        <StationPanel/>
    {:else}
        <SessionPanel/>
    {/if}
</div>
