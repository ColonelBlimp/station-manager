import { types } from '$lib/wailsjs/go/models';

export interface ConfigState {
    default_freq: string;
    default_mode: string;
    default_power: number;
    logbook: types.Logbook;
    // Actually part of the LoggingStation struct, but is needed by the UI
    owners_callsign: string;
    rig_name: string;
    use_power_multiplier: boolean;
    default_random_qso: boolean;
    power_multiplier: number;
    default_fwd_email: string;
    qrz_view_url: string;
    load(this: ConfigState, cfg: types.UiConfig): void;
}

export const configState: ConfigState = $state({
    default_freq: '',
    default_mode: '',
    logbook: new types.Logbook(),
    owners_callsign: '',
    rig_name: '',
    use_power_multiplier: false,
    default_random_qso: true,
    power_multiplier: 1,
    default_power: 5,
    default_fwd_email: '',
    qrz_view_url: '',

    load(this: ConfigState, cfg: types.UiConfig): void {
        this.default_freq = cfg.default_freq;
        this.default_mode = cfg.default_mode;
        this.logbook = cfg.logbook;
        this.owners_callsign = cfg.owners_callsign;
        this.rig_name = cfg.rig_name;
        this.use_power_multiplier = cfg.use_power_multiplier;
        this.default_random_qso = cfg.default_is_random_qso;
        this.power_multiplier = cfg.power_multiplier;
        this.default_power = cfg.default_tx_power;
        this.default_fwd_email = cfg.default_fwd_email;
        this.qrz_view_url = cfg.qrz_view_url;
    },
});
