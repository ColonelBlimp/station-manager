<script lang="ts">
    import { onMount } from 'svelte';
    import { configState } from './lib/states/config.svelte';
    import TabPlaceholder from './lib/ui/TabPlaceholder.svelte';
    import StationTab from './lib/ui/tabs/StationTab.svelte';
    import RigsTab from './lib/ui/tabs/RigsTab.svelte';
    import ForwardingTab from './lib/ui/tabs/ForwardingTab.svelte';
    import Ft8Tab from './lib/ui/tabs/Ft8Tab.svelte';
    import EnrichmentTab from './lib/ui/tabs/EnrichmentTab.svelte';
    import EmailTab from './lib/ui/tabs/EmailTab.svelte';
    import GeneralTab from './lib/ui/tabs/GeneralTab.svelte';
    import ManualLink from './lib/ui/ManualLink.svelte';

    // ── Tab shell ────────────────────────────────────────────────────────────
    // The config SPA is a category-tab shell (design 2026-06-24): one tab per
    // set-once config area. Order is setup-order — what you'd touch on a fresh
    // install, identity first. Bodies are placeholders for now; each tab gets a
    // real panel component as the workstream fills it in. Default landing = the
    // first tab (Station).
    type TabId = 'station' | 'rigs' | 'ft8' | 'forwarding' | 'email' | 'enrichment' | 'general';

    const tabs: { id: TabId; title: string }[] = [
        { id: 'station', title: 'Station' },
        { id: 'rigs', title: 'Rigs' },
        { id: 'ft8', title: 'FT8' },
        { id: 'forwarding', title: 'Forwarding' },
        { id: 'email', title: 'Email' },
        { id: 'enrichment', title: 'Enrichment' },
        // Cross-cutting operator preferences + About/version — a catch-all for knobs
        // that don't belong to a domain tab (decided 2026-06-26). Last in the strip.
        { id: 'general', title: 'General' },
    ];

    const validTab = (s: string): s is TabId => tabs.some((t) => t.id === s);

    // Hash-based deep-linking (#rigs, #forwarding, …): cheap, no router dep, and
    // lets the daemon's future "restart required" hint / docs link straight to a
    // tab. Falls back to the first tab for an empty/unknown hash.
    function tabFromHash(): TabId {
        const h = location.hash.replace(/^#/, '');
        return validTab(h) ? h : tabs[0].id;
    }

    let activeTab: TabId = $state(tabFromHash());

    function selectTab(id: TabId): void {
        activeTab = id;
        if (location.hash !== `#${id}`) {
            location.hash = id;
        }
    }

    // Left/Right arrow moves between tabs (ARIA tab pattern, roving tabindex).
    function onTabKeydown(e: KeyboardEvent): void {
        const dir = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
        if (dir === 0) return;
        e.preventDefault();
        const i = tabs.findIndex((t) => t.id === activeTab);
        const next = tabs[(i + dir + tabs.length) % tabs.length];
        selectTab(next.id);
        document.getElementById(`tab-${next.id}`)?.focus();
    }

    const activeTitle = $derived(tabs.find((t) => t.id === activeTab)?.title ?? '');

    // Daemon-connection dot: green once /v1/config + /v1/rigs load clean, red on
    // error, grey while the first round-trip is in flight.
    const statusColour = $derived(
        !configState.loaded ? 'bg-gray-300' : configState.error ? 'bg-red-500' : 'bg-green-500'
    );
    const statusLabel = $derived(
        !configState.loaded ? 'Connecting…' : configState.error ? 'Daemon error' : 'Daemon OK'
    );

    onMount(() => {
        void configState.load();
        const onHash = (): void => {
            activeTab = tabFromHash();
        };
        window.addEventListener('hashchange', onHash);
        return () => window.removeEventListener('hashchange', onHash);
    });
</script>

<!-- Fixed top-right Manual link, shared with the logging + logbook SPAs (identical
     ManualLink component). This corner is the planned home of the cross-SPA links
     (→ logging, → logbook — backlog), so they'll join it here rather than the header
     right-slot. The header gets extra right padding (pr-32) so its status cluster
     clears the fixed link. -->
<ManualLink />
<div class="flex flex-col min-h-screen mx-auto w-300  bg-gray-50 font-sans text-gray-900">
    <header
        class="flex items-center justify-between border-b border-gray-300 bg-white py-3 pl-6 pr-32"
    >
        <h1 class="text-lg font-semibold">Station Manager — Config</h1>
        <div class="flex items-center gap-2 text-sm text-gray-600">
            <span class="inline-block h-2.5 w-2.5 rounded-full {statusColour}" aria-hidden="true"
            ></span>
            <span>{statusLabel}</span>
        </div>
    </header>

    <!-- Tab strip -->
    <div class="border-b border-gray-300 bg-white px-6">
        <div role="tablist" class="flex flex-row items-center gap-6">
            {#each tabs as tab (tab.id)}
                <button
                    id={`tab-${tab.id}`}
                    type="button"
                    role="tab"
                    aria-selected={activeTab === tab.id}
                    aria-controls={`panel-${tab.id}`}
                    tabindex={activeTab === tab.id ? 0 : -1}
                    onclick={() => selectTab(tab.id)}
                    onkeydown={onTabKeydown}
                    class="-mb-px cursor-pointer border-b-2 py-3 text-sm font-medium transition-colors {activeTab ===
                    tab.id
                        ? 'border-indigo-600 text-indigo-700'
                        : 'border-transparent text-gray-500 hover:text-gray-800'}"
                >
                    {tab.title}
                </button>
            {/each}
        </div>
    </div>

    <!-- Active tab body. Each tab owns its own save footer (the Rigs tab writes
         via rig CRUD, the rest via PUT /v1/config), so there's no shared shell
         footer. Tabs not yet built show a placeholder. -->
    <main class="flex-1 px-6 py-6">
        <div id={`panel-${activeTab}`} role="tabpanel" aria-labelledby={`tab-${activeTab}`}>
            {#if activeTab === 'station'}
                <StationTab />
            {:else if activeTab === 'rigs'}
                <RigsTab />
            {:else if activeTab === 'forwarding'}
                <ForwardingTab />
            {:else if activeTab === 'ft8'}
                <Ft8Tab />
            {:else if activeTab === 'enrichment'}
                <EnrichmentTab />
            {:else if activeTab === 'email'}
                <EmailTab />
            {:else if activeTab === 'general'}
                <GeneralTab />
            {:else}
                <TabPlaceholder title={activeTitle} />
            {/if}
        </div>
    </main>
</div>
