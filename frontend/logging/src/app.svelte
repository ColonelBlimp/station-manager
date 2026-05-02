<script lang="ts">
    import LoggingCard from "./lib/ui/cards/LoggingCard.svelte";
</script>

<!--
    h-120 / w-200 are the whole-app shell dimensions — single use,
    no design-token relationship. Adjust here when the operator decides
    they want a different overall card size; nothing else depends on
    these values.
-->
<main class="rounded-xl border border-gray-200 h-120 w-240 mx-auto mt-10">
    <LoggingCard/>
</main>
