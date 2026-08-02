<script lang="ts">
    // Masked text input with a show/hide toggle — forwarder credentials in the
    // app Settings view. Ported from the config SPA's PasswordField (ADR 0044)
    // onto the app's own classes.
    //
    // CONTROLLED, deliberately: the parent owns `value` and receives edits via
    // `oninput`. A plain bind would tempt echoing the masked placeholder back
    // into the form, and under the masked-on-GET contract a blank field is what
    // KEEPS the stored secret — so the transient form value must stay the
    // parent's, never a round-trip of something the daemon never sent.
    let {
        value,
        oninput,
        placeholder = '',
        id,
        disabled = false,
    }: {
        value: string;
        oninput: (value: string) => void;
        placeholder?: string;
        id?: string;
        disabled?: boolean;
    } = $props();

    let show = $state(false);
</script>

<div class="relative w-full">
    <input
        {id}
        {disabled}
        type={show ? 'text' : 'password'}
        {value}
        oninput={(e) => oninput(e.currentTarget.value)}
        {placeholder}
        autocomplete="off"
        spellcheck="false"
        class="input w-full pr-9"
    />
    <button
        type="button"
        onclick={() => (show = !show)}
        aria-label={show ? 'Hide value' : 'Show value'}
        aria-pressed={show}
        tabindex="-1"
        class="absolute inset-y-0 right-0 flex items-center px-2.5 text-muted hover:text-ink focus:outline-none"
    >
        <!-- Heroicons (outline), the same pair the config SPA's PasswordField
             uses. Inline SVG rather than an emoji: emoji render at the mercy of
             the platform font, carry their own colour, and are announced by
             screen readers as their unicode name. aria-hidden because the
             button's aria-label already says what it does. -->
        {#if show}
            <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                stroke-width="1.5"
                stroke="currentColor"
                class="h-5 w-5"
                aria-hidden="true"
            >
                <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M3.98 8.223A10.477 10.477 0 0 0 1.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.451 10.451 0 0 1 12 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 0 1-4.293 5.774M6.228 6.228 3 3m3.228 3.228 3.65 3.65m7.894 7.894L21 21m-3.228-3.228-3.65-3.65m0 0a3 3 0 1 0-4.243-4.243m4.243 4.243L9.88 9.88"
                />
            </svg>
        {:else}
            <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                stroke-width="1.5"
                stroke="currentColor"
                class="h-5 w-5"
                aria-hidden="true"
            >
                <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z"
                />
                <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"
                />
            </svg>
        {/if}
    </button>
</div>
