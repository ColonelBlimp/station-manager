import { mount } from 'svelte';
import App from './App.svelte';
import { enrich, prefs, setEnricher, setMyGrid } from './lib/operate/enrich.svelte';
import { setHistory } from './lib/operate/worked.svelte';
import { setSubmit } from './lib/operate/qso.svelte';
import { addSessionQso } from './lib/operate/session.svelte';
import { rig } from './lib/operate/rig.svelte';
import { apiEnrich, fetchStationContext, type StationContext } from './lib/api/seams';
import { submitQso } from './lib/api/qso';
import { formatAdifRecord } from './lib/utils/adif';
import { pathInfo } from './lib/utils/bearing';
import { stubHistory } from './lib/dev/historyStub';
import './styles/app.css';

// Backend seams (ADR 0045: coupling is injected here, never imported by
// components). Enrichment, station context and QSO submit are LIVE against
// the daemon; history is still a dev stub — it goes live by swapping its
// set*() argument.
setEnricher(apiEnrich);
setHistory(stubHistory);

// Station facts a submit needs (grid / callsigns / default logbook), fetched
// once at boot. Zero-values on failure: the submit sink refuses with a clear
// message instead of posting against the wrong logbook.
const ctx: StationContext = { myGrid: '', stationCallsign: '', operator: '', logbookId: 0 };
void fetchStationContext().then((c) => {
    Object.assign(ctx, c);
    setMyGrid(c.myGrid); // '' on failure → bearing row hides, fail-soft
});

// Submit sink: draft + rig context + displayed enrichment → one ADIF record →
// POST /v1/qso — the same composition-at-the-wiring-layer shape as the
// daemon's e4 sink (the modules never import each other). Refusals carry an
// operator-facing message (draft preserved); a duplicate is marked so the
// card can offer a force retry.
setSubmit(async (q, opts) => {
    const refuse = (message: string, duplicate = false) => ({
        ok: false as const,
        message,
        duplicate,
    });
    const call = q.callsign.trim().toUpperCase();
    if (ctx.logbookId < 1 || ctx.stationCallsign === '') {
        return refuse('Daemon config unavailable — QSO not logged. Check the daemon and reload.');
    }
    const freqMHz = Number.parseFloat(rig.freq);
    if (!Number.isFinite(freqMHz) || freqMHz <= 0) {
        return refuse('Rig frequency is not set — enter it in the Rig panel.');
    }

    // Only trust the enrichment if it is actually for this call (a fast log
    // can outrun the debounced lookup).
    const e = enrich.call === call ? enrich.data : null;
    // ANT_AZ / ANT_PATH: the same bearing + path the enrichment card shows.
    const path =
        e !== null && e.grid !== '' && ctx.myGrid !== '' ? pathInfo(ctx.myGrid, e.grid) : null;
    const bearing =
        path === null
            ? undefined
            : prefs.path === 'sp'
              ? path.shortPathBearing
              : path.longPathBearing;

    const adif = formatAdifRecord({
        callsign: call,
        rstSent: q.rstSent,
        rstRcvd: q.rstRcvd,
        qsoDate: q.dateOn,
        timeOn: q.timeOn,
        timeOff: q.timeOff,
        qsoDateOff: q.dateOff || undefined,
        mode: rig.mode,
        band: rig.band,
        txFreqHz: Math.round(freqMHz * 1_000_000),
        name: q.name || undefined,
        qth: q.qth || undefined,
        comment: q.comment || undefined,
        gridsquare: q.gridsquare || undefined,
        country: e?.country || undefined,
        // ADIF DXCC is the numeric entity; our display value may be a prefix
        // fallback (e.g. "G") — only emit real numbers.
        dxcc: e !== null && /^\d+$/.test(e.dxcc) ? e.dxcc : undefined,
        stationCallsign: ctx.stationCallsign,
        operator: ctx.operator || undefined,
        myGridSquare: ctx.myGrid || undefined,
        antAz: bearing?.toFixed(1),
        antPath: path === null ? undefined : prefs.path === 'sp' ? 'S' : 'L',
    });

    const out = await submitQso(adif, ctx.logbookId, { force: opts?.force });
    if (out.kind === 'stored') {
        addSessionQso({
            uuid: out.uuid,
            callsign: call,
            timeOn: q.timeOn,
            band: rig.band,
            mode: rig.mode,
            rstSent: q.rstSent,
            rstRcvd: q.rstRcvd,
            name: q.name,
            country: e?.country ?? '',
            comment: q.comment,
        });
        return { ok: true };
    }
    if (out.kind === 'duplicate') {
        // Dedupe is minute-precision on call/band/mode/freq/date/time. The
        // card offers "Log anyway" (force), for the second-contact-same-
        // minute case.
        return refuse('Duplicate — this QSO is already in the log.', true);
    }
    return refuse(`QSO not logged: ${out.message}`);
});

const target = document.getElementById('app');
if (!target) {
    throw new Error('mount target #app not found');
}

const app = mount(App, { target });

export default app;
