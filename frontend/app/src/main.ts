import { mount } from 'svelte';
import App from './App.svelte';
import { enrich, setEnricher, setMyGrid } from './lib/operate/enrich.svelte';
import { setHistory } from './lib/operate/worked.svelte';
import { setSubmit } from './lib/operate/qso.svelte';
import { addSessionQso } from './lib/operate/session.svelte';
import { rig } from './lib/operate/rig.svelte';
import { stubEnrich } from './lib/dev/enrichStub';
import { stubHistory } from './lib/dev/historyStub';
import './styles/app.css';

// Backend seams — stubbed until the /v1 wiring lands (ADR 0045: coupling is
// injected here, never imported by components). My grid comes from
// /v1/config my_gridsquare later.
setEnricher(stubEnrich);
setMyGrid('KH66');
setHistory(stubHistory);

// Submit sink. Composes draft + displayed enrichment into a session row —
// the same shape as the daemon's e4 sink (enrich-then-submit composed at the
// wiring layer, the modules never import each other). Later this becomes
// POST /v1/qso with the session row fed from the response.
setSubmit((q) => {
    const call = q.callsign.trim().toUpperCase();
    addSessionQso({
        callsign: call,
        timeOn: q.timeOn,
        band: rig.band, // rig context merges at log time, not in the draft
        mode: rig.mode,
        rstSent: q.rstSent,
        rstRcvd: q.rstRcvd,
        name: q.name,
        // Only trust the enrichment if it is actually for this call (a fast
        // log can outrun the debounced lookup).
        country: enrich.call === call ? (enrich.data?.country ?? '') : '',
        comment: q.comment,
    });
});

const target = document.getElementById('app');
if (!target) {
    throw new Error('mount target #app not found');
}

const app = mount(App, { target });

export default app;
