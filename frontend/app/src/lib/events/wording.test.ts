import { describe, it, expect } from 'vitest';
import { kindLabel, detailSummary, DETAILS_UNAVAILABLE } from './wording';
import type { StationEvent } from '../api/stationEvents';

const ev = (kind: string, detail: unknown): StationEvent => ({
    id: 1,
    category:
        kind.startsWith('tx') || kind.startsWith('drive') || kind.startsWith('session')
            ? 'alarm'
            : 'notification',
    kind,
    severity: 'info',
    occurred_at: '2026-09-18T10:00:00Z',
    build: 'v-test',
    detail,
});

// AC7 on the reading side: every kind the daemon stores has words, built only
// from its fixed detail fields; anything else is the placeholder, never the raw
// detail.
describe('station event wording', () => {
    it('names every stored kind', () => {
        const kinds = [
            'export.adif_failed',
            'forward.failed',
            'tx_alarm.raised',
            'tx_alarm.cleared',
            'drive_alarm.raised',
            'drive_alarm.cleared',
            'tx.disarmed',
            'session.terminated',
        ];
        for (const k of kinds) expect(kindLabel(k)).not.toBe(k);
        expect(kindLabel('future.kind')).toBe('future.kind');
    });

    it('summarises each kind from its typed fields', () => {
        expect(detailSummary(ev('export.adif_failed', { count: 1, outcome: 'server' }))).toBe(
            '1 QSO · server'
        );
        expect(
            detailSummary(ev('forward.failed', { forwarder: 'qrz', action: 'insert', attempts: 3 }))
        ).toBe('qrz · insert · 3 attempts');
        expect(detailSummary(ev('tx_alarm.raised', { code: 'tx_unconfirmed' }))).toBe(
            'unkey not confirmed'
        );
        expect(
            detailSummary(ev('tx_alarm.cleared', { code: 'tx_still_keyed', active_ms: 700 }))
        ).toBe('rig reported still keyed · stood 700 ms');
        expect(
            detailSummary(ev('tx_alarm.cleared', { code: 'tx_unconfirmed', active_ms: 12_400 }))
        ).toBe('unkey not confirmed · stood 12.4 s');
        expect(detailSummary(ev('tx_alarm.cleared', { code: 'tx_unconfirmed' }))).toBe(
            'unkey not confirmed'
        );
        expect(detailSummary(ev('drive_alarm.raised', { code: 'drive_no_output' }))).toBe(
            'no output from the rig'
        );
        expect(detailSummary(ev('drive_alarm.cleared', { code: 'drive_no_output' }))).toBe(
            'no output from the rig'
        );
        expect(detailSummary(ev('tx.disarmed', { cause: 'cat_lost' }))).toBe('CAT connection lost');
        expect(
            detailSummary(
                ev('session.terminated', {
                    cause: 'unattended',
                    partner_call: 'K1ABC',
                    rung: 'calling',
                })
            )
        ).toBe('K1ABC · browser closed, nobody attending · at calling');
    });

    it('never stringifies an unknown or malformed detail', () => {
        expect(detailSummary(ev('session.terminated', { cause: 'unattended' }))).toBe(
            DETAILS_UNAVAILABLE
        );
        expect(detailSummary(ev('tx_alarm.raised', { code: 42 }))).toBe(DETAILS_UNAVAILABLE);
        expect(detailSummary(ev('future.kind', { anything: 'at all' }))).toBe(DETAILS_UNAVAILABLE);
        expect(detailSummary(ev('forward.failed', 'raw text'))).toBe(DETAILS_UNAVAILABLE);
        expect(detailSummary(ev('forward.failed', null))).toBe(DETAILS_UNAVAILABLE);
        expect(
            detailSummary(ev('tx_alarm.cleared', { code: 'tx_unconfirmed', active_ms: 'bad' }))
        ).toBe(DETAILS_UNAVAILABLE);
        expect(
            detailSummary(ev('tx_alarm.cleared', { code: 'tx_unconfirmed', active_ms: -1 }))
        ).toBe(DETAILS_UNAVAILABLE);
        expect(detailSummary(ev('export.adif_failed', { count: -1, outcome: 'server' }))).toBe(
            DETAILS_UNAVAILABLE
        );
        expect(
            detailSummary(
                ev('forward.failed', { forwarder: 'qrz', action: 'insert', attempts: 1.5 })
            )
        ).toBe(DETAILS_UNAVAILABLE);
        expect(
            detailSummary(
                ev('session.terminated', { cause: 'unattended', partner_call: '', rung: 'calling' })
            )
        ).toBe(DETAILS_UNAVAILABLE);
    });
});
