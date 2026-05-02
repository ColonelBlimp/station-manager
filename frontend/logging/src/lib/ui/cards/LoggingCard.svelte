<script lang="ts">
    import QsoPanel from "../panels/QsoPanel.svelte";
    import CountryPanel from "../panels/CountryPanel.svelte";
</script>

<!--
    h-13.5 / w-72.5 are one-off shell dimensions — they exist only
    here and don't anchor a relationship that warrants a design token.
    If a second header strip appears with the same dimensions, lift
    these to tokens at that point.
-->
<header class="flex items-center h-13.5 pl-4 border-b border-b-gray-200">
    <div class="flex flex-row items-center w-72.5">
        <h1 class="text-xl font-bold tracking-tight">Logging Mode:</h1>
    </div>
</header>
<div class="flex flex-row">
    <QsoPanel/>
    <CountryPanel/>
</div>

