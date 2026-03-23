import { configState } from '$lib/states/config-state.svelte';
import { types } from '$lib/wailsjs/go/models';
import {
    formatCatKHzToDottedMHz,
    frequencyToBandFromDottedMHz,
} from '@station-manager/shared-utils';
import { extractRemoteTime, getDateUTC, getTimeUTC } from '@station-manager/shared-utils';
import { catState } from '$lib/states/cat-state.svelte';
import { getModeBySubmode } from '$lib/utils/mode';
import { QSO_TIMER_INTERVAL_MS } from '$lib/constants/timers';

const CAT_MAPPINGS: { [K in keyof CatForQsoPayload]: K } = {
    cat_vfoa_freq: 'cat_vfoa_freq',
    cat_vfob_freq: 'cat_vfob_freq',
    cat_main_mode: 'cat_main_mode',
};

export interface QsoTimerState {
    elapsed: number;
    running: boolean;
}

export const qsoTimerState: QsoTimerState = $state({ elapsed: 0, running: false });

let elapsedIntervalID: number | null = null;

export type CatForQsoPayload = Partial<
    Pick<QsoState, 'cat_vfoa_freq' | 'cat_vfob_freq' | 'cat_main_mode'>
>;

export interface CatDrivenFields {
    cat_vfoa_freq: string;
    cat_vfob_freq: string;
    cat_main_mode: string;
}

export interface QsoState extends CatDrivenFields {
    original?: types.Qso;
    call: string;
    qso_date: string;
    time_on: string;
    time_off: string;
    rst_sent: string;
    rst_rcvd: string;
    mode: string;
    name: string;
    qth: string;
    comment: string;
    rx_pwr: string;
    tx_pwr: string;
    notes: string;
    cqz: string;
    rig: string;
    qso_random: string;
    qslWanted: string;

    country_name: string;
    isNewEntity: boolean;
    ccode: string;
    ant_path: string;
    short_path_distance: string;
    short_path_bearing: string;
    long_path_distance: string;
    long_path_bearing: string;
    remote_time: string;
    remote_offset: string;

    srx: string;
    stx: string;
    operator: string;
    owner_callsign: string;

    contact_history: types.ContactHistory[];

    web: string;
    email: string;

    cat_enabled: boolean;
    cat_vfoa_freq: string;
    cat_vfob_freq: string;
    cat_main_mode: string;

    reset(this: QsoState): void;

    fromQso(this: QsoState, qso: types.Qso): void;

    toQso(this: QsoState): types.Qso;

    updateFromCAT(this: QsoState, data: CatForQsoPayload): void;

    startTimer(this: QsoState): void;

    stopTimer(this: QsoState): void;

    resetTimer(this: QsoState): void;

    isTimerRunning(this: QsoState): boolean;
}

