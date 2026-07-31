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
            'no-undef': 'off',
            '@typescript-eslint/no-unused-vars': [
                'error',
                {
                    argsIgnorePattern: '^_',
                    varsIgnorePattern: '^_',
                    caughtErrorsIgnorePattern: '^_',
                },
            ],

            // ── Maintainability metrics (2026-07-31) ─────────────────────
            // The SPA half of the same measurement wired into .golangci.yml for
            // Go. Shape, NOT correctness: these find code that is hard to read,
            // never code that is wrong. They would not have caught any defect
            // found on 2026-07-31.
            //
            // MEASURED BASELINE over src/ (178 files, ESLint 10.6), taken with
            // --no-inline-config and the SAME options set below, so the numbers
            // describe the rules as configured rather than a different measurement:
            //   complexity             431 production functions, median 3, max 79
            //                          > 10: 35   > 20: 8   > 30: 6
            //   max-lines-per-function 601 production, median 5, max 133
            //                          > 50: 7    > 80: 3   > 100: 1
            //   max-depth              118 production, median 2, MAX 3
            //
            // skipComments/skipBlankLines are ON deliberately: this codebase
            // mandates extensive why-comments, and counting them would make the
            // rule punish exactly the practice CLAUDE.md requires. It matters —
            // the raw count put 3 functions over 100, the adjusted count puts 1.
            //
            // Thresholds are chosen from that distribution, not from convention,
            // and are green on adoption: the 8 functions already over the line
            // carry inline disables with a BASELINE DEBT why-comment naming the
            // measured value. Grep "BASELINE DEBT" to find them; deleting one is
            // how the debt gets paid down, and these numbers ratchet after.
            //
            // Inline disables rather than config `files:` overrides deliberately:
            // an override would switch the rule off for a WHOLE file, leaving
            // adif.ts and ft8.svelte.ts unguarded against new complexity.
            complexity: ['error', 20],
            'max-depth': ['error', 3], // zero violations today — pure regression guard
            'max-lines-per-function': [
                'error',
                { max: 100, skipBlankLines: true, skipComments: true },
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
        files: ['**/*.test.ts', '**/*.test.svelte.ts'],
        languageOptions: {
            globals: { ...globals.node },
        },
        rules: {
            // ATDD tests are legitimately long: the acceptance criterion and its
            // reasoning live in the test, and 21 of them already exceed 100 lines.
            // Complexity and depth still apply — this codebase leans on tests being
            // readable AS the specification, so a convoluted test is a real defect
            // (and no test exceeds complexity 10 today).
            'max-lines-per-function': 'off',
        },
    },
    prettier
);
