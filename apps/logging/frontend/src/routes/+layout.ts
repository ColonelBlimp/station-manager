import { loadCatStateValues } from '$lib/stores/cat-state-store';
import { handleAsyncError } from '$lib/utils/error-handler';
import { FetchUiConfig } from '$lib/wailsjs/go/facade/Service';
import { LogError } from '$lib/wailsjs/runtime';
import { types } from '$lib/wailsjs/go/models';
import { configState } from '$lib/states/config-state.svelte';
import { qsoState } from '$lib/states/new-qso-state.svelte';

export const prerender = true;
export const ssr = false;

/**
 * Asynchronously loads and initializes application layout data.
 *
 * This function attempts to load necessary configuration data for the
 * application. If an error occurs during the configuration loading process,
 * it logs the error message for debugging purposes.
 */
export const load = async (): Promise<void> => {
    try {
        const cfg: types.UiConfig | null | undefined = await FetchUiConfig();
        let activeCfg = cfg;
        if (!activeCfg) {
            const msg = 'UiConfig fetch returned null or undefined; using defaults.';
            LogError(msg);
            activeCfg = new types.UiConfig();
        }
        // Load the configuration into the state object
        configState.load(activeCfg);
        // Reset the qsoState so that it reflects some of the default settings as we don't know yet
        // whether CAT is enabled.
        qsoState.reset();
        await loadCatStateValues();
    } catch (e: unknown) {
        handleAsyncError(e, '+layout.ts->load: LayoutData');
    }
};
