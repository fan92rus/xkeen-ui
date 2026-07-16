// ESLint v9 flat config for xkeen-ui frontend
// Docs: https://eslint.org/docs/latest/use/configure/configuration-files
import js from '@eslint/js';
import pluginVue from 'eslint-plugin-vue';

export default [
    // Base JS recommended rules
    js.configs.recommended,

    // Vue 3 recommended rules
    ...pluginVue.configs['flat/recommended'],

    // Global settings
    {
        languageOptions: {
            ecmaVersion: 2022,
            sourceType: 'module',
            globals: {
                // Browser globals
                window: 'readonly',
                document: 'readonly',
                navigator: 'readonly',
                console: 'readonly',
                fetch: 'readonly',
                EventSource: 'readonly',
                WebSocket: 'readonly',
                URL: 'readonly',
                Blob: 'readonly',
                FileReader: 'readonly',
                FormData: 'readonly',
                localStorage: 'readonly',
                location: 'readonly',
                history: 'readonly',
                HTMLElement: 'readonly',
                Event: 'readonly',
                CustomEvent: 'readonly',
                setTimeout: 'readonly',
                clearTimeout: 'readonly',
                setInterval: 'readonly',
                clearInterval: 'readonly',
                btoa: 'readonly',
                atob: 'readonly',
                process: 'readonly', // Vite env access
            },
        },
    },

    // Project-specific rules
    {
        rules: {
            // Relax Vue rules that conflict with project style
            'vue/multi-word-component-names': 'off', // Single-word tab names are fine
            'vue/max-attributes-per-line': 'off',    // Let formatter handle this
            'vue/singleline-html-element-content-newline': 'off',

            // JS rules
            'no-unused-vars': ['warn', {
                argsIgnorePattern: '^_',
                varsIgnorePattern: '^_',
            }],
            'no-console': 'off', // We use utils/logger.js but console is ok in dev
            'no-empty': ['error', { allowEmptyCatch: true }],
        },
    },

    // Test files: relaxed rules
    {
        files: ['test/**/*.js', '**/*.test.js'],
        rules: {
            'no-unused-vars': 'off',
        },
    },

    // Ignore patterns
    {
        ignores: [
            'dist/**',
            'node_modules/**',
            'static/**',
        ],
    },
];
