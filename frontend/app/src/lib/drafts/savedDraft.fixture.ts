// A complete saved-draft record for tests (imported only by *.test.ts).
import type { SavedDraft } from './savedDraft';

export function sampleRecord(over: Partial<SavedDraft> = {}): SavedDraft {
    return {
        version: 1,
        id: 'd-1',
        savedAt: '2026-09-30T12:10:00.000Z',
        archiveId: 'arch-a',
        archiveLabel: 'Home',
        logbookUuid: 'lb-a',
        logbookId: 1,
        logbookName: 'Home log',
        stationCallsign: '7Q5MLV',
        operator: '7Q5MLV',
        myGrid: 'KH66',
        fields: {
            callsign: 'g0abc',
            rstSent: '59',
            rstRcvd: '57',
            name: 'Bob',
            qth: '',
            gridsquare: '',
            dateOn: '2026-09-30',
            timeOn: '12:00:00',
            dateOff: '',
            timeOff: '',
            comment: 'tnx',
            rig: '',
            notes: '',
            rxPwr: '',
        },
        rig: {
            freqHz: 14_255_000,
            band: '20m',
            mode: 'USB',
            adifMode: 'SSB',
            subMode: 'USB',
            basis: 'before-drop',
            capturedAt: '2026-09-30T12:05:00.000Z',
        },
        outcome: 'unlogged',
        ...over,
    };
}
