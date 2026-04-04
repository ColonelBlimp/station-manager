import {types} from "$lib/wailsjs/go/models";

export interface ConfigState {
    cfg: types.AppConfig;
    load(this: ConfigState, cfg: types.AppConfig): void;
}

export const configState: ConfigState = $state({
    cfg: new types.AppConfig(),
    load(this: ConfigState, cfg: types.AppConfig): void {
        this.cfg = cfg;
    }
});
