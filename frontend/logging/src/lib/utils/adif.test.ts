import { describe, it, expect } from 'vitest';
import { formatAdifRecord, type AdifQsoFields } from './adif';

/**
 * ADIF formatter spec — written test-first to pin the wire shape
 * before the QSO-submit code lands. The "complete record" test below
 * is the canonical spec: given a fully-populated draft, the output is
 * byte-identical to the expected ADIF string. All other tests pin
 * individual rules (omission of empty optionals, frequency precision,
 * date/time stripping, etc.).
 */

const baseFields: AdifQsoFields = {
    callsign: 'M0ABC',
    rstSent: '59',
    rstRcvd: '59',
    qsoDate: '2026-05-02',
    timeOn: '15:30',
    timeOff: '15:35',
    mode: 'USB',
    band: '20m',
    txFreqHz: 14_250_000,
    txPower: 100,
};

describe('formatAdifRecord — required fields', () => {
    it('emits CALL with length prefix', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<CALL:5>M0ABC');
    });

    it('emits QSO_DATE as YYYYMMDD (dashes stripped)', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<QSO_DATE:8>20260502');
    });

    it('emits TIME_ON as HHMM (colons stripped)', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<TIME_ON:4>1530');
    });

    it('emits TIME_OFF as HHMM', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<TIME_OFF:4>1535');
    });

    it('emits MODE pass-through', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<MODE:3>USB');
    });

    it('emits FREQ in MHz with 6 decimal places', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<FREQ:9>14.250000');
    });

    it('emits BAND', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<BAND:3>20m');
    });

    it('emits RST_SENT and RST_RCVD', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).toContain('<RST_SENT:2>59');
        expect(adif).toContain('<RST_RCVD:2>59');
    });

    it('always ends with <EOR>', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif.endsWith('<EOR>')).toBe(true);
    });
});

describe('formatAdifRecord — optional fields', () => {
    it('omits NAME when missing', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('NAME');
    });

    it('omits NAME when empty string', () => {
        const adif = formatAdifRecord({ ...baseFields, name: '' });
        expect(adif).not.toContain('NAME');
    });

    it('emits NAME when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, name: 'John' });
        expect(adif).toContain('<NAME:4>John');
    });

    it('omits QTH when missing/empty', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('QTH');
    });

    it('emits QTH when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, qth: 'London' });
        expect(adif).toContain('<QTH:6>London');
    });

    it('omits COMMENT when missing/empty', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('COMMENT');
    });

    it('emits COMMENT when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, comment: 'first contact' });
        expect(adif).toContain('<COMMENT:13>first contact');
    });

    it('omits SUBMODE when empty', () => {
        const adif = formatAdifRecord({ ...baseFields, subMode: '' });
        expect(adif).not.toContain('SUBMODE');
    });

    it('emits SUBMODE when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, mode: 'CW', subMode: 'CW-N' });
        expect(adif).toContain('<SUBMODE:4>CW-N');
    });
});

describe('formatAdifRecord — FREQ_RX (split mode)', () => {
    it('omits FREQ_RX when rxFreqHz is missing', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('FREQ_RX');
    });

    it('omits FREQ_RX when rxFreqHz === txFreqHz (not split)', () => {
        const adif = formatAdifRecord({ ...baseFields, rxFreqHz: 14_250_000 });
        expect(adif).not.toContain('FREQ_RX');
    });

    it('emits FREQ_RX when split', () => {
        const adif = formatAdifRecord({ ...baseFields, rxFreqHz: 14_300_000 });
        expect(adif).toContain('<FREQ_RX:9>14.300000');
    });
});

