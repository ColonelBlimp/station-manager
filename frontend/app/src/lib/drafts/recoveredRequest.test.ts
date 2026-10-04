/*
    The exact request a recovered Log sends (ADR 0085 RS12, RS20).

      RQ1 Every saved field and the original times go out as saved; a blank end
          time stays blank (it never becomes "now").
      RQ2 The rig values are the recovered ones — the correction if any, else
          the original reading — never the live rig.
      RQ3 OPERATOR, MY_NAME and MY_GRIDSQUARE are the saved ones; a known-empty
          OPERATOR or MY_NAME is left out, so the server's check compares it
          with what it would stamp today.
      RQ4 Enrichment extras for the same call are carried, as for any QSO.
*/
import { describe, expect, it } from 'vitest';
import { buildRecoveredAdif } from './recoveredRequest';
import { sampleRecord } from './savedDraft.fixture';

const tag = (adif: string, name: string): string | null => {
    const m = new RegExp(`<${name}:(\\d+)>`, 'i').exec(adif);
    return m === null
        ? null
        : adif.slice(m.index + m[0].length, m.index + m[0].length + Number(m[1]));
};

describe('buildRecoveredAdif', () => {
    it('RQ1 saved fields and times as saved; a blank end time stays blank', () => {
        const r = sampleRecord();
        const adif = buildRecoveredAdif(r, r.fields, r.rig, {});
        expect(tag(adif, 'CALL')).toBe('G0ABC');
        expect(tag(adif, 'QSO_DATE')).toBe('20260930');
        expect(tag(adif, 'TIME_ON')).toBe('120000');
        expect(tag(adif, 'TIME_OFF')).toBe(''); // blank, never "now"
        expect(tag(adif, 'RST_SENT')).toBe('59');
        expect(tag(adif, 'RST_RCVD')).toBe('57');
        expect(tag(adif, 'NAME')).toBe('Bob');
        expect(tag(adif, 'COMMENT')).toBe('tnx');
    });

    it('RQ2 the recovered rig values, never the live rig', () => {
        const r = sampleRecord();
        const corrected = { ...r.rig, freqHz: 7_074_000, band: '40m', adifMode: 'CW', subMode: '' };
        const adif = buildRecoveredAdif(r, r.fields, corrected, {});
        expect(tag(adif, 'FREQ')).toBe('7.074000');
        expect(tag(adif, 'BAND')).toBe('40m');
        expect(tag(adif, 'MODE')).toBe('CW');
        expect(tag(adif, 'SUBMODE')).toBeNull();
    });

    it('RQ3 the saved attribution; known-empty values are left out', () => {
        const r = sampleRecord();
        const adif = buildRecoveredAdif(r, r.fields, r.rig, {});
        expect(tag(adif, 'OPERATOR')).toBe('7Q5MLV');
        expect(tag(adif, 'MY_NAME')).toBe('Marc');
        expect(tag(adif, 'MY_GRIDSQUARE')).toBe('KH66');
        const empty = sampleRecord({ attribution: { myRig: '', operator: '', myName: '' } });
        const e = buildRecoveredAdif(empty, empty.fields, empty.rig, {});
        expect(tag(e, 'OPERATOR')).toBeNull();
        expect(tag(e, 'MY_NAME')).toBeNull();
        expect(tag(e, 'MY_RIG')).toBeNull(); // always stamped by the server
    });

    it('RQ4 enrichment extras are carried', () => {
        const r = sampleRecord();
        const adif = buildRecoveredAdif(r, r.fields, r.rig, { country: 'England', dxcc: '223' });
        expect(tag(adif, 'COUNTRY')).toBe('England');
        expect(tag(adif, 'DXCC')).toBe('223');
    });
});
