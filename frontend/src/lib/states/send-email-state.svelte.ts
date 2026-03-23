import { configState } from '$lib/states/config-state.svelte';

export interface SendEmailState {
    dialogOpen: boolean;
}

export const sendEmailState: SendEmailState = $state({
    dialogOpen: false,
});

export const sendEmailFormState = $state({
    email: configState.defaultEmail,
});
