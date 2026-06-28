<script lang="ts">
    import LogbookView from './lib/ui/LogbookView.svelte';
    import ManualLink from './lib/ui/ManualLink.svelte';
</script>

<!--
    Logbook SPA. The logbook client owns logbook-browse + (future) QSL-awaiting,
    edit-history, and management surfaces — distinct from the live-logging client at
    the root. First surface (2026-06-26): the LogbookView — pick a logbook, page
    through its QSOs (read-only). See docs/v2-design/frontend-spa.md.
-->
<ManualLink />
<LogbookView />
