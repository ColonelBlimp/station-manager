<script lang="ts">
    // An ⓘ that holds a field's help (operator ruling 2026-09-26: help behind
    // an ⓘ, not a line under the field). A BUTTON so the keyboard reaches it;
    // hover and focus (a tap focuses it) reveal the text, and
    // aria-describedby reads it to a screen reader — the same pattern as the
    // Archives failure ⚠.
    let { label, text }: { label: string; text: string } = $props();
    const id = $props.id();
</script>

<span class="relative inline-block align-middle">
    <button
        type="button"
        class="peer cursor-help text-muted hover:text-ink"
        aria-label={label}
        aria-describedby="help-{id}"
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
    </button>
    <span
        id="help-{id}"
        role="tooltip"
        class="absolute top-full left-0 z-10 mt-1 hidden w-72 rounded-md border border-line bg-surface px-2 py-1 text-xs font-normal text-ink shadow peer-hover:block peer-focus:block"
        >{text}</span
    >
</span>
