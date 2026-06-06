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
<div class="flex items-center justify-center min-h-96 text-gray-500">
    <div class="text-center border w-full">
        <h2 class="text-lg font-semibold">FT8</h2>
        <p class="mt-1 text-sm">Live decode view coming soon.</p>
    </div>
    <div class="flex flex-col w-40">
        <Button label="160m" />
    </div>
</div>
