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
    /** Present on the caller ladder's CQ rung only — the parts around the operator's
     *  CQ token (W-0011), so the view can render the token field between "CQ" and
     *  the call while no session is active. `text` stays the whole message. */
    cq?: { call: string; grid: string };
}

/** The CQ rung's text: the operator's token between CQ and the call ("CQ AF 7Q5MLV
 *  KH78"). Once a Call-CQ run is live and calling, the daemon's own next message wins,
 *  so the ladder reads exactly what goes on air (the token typed here need not be the
 *  one the run started with — another tab, a change after the start). */
function cqRungText(qso: Ft8QsoStatus, me: string, grid: string, cqModifier: string): string {
    if (qso.active && qso.role === 'caller' && qso.nextMessage.startsWith('CQ ')) {
        return qso.nextMessage;
    }
    if (!me) return 'CQ';
    const token = cqModifier.trim().toUpperCase();
    return `CQ ${token ? `${token} ` : ''}${me}${grid ? ` ${grid}` : ''}`;
}
export interface Ladder {
    rungs: Rung[];
    /** Index of the current (highlighted) rung. */
    step: number;
}

// BASELINE DEBT 2026-07-31 (complexity 37) — one branch per rung across
// the seven sequencer modes; splitting it would scatter the ladder's shape.
// eslint-disable-next-line complexity
export function buildLadder(
    qso: Ft8QsoStatus,
    transmitting: boolean,
    myCall: string,
    myGrid: string,
    cqModifier = ''
): Ladder {
    const me = myCall.trim().toUpperCase();
    const grid = myGrid.trim().toUpperCase().slice(0, 4); // FT8 messages carry only the 4-char field
    const dxCall = qso.theirCall || '<DX>';
    const dxGrid = qso.theirGrid || '<GRID>';
    const ourRst = qso.ourReport || '<RST>';
    const theirRst = qso.theirReport || '<RST>';
    const cqMessage = cqRungText(qso, me, grid, cqModifier);

    // The highlighted row for a TX rung at index txRow. While transmitting — OR
    // before this rung has been sent at all (repeats === 0) — the TX rung is
    // current; only once sent and awaiting the reply (repeats > 0, not
    // transmitting) does the RX row below become current.
    const rowFor = (txRow: number, len: number): number =>
        Math.min(!transmitting && qso.repeats > 0 ? txRow + 1 : txRow, len - 1);

    const answering = qso.active && qso.role === 'answerer';
    const working = qso.active && qso.role === 'worker';

    // Reduced type-4 (nonstandard/compound call, ADR 0048): no grid/report rungs.
    // Answer: bare opening → their roger → our 73. Work: their bare call → our RR73.
    if (qso.type4 && answering) {
        const rungs: Rung[] = [
            { dir: 'tx', text: `${dxCall} ${me}` },
            { dir: 'rx', text: `${me} ${dxCall} RR73` },
            { dir: 'tx', text: `${dxCall} ${me} 73` },
        ];
        return { rungs, step: rowFor(qso.state === 'confirming' ? 2 : 0, rungs.length) };
    }
    if (qso.type4 && working) {
        const rungs: Rung[] = [
            { dir: 'rx', text: `${me} ${dxCall}` },
            { dir: 'tx', text: `${dxCall} ${me} RR73` },
            { dir: 'rx', text: `${me} ${dxCall} 73` },
        ];
        return { rungs, step: rowFor(1, rungs.length) };
    }

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
        { dir: 'tx', text: cqMessage, cq: { call: me, grid } },
        { dir: 'rx', text: `${me} ${dxCall} ${dxGrid}` },
        { dir: 'tx', text: `${dxCall} ${me} ${ourRst}` },
        { dir: 'rx', text: `${me} ${dxCall} R${theirRst}` },
        { dir: 'tx', text: `${dxCall} ${me} RR73` },
        { dir: 'rx', text: `${me} ${dxCall} 73` },
    ];
    const txRow = qso.state === 'reporting' ? 2 : qso.state === 'rogering' ? 4 : 0;
    return { rungs, step: qso.active ? rowFor(txRow, rungs.length) : txRow };
}