export const qsoState: QsoState = $state({
    original: undefined,
    call: '',
    qso_date: '',
    time_on: '',
    time_off: '',
    rst_sent: '',
    rst_rcvd: '',
    mode: '',
    name: '',
    qth: '',
    comment: '',
    rx_pwr: '',
    tx_pwr: '',
    notes: '',
    cqz: '',
    web: '',
    email: '',
    rig: '',
    qso_random: '',
    qslWanted: '',

    country_name: '',
    isNewEntity: false,
    ccode: '',
    ant_path: '',
    short_path_distance: '',
    short_path_bearing: '',
    long_path_distance: '',
    long_path_bearing: '',
    remote_time: '',
    remote_offset: '',

    contact_history: [],

    // Contest
    srx: '',
    stx: '',
    operator: '',
    owner_callsign: '',

    cat_enabled: false,
    cat_vfoa_freq: '',
    cat_vfob_freq: '',
    cat_main_mode: '',

    reset(this: QsoState): void {
        if (!this.cat_enabled) {
            // These checks allow us to maintain user entered values when CAT is disabled
            // as CAT values are used directly in the UI.
            if (this.cat_vfoa_freq === '') {
                this.cat_vfoa_freq = configState.default_freq;
            }
            if (this.cat_main_mode === '') {
                this.cat_main_mode = configState.default_mode;
            }
        }
        rstHelper(this);
        randomQsoHelper(this);
        this.call = '';
        this.name = '';
        this.qth = '';
        this.comment = '';
        this.notes = '';
        this.qso_date = getDateUTC();
        this.time_on = getTimeUTC();
        this.time_off = getTimeUTC();
        this.country_name = '';
        this.isNewEntity = false;
        this.ccode = '';
        this.remote_time = '';
        this.remote_offset = '';
        this.contact_history = [];
        this.qslWanted = '';

        this.srx = '';
        this.stx = '';
        this.operator = '';
        this.owner_callsign = '';

        this.email = '';
        this.rx_pwr = '';
        this.tx_pwr = '';
        this.rig = '';
        this.web = '';
    },

    /**
     * Updates the current QsoState instance based on the provided QSO data. This is called when a new QSO is loaded
     * from the backend.
     *
     * @return {void} This method does not return a value.
     */
    fromQso(this: QsoState, qso: types.Qso): void {
        if (!qso) return;
        this.original = qso;
        this.call = qso.call ?? '';
        this.name = qso.name ?? '';
        this.qth = qso.qth ?? '';
        this.email = qso.email ?? '';
        this.web = qso.web ?? '';
        this.country_name = qso.country ?? '';
        this.isNewEntity = qso.country_details.is_new_entity ?? false;
        this.ccode = qso.country_details.ccode ?? '';
        this.remote_time = extractRemoteTime(qso.country_details.local_time);
        this.remote_offset = qso.country_details.time_offset ?? '';
        this.ant_path = qso.ant_path ?? 'S';
        this.long_path_distance = qso.country_details.long_path_distance ?? '';
        this.long_path_bearing = qso.country_details.long_path_bearing ?? '';
        this.short_path_distance = qso.country_details.short_path_distance ?? '';
        this.short_path_bearing = qso.country_details.short_path_bearing ?? '';
        this.contact_history = qso.contact_history ?? [];
        this.rig = qso.rig ?? '';

        rstHelper(this);
        randomQsoHelper(this);
    },
    /**
     * Transforms the current QsoState instance into a Qso object. This is called when the user clicks the "Log Contact"
     * before passing the QSO data to the backend.
     *
     * @return {types.Qso} A new Qso object created from the current state of the QsoState instance.
     */
    toQso(this: QsoState): types.Qso {
        const base = this.original ? types.Qso.createFrom(this.original) : new types.Qso({});
        base.call = this.call;
        base.name = this.name;
        base.qth = this.qth;
        base.comment = this.comment;
        base.notes = this.notes;

        base.rst_sent = this.rst_sent;
        base.rst_rcvd = this.rst_rcvd;

        base.mode = getModeBySubmode(this.cat_main_mode);
        base.submode = this.cat_main_mode;

        // We use the catState object here as it has the 'real-time' cat values. The qsoObject has the shadowed values, and
        // particularly the 'freq' fields are formatted for display to the user. This way of doing things allows us to
        // bind values directly to/from the UI so that we capture values entered by the user even when CAT is disabled.
        if (catState.select === 'VFO-A' || catState.select === '') {
            if (catState.split === 'OFF' || catState.split === '') {
                base.freq = qsoState.cat_vfoa_freq;
                base.band = frequencyToBandFromDottedMHz(qsoState.cat_vfoa_freq);
            } else {
                base.freq = qsoState.cat_vfoa_freq;
                base.band = frequencyToBandFromDottedMHz(qsoState.cat_vfoa_freq);
                base.freq_rx = qsoState.cat_vfob_freq;
                base.band_rx = frequencyToBandFromDottedMHz(qsoState.cat_vfob_freq);
            }
        } else {
            if (catState.split === 'OFF' || catState.split === '') {
                base.freq = qsoState.cat_vfob_freq;
                base.band = frequencyToBandFromDottedMHz(qsoState.cat_vfob_freq);
            } else {
                base.freq = qsoState.cat_vfob_freq;
                base.band = frequencyToBandFromDottedMHz(qsoState.cat_vfob_freq);
                base.freq_rx = qsoState.cat_vfoa_freq;
                base.band_rx = frequencyToBandFromDottedMHz(qsoState.cat_vfoa_freq);
            }
        }

        // Sanitize the freq and freq_rx
        base.freq = base.freq.replace(/[^0-9]/g, '');
        base.freq_rx = base.freq_rx.replace(/[^0-9]/g, '');

        base.qso_date = this.qso_date;
        base.time_on = this.time_on;
        base.time_off = this.time_off;
        base.tx_pwr = this.tx_pwr;
        base.rx_pwr = this.rx_pwr;
        base.rig = this.rig;

        // Note: the backend set the ant_path to 'S' as a default.
        if (this.ant_path === 'S') {
            base.ant_az = this.short_path_bearing;
            base.distance = this.short_path_distance;
        } else {
            base.ant_az = this.long_path_bearing;
            base.distance = this.long_path_distance;
        }

        base.srx = this.srx;
        base.stx = this.stx;
        base.operator = this.operator;
        base.owner_callsign = this.owner_callsign;

        return base;
    },
    updateFromCAT(this: QsoState, data: CatForQsoPayload): void {
        if (!data) return;

        (Object.entries(data) as Array<[keyof CatForQsoPayload, string | undefined]>).forEach(
            ([key, value]) => {
                if (value === undefined) return;
                const catKey = CAT_MAPPINGS[key];
                if (!catKey) return;
                switch (catKey) {
                    case 'cat_vfoa_freq':
                    case 'cat_vfob_freq':
                        // We do this formatting here because this value is displayed to the user. If we don't format it
                        // here, we will have to do it at the UI level, which means that the value cannot be bound
                        // as it comes from a function rather than the catState object. Therefore, any value entered by
                        // the user will not be reflected in the catState object.
                        value = formatCatKHzToDottedMHz(value);
                        break;
                }
                this[catKey] = value;
            }
        );

        this.cat_enabled = true;
    },
    startTimer(this: QsoState): void {
        // If a timer is already active, don't start another.
        if (elapsedIntervalID !== null) {
            return;
        }

        qsoState.time_on = getTimeUTC();
        qsoState.time_off = qsoState.time_on;

        // Store interval id in a module-scope variable so we can reliably clear it.
        elapsedIntervalID = window.setInterval(() => {
            // Always write through the shared qsoState instance to avoid any
            // confusion around `this` binding inside the interval callback.
            qsoState.time_off = getTimeUTC();
        }, QSO_TIMER_INTERVAL_MS);

        // Mark the timer as running so subscribers know.
        qsoTimerState.running = true;
    },
    stopTimer(this: QsoState): void {
        if (elapsedIntervalID !== null) {
            clearInterval(elapsedIntervalID);
            elapsedIntervalID = null;
        }
        qsoTimerState.running = false;
        qsoState.time_on = getTimeUTC();
        qsoState.time_off = qsoState.time_on;
    },
    resetTimer(this: QsoState): void {
        // Ensure no interval continues running.
        this.stopTimer();

        const date = getDateUTC();
        const time = getTimeUTC();

        this.qso_date = date;
        this.time_on = time;
        this.time_off = time;
    },
    isTimerRunning(this: QsoState): boolean {
        return elapsedIntervalID !== null;
    },
});

const rstHelper = (qso: QsoState): void => {
    if (!qso) return;
    if (qso.cat_main_mode === 'CW-U' || qso.cat_main_mode === 'CW-L') {
        qso.rst_rcvd = '599';
        qso.rst_sent = '599';
    } else {
        qso.rst_rcvd = '59';
        qso.rst_sent = '59';
    }
};

const randomQsoHelper = (qso: QsoState): void => {
    if (!qso) return;
    qso.qso_random = configState.default_random_qso ? 'Y' : 'N';
};
