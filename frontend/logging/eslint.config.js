import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import { defineConfig } from 'eslint/config';
import globals from 'globals';
import ts from 'typescript-eslint';
import prettier from 'eslint-config-prettier';
import svelteConfig from './svelte.config.js';

export default defineConfig(
    {
        ignores: [
            'dist/**',
            'build/**',
            'node_modules/**',
            'coverage/**',
            // Config files at project root — linted with type-unaware rules below.
            'eslint.config.js',
            'svelte.config.js',
            'vite.config.ts',
        ],
    },
    js.configs.recommended,
    ...ts.configs.recommendedTypeChecked,
    ...svelte.configs.recommended,
    {
        languageOptions: {
            globals: { ...globals.browser, ...globals.node },
            parserOptions: {
                projectService: true,
                tsconfigRootDir: import.meta.dirname,
            },
        },
        rules: {
            // TS itself enforces no-undef better than the eslint rule.
            // https://typescript-eslint.io/troubleshooting/faqs/eslint/#i-get-errors-from-the-no-undef-rule-about-global-variables-not-being-defined
            'no-undef': 'off',
            // Allow leading-underscore-prefixed unused params/vars by convention.
            '@typescript-eslint/no-unused-vars': [
                'error',
                {
                    argsIgnorePattern: '^_',
                    varsIgnorePattern: '^_',
                    caughtErrorsIgnorePattern: '^_',
                },
            ],
        },
    },
    {
        files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
        languageOptions: {
            parserOptions: {
                projectService: true,
                extraFileExtensions: ['.svelte'],
                parser: ts.parser,
                svelteConfig,
            },
        },
    },
    {
        // Vitest globals (describe/it/expect) used by *.test.ts files.
        files: ['**/*.test.ts', '**/*.test.svelte.ts'],
        languageOptions: {
            globals: { ...globals.node },
        },
    },
    prettier
);
