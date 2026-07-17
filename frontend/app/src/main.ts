import { mount } from 'svelte';
import App from './App.svelte';
import { enrich, prefs, setEnricher, setMyGrid } from './lib/operate/enrich.svelte';
import { setHistory } from './lib/operate/worked.svelte';
import { setSubmit } from './lib/operate/qso.svelte';
import { addSessionQso, session } from './lib/operate/session.svelte';
import { setMailer } from './lib/operate/mailer.svelte';
import { setLayoutPersistence, type LayoutValue } from './lib/operate/layout.svelte';
import {
    rig,
    catLink,
    setModeMappings,
    setRigCaps,
    setTuneSender,
    setCommandSender,
    setOperatingBands,
    setFt8Frequencies,
} from './lib/operate/rig.svelte';
import { openRigEvents } from './lib/api/rig-sse';
import { openFt8Events } from './lib/api/ft8-sse';
import {
    setFt8Transport,
    setFt8OperatorCall,
    setFt8MyGrid,
    setFt8TxActions,
    setFt8LoggedSink,
    setFt8DisplayPrefs,
    type Ft8TxResult,
} from './lib/operate/ft8.svelte';
import { setStationInfo, setLogbookCount } from './lib/operate/station.svelte';
import { setFt8Enricher, setFt8Dupe, ft8EnrichState } from './lib/operate/ft8Enrich.svelte';
import { fetchContestDupe } from './lib/api/contest-dupe';
import { armFt8Tx, type Ft8TxOutcome } from './lib/api/ft8tx';
import {
    startFt8Qso,
    startFt8WorkCaller,
    startFt8Cq,
    abandonFt8Qso,
    skipFt8Qso,
    type Ft8QsoOutcome,
} from './lib/api/ft8qso';
import { toasts } from './lib/ui/toasts.svelte';
import { setup, setSetupSave } from './lib/setup.svelte';
import { completeSetup } from './lib/api/setup';
import { sendRigTune } from './lib/api/rig-tune';
import { sendRigCommand } from './lib/api/rig-command';
import {
    apiEnrich,
    apiHistory,
    fetchStationContext,
    fetchLogbookCount,
    type StationContext,
} from './lib/api/seams';
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

// FT8 SSE transport (ADR 0045: the ft8 state module never imports lib/api). The
// FT8 view opens/closes it view-scoped via startFt8/stopFt8 — this only injects
// the opener.
setFt8Transport(openFt8Events);

// FT8 TX action seam (ADR 0029/0030/0031/0033) — the first RF path from this SPA.
// The daemon owns arming + the guaranteed stop + the CQ→73 sequencing; the SPA
// sends only intent, so these adapt the rich lib/api ft8tx/ft8qso outcomes to the
// {ok,message} the control bar / Band Activity clicks expect. Confirm-by-push:
// TX / QSO progress arrives over the ft8-tx / ft8-qso SSE, never these responses.
const toTxResult = (o: Ft8TxOutcome | Ft8QsoOutcome): Ft8TxResult =>
    o.kind === 'ok' ? { ok: true, message: '' } : { ok: false, message: o.message };
setFt8TxActions({
    arm: (armed) => armFt8Tx(armed).then(toTxResult),
    callCq: (offsetHz, opFreqMHz, parity) =>
        startFt8Cq(offsetHz, opFreqMHz, parity).then(toTxResult),
    answerCq: (a) =>
        startFt8Qso(
            a.theirCall,
            a.theirGrid,
            a.slotUtc,
            a.offsetHz,
            a.opFreqMHz,
            a.type4 ? 'type4' : a.fd ? 'fd' : 'standard',
            a.theirSnr
        ).then(toTxResult),
    workCaller: (a) =>
        startFt8WorkCaller(
            a.theirCall,
            a.theirGrid,
            a.theirSnr,
            a.slotUtc,
            a.offsetHz,
            a.opFreqMHz,
            a.fd
        ).then(toTxResult),
    abandon: () => abandonFt8Qso().then(toTxResult),
    skip: (armed) => skipFt8Qso(armed).then(toTxResult),
});

