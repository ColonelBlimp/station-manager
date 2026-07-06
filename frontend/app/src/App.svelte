<script lang="ts">
    import Sidebar from './lib/ui/Sidebar.svelte';
    import Header from './lib/ui/Header.svelte';
    import Operate from './lib/operate/Operate.svelte';
    import { ui, THEME_KEY, NAV_KEY, UTIL_KEY } from './lib/ui/state.svelte';
    import { router, type View } from './lib/router.svelte';
    import { operate } from './lib/operate/state.svelte';

    // Reflect shell state onto <html> (drives the token swap + collapse rules)
    // and persist it. Initial attributes are set pre-mount in index.html, so
    // these run in sync on the very first pass — no flash.
    $effect(() => {
        document.documentElement.dataset.theme = ui.theme;
        localStorage.setItem(THEME_KEY, ui.theme);
    });
    $effect(() => {
        document.documentElement.dataset.nav = ui.navMode;
        localStorage.setItem(NAV_KEY, ui.navMode);
    });
    $effect(() => {
        document.documentElement.dataset.util = ui.utilMode;
        localStorage.setItem(UTIL_KEY, ui.utilMode);
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
    };
</script>

<Sidebar />

<div class="content-wrap pl-[var(--sidebar-w)]">
    <Header />
    <main class="bg-canvas py-10">
        <div class="px-4 sm:px-6 lg:px-8">
            {#if router.view === 'operate'}
                <Operate />
            {:else}
                <!-- Placeholder for views not yet built. -->
                <h1 class="text-2xl font-semibold text-ink">{titles[router.view]}</h1>
                <div class="mt-6 h-[60vh] rounded-xl border border-dashed border-line"></div>
            {/if}
        </div>
    </main>
</div>
