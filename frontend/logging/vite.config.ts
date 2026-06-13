import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
    plugins: [svelte(), svelteTesting(), tailwindcss()],
    server: {
        port: 5173,
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
                // references the SAME /assets/index.js + /assets/index.css on every
                // build. index.html is committed (so `//go:embed all:logging/dist`
                // compiles before a build), and dist/assets/ is gitignored — if the
                // entry names carried a content hash they'd drift from the committed
                // index.html on the next build/clone, and the daemon would embed a
                // mismatch that 404s its bundle (blank page). Stable names make the
                // committed index.html safe to re-commit and always match a fresh
                // build. Cache-busting is handled instead by `Cache-Control: no-cache`
                // on the SPA handler (internal/api/spa.go), since the embed FS has no
                // modtime for the browser to revalidate against.
                entryFileNames: 'assets/index.js',
                // Code-split chunks + non-CSS assets (the many flag SVGs) KEEP their
                // content hash — they're referenced from the JS, not index.html, so
                // they don't drift the committed file, and hashing keeps their names
                // unique (two variants per country code would otherwise collide).
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
