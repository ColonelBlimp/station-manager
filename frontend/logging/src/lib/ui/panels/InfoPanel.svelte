<script lang="ts">
    /*
        Stable identifiers for each tab. Distinct from the displayed
        title (which may change for i18n / wording polish) so the
        ===-comparisons against activeTab are typo-safe at compile
        time. Used as keys on the tab record and as the suffix in
        the panel's aria-controls / id pair.
    */
    import WorkedPanel from './WorkedPanel.svelte';
    import DetailsPanel from './DetailsPanel.svelte';
    import MyStationPanel from './MyStationPanel.svelte';
    import SessionPanel from './SessionPanel.svelte';
    import SessionEmailControls from './SessionEmailControls.svelte';
    import { contactHistoryState } from '../../states/contactHistory.svelte';
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { configState } from '../../states/config.svelte';
    import type { Snippet } from 'svelte';

    type TabId = 'worked' | 'details' | 'station' | 'session';

    interface Tab {
        id: TabId;
        title: string;
        /** Optional count rendered as " (N)" after the title. */
        count?: number;
    }

    /*
        Tab list as data. Reorder / rename / add by editing this
        array; markup below iterates over it and the per-id icon
        switch picks the matching snippet. Adding a fifth tab means:
        new TabId in the union, new entry here, new arm in iconFor +
        the panel switch at the bottom.

        `tabs` is `$derived` so the Worked count auto-updates when a
        Tab fetches contact-history and contactHistoryState.count
        flips from 0 to N. Session count auto-updates the same way
        when a QSO lands in the session list.
    */
    const tabs: Tab[] = $derived([
        { id: 'worked', title: 'Worked', count: contactHistoryState.count },
        { id: 'details', title: 'Details' },
        { id: 'station', title: 'My Station' },
        { id: 'session', title: 'Session', count: sessionQsosState.count },
    ]);

    let activeTab: TabId = $state('worked');

    /*
        WAI-ARIA tabs keyboard contract:
          - ArrowLeft / ArrowRight cycle between tabs (wraps at ends).
          - Home / End jump to first / last.
          - Roving tabindex: the active tab is tabindex=0, the others
            tabindex=-1, so a single Tab from the previous focusable
            lands on the active tab; further movement uses arrows.
          - Auto-activation: arrow movement also changes activeTab so
            the panel content follows focus. The reviewer's WAI-ARIA
            pattern reference allows either auto- or manual-activation;
            auto-activation matches operator expectation for a small
            fixed tab set.
    */
    function moveTab(delta: number): void {
        const idx = tabs.findIndex((t) => t.id === activeTab);
        const next = (idx + delta + tabs.length) % tabs.length;
        activeTab = tabs[next].id;
        const btn = document.getElementById(`tab-${tabs[next].id}`);
        btn?.focus();
    }

    function handleTabKeydown(e: KeyboardEvent): void {
        switch (e.key) {
            case 'ArrowRight':
                e.preventDefault();
                moveTab(1);
                break;
            case 'ArrowLeft':
                e.preventDefault();
                moveTab(-1);
                break;
            case 'Home':
                e.preventDefault();
                activeTab = tabs[0].id;
                document.getElementById(`tab-${tabs[0].id}`)?.focus();
                break;
            case 'End':
                e.preventDefault();
                activeTab = tabs[tabs.length - 1].id;
                document.getElementById(`tab-${tabs[tabs.length - 1].id}`)?.focus();
                break;
        }
    }

    /*
        Single class helper. Active state suppresses the cursor and
        flips the colour; everything else is inherited from
        .tab-item (see app.css).
    */
    const tabItemClass = (isActive: boolean): string =>
        isActive
            ? 'text-indigo-700 cursor-default'
            : 'text-gray-500 hover:text-gray-700 cursor-pointer';
</script>

<!--
    Icon snippets defined first so the tabs[] array below can reference
    them. Each is a one-off Heroicon (outline, 24/24, stroke-width 1.5).
    Inline rather than extracted into separate components because they
    have no other consumer and the SVG bodies are short enough to read
    in context.
