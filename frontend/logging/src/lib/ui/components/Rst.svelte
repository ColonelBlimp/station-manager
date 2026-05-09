<script lang="ts">
    import ValidatedInput from './ValidatedInput.svelte';
    import { isValidRst } from '../../validators/rst';

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
    }

    let { id, label, value = $bindable(''), widthClass = 'w-16' }: Props = $props();

    /*
        RST is digits only — readability/strength (and tone for CW).
        Strip non-digits as the operator types so "59A" lands as "59"
        rather than triggering only a red border. Idempotent: a paste
        of already-clean digits is unchanged.
    */
    const stripNonDigits = (raw: string): string => raw.replace(/[^0-9]/g, '');
</script>

<ValidatedInput
    {id}
    {label}
    bind:value
    {widthClass}
    validator={isValidRst}
    transform={stripNonDigits}
    maxlength={3}
    minlength={2}
/>
