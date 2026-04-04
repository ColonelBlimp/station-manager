import {GetConfig} from "$lib/wailsjs/go/facade/Service";
import {LogError} from "$lib/wailsjs/runtime";
import {types} from "$lib/wailsjs/go/models";
import {configState} from "$lib/states/config-state.svelte";

export const prerender = true;
export const ssr = false;

export const load = async (): Promise<void> => {
    try {
        let cfg: types.AppConfig | null | undefined = await GetConfig();
        if (!cfg) {
            const msg = 'GetConfig() returned null or undefined; using defaults.';
            LogError(msg);
            cfg = new types.AppConfig();
        }

        configState.load(cfg);
    } catch (e: unknown) {

    }
}
