import js from '@eslint/js';
import globals from 'globals';

export default [
    { ignores: ['node_modules/'] },
    js.configs.recommended,
    {
        files: ['**/*.{js,mjs}'],
        languageOptions: {
            ecmaVersion: 2024,
            sourceType: 'module',
            globals: { ...globals.node },
        },
    },
    {
        // W-0027 D1: the installed app ships src/ without test/ or harness/, so a shipped module
        // may import only electron, node: built-ins and its src/ siblings. An allowlist (every
        // other specifier is refused), applied to import and export-from alike; the development
        // path reaches the fixtures only through dynamic import() (test/imports.test.mjs).
        files: ['src/**/*.mjs'],
        rules: {
            'no-restricted-imports': [
                'error',
                {
                    patterns: [
                        {
                            regex: '^(?!(?:electron|node:[a-z_/]+|\\./[\\w-]+\\.mjs)$)',
                            message:
                                'src/ is shipped: import only electron, node: built-ins and ./ siblings (W-0027 D1).',
                        },
                    ],
                },
            ],
        },
    },
];
