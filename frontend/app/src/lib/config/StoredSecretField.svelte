<script lang="ts">
    // A credential field whose stored value the daemon never sends back
    // (operator ruling 2026-09-26). The old "•••••••• (set — leave blank to
    // keep)" placeholder packed a status, a rule and an instruction into an
    // empty-looking box. Instead:
    //   - nothing stored        → the labelled input;
    //   - stored                → "✓ saved" with Replace (and Remove where the
    //                              caller allows it), no input;
    //   - Replace, or a typed   → the input with Cancel; Cancel drops what was
    //     value over a stored     typed, so blank still means keep on the wire;
    //   - removal pending       → its own line with Undo.
    // The parent owns every value (controlled, like MaskedField): this holds
    // only whether Replace was pressed.
    import MaskedField from './MaskedField.svelte';
    import HelpTip from './HelpTip.svelte';

    let {
        label,
        kind = 'password',
        stored,
        value,
        cleared = false,
        removable = false,
        removeLabel = 'Remove',
        removedNote = 'Removed when you save.',
        emptyPlaceholder = '',
        help = '',
        invalid = false,
        invalidNote = '',
        disabled = false,
        oninput,
        onremove = () => undefined,
        onundo = () => undefined,
    }: {
        label: string;
        kind?: string;
        stored: boolean;
        value: string;
        cleared?: boolean;
        removable?: boolean;
        removeLabel?: string;
        removedNote?: string;
        emptyPlaceholder?: string;
        help?: string;
        invalid?: boolean;
        invalidNote?: string;
        disabled?: boolean;
        oninput: (value: string) => void;
        onremove?: () => void;
        onundo?: () => void;
    } = $props();

    const id = $props.id();
    let replacing = $state(false);
    // The input shows when there is something to type into: nothing stored,
    // Replace pressed, a value already typed, or a mark to see.
    const editing = $derived(!cleared && (!stored || replacing || value !== '' || invalid));

    function cancel(): void {
        oninput('');
        replacing = false;
    }
</script>

<div class="flex flex-col gap-1">
    <span class="flex items-center gap-1.5 text-sm font-medium text-ink">
        {#if editing}<label for="field-{id}">{label}</label>{:else}<span>{label}</span>{/if}
        {#if help}<HelpTip label="About {label}" text={help} />{/if}
    </span>

    {#if cleared}
        <span class="flex items-center gap-2 text-sm text-warning" data-testid="removal-pending">
            {removedNote}
            <button
                type="button"
                class="underline"
                {disabled}
                aria-label="Undo removing {label}"
                onclick={() => onundo()}>Undo</button
            >
        </span>
    {:else if !editing}
        <span class="flex items-center gap-3 text-sm">
            <span class="text-ink" data-testid="saved-status">✓ saved</span>
            <button
                type="button"
                class="text-muted underline hover:text-ink"
                {disabled}
                aria-label="Replace {label}"
                onclick={() => (replacing = true)}>Replace</button
            >
            {#if removable}
                <button
                    type="button"
                    class="text-muted underline hover:text-ink"
                    {disabled}
                    aria-label="{removeLabel} {label}"
                    onclick={() => onremove()}>{removeLabel}</button
                >
            {/if}
        </span>
    {:else}
        <div class="flex items-center gap-2">
            <div class="flex-1">
                {#if kind === 'password'}
                    <MaskedField
                        id="field-{id}"
                        {value}
                        {invalid}
                        {disabled}
                        placeholder={stored ? '' : emptyPlaceholder}
                        {oninput}
                    />
                {:else}
                    <input
                        id="field-{id}"
                        type="text"
                        class="input w-full"
                        class:input-error={invalid}
                        aria-invalid={invalid || undefined}
                        {disabled}
                        {value}
                        placeholder={stored ? '' : emptyPlaceholder}
                        oninput={(e) => oninput(e.currentTarget.value)}
                        autocomplete="off"
                        spellcheck="false"
                    />
                {/if}
            </div>
            {#if stored}
                <button
                    type="button"
                    class="btn"
                    {disabled}
                    aria-label="Cancel replacing {label}"
                    onclick={cancel}>Cancel</button
                >
            {/if}
        </div>
        {#if invalid && invalidNote}
            <span class="text-xs text-invalid">{invalidNote}</span>
        {/if}
    {/if}
</div>
