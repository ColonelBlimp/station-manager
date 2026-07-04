<script lang="ts">
    import { onMount } from 'svelte';
    import type { Snippet } from 'svelte';
    import { buildEnv } from '../states/buildEnv.svelte';

    onMount(() => void buildEnv.load());
</script>

<!--
    Fixed top-right cross-SPA nav cluster (this is the LOGBOOK copy) — links to the
    other SPAs + the Manual; the self-link (Logbook) is omitted. Duplicated per SPA
    (they are separate Vite projects) rather than shared via a package — a little
    duplication is cheaper than a cross-project shared lib ("build specific, not
    generic"). If you change one copy, mirror it to the others (each just omits its
    own self-link + supplies the matching icon).

    The link shell (classes, target/rel, layout) lives in ONE place — the navLink
    snippet — so styling/markup changes happen once; each node supplies its
    href/label/title + an icon snippet.

    App links navigate in the SAME tab; only the Manual opens a new tab. Each SPA
    holds long-lived SSE streams and a browser caps ~6 connections per host, so
    opening every nav click in a new tab piled up SSE until the browser starved
    (hang). Same-tab keeps one SPA's SSE live at a time; the Manual is static (no SSE).
    - hrefs are absolute root paths, resolving the same from every SPA base.
    - rel="noopener" drops the new tab's window.opener handle.
-->

{#snippet pencilIcon()}
    <svg
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        class="size-5"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L10.582 16.07a4.5 4.5 0 0 1-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 0 1 1.13-1.897l8.932-8.931Zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0 1 15.75 21H5.25A2.25 2.25 0 0 1 3 18.75V8.25A2.25 2.25 0 0 1 5.25 6H10"
        />
    </svg>
{/snippet}

{#snippet cogIcon()}
    <svg
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        class="size-5"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.24-.438.613-.431.992a6.759 6.759 0 0 1 0 .255c-.007.378.138.75.43.99l1.005.828c.424.35.534.954.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.992a6.932 6.932 0 0 1 0-.255c.007-.378-.138-.75-.43-.99l-1.004-.828a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.281Z"
        />
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"
        />
    </svg>
{/snippet}

{#snippet bookIcon()}
    <svg
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        class="size-5"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 6.042A8.967 8.967 0 0 0 6 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 0 1 6 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 0 1 6-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0 0 18 18a8.967 8.967 0 0 0-6 2.292m0-14.25v14.25"
        />
    </svg>
{/snippet}

{#snippet navLink(href: string, label: string, title: string, icon: Snippet, newTab: boolean)}
    <a
        {href}
        target={newTab ? '_blank' : undefined}
        rel={newTab ? 'noopener' : undefined}
        {title}
        aria-label={title}
        class="inline-flex items-center gap-1.5 rounded-md border border-gray-300 bg-white/95 px-2.5 py-1 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
    >
        {@render icon()}
        <span class="w-15">{label}</span>
    </a>
{/snippet}

<!-- z-40 keeps this fixed nav BELOW modal overlays (the QSO edit modal is z-50).
     At z-50 the nav shared the modal's layer and, painting later, stayed on top —
     its links remained clickable/tabbable over an open modal. Must stay < the
     modal layer; still well above page content (which tops out at z-20). -->
<nav class="fixed top-2 right-3 z-40 flex flex-col items-center gap-1.5">
    {#if buildEnv.isDev}
        <span
            class="rounded-md border border-amber-400 bg-amber-100 px-2 py-0.5 text-xs font-bold text-amber-800 shadow-sm"
            title="Development daemon (task run:smd) — not the deployed build"
        >
            DEV
        </span>
    {/if}
    {@render navLink('/', 'Logging', 'Open Station Manager logging', pencilIcon, false)}
    {@render navLink('/config/', 'Config', 'Open Station Manager configuration', cogIcon, false)}
    {@render navLink('/manual/', 'Manual', 'Open the manual in a new tab', bookIcon, true)}
</nav>
