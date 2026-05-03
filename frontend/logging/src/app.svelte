<script lang="ts">
    import LoggingCard from "./lib/ui/cards/LoggingCard.svelte";
    import Toasts from "./lib/ui/Toasts.svelte";
</script>

<!--
    h-120 / w-200 are the whole-app shell dimensions — single use,
    no design-token relationship. Adjust here when the operator decides
    they want a different overall card size; nothing else depends on
    these values.

    <Toasts/> is the single mount point for the notification queue per
    ADR 0008. Sits as a sibling of <main> because it's a fixed-position
    overlay; nesting it inside <main> would constrain its z-index stack
    and inherit unrelated layout rules.
-->
<main class="rounded-xl border border-line-soft h-120 w-fit mx-auto mt-12">
    <LoggingCard/>
</main>
<Toasts/>
