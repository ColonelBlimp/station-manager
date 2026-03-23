import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { resolve } from 'path';

export default defineConfig({
    plugins: [
        svelte({
            compilerOptions: {
                // Ensure client-side compilation for tests
                dev: true,
            },
        }),
    ],
    test: {
        environment: 'jsdom',
        globals: true,
        include: ['src/**/*.{test,spec}.{js,ts}'],
        setupFiles: ['src/test-setup.ts'],
        alias: {
            $lib: resolve(process.cwd(), './src/lib'),
            $app: resolve(process.cwd(), './node_modules/@sveltejs/kit/src/runtime/app'),
        },
    },
    resolve: {
        conditions: ['browser'],
    },
});
