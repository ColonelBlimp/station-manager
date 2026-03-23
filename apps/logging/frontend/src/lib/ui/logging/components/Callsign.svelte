<script lang="ts">
    import {CALLSIGN_PATTERN, isValidCallsignLength} from "$lib/constants/callsign";
    import {handleAsyncError} from "$lib/utils/error-handler";
    import {NewQso, IsContestDuplicate} from "$lib/wailsjs/go/facade/Service";
    import {qsoState} from "$lib/states/new-qso-state.svelte";
    import {appState} from "$lib/states/app-state.svelte";
    import {WORKED_TAB_TITLE} from "$lib/ui/logging/panels/constants";
    import {isContestMode} from "$lib/stores/logging-mode-store";
    import {frequencyToBandFromDottedMHz} from "@station-manager/shared-utils";
    import {showToast} from "$lib/utils/toast";
    import type {FocusRefs} from "@station-manager/shared-utils/svelte";
    import {inputBaseUppercase, inputWrapper, labelBase} from "@station-manager/shared-utils";

    interface Props {
        id: string;
        label: string;
        value: string;
        labelCss?: string;
        divCss?: string;
        inputCss?: string;
        overallWidthCss?: string;
        focusRefKey?: keyof FocusRefs;
        focusRefs?: FocusRefs;
    }
    let {
        id,
        label,
        value = $bindable(),
        labelCss = labelBase,
        divCss = inputWrapper,
        inputCss = inputBaseUppercase,
        overallWidthCss = 'w-[150px]',
        focusRefKey,
        focusRefs,
    }: Props = $props();

    let invalid = $state(false);
    let inputElement: HTMLInputElement;
    let lastKey: string | null = null;

    // Sync the local inputElement to the focus context if provided
    $effect(() => {
        if (focusRefs && focusRefKey && inputElement) {
            focusRefs[focusRefKey] = inputElement;
        }
    });

    const isValid = (v: string): boolean => {
        const value = v.trim().toUpperCase();
        return isValidCallsignLength(value) && CALLSIGN_PATTERN.test(value);
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

    const validateAndFocus = async (): Promise<void> => {
        const tabbed = lastKey === "Tab";
        lastKey = null;
        if (!tabbed) return;

        invalid = !isValid(value);
        if (invalid && inputElement) {
            inputElement.focus();
            inputElement.select();
            return;
        }

        try {
            if ($isContestMode) {
                const dup: boolean = await IsContestDuplicate(value.toUpperCase(), frequencyToBandFromDottedMHz(qsoState.cat_vfoa_freq));
                if (dup) {
                    inputElement.focus();
                    inputElement.select();
                    showToast.WARN("Duplicate. Please check.", 2500);
                    return;
                }
            }

            const qso = await NewQso(value);
            qsoState.fromQso(qso);
            qsoState.startTimer();
            appState.activePanel = WORKED_TAB_TITLE;
        } catch (e: unknown) {
            // Any error here is serious and means we cannot continue: either there is something wrong with the
            // provided callsign or the backend is not available.
            handleAsyncError(e, "Callsign.svelte: validateAndFocus");
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
                id={id}
                class="{inputCss} {invalid ? 'outline-red-600 focus:outline-red-600' : ''}"
                autocomplete="off"
                spellcheck="false"
                aria-invalid={invalid}
                oninput={handleInput}
                onblur={validateAndFocus}
                onkeydown={(e) => lastKey = e.key}
        />
    </div>
</div>
