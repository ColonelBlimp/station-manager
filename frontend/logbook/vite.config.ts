import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
    // The logbook SPA is served by the daemon under the /logbook/ sub-path
    // (the logging SPA owns the root). `base` makes the built index.html
    // reference /logbook/assets/* so the same StripPrefix("/logbook") route
    // resolves both index.html and its bundle. See docs/v2-design/frontend-spa.md.
    base: '/logbook/',
    plugins: [svelte(), svelteTesting(), tailwindcss()],
    server: {
        // 5175 so the logbook dev server runs alongside the logging SPA's
        // 5173 and the config SPA's 5174 without a port clash
        // (`task frontend:dev` + `task frontend:config:dev` +
        // `task frontend:logbook:dev` concurrently).
        port: 5175,
        proxy: {
            '/v1': 'http://localhost:8080',
        },
    },
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        rollupOptions: {
            output: {
                // Stable, hash-free names for the entry bundle so dist/index.html
                // references the SAME /logbook/assets/index.js + index.css on every
                // build. index.html is committed (so `//go:embed all:logbook/dist`
                // compiles before a build), and dist/assets/ is gitignored — if the
                // entry names carried a content hash they'd drift from the committed
                // index.html on the next build/clone, and the daemon would embed a
                // mismatch that 404s its bundle (blank page). Cache-busting is handled
                // by `Cache-Control: no-cache` on the SPA handler (internal/api/spa.go)
                // instead, since the embed FS has no modtime to revalidate against.
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