// Completed-FT8-QSO sink (ft8-logged SSE): a finished exchange is logged daemon-side
// (no form to submit), so route it into the shared session log — FT8 rows sit
// alongside Phone/CW ones — grey the station out in Band Activity so it can't be
// re-worked, and toast (the only "it's logged" signal for FT8). The event is
// one-shot (not replay-cached); the uuid guard still defends against a stray
// double-delivery duplicating a session row.
setFt8LoggedSink((p) => {
    const uuid = p.uuid ?? '';
    if (uuid !== '' && session.qsos.some((q) => q.uuid === uuid)) return;
    const call = p.callsign ?? '';
    const band = p.band ?? '';
    addSessionQso({
        uuid: uuid || undefined,
        callsign: call,
        timeOn: p.time_on ?? '',
        band,
        mode: p.mode ?? 'FT8',
        rstSent: p.rst_sent ?? '',
        rstRcvd: p.rst_rcvd ?? '',
        name: p.name ?? '',
        country: p.country ?? '',
        comment: '',
    });
    if (call !== '') ft8EnrichState.markWorked(call, band);
    toasts.info(call !== '' ? `QSO logged — ${call}${band ? ` (${band})` : ''}` : 'QSO logged');
    refreshLogbookCount(); // header "(n)" ticks up on each logged FT8 QSO
});

// Band Activity flag enricher (ADR 0045) — the same /v1/enrich/callsign lookup
// the logging card uses; fail-soft (a miss leaves the row undecorated). The
// worked-before dupe seam is wired below, where the default logbook id is known.
setFt8Enricher(apiEnrich);

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
    configOk: false,
    setupComplete: false,
    myGrid: '',
    stationCallsign: '',
    operator: '',
    logbookId: 0,
    catEnabled: false,
    modeMappings: {},
    ops: [],
    tune: false,
    rigModes: [],
    operatingBands: [],
    mailerEnabled: false,
    mailerDefaultRecipient: '',
    ft8FeedMode: 'accumulate',
    ft8HistoryMax: 100,
    ft8CqToTop: false,
    ft8HideHashed: false,
    ft8Frequencies: {},
    mapBandColors: {},
    logbookName: '',
    rigName: '',
};

// Live logbook QSO count for the header "(n)" readout. Fire-and-forget: seeded
// once config resolves and re-fetched after every logged QSO (FT8 + Phone/CW),
// so the count ticks up as stations are worked. Closes over ctx for the current
// default-logbook id; fail-soft (a blip leaves the last good count on screen).
// Hoisted so the ft8-logged sink registered above can call it.
function refreshLogbookCount(): void {
    if (ctx.logbookId < 1) return;
    void fetchLogbookCount(ctx.logbookId).then((n) => {
        if (n !== null) setLogbookCount(n);
    });
}

// Worked-before dupe seam (/v1/contest-dupe), closing over ctx so it reads the
// resolved default-logbook id at lookup time. No logbook → unknown (skip).
setFt8Dupe((call, band, mode) => {
    if (ctx.logbookId < 1) return Promise.resolve(null);
    return fetchContestDupe({ logbook: ctx.logbookId, call, band, mode })
        .then((o) => (o.kind === 'ok' ? o.duplicate : null))
        .catch(() => null);
});

// Guard: applyStationContext can run more than once (boot, then again after
// first-run setup completes) — the rig event stream must not double-open.
let rigEventsOpen = false;

