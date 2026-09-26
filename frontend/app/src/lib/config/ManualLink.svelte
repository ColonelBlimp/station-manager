<script lang="ts">
    // An ⓘ beside a section heading that opens its manual section in a new
    // tab, like the sidebar's Manual link (operator ruling 2026-09-26). A
    // native tooltip cannot hold a link, so the icon IS the link, named by
    // `label` (tooltip and accessible name). Place it OUTSIDE the heading so
    // the section's name stays the heading alone.
    let { anchor, label }: { anchor: string; label: string } = $props();
</script>

<a
    href={`/manual/#${anchor}`}
    target="_blank"
    rel="noopener"
    title={label}
    aria-label={label}
    class="text-muted hover:text-ink"
>
    <svg
        class="size-4"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z"
        />
    </svg>
</a>