describe('formatAdifRecord — operator station (MY_* / STATION_CALLSIGN)', () => {
    it('omits STATION_CALLSIGN when missing/empty', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('STATION_CALLSIGN');
    });

    it('emits STATION_CALLSIGN when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, stationCallsign: 'M5OPR' });
        expect(adif).toContain('<STATION_CALLSIGN:5>M5OPR');
    });

    it('omits MY_GRIDSQUARE when missing/empty', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('MY_GRIDSQUARE');
    });

    it('emits MY_GRIDSQUARE when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, myGridSquare: 'IO91vl' });
        expect(adif).toContain('<MY_GRIDSQUARE:6>IO91vl');
    });

    it('omits MY_NAME when missing/empty', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('MY_NAME');
    });

    it('emits MY_NAME when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, myName: 'Marc' });
        expect(adif).toContain('<MY_NAME:4>Marc');
    });

    it('omits MY_RIG when missing/empty', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('MY_RIG');
    });

    it('emits MY_RIG when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, myRig: 'IC-7300' });
        expect(adif).toContain('<MY_RIG:7>IC-7300');
    });

    it('omits MY_ANTENNA when missing/empty', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('MY_ANTENNA');
    });

    it('emits MY_ANTENNA when non-empty', () => {
        const adif = formatAdifRecord({ ...baseFields, myAntenna: 'OCF dipole' });
        expect(adif).toContain('<MY_ANTENNA:10>OCF dipole');
    });

    it('does not confuse STATION_CALLSIGN with the contact CALL when both are set', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            callsign: 'M0ABC',
            stationCallsign: 'M5OPR',
        });
        expect(adif).toContain('<CALL:5>M0ABC');
        expect(adif).toContain('<STATION_CALLSIGN:5>M5OPR');
    });

    it('does not confuse MY_NAME (operator) with NAME (contact) when both are set', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            name: 'John',
            myName: 'Marc',
        });
        expect(adif).toContain('<NAME:4>John');
        expect(adif).toContain('<MY_NAME:4>Marc');
    });
});

describe('formatAdifRecord — TX_PWR', () => {
    it('omits TX_PWR when 0 ("not set")', () => {
        const adif = formatAdifRecord({ ...baseFields, txPower: 0 });
        expect(adif).not.toContain('TX_PWR');
    });

    it('omits TX_PWR when undefined', () => {
        const adif = formatAdifRecord({ ...baseFields, txPower: undefined });
        expect(adif).not.toContain('TX_PWR');
    });

    it('emits TX_PWR rounded to integer', () => {
        const adif = formatAdifRecord({ ...baseFields, txPower: 99.6 });
        expect(adif).toContain('<TX_PWR:3>100');
    });

    it('emits TX_PWR for typical 100W', () => {
        const adif = formatAdifRecord({ ...baseFields, txPower: 100 });
        expect(adif).toContain('<TX_PWR:3>100');
    });

    it('emits TX_PWR for amp-multiplied 200W', () => {
        const adif = formatAdifRecord({ ...baseFields, txPower: 200 });
        expect(adif).toContain('<TX_PWR:3>200');
    });
});

describe('formatAdifRecord — frequency formatting', () => {
    it('formats below 10 MHz with 6 decimal places (no leading zero stripped)', () => {
        const adif = formatAdifRecord({ ...baseFields, txFreqHz: 7_100_000 });
        expect(adif).toContain('<FREQ:8>7.100000');
    });

    it('formats with sub-kHz precision (Hz preserved)', () => {
        const adif = formatAdifRecord({ ...baseFields, txFreqHz: 14_250_500 });
        expect(adif).toContain('<FREQ:9>14.250500');
    });

    it('formats VHF frequencies', () => {
        const adif = formatAdifRecord({ ...baseFields, txFreqHz: 144_300_000 });
        expect(adif).toContain('<FREQ:10>144.300000');
    });
});

describe('formatAdifRecord — date / time stripping', () => {
    it('strips dashes from QSO_DATE in any month', () => {
        const adif = formatAdifRecord({ ...baseFields, qsoDate: '2024-12-31' });
        expect(adif).toContain('<QSO_DATE:8>20241231');
    });

    it('strips colons from TIME_ON / TIME_OFF', () => {
        const adif = formatAdifRecord({ ...baseFields, timeOn: '07:05', timeOff: '07:25' });
        expect(adif).toContain('<TIME_ON:4>0705');
        expect(adif).toContain('<TIME_OFF:4>0725');
    });
});

