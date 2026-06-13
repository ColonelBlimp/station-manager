<script lang="ts">
    import ValidatedInput from './ValidatedInput.svelte';
    import { isValidRst, isValidSignalReport } from '../../validators/rst';
    import { usesSignalReport } from '../../utils/mode';

    interface Props {
        id: string;
        label: string;
        value: string;
        widthClass?: string;
        /*
            ADIF mode (or submode) of the QSO. The WSJT-X-family weak-signal
            modes (FT8/FT4/JT*) report a signed dB SNR (e.g. -12) rather than
            RST digits, so validation + input cleaning switch on it. Defaults
            to RST (phone/CW) when unset.
        */
        mode?: string;
    }

    let { id, label, value = $bindable(''), widthClass = 'w-16', mode = '' }: Props = $props();

    const signalReport = $derived(usesSignalReport(mode));

    /*
        Strip the disallowed characters as the operator types so a stray key
        only ever shortens the value rather than flagging a red border.
        Idempotent on already-clean input.
          - RST is digits only — readability/strength (and tone for CW), so
            "59A" lands as "59".
          - A signal report keeps a single leading +/- sign, so "-12x" lands
            as "-12".
    */
    const stripNonDigits = (raw: string): string => raw.replace(/[^0-9]/g, '');
    const stripToSignedInt = (raw: string): string => {
        const sign = /^\s*-/.test(raw) ? '-' : /^\s*\+/.test(raw) ? '+' : '';
        return sign + raw.replace(/[^0-9]/g, '');
    };
</script>

<ValidatedInput
    {id}
    {label}
    bind:value
    {widthClass}
    validator={signalReport ? isValidSignalReport : isValidRst}
    transform={signalReport ? stripToSignedInt : stripNonDigits}
    maxlength={3}
    minlength={signalReport ? 1 : 2}
/>
