import { render, screen } from '@testing-library/svelte';
import App from './app.svelte';

// Scaffold smoke test: the blank logbook page mounts and shows its title.
// Also keeps the vitest gate non-empty (CI runs `vitest run`, which fails
// on zero test files). Replace/expand as the logbook UX lands.
describe('logbook App', () => {
    it('renders the page heading', () => {
        render(App);
        expect(
            screen.getByRole('heading', { name: /Station Manager — Logbook/ })
        ).toBeInTheDocument();
    });
});
