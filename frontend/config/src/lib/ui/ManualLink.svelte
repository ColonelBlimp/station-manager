<script lang="ts">
    import { onMount } from 'svelte';
    import type { Snippet } from 'svelte';
    import { buildEnv } from '../states/buildEnv.svelte';

    onMount(() => void buildEnv.load());
</script>

<!--
    Fixed top-right cross-SPA nav cluster (this is the CONFIG copy) — links to the
    other SPAs + the Manual; the self-link (Config) is omitted. Duplicated per SPA
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

{#snippet listIcon()}
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
            d="M3.75 12h16.5m-16.5 3.75h16.5M3.75 19.5h16.5M5.625 4.5h12.75a1.875 1.875 0 0 1 0 3.75H5.625a1.875 1.875 0 0 1 0-3.75Z"
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

<nav class="fixed top-2 right-3 z-50 flex flex-col items-center gap-1.5">
    {#if buildEnv.isDev}
        <span
            class="rounded-md border border-amber-400 bg-amber-100 px-2 py-0.5 text-xs font-bold text-amber-800 shadow-sm"
            title="Development daemon (task run:smd) — not the deployed build"
        >
            DEV
        </span>
    {/if}
    {@render navLink('/', 'Logging', 'Open Station Manager logging', pencilIcon, false)}
    {@render navLink('/logbook/', 'Logbook', 'Open the Station Manager logbook', listIcon, false)}
    {@render navLink('/manual/', 'Manual', 'Open the manual in a new tab', bookIcon, true)}
</nav>
