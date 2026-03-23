import { types } from '$lib/wailsjs/go/models';
import { formatDate, formatTime } from '@station-manager/shared-utils';

export interface QsoEditState {
    original: types.Qso;
    id: number;
    logbook_id: number;
    session_id: number;
    call: string;
    freq: string;
    freq_rx: string;
    qso_date: string;
    time_on: string;
    time_off: string;
    rst_sent: string;
    rst_rcvd: string;
    mode: string;
    submode: string;
    name: string;
    qth: string;
    comment: string;
    notes: string;
    rig: string;
    rx_pwr: string;
    tx_pwr: string;
    band: string;
    station_callsign: string;
    ant_path: string;
    fromQso(this: QsoEditState, qso: types.Qso): void;
    toQso(this: QsoEditState): types.Qso;
}

export const qsoEditState: QsoEditState = $state({
    original: new types.Qso({}),
    id: 0,
    logbook_id: 0,
    session_id: 0,
    call: '',
    freq: '',
    freq_rx: '',
    qso_date: '',
    time_on: '',
    time_off: '',
    rst_sent: '',
    rst_rcvd: '',
    mode: '',
    submode: '',
    name: '',
    qth: '',
    comment: '',
    notes: '',
    rig: '',
    rx_pwr: '',
    tx_pwr: '',
    band: '',
    station_callsign: '',
    ant_path: '',
    fromQso(this: QsoEditState, qso: types.Qso): void {
        this.original = qso;
        this.id = qso.id;
        this.logbook_id = qso.logbook_id;
        this.session_id = qso.session_id;
        this.call = qso.call ?? '';
        this.rst_sent = qso.rst_sent ?? '';
        this.rst_rcvd = qso.rst_rcvd ?? '';
        this.mode = qso.mode ?? '';
        this.submode = qso.submode;
        this.freq = qso.freq ?? '';
        this.freq_rx = qso.freq_rx;
        this.name = qso.name;
        this.qth = qso.qth;
        this.comment = qso.comment;
        this.notes = qso.notes;
        this.qso_date = formatDate(qso.qso_date);
        this.time_on = formatTime(qso.time_on);
        this.time_off = formatTime(qso.time_off);
        this.rig = qso.rig;
        this.rx_pwr = qso.rx_pwr;
        this.tx_pwr = qso.tx_pwr;
        this.band = qso.band ?? '';
        this.station_callsign = qso.station_callsign;
        this.ant_path = qso.ant_path ?? '';
    },
    toQso(this: QsoEditState): types.Qso {
        let qsoObject: types.Qso;
        if (this.original) {
            qsoObject = types.Qso.createFrom(this.original);
        } else {
            qsoObject = new types.Qso({});
        }

        qsoObject.call = this.call;
        qsoObject.rst_rcvd = this.rst_rcvd;
        qsoObject.rst_sent = this.rst_sent;
        qsoObject.mode = this.mode;
        qsoObject.submode = this.submode;
        qsoObject.band = this.band;
        qsoObject.freq = this.freq;
        qsoObject.freq_rx = this.freq_rx;
        qsoObject.name = this.name;
        qsoObject.qth = this.qth;
        qsoObject.comment = this.comment;
        qsoObject.notes = this.notes;
        qsoObject.qso_date = this.qso_date;
        qsoObject.time_on = this.time_on;
        qsoObject.time_off = this.time_off;
        qsoObject.rx_pwr = this.rx_pwr;
        qsoObject.tx_pwr = this.tx_pwr;
        qsoObject.ant_path = this.ant_path;

        return qsoObject;
    },
});
