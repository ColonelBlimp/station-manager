<script lang="ts">
    // Minimal labelled text input for the config SPA's forms. All LoggingStation
    // fields are strings daemon-side, so this is text-only; `inputmode` just hints
    // the on-screen keyboard for numeric-ish fields (zones, altitude). Validation
    // is deliberately light here — the daemon validates on PUT and the tab footer
    // surfaces any rejection. (A richer ValidatedInput can come with the shared-
    // theme / UI-consistency work; see docs/backlog.md.)
    let {
        label,
        value = $bindable(''),
        hint,
        placeholder = '',
        inputmode = 'text',
        maxlength,
        widthClass = 'w-full',
    }: {
        label: string;
        value?: string;
        hint?: string;
        placeholder?: string;
        inputmode?: 'text' | 'numeric' | 'decimal';
        maxlength?: number;
        widthClass?: string;
    } = $props();
</script>

<label class="flex flex-col gap-1 {widthClass}">
    <span class="text-sm font-medium text-gray-700">{label}</span>
    <input
        type="text"
        {inputmode}
        {placeholder}
        {maxlength}
        bind:value
        class="rounded-md border border-gray-300 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
    />
    {#if hint}<span class="text-xs text-gray-400">{hint}</span>{/if}
</label>