-->
{#snippet workedIcon()}
    <svg
        class="size-6"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M9.348 14.652a3.75 3.75 0 0 1 0-5.304m5.304 0a3.75 3.75 0 0 1 0 5.304m-7.425 2.121a6.75 6.75 0 0 1 0-9.546m9.546 0a6.75 6.75 0 0 1 0 9.546M5.106 18.894c-3.808-3.807-3.808-9.98 0-13.788m13.788 0c3.808 3.807 3.808 9.98 0 13.788M12 12h.008v.008H12V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"
        />
    </svg>
{/snippet}

{#snippet detailsIcon()}
    <svg
        class="size-6"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M15 9h3.75M15 12h3.75M15 15h3.75M4.5 19.5h15a2.25 2.25 0 0 0 2.25-2.25V6.75A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25v10.5A2.25 2.25 0 0 0 4.5 19.5Zm6-10.125a1.875 1.875 0 1 1-3.75 0 1.875 1.875 0 0 1 3.75 0Zm1.294 6.336a6.721 6.721 0 0 1-3.17.789 6.721 6.721 0 0 1-3.168-.789 3.376 3.376 0 0 1 6.338 0Z"
        />
    </svg>
{/snippet}

{#snippet stationIcon()}
    <svg
        class="size-6"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M10.343 3.94c.09-.542.56-.94 1.11-.94h1.093c.55 0 1.02.398 1.11.94l.149.894c.07.424.384.764.78.93.398.164.855.142 1.205-.108l.737-.527a1.125 1.125 0 0 1 1.45.12l.773.774c.39.389.44 1.002.12 1.45l-.527.737c-.25.35-.272.806-.107 1.204.165.397.505.71.93.78l.893.15c.543.09.94.559.94 1.109v1.094c0 .55-.397 1.02-.94 1.11l-.894.149c-.424.07-.764.383-.929.78-.165.398-.143.854.107 1.204l.527.738c.32.447.269 1.06-.12 1.45l-.774.773a1.125 1.125 0 0 1-1.449.12l-.738-.527c-.35-.25-.806-.272-1.203-.107-.398.165-.71.505-.781.929l-.149.894c-.09.542-.56.94-1.11.94h-1.094c-.55 0-1.019-.398-1.11-.94l-.148-.894c-.071-.424-.384-.764-.781-.93-.398-.164-.854-.142-1.204.108l-.738.527c-.447.32-1.06.269-1.45-.12l-.773-.774a1.125 1.125 0 0 1-.12-1.45l.527-.737c.25-.35.272-.806.108-1.204-.165-.397-.506-.71-.93-.78l-.894-.15c-.542-.09-.94-.56-.94-1.109v-1.094c0-.55.398-1.02.94-1.11l.894-.149c.424-.07.765-.383.93-.78.165-.398.143-.854-.108-1.204l-.526-.738a1.125 1.125 0 0 1 .12-1.45l.773-.773a1.125 1.125 0 0 1 1.45-.12l.737.527c.35.25.807.272 1.204.107.397-.165.71-.505.78-.929l.15-.894Z"
        />
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"
        />
    </svg>
{/snippet}

{#snippet sessionIcon()}
    <svg
        class="size-6"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M8.25 6.75h12M8.25 12h12m-12 5.25h12M3.75 6.75h.007v.008H3.75V6.75Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0ZM3.75 12h.007v.008H3.75V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Zm-.375 5.25h.007v.008H3.75v-.008Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"
        />
    </svg>
{/snippet}

<div class="flex flex-col w-full px-6 pt-6">
    <!--
        Plain <div> with role="tablist" rather than <nav>. The
        tablist ARIA role implies interactive tab semantics, which
        clashes with <nav>'s "navigation landmark" semantics — they
        describe different reading patterns to assistive tech.
        The W3C ARIA Authoring Practices Guide for tab patterns
        uses a <div> for the tablist for this reason.

        Outer flex with `justify-between` so tab buttons sit on the
        left and the per-tab action area (today: Session-tab email
        controls) sits on the right. The action area is `<aside>`
        rather than another tablist member so screen readers don't
        try to read it as another tab.
    -->
    <div class="flex flex-row items-center justify-between border-b border-gray-400 pb-3">
        <div role="tablist" class="flex flex-row items-center space-x-20">
            {#each tabs as tab (tab.id)}
                <!--
                    The whole row (icon + label) is a single <button>. A prior
                    shape wrapped a `<div>` around the icon and a sibling
                    `<button>` for the label, which made the icon hover with
                    cursor:pointer (parent class) but ignore clicks (no handler
                    reached the sibling button). Collapsing into one button
                    keeps role="tab" semantics intact and makes the entire
                    row one click target.
                -->
                {@const tabIcons: Record<TabId, Snippet> = {
                    worked: workedIcon,
                    details: detailsIcon,
                    station: stationIcon,
                    session: sessionIcon,
                }}
                <button
                    id={`tab-${tab.id}`}
                    type="button"
                    role="tab"
                    class="tab-item {tabItemClass(activeTab === tab.id)}"
                    aria-selected={activeTab === tab.id}
                    aria-controls={`panel-${tab.id}`}
                    tabindex={activeTab === tab.id ? 0 : -1}
                    onclick={() => (activeTab = tab.id)}
                    onkeydown={handleTabKeydown}
                >
                    {@render tabIcons[tab.id]()}
                    <span>{tab.title}{tab.count !== undefined ? ` (${tab.count})` : ''}</span>
                </button>
            {/each}
            {#if activeTab === 'session' && configState.mailer.enabled}
                <aside
                    class="-ml-10 -mt-2 flex shrink-0 flex-row items-center gap-x-2"
                    aria-label="Email session ADIF"
                >
                    <SessionEmailControls />
                </aside>
            {/if}
        </div>
    </div>

    {#if activeTab === 'worked'}
        <div id="panel-worked" role="tabpanel" aria-labelledby="tab-worked"><WorkedPanel /></div>
    {:else if activeTab === 'details'}
        <div id="panel-details" role="tabpanel" aria-labelledby="tab-details"><DetailsPanel /></div>
    {:else if activeTab === 'station'}
        <div id="panel-station" role="tabpanel" aria-labelledby="tab-station">
            <MyStationPanel />
        </div>
    {:else if activeTab === 'session'}
        <div id="panel-session" role="tabpanel" aria-labelledby="tab-session">
            <SessionPanel cssTableHeight="h-60" />
        </div>
    {/if}
</div>
