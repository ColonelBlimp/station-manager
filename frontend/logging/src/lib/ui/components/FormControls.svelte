<!--
    FormControls — the Clear / Log Contact button pair for the QSO panel.

    Buttons are presentational here; the parent (QsoPanel) owns the
    draft state and supplies `onClear` / `onSubmit` callbacks. This
    keeps QsoPanel as the orchestrator and FormControls as a reusable
    visual cluster (a future panel could mount its own pair).

    Colour tokens (per app.css `@theme`):
      - Clear:        bg-surface (resting), hover bg-surface-disabled,
                      ring-line, focus-visible:outline-focus.
      - Log Contact:  bg-focus (primary CTA = focus accent), hover
                      bg-focus-ring, focus-visible:outline-focus.

    `submitDisabled` gates Log Contact only; Clear stays always enabled
    (clearing an empty form is a no-op). Form-level validation gating
    (the canSubmit derivation) lives in the parent.
-->
<script lang="ts">
    interface Props {
        onClear?: () => void;
        onSubmit?: () => void;
        submitDisabled?: boolean;
    }

    let { onClear, onSubmit, submitDisabled = false }: Props = $props();
</script>

<div class="flex justify-end items-end space-x-2 pl-4">
    <button
        id="clear-qso-btn"
        type="button"
        title="ESC"
        tabindex={-1}
        onclick={() => onClear?.()}
        class="h-9 w-18.5 cursor-pointer rounded-md bg-surface px-2.5 py-1.5 text-base font-semibold ring-1 shadow-sm ring-line ring-inset hover:bg-surface-disabled focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >Clear
    </button>
    <button
        id="log-qso-btn"
        type="button"
        title="Ctrl+Enter"
        onclick={() => onSubmit?.()}
        disabled={submitDisabled}
        class="h-9 cursor-pointer rounded-md bg-focus p-2.5 py-1.5 text-base font-semibold text-white shadow-sm hover:bg-focus-ring focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-50 disabled:cursor-not-allowed"
        >Log Contact</button
    >
</div>
