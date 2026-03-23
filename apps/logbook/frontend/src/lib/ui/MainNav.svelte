<script lang="ts">
    import {page} from "$app/state";

    const baseLink = "px-3 py-1.5 rounded-md text-sm font-medium text-white";
    const selectedCss = "bg-indigo-700";
    const hoverCss = "hover:bg-white/10";

    let currentPath: string = $derived.by(() => page.url.pathname);

    const linkClass = (p: string): string =>
        `${baseLink} ${currentPath === p ? selectedCss : hoverCss}`;
</script>
<!-- eslint-disable svelte/no-navigation-without-resolve -->
<nav class="flex items-center bg-indigo-600 text-white h-12.5 space-x-4 px-4">
    <a href="/" class={linkClass("/")} title="Current Log Book">Log Book</a>
    <a href="/manager" class={linkClass("/manaager")} title="Logbook Manager">Manager</a>
</nav>
