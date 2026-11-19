import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
    // The consolidated app SPA (ADR 0044) is served by the daemon at the CANONICAL
    // ROOT (W-0003 retired the /app/ transition mount; all legacy SPAs are gone).
    // `base: '/'` makes the built index.html reference /assets/* so the root `GET /`
    // mount resolves both index.html and its bundle. See docs/v2-design/frontend-spa.md.
    base: '/',
    plugins: [svelte(), svelteTesting(), tailwindcss()],
    // No dev server: smd serves only its desktop app (W-0027 C2e), so a page from a Vite
    // dev server cannot reach it. Vite builds the bundle and runs Vitest; the built app is
    // seen in the desktop app.
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        // The Natural Earth 50m basemap (world-atlas/countries-50m.json) is a
        // ~756 KB DATA chunk — already code-split behind a dynamic import and
        // fetched only when the map zooms past the LOD threshold, so the
        // default 500 kB advisory has nothing actionable to say about it.
        // Anything NEW past this raised limit deserves a look.
        chunkSizeWarningLimit: 800,
        rollupOptions: {
            output: {
                // Stable, hash-free entry names so the (future) committed
                // dist/index.html always references the same /assets/index.js +
                // /assets/index.css — matches the embed convention used by the
                // other embedded SPAs. Code-split chunks + non-CSS assets keep their hash.
                entryFileNames: 'assets/index.js',
                chunkFileNames: 'assets/[name]-[hash].js',
                assetFileNames: (info) =>
                    info.name?.endsWith('.css')
                        ? 'assets/index.css'
                        : 'assets/[name]-[hash][extname]',
            },
        },
    },
    test: {
        environment: 'jsdom',
        globals: true,
        setupFiles: ['./src/test/setup.ts'],
        include: ['src/**/*.{test,spec}.{ts,js}'],
    },
});
