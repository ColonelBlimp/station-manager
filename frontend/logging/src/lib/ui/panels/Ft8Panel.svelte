<script lang="ts">
import Button from "../components/Button.svelte";
</script>

<!--
    FT8 operating-mode holding page. Per the cards-vs-panels convention this is
    a content panel inside LoggingCard, shown when the header's Operating Mode
    switch is set to "FT8" (Phone/CW renders QsoPanel + CountryPanel + InfoPanel
    instead). Deliberately an empty scaffold for now — the eventual surface is
    the live decode list (per-slot freq / DT / text) plus the decode→QSO
    workflow (ADR 0024; see docs/session-handoff "FT8 SPA surface not built").
-->
<div class="flex justify-center h-115 text-gray-500 mt-4 space-x-2">
    <div class="text-center border w-101">
        <h2 class="text-lg font-semibold">FT8</h2>
        <p class="mt-1 text-sm">Live decode view coming soon.</p>
    </div>
    <div class="text-center border w-101">
        Other
    </div>
    <div class="flex flex-col w-40 h-115 place-items-center px-4 space-y-2">
        <Button id="160m" label="160m" />
        <Button id="80m" label="80m" />
        <Button id="60m" label="60m" />
        <Button id="40m" label="40m" />
        <Button id="30m" label="30m" />
        <Button id="20m" label="20m" />
        <Button id="18m" label="18m" />
        <Button id="15m" label="15m" />
        <Button id="12m" label="12m" />
        <Button id="10m" label="10m" />
        <Button id="6m" label="6m" />
    </div>
</div>
