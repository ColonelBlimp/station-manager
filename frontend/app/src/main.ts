import { mount } from 'svelte';
import App from './App.svelte';
import { setEnricher, setMyGrid } from './lib/operate/enrich.svelte';
import { setHistory } from './lib/operate/worked.svelte';
import { stubEnrich } from './lib/dev/enrichStub';
import { stubHistory } from './lib/dev/historyStub';
import './styles/app.css';

// Backend seams — stubbed until the /v1 wiring lands (ADR 0045: coupling is
// injected here, never imported by components). My grid comes from
// /v1/config my_gridsquare later.
setEnricher(stubEnrich);
setMyGrid('KH66');
setHistory(stubHistory);

const target = document.getElementById('app');
if (!target) {
    throw new Error('mount target #app not found');
}

const app = mount(App, { target });

export default app;
