<script lang="ts">
    import QsoPanel from "../panels/QsoPanel.svelte";
    import CountryPanel from "../panels/CountryPanel.svelte";
    import SessionTimer from "../components/SessionTimer.svelte";
</script>

<!--
    h-13.5 / w-72.5 are one-off shell dimensions — they exist only
    here and don't anchor a relationship that warrants a design token.
    If a second header strip appears with the same dimensions, lift
    these to tokens at that point.
-->
<header class="flex items-center h-13.5 px-4 border-b border-b-line-soft">
    <div class="flex flex-row items-center w-72.5">
        <h1 class="text-xl font-bold tracking-tight">Logging Mode:</h1>
    </div>
    <div class="flex w-40">

    </div>
    <div class="flex w-40">

    </div>
    <div class="flex w-full">

    </div>
    <div class="flex text-sm font-semibold w-48">
        <div class="w-24">Session Time:</div>
        <div class="text-right w-17Wiat"><SessionTimer/></div>
    </div>
</header>
<div class="flex flex-row">
    <QsoPanel/>
    <CountryPanel/>
</div>
