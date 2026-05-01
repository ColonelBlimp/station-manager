<script lang="ts">
    import ValidatedInput from './ValidatedInput.svelte';
    import { isValidRst } from '../../validators/rst';

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
    }

    let {
        id,
        label,
        value = $bindable(''),
        widthClass = 'w-16',
    }: Props = $props();
</script>

<ValidatedInput
    {id}
    {label}
    bind:value
    {widthClass}
    validator={isValidRst}
    maxlength={3}
    minlength={2}
/>
