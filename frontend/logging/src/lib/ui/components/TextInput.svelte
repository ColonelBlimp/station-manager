<!--
    TextInput — generic free-text input, no validation, no domain
    behaviour. Use this for plain string fields like Name and QTH where
    well-formedness has no rule.

    Pick instead:
      - `ValidatedInput` for fields with a well-formed-only predicate.
      - `Callsign`, `Rst`, etc. for domain components with side-effects.
-->
<script lang="ts">
    interface Props {
        id: string;
        label: string;
        value: string;
        disabled?: boolean;
        widthClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        disabled = false,
        widthClass = 'w-54',
    }: Props = $props();
</script>

<div class="{widthClass} input-row">
    <label for={id} class="input-label">{label}</label>
    <div class="mt-1">
        <input
            {id}
            bind:value
            {disabled}
            class="input-base disabled:bg-gray-100 disabled:cursor-not-allowed"
            type="text"
            autocomplete="off"
            spellcheck="false"
        />
    </div>
</div>
