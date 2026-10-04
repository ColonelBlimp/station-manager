// The exact ADIF a recovered Log sends (ADR 0085 RS12, RS20): the saved fields
// and original times as saved (a blank end time stays blank — never "now"), the
// recovered rig values (the correction, else the original reading — never the
// live rig), and the saved OPERATOR, MY_NAME and MY_GRIDSQUARE. A known-empty
// OPERATOR or MY_NAME is left out, so the server's check compares it with what
// it would stamp today. MY_RIG and STATION_CALLSIGN are always the server's.

import { formatAdifRecord } from '../utils/adif';
import { isValidMaidenhead } from '../validators/maidenhead';
import type { QsoDraft } from '../operate/qso.svelte';
import type { RigReading } from '../operate/rigSnapshot.svelte';
import type { SavedDraft } from './savedDraft';

/** Enrichment for the same call, as an ordinary Log carries it. */
export interface RecoveredExtras {
    country?: string;
    dxcc?: string;
    cqZone?: string;
    ituZone?: string;
    antAz?: string;
    antPath?: string;
}

export function buildRecoveredAdif(
    record: SavedDraft,
    fields: QsoDraft,
    rig: RigReading,
    extras: RecoveredExtras
): string {
    const a = record.attribution;
    const opt = (s: string): string | undefined => (s === '' ? undefined : s);
    return formatAdifRecord({
        callsign: fields.callsign.trim().toUpperCase(),
        rstSent: fields.rstSent,
        rstRcvd: fields.rstRcvd,
        qsoDate: fields.dateOn,
        timeOn: fields.timeOn,
        timeOff: fields.timeOff,
        qsoDateOff: opt(fields.dateOff),
        mode: rig.adifMode,
        subMode: opt(rig.subMode),
        band: rig.band,
        txFreqHz: rig.freqHz ?? 0,
        name: opt(fields.name),
        qth: opt(fields.qth),
        comment: opt(fields.comment),
        rig: opt(fields.rig),
        notes: opt(fields.notes),
        rxPwr: opt(fields.rxPwr),
        gridsquare:
            fields.gridsquare !== '' && isValidMaidenhead(fields.gridsquare) === null
                ? fields.gridsquare
                : undefined,
        stationCallsign: record.stationCallsign,
        operator: a === null ? undefined : opt(a.operator),
        myName: a === null ? undefined : opt(a.myName),
        myGridSquare: opt(record.myGrid),
        ...extras,
    });
}
