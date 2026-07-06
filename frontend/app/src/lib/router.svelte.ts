// Client-side router — History-API real paths (ADR 0044 sub-decision). A tiny
// hand-rolled router, no dependency: one reactive `view`, navigate() pushes the
// path, and popstate syncs back on browser back/forward. Deep links + refresh
// work because both Vite's dev server (appType 'spa') and the daemon's spaHandler
// index-fall-back unknown paths to index.html.

export type View = 'dashboard' | 'operate' | 'logbook' | 'config';

const viewToPath: Record<View, string> = {
    dashboard: '/',
    operate: '/operate',
    logbook: '/logbook',
    config: '/config', // route stays /config; nav label is "Settings" (ADR 0044)
};

function viewForPath(path: string): View {
    switch (path) {
        case '/operate':
            return 'operate';
        case '/logbook':
            return 'logbook';
        case '/config':
            return 'config';
        default:
            return 'dashboard'; // '/' and anything unrecognised
    }
}

export const router = $state({ view: viewForPath(window.location.pathname) });

export function navigate(view: View): void {
    router.view = view;
    const path = viewToPath[view];
    if (window.location.pathname !== path) {
        window.history.pushState({}, '', path);
    }
}

// Keep the view in sync with browser back/forward. Registered once on import.
window.addEventListener('popstate', () => {
    router.view = viewForPath(window.location.pathname);
});
