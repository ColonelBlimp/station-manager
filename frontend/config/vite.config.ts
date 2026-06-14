import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
    // The config SPA is served by the daemon under the /config/ sub-path
    // (the logging SPA owns the root). `base` makes the built index.html
    // reference /config/assets/* so the same StripPrefix("/config") route
    // resolves both index.html and its bundle. See docs/v2-design/frontend-spa.md.
    base: '/config/',
    plugins: [svelte(), svelteTesting(), tailwindcss()],
    server: {
        // 5174 so the config dev server runs alongside the logging SPA's
        // 5173 without a port clash (`task frontend:dev` +
        // `task frontend:config:dev` concurrently).
        port: 5174,
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
                // references the SAME /config/assets/index.js + index.css on every
                // build. index.html is committed (so `//go:embed all:config/dist`
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
