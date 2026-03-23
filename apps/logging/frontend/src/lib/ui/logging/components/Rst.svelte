<script lang="ts">
    import {inputInvalid} from "@station-manager/shared-utils";

    interface Props {
        id: string;
        label: string;
        value: string;
        labelCss?: string;
        divCss?: string;
        inputCss?: string;
        overallWidthCss?: string;
    }
    let {
        id,
        label,
        value = $bindable(),
        labelCss = 'block text-sm/5 font-medium',
        divCss = 'mt-2',
        inputCss = 'uppercase block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600',
        overallWidthCss = 'w-[70px]'
    }: Props = $props();

    let invalid = $state(false);
    let inputElement: HTMLInputElement;

    const isValid = (v: string): boolean => {
        if (v.length > 3 || v.length < 2) {
            return false;
        }
        return /^[0-9]+$/.test(v);
    }

    const handleInput = (e: Event): void => {
        const target = e.currentTarget as HTMLInputElement;
        if (!target) return;
        const v = target.value;
        if (v === '') {
            invalid = false;
            return;
        }
        invalid = !isValid(v);
    }

    const validateAndFocus = (): void => {
        invalid = !isValid(value);
        if (invalid && inputElement) {
            inputElement.focus();
            inputElement.select();
        }
    }
</script>

<div class={overallWidthCss}>
    <label for={id} class={labelCss}>{label}</label>
    <div class={divCss}>
        <input
                bind:this={inputElement}
                bind:value={value}
                type="text"
                maxlength="3"
                minlength="2"
                id={id}
                class="{inputCss} {invalid ? inputInvalid : ''}"
                autocomplete="off"
                oninput={handleInput}
                onblur={validateAndFocus}
        />
    </div>
</div>
