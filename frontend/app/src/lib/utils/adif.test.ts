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

describe('formatAdifRecord — contacted-station enrichment', () => {
    it('emits COUNTRY / CQZ / ITUZ / DXCC / GRIDSQUARE when present', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            country: 'Russia',
            cqZone: '17',
            ituZone: '30',
            dxcc: '54',
            gridsquare: 'KO85',
        });
        expect(adif).toContain('<COUNTRY:6>Russia');
        expect(adif).toContain('<CQZ:2>17');
        expect(adif).toContain('<ITUZ:2>30');
        expect(adif).toContain('<DXCC:2>54');
        expect(adif).toContain('<GRIDSQUARE:4>KO85');
    });

    it('omits each contacted-station field when empty (station unknown)', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('<COUNTRY:');
        expect(adif).not.toContain('<CQZ:');
        expect(adif).not.toContain('<ITUZ:');
        expect(adif).not.toContain('<DXCC:');
        expect(adif).not.toContain('<GRIDSQUARE:');
    });

    it('byte-counts a non-ASCII country name correctly', () => {
        // "Côte d'Ivoire" — the ô is 2 bytes in UTF-8, so the prefix must
        // be the byte length, not the JS string length.
        const adif = formatAdifRecord({ ...baseFields, country: "Côte d'Ivoire" });
        expect(adif).toContain("<COUNTRY:14>Côte d'Ivoire");
    });
});

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

describe('formatAdifRecord — QSO_RANDOM', () => {
    it('omits QSO_RANDOM when missing', () => {
        const adif = formatAdifRecord(baseFields);
        expect(adif).not.toContain('QSO_RANDOM');
    });

    it('emits QSO_RANDOM=Y when set to Y', () => {
        const adif = formatAdifRecord({ ...baseFields, qsoRandom: 'Y' });
        expect(adif).toContain('<QSO_RANDOM:1>Y');
    });

    it('emits QSO_RANDOM=N when set to N', () => {
        const adif = formatAdifRecord({ ...baseFields, qsoRandom: 'N' });
        expect(adif).toContain('<QSO_RANDOM:1>N');
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

    it('emits OPERATOR when set', () => {
        const adif = formatAdifRecord({ ...baseFields, operator: 'M5OPR' });
        expect(adif).toContain('<OPERATOR:5>M5OPR');
    });

    it('emits OWNER_CALLSIGN when set', () => {
        const adif = formatAdifRecord({ ...baseFields, ownerCallsign: 'G7OWN' });
        expect(adif).toContain('<OWNER_CALLSIGN:5>G7OWN');
    });

    it('emits MY_LAT and MY_LON when set', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            myLat: 'N051 30.000',
            myLon: 'W001 00.000',
        });
        expect(adif).toContain('<MY_LAT:11>N051 30.000');
        expect(adif).toContain('<MY_LON:11>W001 00.000');
    });

    it('emits MY_CITY, MY_COUNTRY, MY_POSTAL_CODE, MY_STREET when set', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            myStreet: '1 Main St',
            myCity: 'London',
            myPostalCode: 'SW1A 1AA',
            myCountry: 'United Kingdom',
        });
        expect(adif).toContain('<MY_STREET:9>1 Main St');
        expect(adif).toContain('<MY_CITY:6>London');
        expect(adif).toContain('<MY_POSTAL_CODE:8>SW1A 1AA');
        expect(adif).toContain('<MY_COUNTRY:14>United Kingdom');
    });

    it('emits MY_ALTITUDE, MY_CQ_ZONE, MY_ITU_ZONE, MY_DXCC when set', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            myAltitude: '120',
            myCqZone: '14',
            myItuZone: '27',
            myDxcc: '223',
        });
        expect(adif).toContain('<MY_ALTITUDE:3>120');
        expect(adif).toContain('<MY_CQ_ZONE:2>14');
        expect(adif).toContain('<MY_ITU_ZONE:2>27');
        expect(adif).toContain('<MY_DXCC:3>223');
    });

    it('emits MY_MORSE_KEY_TYPE and MY_MORSE_KEY_INFO when set', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            myMorseKeyType: 'PADDLE',
            myMorseKeyInfo: 'Begali Sculpture',
        });
        expect(adif).toContain('<MY_MORSE_KEY_TYPE:6>PADDLE');
        expect(adif).toContain('<MY_MORSE_KEY_INFO:16>Begali Sculpture');
    });

    it('emits ANT_AZ when set', () => {
        const adif = formatAdifRecord({ ...baseFields, antAz: '273.4' });
        expect(adif).toContain('<ANT_AZ:5>273.4');
    });

    it('emits ANT_PATH when set', () => {
        expect(formatAdifRecord({ ...baseFields, antPath: 'L' })).toContain('<ANT_PATH:1>L');
        expect(formatAdifRecord({ ...baseFields, antPath: 'S' })).toContain('<ANT_PATH:1>S');
    });

    it('omits all new MY_* fields when missing', () => {
        const adif = formatAdifRecord(baseFields);
        for (const tag of [
            'OPERATOR',
            'OWNER_CALLSIGN',
            'MY_LAT',
            'MY_LON',
            'MY_STREET',
            'MY_CITY',
            'MY_POSTAL_CODE',
            'MY_COUNTRY',
            'MY_ALTITUDE',
            'MY_CQ_ZONE',
            'MY_ITU_ZONE',
            'MY_DXCC',
            'MY_MORSE_KEY_TYPE',
            'MY_MORSE_KEY_INFO',
            'ANT_AZ',
            'ANT_PATH',
        ]) {
            expect(adif).not.toContain(tag);
        }
    });
});

