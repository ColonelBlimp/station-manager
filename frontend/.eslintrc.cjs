module.exports = {
    root: true,
    parser: '@typescript-eslint/parser',
    parserOptions: {
        sourceType: 'module',
        ecmaVersion: 'latest',
        project: ['./tsconfig.json'],
        tsconfigRootDir: __dirname,
    },
    env: {
        browser: true,
        es2021: true,
    },
    plugins: ['@typescript-eslint', 'svelte'],
    extends: [
        'eslint:recommended',
        'plugin:@typescript-eslint/recommended',
        'plugin:@typescript-eslint/recommended-type-checked',
        'plugin:svelte/recommended',
        'prettier',
    ],
    ignorePatterns: ['src/lib/wailsjs/**'],
    overrides: [
        {
            files: ['*.svelte'],
            parser: 'svelte-eslint-parser',
            parserOptions: {
                parser: '@typescript-eslint/parser',
                extraFileExtensions: ['.svelte'],
                project: ['./tsconfig.json'],
                tsconfigRootDir: __dirname,
            },
        },
        {
            files: ['**/*.d.ts', 'src/lib/wailsjs/**/*'],
            rules: {
                '@typescript-eslint/no-explicit-any': 'off',
            },
        },
    ],
    rules: {
        '@typescript-eslint/naming-convention': [
            'error',
            {
                selector: 'default',
                format: ['camelCase'],
                leadingUnderscore: 'allow',
                trailingUnderscore: 'allow',
            },
            {
                selector: 'variableLike',
                format: ['camelCase', 'UPPER_CASE'],
                leadingUnderscore: 'allow',
            },
            {
                selector: 'typeLike',
                format: ['PascalCase'],
            },
            {
                selector: 'enumMember',
                format: ['PascalCase', 'UPPER_CASE'],
            },
            {
                selector: 'property',
                format: null,
            },
        ],
    },
};
