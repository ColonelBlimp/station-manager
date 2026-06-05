<script lang="ts">
    import QsoPanel from './QsoPanel.svelte';
    import CountryPanel from './CountryPanel.svelte';
    import InfoPanel from './InfoPanel.svelte';
</script>

<!--
    Phone/CW operating-mode body for LoggingCard — the manual-entry logging
    layout (QsoPanel + CountryPanel on the top row, InfoPanel below). Symmetric
    with Ft8Panel: LoggingCard owns the header + Operating Mode switch and
    dispatches to one of the two mode panels, keeping the card a thin shell
    (cards-vs-panels: card = shell, panel = content).
-->
<div class="flex">
    <QsoPanel />
    <CountryPanel />
</div>
<div class="flex">
    <InfoPanel />
</div>