describe('formatAdifRecord — complete record (canonical spec)', () => {
    it('produces a byte-identical record for a fully-populated draft including MY_* fields', () => {
        const adif = formatAdifRecord({
            callsign: 'M0ABC',
            rstSent: '59',
            rstRcvd: '59',
            name: 'John',
            qth: 'London',
            comment: 'first contact',
            qsoDate: '2026-05-02',
            timeOn: '15:30',
            timeOff: '15:35',
            mode: 'USB',
            subMode: '',
            txFreqHz: 14_250_000,
            band: '20m',
            txPower: 100,
            stationCallsign: 'M5OPR',
            myGridSquare: 'IO91vl',
            myName: 'Marc',
            myRig: 'IC-7300',
            myAntenna: 'OCF dipole',
        });

        const expected = [
            '<CALL:5>M0ABC',
            '<QSO_DATE:8>20260502',
            '<TIME_ON:4>1530',
            '<TIME_OFF:4>1535',
            '<MODE:3>USB',
            '<FREQ:9>14.250000',
            '<BAND:3>20m',
            '<RST_SENT:2>59',
            '<RST_RCVD:2>59',
            '<TX_PWR:3>100',
            '<NAME:4>John',
            '<QTH:6>London',
            '<COMMENT:13>first contact',
            '<STATION_CALLSIGN:5>M5OPR',
            '<MY_GRIDSQUARE:6>IO91vl',
            '<MY_NAME:4>Marc',
            '<MY_RIG:7>IC-7300',
            '<MY_ANTENNA:10>OCF dipole',
            '<EOR>',
        ].join('\n');

        expect(adif).toBe(expected);
    });

    it('produces a byte-identical record for a split-mode QSO with submode and MY_* fields', () => {
        const adif = formatAdifRecord({
            callsign: 'JA1ABC',
            rstSent: '599',
            rstRcvd: '589',
            qsoDate: '2026-05-02',
            timeOn: '08:15',
            timeOff: '08:18',
            mode: 'CW',
            subMode: 'CW-N',
            txFreqHz: 14_025_000,
            rxFreqHz: 14_028_000,
            band: '20m',
            txPower: 200,
            stationCallsign: 'M5OPR',
            myGridSquare: 'IO91vl',
        });

        const expected = [
            '<CALL:6>JA1ABC',
            '<QSO_DATE:8>20260502',
            '<TIME_ON:4>0815',
            '<TIME_OFF:4>0818',
            '<MODE:2>CW',
            '<FREQ:9>14.025000',
            '<BAND:3>20m',
            '<RST_SENT:3>599',
            '<RST_RCVD:3>589',
            '<SUBMODE:4>CW-N',
            '<FREQ_RX:9>14.028000',
            '<TX_PWR:3>200',
            '<STATION_CALLSIGN:5>M5OPR',
            '<MY_GRIDSQUARE:6>IO91vl',
            '<EOR>',
        ].join('\n');

        expect(adif).toBe(expected);
    });

    it('produces a minimal record (required fields only, no optionals or MY_*)', () => {
        const adif = formatAdifRecord({
            callsign: 'M0ABC',
            rstSent: '59',
            rstRcvd: '59',
            qsoDate: '2026-05-02',
            timeOn: '15:30',
            timeOff: '15:35',
            mode: 'USB',
            txFreqHz: 14_250_000,
            band: '20m',
        });

        const expected = [
            '<CALL:5>M0ABC',
            '<QSO_DATE:8>20260502',
            '<TIME_ON:4>1530',
            '<TIME_OFF:4>1535',
            '<MODE:3>USB',
            '<FREQ:9>14.250000',
            '<BAND:3>20m',
            '<RST_SENT:2>59',
            '<RST_RCVD:2>59',
            '<EOR>',
        ].join('\n');

        expect(adif).toBe(expected);
    });
});
