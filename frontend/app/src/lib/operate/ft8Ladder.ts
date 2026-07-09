// FT8 message ladder — the role-aware rung sequence the Operate pane renders,
// extracted from the shipping Ft8MsgPanel derivations into a pure, testable
// function. Given the daemon's live QSO status (ft8State.qso) + whether we're
// transmitting + the operator's call/grid, it returns the rungs (our TX messages
// interleaved with the remote station's expected replies) and the current
// ("highlighted") row.
//
// Three standard ladders, branched on qso.role, plus their ARRL Field Day twins:
//   - answerer (answer a CQ): grid → R-report → 73
//   - caller   (call CQ):     CQ → report → RR73  (also the idle static preview)
//   - worker   (work a caller): no CQ row — their call to us → report → RR73
// FD carries class+section instead of grid+report.

import type { Ft8QsoStatus } from './ft8.svelte';

export interface Rung {
    dir: 'tx' | 'rx';
    text: string;
}
export interface Ladder {
    rungs: Rung[];
    /** Index of the current (highlighted) rung. */
    step: number;
}

export function buildLadder(
    qso: Ft8QsoStatus,
    transmitting: boolean,
    myCall: string,
    myGrid: string
): Ladder {
    const me = myCall.trim().toUpperCase();
    const grid = myGrid.trim().toUpperCase().slice(0, 4); // FT8 messages carry only the 4-char field
    const dxCall = qso.theirCall || '<DX>';
    const dxGrid = qso.theirGrid || '<GRID>';
    const ourRst = qso.ourReport || '<RST>';
    const theirRst = qso.theirReport || '<RST>';
    const cqMessage = me ? `CQ ${me}${grid ? ` ${grid}` : ''}` : 'CQ';

    // The highlighted row for a TX rung at index txRow. While transmitting — OR
    // before this rung has been sent at all (repeats === 0) — the TX rung is
    // current; only once sent and awaiting the reply (repeats > 0, not
    // transmitting) does the RX row below become current.
    const rowFor = (txRow: number, len: number): number =>
        Math.min(!transmitting && qso.repeats > 0 ? txRow + 1 : txRow, len - 1);

    const answering = qso.active && qso.role === 'answerer';
    const working = qso.active && qso.role === 'worker';

    if (qso.fd && answering) {
        const oCls = qso.ourClass || '<CLS>';
        const oSec = qso.ourSection || '<SEC>';
        const tCls = qso.theirClass || '<CLS>';
        const tSec = qso.theirSection || '<SEC>';
        const rungs: Rung[] = [
            { dir: 'tx', text: `${dxCall} ${me} ${oCls} ${oSec}` },
            { dir: 'rx', text: `${me} ${dxCall} R ${tCls} ${tSec}` },
            { dir: 'tx', text: `${dxCall} ${me} RR73` },
        ];
        return { rungs, step: rowFor(qso.state === 'rogering' ? 2 : 0, rungs.length) };
    }
    if (qso.fd && working) {
        const oCls = qso.ourClass || '<CLS>';
        const oSec = qso.ourSection || '<SEC>';
        const tCls = qso.theirClass || '<CLS>';
        const tSec = qso.theirSection || '<SEC>';
        const rungs: Rung[] = [
            { dir: 'rx', text: `${me} ${dxCall} ${tCls} ${tSec}` },
            { dir: 'tx', text: `${dxCall} ${me} R ${oCls} ${oSec}` },
            { dir: 'rx', text: `${me} ${dxCall} RR73` },
            { dir: 'tx', text: `${dxCall} ${me} RR73` },
        ];
        return { rungs, step: rowFor(qso.state === 'rogering' ? 3 : 1, rungs.length) };
    }

    if (answering) {
        const rungs: Rung[] = [
            { dir: 'tx', text: `${dxCall} ${me}${grid ? ` ${grid}` : ' <GRID>'}` },
            { dir: 'rx', text: `${me} ${dxCall} ${theirRst}` },
            { dir: 'tx', text: `${dxCall} ${me} R${ourRst}` },
            { dir: 'rx', text: `${me} ${dxCall} RR73` },
            { dir: 'tx', text: `${dxCall} ${me} 73` },
        ];
        const txRow = qso.state === 'reporting' ? 2 : qso.state === 'confirming' ? 4 : 0;
        return { rungs, step: rowFor(txRow, rungs.length) };
    }
    if (working) {
        const rungs: Rung[] = [
            { dir: 'rx', text: `${me} ${dxCall} ${dxGrid}` },
            { dir: 'tx', text: `${dxCall} ${me} ${ourRst}` },
            { dir: 'rx', text: `${me} ${dxCall} R${theirRst}` },
            { dir: 'tx', text: `${dxCall} ${me} RR73` },
            { dir: 'rx', text: `${me} ${dxCall} 73` },
        ];
        return { rungs, step: rowFor(qso.state === 'rogering' ? 3 : 1, rungs.length) };
    }

    // Caller ladder — live while calling CQ, else a static preview (CQ row current).
    const rungs: Rung[] = [
        { dir: 'tx', text: cqMessage },
        { dir: 'rx', text: `${me} ${dxCall} ${dxGrid}` },
        { dir: 'tx', text: `${dxCall} ${me} ${ourRst}` },
        { dir: 'rx', text: `${me} ${dxCall} R${theirRst}` },
        { dir: 'tx', text: `${dxCall} ${me} RR73` },
        { dir: 'rx', text: `${me} ${dxCall} 73` },
    ];
    const txRow = qso.state === 'reporting' ? 2 : qso.state === 'rogering' ? 4 : 0;
    return { rungs, step: qso.active ? rowFor(txRow, rungs.length) : txRow };
}