function applyStationContext(c: StationContext): void {
    Object.assign(ctx, c);
    setMyGrid(c.myGrid); // '' on failure → bearing row hides, fail-soft
    // The operator's "CAT enabled" intent gates the stream (shipping rule):
    // when false the SPA stays manual and never opens it. Config is fetched
    // once at boot, so no enable/disable tracking here — this SPA's config
    // surface will re-wire that when it lands. The page unload closes the
    // EventSource; the browser owns transient reconnects.
    setModeMappings(c.modeMappings);
    // Bridge capabilities gate the rig-control surfaces (Tune button, and the
    // VFO/band/freq/mode ops in later slices). Set unconditionally: they're
    // valid whether or not the rig is CURRENTLY connected — a supported-but-
    // offline rig shows its Tune button disabled, not absent.
    setRigCaps({ ops: c.ops, tune: c.tune, rigModes: c.rigModes });
    // Operator's configured bands drive the band selector (+ later FT8 buttons
    // and the keyboard band-jump). Empty → the module's HF..6m default.
    setOperatingBands(c.operatingBands);
    // Per-band FT8 watering-hole freqs → the FT8 rig card's band buttons jump to the
    // configured dial freq (set_freq) instead of the rig's band-stack freq.
    setFt8Frequencies(c.ft8Frequencies);
    // Tune-carrier write seam: adapt the rich rig-tune outcome to {ok,message}.
    // The daemon owns keying + the guaranteed stop; the SPA sends only intent.
    setTuneSender(async (active) => {
        const o = await sendRigTune(active);
        return o.kind === 'ok' ? { ok: true, message: '' } : { ok: false, message: o.message };
    });
    // Rig command write seam (VFO swap now; band/freq/mode in later slices) —
    // adapt the rich command outcome to {ok,message}, same shape as tune.
    setCommandSender(async (op, value) => {
        const o = await sendRigCommand(op, value);
        return o.kind === 'ok' ? { ok: true, message: '' } : { ok: false, message: o.message };
    });
    setMailer(c.mailerEnabled, c.mailerDefaultRecipient);
    // Always-visible station identity in the header: which logbook this session
    // writes to and which rig is configured (both config, not CAT — visible before
    // the rig connects).
    setStationInfo({ logbookName: c.logbookName, rigName: c.rigName });
    // Seed the header's live logbook count now that the default-logbook id is known.
    refreshLogbookCount();
    // Operator callsign → Band Activity flags decodes calling US (`<me> <them>`).
    setFt8OperatorCall(c.stationCallsign);
    // Operator grid → the near end of Band Activity's per-CQ short-path bearing.
    setFt8MyGrid(c.myGrid);
    // FT8 Band Activity display prefs (config ft8.display): feed_mode drives the
    // decode-feed roll (accumulate vs single-slot) in the state module; cq_to_top
    // + history_max shape the Band Activity render. Injected once at boot — this
    // SPA fetches config once (no live reload yet).
    setFt8DisplayPrefs({
        feedMode: c.ft8FeedMode,
        historyMax: c.ft8HistoryMax,
        cqToTop: c.ft8CqToTop,
        hideHashedCalls: c.ft8HideHashed,
    });
    if (c.catEnabled && !rigEventsOpen) {
        rigEventsOpen = true;
        openRigEvents(catLink);
    }
}

void fetchStationContext().then((c) => {
    applyStationContext(c);
    // First-run gate: only a REACHED config saying setup_complete=false shows
    // setup — a daemon outage falls through to the fail-soft shell instead of
    // greeting a configured operator with the welcome card.
    setup.status = c.configOk && !c.setupComplete ? 'needed' : 'complete';
});

// First-run save (injected per ADR 0045 — the setup module never imports
// lib/api): PUT the callsign, then re-fetch + re-wire the station context so
// the freshly-seeded default logbook (id, name, count) is live with no reload.
setSetupSave(async (callsign) => {
    const out = await completeSetup(callsign);
    if (out.kind !== 'ok') return { ok: false, message: out.message };
    applyStationContext(await fetchStationContext());
    return { ok: true, message: '' };
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
        refreshLogbookCount(); // header "(n)" ticks up on each logged Phone/CW QSO
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