describe('formatAdifRecord — full MY_* canonical record', () => {
    it('emits all MY_* fields in stable order, byte-identical', () => {
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
            stationCallsign: 'M5OPR',
            operator: 'M5OPR',
            ownerCallsign: 'G7OWN',
            myGridSquare: 'IO91vl',
            myLat: 'N051 30.000',
            myLon: 'W001 00.000',
            myStreet: '1 Main St',
            myCity: 'London',
            myPostalCode: 'SW1A 1AA',
            myCountry: 'United Kingdom',
            myAltitude: '120',
            myCqZone: '14',
            myItuZone: '27',
            myDxcc: '223',
            myName: 'Marc',
            myRig: 'IC-7300',
            myAntenna: 'OCF dipole',
            myMorseKeyType: 'PADDLE',
            myMorseKeyInfo: 'Begali Sculpture',
            antAz: '273.4',
            antPath: 'S',
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
            '<STATION_CALLSIGN:5>M5OPR',
            '<OPERATOR:5>M5OPR',
            '<OWNER_CALLSIGN:5>G7OWN',
            '<MY_GRIDSQUARE:6>IO91vl',
            '<MY_LAT:11>N051 30.000',
            '<MY_LON:11>W001 00.000',
            '<MY_STREET:9>1 Main St',
            '<MY_CITY:6>London',
            '<MY_POSTAL_CODE:8>SW1A 1AA',
            '<MY_COUNTRY:14>United Kingdom',
            '<MY_ALTITUDE:3>120',
            '<MY_CQ_ZONE:2>14',
            '<MY_ITU_ZONE:2>27',
            '<MY_DXCC:3>223',
            '<MY_NAME:4>Marc',
            '<MY_RIG:7>IC-7300',
            '<MY_ANTENNA:10>OCF dipole',
            '<MY_MORSE_KEY_TYPE:6>PADDLE',
            '<MY_MORSE_KEY_INFO:16>Begali Sculpture',
            '<ANT_AZ:5>273.4',
            '<ANT_PATH:1>S',
            '<EOR>',
        ].join('\n');

        expect(adif).toBe(expected);
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

describe('formatAdifRecord — RX_PWR', () => {
    it('emits RX_PWR for a typical value', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '100' });
        expect(adif).toContain('<RX_PWR:3>100');
    });

    it('emits a fractional value', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '2.5' });
        expect(adif).toContain('<RX_PWR:3>2.5');
    });

    it('emits a small decimal literally, never in exponent notation', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '0.0000001' });
        expect(adif).toContain('<RX_PWR:9>0.0000001');
        expect(adif).not.toContain('e-');
    });

    it('normalises a leading dot (".5" → "0.5")', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '.5' });
        expect(adif).toContain('<RX_PWR:3>0.5');
    });

    it('normalises a trailing dot ("100." → "100")', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '100.' });
        expect(adif).toContain('<RX_PWR:3>100');
    });

    it('omits RX_PWR when blank', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '' });
        expect(adif).not.toContain('RX_PWR');
    });

    it('omits RX_PWR for zero ("not set")', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '0' });
        expect(adif).not.toContain('RX_PWR');
    });

    it('omits non-numeric junk instead of emitting invalid ADIF', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '100W' });
        expect(adif).not.toContain('RX_PWR');
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

