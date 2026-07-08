import { mount } from 'svelte';
import App from './App.svelte';
import { enrich, prefs, setEnricher, setMyGrid } from './lib/operate/enrich.svelte';
import { setHistory } from './lib/operate/worked.svelte';
import { setSubmit } from './lib/operate/qso.svelte';
import { addSessionQso } from './lib/operate/session.svelte';
import { setMailer } from './lib/operate/mailer.svelte';
import { setLayoutPersistence, type LayoutValue } from './lib/operate/layout.svelte';
import { rig, catLink, setModeMappings } from './lib/operate/rig.svelte';
import { openRigEvents } from './lib/api/rig-sse';
import { apiEnrich, apiHistory, fetchStationContext, type StationContext } from './lib/api/seams';
import { submitQso } from './lib/api/qso';
import { formatAdifRecord } from './lib/utils/adif';
import { resolveModeAndSubmode } from './lib/utils/mode';
import { isValidMaidenhead } from './lib/validators/maidenhead';
import { parseFrequency } from './lib/validators/frequency';
import { pathInfo } from './lib/utils/bearing';
import './styles/app.css';

// Backend seams (ADR 0045: coupling is injected here, never imported by
// components). Enrichment, worked-before history, station context, QSO submit
// and the rig SSE are all LIVE against the daemon — no stub surface remains.
setEnricher(apiEnrich);
setHistory(apiHistory);

// Tile-layout persistence (ADR 0046) — localStorage today; the seam swaps to a
// config.json op-profile field later without touching the layout module. The
// key is profile/sub-mode composable ('default.phone' for now).
const LAYOUT_KEY = 'sm.layout.default.phone';
setLayoutPersistence({
    load(): LayoutValue | null {
        try {
            const raw = localStorage.getItem(LAYOUT_KEY);
            return raw ? (JSON.parse(raw) as LayoutValue) : null;
        } catch {
            return null;
        }
    },
    save(l: LayoutValue): void {
        try {
            localStorage.setItem(LAYOUT_KEY, JSON.stringify(l));
        } catch {
            // storage unavailable (private mode) — pin becomes session-only, fine
        }
    },
    clear(): void {
        try {
            localStorage.removeItem(LAYOUT_KEY);
        } catch {
            /* ignore */
        }
    },
});

// Station facts a submit needs (grid / callsigns / default logbook) plus the
// bridge facts the rig seam needs, fetched once at boot. Zero-values on
// failure: the submit sink refuses with a clear message instead of posting
// against the wrong logbook, and the rig surface stays fully manual.
const ctx: StationContext = {
    myGrid: '',
    stationCallsign: '',
    operator: '',
    logbookId: 0,
    catEnabled: false,
    modeMappings: {},
    mailerEnabled: false,
    mailerDefaultRecipient: '',
};
void fetchStationContext().then((c) => {
    Object.assign(ctx, c);
    setMyGrid(c.myGrid); // '' on failure → bearing row hides, fail-soft
    // The operator's "CAT enabled" intent gates the stream (shipping rule):
    // when false the SPA stays manual and never opens it. Config is fetched
    // once at boot, so no enable/disable tracking here — this SPA's config
    // surface will re-wire that when it lands. The page unload closes the
    // EventSource; the browser owns transient reconnects.
    setModeMappings(c.modeMappings);
    setMailer(c.mailerEnabled, c.mailerDefaultRecipient);
    if (c.catEnabled) openRigEvents(catLink);
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
    // rig.freq is the dot-grouped display form when rig-fed ("14.199.950"),
    // decimal MHz when hand-typed — parseFrequency takes both; parseFloat
    // would silently misread the grouped form.
    const freqHz = parseFrequency(rig.freq);
    if (freqHz === null) {
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

    // The rig state holds the operator-friendly literal (USB, PSK31); ADIF
    // wants the canonical (MODE, SUBMODE) pair — the daemon 400s on MODE=USB.
    const { mode, subMode } = resolveModeAndSubmode(rig.mode);

    const adif = formatAdifRecord({
        callsign: call,
        rstSent: q.rstSent,
        rstRcvd: q.rstRcvd,
        qsoDate: q.dateOn,
        timeOn: q.timeOn,
        timeOff: q.timeOff,
        qsoDateOff: q.dateOff || undefined,
        mode,
        subMode: subMode || undefined,
        band: rig.band,
        txFreqHz: freqHz,
        name: q.name || undefined,
        qth: q.qth || undefined,
        comment: q.comment || undefined,
        rig: q.rig || undefined,
        notes: q.notes || undefined,
        // An invalid grid is omitted, never a block: the grid can arrive via
        // enrichment, and gating Log on it would let a bad upstream value
        // stop logging (invariant). The Details panel shows the warning.
        gridsquare:
            q.gridsquare !== '' && isValidMaidenhead(q.gridsquare) === null
                ? q.gridsquare
                : undefined,
        country: e?.country || undefined,
        // ADIF DXCC is the numeric entity; our display value may be a prefix
        // fallback (e.g. "G") — only emit real numbers.
        dxcc: e !== null && /^\d+$/.test(e.dxcc) ? e.dxcc : undefined,
        // Contacted-station zones from the country enrichment (display + log).
        cqZone: e?.cqZone || undefined,
        ituZone: e?.ituZone || undefined,
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
