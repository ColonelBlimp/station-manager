import { render, screen } from '@testing-library/svelte';
import App from './app.svelte';

// Smoke test: the logbook SPA mounts the LogbookView and shows its heading. The
// view fires a /v1/logbook fetch on mount which simply fails (no daemon in jsdom)
// and lands the error state — the heading renders regardless, which is all this
// gate checks. Behavioural paging/table tests would mock the api/logbooks layer.
describe('logbook App', () => {
    it('renders the Logbook view heading', () => {
        render(App);
        expect(screen.getByRole('heading', { name: /Logbook/ })).toBeInTheDocument();
    });
});
