import { FetchUiConfig, GetLogbookList } from '$lib/wailsjs/go/facade/Service';
import { LogError } from '$lib/wailsjs/runtime';
import { types } from '$lib/wailsjs/go/models';
import { configState } from '$lib/states/config-state.svelte';
import { handleAsyncError } from '$lib/utils/error-handler';
import { logbookListState } from '$lib/states/logbook-list-state.svelte';

export const prerender = true;
export const ssr = false;

export const load = async (): Promise<void> => {
    try {
        let activeCfg: types.UiConfig = await FetchUiConfig();
        if (!activeCfg) {
            const msg = 'UiConfig fetch returned null or undefined; using defaults.';
            LogError(msg);
            activeCfg = new types.UiConfig();
        }
        // Load the configuration into the state object
        configState.load(activeCfg);
        logbookListState.list = await GetLogbookList();
    } catch (e: unknown) {
        handleAsyncError(e, '+layout.ts->load: LayoutData');
    }
};