describe('formatAdifRecord — Details panel fields', () => {
    it('emits RX_PWR when set', () => {
        const adif = formatAdifRecord({ ...baseFields, rxPwr: '100' });
        expect(adif).toContain('<RX_PWR:3>100');
    });

    it('emits RIG when set', () => {
        const adif = formatAdifRecord({ ...baseFields, rig: 'IC-7300 + linear' });
        expect(adif).toContain('<RIG:16>IC-7300 + linear');
    });

    it('emits NOTES (separate from COMMENT) when set', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            comment: 'shared during QSO',
            notes: 'private record',
        });
        expect(adif).toContain('<COMMENT:17>shared during QSO');
        expect(adif).toContain('<NOTES:14>private record');
    });

    it('emits APP_SM_REQUEST_QSL=Y when true', () => {
        const adif = formatAdifRecord({ ...baseFields, appSmRequestQsl: true });
        expect(adif).toContain('<APP_SM_REQUEST_QSL:1>Y');
    });

    it('omits APP_SM_REQUEST_QSL when false (no Y/N noise for the common case)', () => {
        const adif = formatAdifRecord({ ...baseFields, appSmRequestQsl: false });
        expect(adif).not.toContain('APP_SM_REQUEST_QSL');
    });

    it('omits all Details fields when unset', () => {
        const adif = formatAdifRecord(baseFields);
        for (const tag of ['RX_PWR', 'RIG', 'NOTES', 'APP_SM_REQUEST_QSL']) {
            expect(adif).not.toContain(tag);
        }
    });
});

describe('formatAdifRecord — UTF-8 length prefixes', () => {
    // The daemon parser at internal/adif/parse.go slices by BYTE, not
    // by UTF-16 code unit. JS string.length under-counts every
    // non-ASCII character (codepoints in the U+0080+ range need 2+
    // UTF-8 bytes). A wrong prefix corrupts the value and breaks the
    // next tag boundary. These tests pin the byte-counted contract.

    it('emits byte length for an accented NAME', () => {
        // "José" — UTF-16 code units: 4. UTF-8 bytes: J(1)+o(1)+s(1)+é(2) = 5.
        const adif = formatAdifRecord({ ...baseFields, name: 'José' });
        expect(adif).toContain('<NAME:5>José');
    });

    it('emits byte length for an accented MY_COUNTRY', () => {
        // "Côte d'Ivoire" — UTF-16: 13. UTF-8: ô is 2 bytes, rest ASCII → 14.
        const adif = formatAdifRecord({ ...baseFields, myCountry: "Côte d'Ivoire" });
        expect(adif).toContain("<MY_COUNTRY:14>Côte d'Ivoire");
    });

    it('emits byte length for a multibyte COMMENT', () => {
        // Mix of 2-byte (ñ) and 3-byte (CJK 漢字) codepoints.
        // UTF-16 code units: 9 (every glyph is BMP).
        // UTF-8 bytes: M(1)+a(1)+ñ(2)+a(1)+n(1)+a(1)+ (1)+漢(3)+字(3) = 14.
        const adif = formatAdifRecord({ ...baseFields, comment: 'Mañana 漢字' });
        expect(adif).toContain('<COMMENT:14>Mañana 漢字');
    });

    it('still produces correct lengths for pure ASCII', () => {
        // Regression guard: the UTF-8 path must agree with JS .length
        // for ASCII-only inputs.
        const adif = formatAdifRecord({ ...baseFields, name: 'Bob' });
        expect(adif).toContain('<NAME:3>Bob');
    });
});

describe('formatAdifRecord — QSO_DATE_OFF (midnight rollover)', () => {
    it('emits QSO_DATE_OFF (dashes stripped) when the QSO crossed midnight', () => {
        const adif = formatAdifRecord({
            ...baseFields,
            qsoDate: '2026-05-02',
            timeOn: '23:59',
            timeOff: '00:03',
            qsoDateOff: '2026-05-03',
        });
        expect(adif).toContain('<QSO_DATE_OFF:8>20260503');
    });

    it('omits QSO_DATE_OFF when the field is absent or empty (builder is generic; the live path always supplies it)', () => {
        expect(formatAdifRecord(baseFields)).not.toContain('<QSO_DATE_OFF:');
        expect(formatAdifRecord({ ...baseFields, qsoDateOff: '' })).not.toContain('<QSO_DATE_OFF:');
    });
});
