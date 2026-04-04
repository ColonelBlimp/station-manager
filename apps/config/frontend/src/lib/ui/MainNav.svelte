<script lang="ts">
    import {page} from "$app/state";

    const baseLink = "px-3 py-1 rounded-md text-base font-medium text-white";
    const selectedCss = "bg-indigo-700";
    const hoverCss = "hover:bg-white/10";

    let currentPath: string = $derived.by(() => page.url.pathname);

    const linkClass = (p: string): string =>
        `${baseLink} ${currentPath === p ? selectedCss : hoverCss}`;
</script>
<!-- eslint-disable svelte/no-navigation-without-resolve -->
<nav class="flex items-center bg-indigo-600 text-white h-12.5 space-x-4 px-4">
    <a href="/" class={linkClass("/")} title="General">General</a>
    <a href="/station" class={linkClass("/station")} title="Station">Station</a>
    <a href="/lookup" class={linkClass("/lookup")} title="Lookup">Lookup</a>
    <a href="/forwarders" class={linkClass("/forwarders")} title="Forwarders">Forwarders</a>
    <a href="/listeners" class={linkClass("/listeners")} title="Listeners">Listeners</a>
    <a href="/email" class={linkClass("/email")} title="Email">Email</a>
    <a href="/rigs" class={linkClass("/rigs")} title="Rigs">Rigs</a>
    <!-- a href="/manager" class={linkClass("/manager")} title="Logbook Manager">Manager</a -->
</nav>
