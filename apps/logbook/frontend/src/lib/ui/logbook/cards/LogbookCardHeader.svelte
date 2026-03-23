<script lang="ts">
import {configState} from "$lib/states/config-state.svelte";
</script>

<header class="flex items-center h-12.5 pl-4 border-b border-b-gray-300">
    <h2 class="font-semibold">Log book: {configState.logbook.name}</h2>
</header>
