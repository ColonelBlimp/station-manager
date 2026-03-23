import { WORKED_TAB_TITLE } from '$lib/ui/logging/panels/constants';

export interface AppState {
    activePanel: string;
}

export const appState: AppState = $state({
    activePanel: WORKED_TAB_TITLE,
});
