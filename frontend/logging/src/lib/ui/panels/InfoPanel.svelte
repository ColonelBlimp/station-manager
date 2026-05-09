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
    import { contactHistoryState } from '../../states/contactHistory.svelte';
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { configState } from '../../states/config.svelte';
    import { sendSessionEmail } from '../../api/session-email';
    import { toasts } from '../../states/toasts.svelte';

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
        Single class helper. Active state suppresses the cursor and
        flips the colour; everything else is inherited from
        .tab-item (see app.css).
    */
    const tabItemClass = (isActive: boolean): string =>
        isActive
            ? 'text-indigo-700 cursor-default'
            : 'text-gray-500 hover:text-gray-700 cursor-pointer';

    /*
        Session-tab email controls — recipient input + paper-plane
        send button on the right of the tab strip. Only rendered
        when (a) the Session tab is active and (b) the daemon's
        mailer is enabled (configState.mailer.enabled). When the
        operator hasn't configured SMTP, the controls are hidden
        rather than disabled — there's nothing for the operator to
        do here without daemon-side config, so a non-functional
        button would be confusing.

        recipient seeds from configState.mailer.defaultRecipient
        the first time it lands non-empty, then becomes operator-
        owned — subsequent config changes don't clobber what the
        operator typed. The boolean guard prevents the seed from
        repeating after the operator clears the input.
    */
    let recipient: string = $state('');
    let recipientSeeded: boolean = false;
    $effect(() => {
        const def = configState.mailer.defaultRecipient;
        if (!recipientSeeded && def !== '') {
            recipient = def;
            recipientSeeded = true;
        }
    });

    let sending: boolean = $state(false);

    /*
        canSend — gating logic for the paper-plane button. All four
        clauses must hold:
          - mailer enabled (button shouldn't render otherwise but the
            check is cheap belt-and-braces).
          - at least one logged QSO in the session.
          - recipient non-empty and contains '@' (daemon validates
            strictly; this is just the obvious-typo guard so we don't
            round-trip a 400 for an empty input).
          - not already mid-send.
    */
    const canSend: boolean = $derived(
        configState.mailer.enabled &&
            sessionQsosState.count > 0 &&
            recipient.trim() !== '' &&
            recipient.includes('@') &&
            !sending
    );

    /*
        handleSend — orchestrate the send + toast flow.

        - Push a "Sending…" info toast that sticks (ttl=0) so it
          stays visible during a slow handshake. The operator's
          internet is on the slow side per the network memory.
        - Concatenate every session QSO's pre-built ADIF (each
          already has a trailing <eor>) into one body. Order is
          submit-order — the daemon doesn't care, but it matches
          what the operator's QSL manager expects to see.
        - Map the discriminated outcome to a toast level + message.
          Sticky "Sending…" is dismissed before the result toast
          fires either way.
        - Don't clear sessionQsosState on success — the operator
          may want to keep eyeballing the list during a session,
          and a manual "Clear session" affordance is a separate
          stage if/when it's needed.
    */
    async function handleSend(): Promise<void> {
        if (!canSend) return;
        sending = true;
        const sendingToastId = toasts.info('Sending…', 0);
        try {
            const adif = sessionQsosState.items.map((q) => q.adif).join('');
            const outcome = await sendSessionEmail({
                to: recipient.trim(),
                adif,
            });
            toasts.dismiss(sendingToastId);
            switch (outcome.kind) {
                case 'sent':
                    toasts.info(`Sent to ${recipient.trim()}`);
                    break;
                case 'mailer_disabled':
                    toasts.error('Email not configured (SMTP block missing)');
                    break;
                case 'invalid':
                    toasts.error(outcome.message);
                    break;
                case 'smtp_failure':
                    toasts.error('Email send failed; check daemon logs');
                    break;
                case 'server':
                    toasts.error(`Email send failed: ${outcome.message}`);
                    break;
                case 'network':
                    toasts.error('Cannot reach daemon');
                    break;
            }
        } finally {
            sending = false;
        }
    }
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

<!--
    Heroicon "paper-airplane" (outline) — the send-email button on
    the Session tab. Stroke-width matches the tab icons so it sits
    visually flush with them.
-->
{#snippet paperPlaneIcon()}
    <svg
        class="size-5"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M6 12 3.269 3.125A59.769 59.769 0 0 1 21.485 12 59.768 59.768 0 0 1 3.27 20.875L5.999 12Zm0 0h7.5"
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
        <div role="tablist" class="flex flex-row items-center space-x-24">
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
                <button
                    type="button"
                    role="tab"
                    class="tab-item {tabItemClass(activeTab === tab.id)}"
                    aria-selected={activeTab === tab.id}
                    aria-controls={`panel-${tab.id}`}
                    onclick={() => (activeTab = tab.id)}
                >
                    {#if tab.id === 'worked'}{@render workedIcon()}
                    {:else if tab.id === 'details'}{@render detailsIcon()}
                    {:else if tab.id === 'station'}{@render stationIcon()}
                    {:else if tab.id === 'session'}{@render sessionIcon()}
                    {/if}
                    <span>{tab.title}{tab.count !== undefined ? ` (${tab.count})` : ''}</span>
                </button>
            {/each}
        </div>

        {#if activeTab === 'session' && configState.mailer.enabled}
            <aside class="flex flex-row items-center gap-2" aria-label="Email session ADIF">
                <input
                    type="email"
                    bind:value={recipient}
                    placeholder="recipient@example.com"
                    class="text-sm border border-gray-300 rounded px-2 py-1 w-64 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                    aria-label="Recipient email address"
                />
                <button
                    type="button"
                    onclick={handleSend}
                    disabled={!canSend}
                    class="p-1.5 rounded text-indigo-600 hover:text-indigo-800 hover:bg-indigo-50 disabled:text-gray-400 disabled:hover:bg-transparent disabled:cursor-not-allowed cursor-pointer"
                    aria-label="Send session ADIF"
                    title={sending ? 'Sending…' : 'Send session ADIF'}
                >
                    {@render paperPlaneIcon()}
                </button>
            </aside>
        {/if}
    </div>

    {#if activeTab === 'worked'}
        <div id="panel-worked" role="tabpanel"><WorkedPanel /></div>
    {:else if activeTab === 'details'}
        <div id="panel-details" role="tabpanel"><DetailsPanel /></div>
    {:else if activeTab === 'station'}
        <div id="panel-station" role="tabpanel"><MyStationPanel /></div>
    {:else if activeTab === 'session'}
        <div id="panel-session" role="tabpanel"><SessionPanel /></div>
    {/if}
</div>
