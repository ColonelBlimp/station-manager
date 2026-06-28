<script lang="ts"></script>

<!--
    Fixed top-right "Manual" link — opens the embedded operator manual (served by the
    daemon at GET /manual/) in a NEW TAB. Identical across all three SPAs (logging /
    config / logbook); they are separate Vite projects, so this small component is
    duplicated rather than shared via a package — a little duplication is cheaper than
    a cross-project shared lib (project lesson "build specific, not generic"). If you
    change one copy, change the other two.

    This corner is the planned home of the cross-SPA navigation cluster (→ logging /
    config / logbook — backlog "Cross-SPA navigation links"); manual-only for now.

    - href is an absolute root path, so it resolves the same from every SPA base
      (/, /config/, /logbook/); the daemon serves it whenever the SPAs are served.
    - rel="noopener" drops the new tab's window.opener handle (target=_blank hygiene).
-->
<a
    href="/manual/"
    target="_blank"
    rel="noopener"
    title="Open the Station Manager manual in a new tab"
    aria-label="Open the Station Manager manual in a new tab"
    class="fixed top-2 right-3 z-50 inline-flex items-center gap-1.5 rounded-md border border-gray-300 bg-white/95 px-2.5 py-1 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 hover:text-gray-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
>
    <svg
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        class="h-4 w-4"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 6.042A8.967 8.967 0 0 0 6 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 0 1 6 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 0 1 6-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0 0 18 18a8.967 8.967 0 0 0-6 2.292m0-14.25v14.25"
        />
    </svg>
    <span>Manual</span>
</a>
