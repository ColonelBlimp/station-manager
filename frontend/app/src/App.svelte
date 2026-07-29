<script lang="ts">
    import Sidebar from './lib/ui/Sidebar.svelte';
    import Header from './lib/ui/Header.svelte';
    import TxAlarmBanner from './lib/ui/TxAlarmBanner.svelte';
    import DriveAlarmBanner from './lib/ui/DriveAlarmBanner.svelte';
    import Toasts from './lib/ui/Toasts.svelte';
    import SetupCard from './lib/ui/SetupCard.svelte';
    import { setup } from './lib/setup.svelte';
    import Operate from './lib/operate/Operate.svelte';
    import Logbook from './lib/logbook/Logbook.svelte';
    import Settings from './lib/config/Settings.svelte';
    import {
        ui,
        THEME_KEY,
        NAV_KEY,
        UTIL_KEY,
        effectiveNav,
        effectiveUtil,
    } from './lib/ui/state.svelte';
    import { router, type View } from './lib/router.svelte';
    import { operate } from './lib/operate/state.svelte';
    import { storageSet } from './lib/utils/storage';

    // Reflect shell state onto <html> (drives the token swap + collapse rules)
    // and persist it. Initial attributes are set pre-mount in index.html, so
    // these run in sync on the very first pass — no flash.
    $effect(() => {
        document.documentElement.dataset.theme = ui.theme;
        storageSet(THEME_KEY, ui.theme);
    });
    // Apply the EFFECTIVE collapse (preference or forced-by-width) to <html>, but
    // persist only the operator's PREFERENCE so widening restores it.
    $effect(() => {
        document.documentElement.dataset.nav = effectiveNav();
        storageSet(NAV_KEY, ui.navMode);
    });
    $effect(() => {
        document.documentElement.dataset.util = effectiveUtil();
        storageSet(UTIL_KEY, ui.utilMode);
    });
    // The right util rail (and its content offset / pile-up push) exists in
    // Operate — both Phone/CW and FT8. Reflected onto <html> so the CSS gates on it.
    $effect(() => {
        const railOn = router.view === 'operate';
        document.documentElement.dataset.rail = railOn ? 'on' : '';
        document.documentElement.dataset.drawer = railOn && operate.pileup ? 'open' : '';
    });

    const titles: Record<View, string> = {
        dashboard: 'Dashboard',
        operate: 'Operate',
        logbook: 'Logbook',
        config: 'Settings',
        map: 'Contacts Map',
    };
</script>

<!-- First-run gate: every logbook-backed surface (map tab included) 404s
     until setup seeds the default logbook, so the gate sits above the router.
     'loading' renders nothing — the boot /v1/config fetch settles in tens of
     ms on localhost, and holding blank beats flashing the shell at a fresh
     install. A daemon outage resolves to 'complete' (fail-soft shell), never
     to a false first-run greeting. -->
{#if setup.status === 'loading'}
    <Toasts />
{:else if setup.status === 'needed' || setup.justCompleted}
    <SetupCard />
    <Toasts />
{:else if router.view === 'map'}
    <!-- Full-window, no shell chrome: the map opens in its OWN tab (second
         monitor) from the Session tile, so sidebar/header would be dead
         weight here. Lazy import = its own chunk (ADR 0044 code-splitting):
         the bundled basemap + d3-geo never load unless the map is opened. -->
    {#await import('./lib/map/MapView.svelte') then mapModule}
        <mapModule.default />
    {/await}
{:else}
    <Sidebar />

    <!-- System messages (info/warn/error) — single mount, fixed overlay, never
         reflows the working surface. Pushed via lib/ui/toasts.svelte.ts. -->
    <Toasts />

    <div class="content-wrap flex h-screen flex-col pl-[var(--sidebar-w)]">
        <Header />
        <TxAlarmBanner />
        <DriveAlarmBanner />
        <!-- main is the horizontal (and vertical) scroll container. Its width is
             bounded by the rail offsets (content-wrap pl/pr), so a min-width card
             scrolls WITHIN it and the fixed rails can't scroll over the card. -->
        <main class="flex-1 overflow-auto bg-canvas">
            <div class="p-4 sm:p-6 lg:p-8">
                {#if router.view === 'operate'}
                    <Operate />
                {:else if router.view === 'logbook'}
                    <Logbook />
                {:else if router.view === 'config'}
                    <Settings />
                {:else}
                    <!-- Placeholder for views not yet built (dashboard). -->
                    <h1 class="text-2xl font-semibold text-ink">{titles[router.view]}</h1>
                    <div class="mt-6 h-[60vh] rounded-xl border border-dashed border-line"></div>
                {/if}
            </div>
        </main>
    </div>
{/if}
