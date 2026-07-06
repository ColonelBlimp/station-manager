import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
    plugins: [svelte(), svelteTesting(), tailwindcss()],
    server: {
        // 5173 logging · 5174 config · 5175 logbook · 5176 app (avoid clashes so
        // they can run side by side during the consolidation).
        port: 5176,
        proxy: {
            '/v1': 'http://localhost:8080',
        },
    },
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        rollupOptions: {
            output: {
                // Stable, hash-free entry names so the (future) committed
                // dist/index.html always references the same /assets/index.js +
                // /assets/index.css — matches the embed convention used by the
                // other SPAs (see frontend/logging/vite.config.ts for the full
                // rationale). Code-split chunks + non-CSS assets keep their hash.
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
